package poll

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/mdg-labs/release-ops/internal/providers/source"
	"github.com/mdg-labs/release-ops/internal/providers/ticket"
	"github.com/mdg-labs/release-ops/internal/store"
	"github.com/mdg-labs/release-ops/internal/tickettemplate"
)

// Poll actions recorded in poll_run_events (specs §4.6).
const (
	ActionBaseline  = "baseline"
	ActionSkip      = "skip"
	ActionCreate    = "create"
	ActionSupersede = "supersede"
	ActionMerge     = "merge"
	ActionSkipOpen  = "skip_open"
	ActionError     = "error"
)

// Engine evaluates release poll decisions for monitored repos (specs §5).
type Engine struct {
	pollRepo store.PollRepository
}

// NewEngine returns a poll decision engine that updates poll state via pollRepo.
func NewEngine(pollRepo store.PollRepository) *Engine {
	return &Engine{pollRepo: pollRepo}
}

// RepoEvaluation is the outcome of evaluating one monitored repo.
type RepoEvaluation struct {
	Actions []string
	Repo    *store.MonitoredRepo
	Detail  string
}

// EvaluateRepo applies release decision logic and updates monitored_repos poll state (specs §5.1–§5.2).
func (e *Engine) EvaluateRepo(
	ctx context.Context,
	repo store.MonitoredRepo,
	release *source.Release,
	fetchErr error,
	project ticket.TicketProject,
	provider ticket.TicketProvider,
	repoWebURL string,
) (*RepoEvaluation, error) {
	if fetchErr != nil {
		return e.recordError(ctx, repo, fetchErr)
	}

	tagAction, tag := decideTagChange(repo.LastKnownTag, release)
	switch tagAction {
	case tagActionBaseline:
		return e.applyBaseline(ctx, repo, release, tag)
	case tagActionSkip:
		return e.applySkip(ctx, repo)
	case tagActionNewTag:
		return e.applyNewTag(ctx, repo, release, project, provider, repoWebURL)
	default:
		return e.recordError(ctx, repo, fmt.Errorf("unexpected tag decision"))
	}
}

// TicketProjectFromStore converts a store row to ticket.TicketProject for EvaluateRepo.
func TicketProjectFromStore(row store.TicketProject, integrationKind string) (ticket.TicketProject, error) {
	mapping, err := ticket.ParseStatusMapping(row.StatusMapping)
	if err != nil {
		return ticket.TicketProject{}, err
	}

	var createConfig map[string]any
	if row.CreateConfig != "" {
		if err := json.Unmarshal([]byte(row.CreateConfig), &createConfig); err != nil {
			return ticket.TicketProject{}, fmt.Errorf("parse create_config: %w", err)
		}
	}

	templates, err := tickettemplate.ParseContentTemplates(row.ContentTemplates)
	if err != nil {
		return ticket.TicketProject{}, err
	}

	policy := row.OnOpenTicketPolicy
	if policy == "" {
		policy = ticket.PolicySupersede
	}

	return ticket.TicketProject{
		ID:                 row.ID,
		IntegrationID:      row.IntegrationID,
		IntegrationKind:    integrationKind,
		ExternalProjectID:  row.ExternalProjectID,
		CreateConfig:       createConfig,
		StatusMapping:      mapping,
		ContentTemplates: ticket.ContentTemplates{
			Title:            templates.Title,
			Description:      templates.Description,
			SupersedeComment: templates.SupersedeComment,
		},
		OnOpenTicketPolicy: policy,
	}, nil
}

type tagDecision int

const (
	tagActionBaseline tagDecision = iota
	tagActionSkip
	tagActionNewTag
)

func decideTagChange(lastKnown *string, release *source.Release) (tagDecision, string) {
	if release == nil {
		if lastKnown == nil {
			return tagActionBaseline, ""
		}
		return tagActionSkip, ""
	}

	tag := release.Tag
	if lastKnown == nil {
		return tagActionBaseline, tag
	}
	if *lastKnown == tag {
		return tagActionSkip, tag
	}
	return tagActionNewTag, tag
}

func basePollUpdate(repo store.MonitoredRepo) store.PollStateUpdate {
	return store.PollStateUpdate{
		OpenTicketExternalID:   repo.OpenTicketExternalID,
		OpenTicketTag:          repo.OpenTicketTag,
		LastKnownTag:           repo.LastKnownTag,
		LastReleasePublishedAt: repo.LastReleasePublishedAt,
		LastPolledAt:           repo.LastPolledAt,
		LastError:              repo.LastError,
	}
}

func releasePublishedAtPtr(release *source.Release) *string {
	if release == nil || release.PublishedAt.IsZero() {
		return nil
	}
	formatted := release.PublishedAt.UTC().Format("2006-01-02T15:04:05.000Z")
	return &formatted
}

func (e *Engine) applyBaseline(ctx context.Context, repo store.MonitoredRepo, release *source.Release, tag string) (*RepoEvaluation, error) {
	now := pollNowUTC()
	update := basePollUpdate(repo)
	update.LastPolledAt = &now
	update.LastError = nil
	if tag != "" {
		update.LastKnownTag = &tag
		update.LastReleasePublishedAt = releasePublishedAtPtr(release)
	}

	updated, err := e.pollRepo.UpdatePollState(ctx, repo.ID, update)
	if err != nil {
		return nil, err
	}
	return &RepoEvaluation{Actions: []string{ActionBaseline}, Repo: updated}, nil
}

func (e *Engine) applySkip(ctx context.Context, repo store.MonitoredRepo) (*RepoEvaluation, error) {
	now := pollNowUTC()
	update := basePollUpdate(repo)
	update.LastPolledAt = &now
	update.LastError = nil
	updated, err := e.pollRepo.UpdatePollState(ctx, repo.ID, update)
	if err != nil {
		return nil, err
	}
	return &RepoEvaluation{Actions: []string{ActionSkip}, Repo: updated}, nil
}

func (e *Engine) applyNewTag(
	ctx context.Context,
	repo store.MonitoredRepo,
	release *source.Release,
	project ticket.TicketProject,
	provider ticket.TicketProvider,
	repoWebURL string,
) (*RepoEvaluation, error) {
	if release == nil {
		return e.recordError(ctx, repo, fmt.Errorf("new tag decision without release"))
	}

	repoForPolicy := repo
	if repo.OpenTicketExternalID != nil && *repo.OpenTicketExternalID != "" {
		classified, rawStatus, err := liveTicketClass(ctx, provider, project.StatusMapping, *repo.OpenTicketExternalID)
		if err != nil {
			return e.recordError(ctx, repo, err)
		}

		switch classified {
		case ticket.StatusDone, ticket.StatusCancelled:
			repoForPolicy = clearOpenTicketFields(repo)
		case ticket.StatusOpen:
			return e.applyOpenTicketPolicy(ctx, repo, release, project, provider, repoWebURL)
		default:
			return e.recordError(ctx, repo, fmt.Errorf("unknown ticket status %q", rawStatus))
		}
	}

	return e.applyCreate(ctx, repoForPolicy, release, project, provider, repoWebURL)
}

func clearOpenTicketFields(repo store.MonitoredRepo) store.MonitoredRepo {
	repo.OpenTicketExternalID = nil
	repo.OpenTicketTag = nil
	return repo
}

func liveTicketClass(
	ctx context.Context,
	provider ticket.TicketProvider,
	mapping ticket.StatusMapping,
	externalID string,
) (classified, rawStatus string, err error) {
	rawStatus, err = provider.GetTicketStatus(ctx, externalID)
	if err != nil {
		return "", "", fmt.Errorf("get ticket status: %w", err)
	}
	return ticket.ClassifyStatus(mapping, rawStatus), rawStatus, nil
}

func (e *Engine) applyOpenTicketPolicy(
	ctx context.Context,
	repo store.MonitoredRepo,
	release *source.Release,
	project ticket.TicketProject,
	provider ticket.TicketProvider,
	repoWebURL string,
) (*RepoEvaluation, error) {
	policy := project.OnOpenTicketPolicy
	if policy == "" {
		policy = ticket.PolicySupersede
	}

	switch policy {
	case ticket.PolicySupersede:
		return e.applySupersede(ctx, repo, release, project, provider, repoWebURL)
	case ticket.PolicyMerge:
		return e.applyMerge(ctx, repo, release, project, provider, repoWebURL)
	case ticket.PolicySkipIfOpen:
		return e.applySkipIfOpen(ctx, repo, release)
	default:
		return e.recordError(ctx, repo, fmt.Errorf("unknown on_open_ticket_policy %q", policy))
	}
}

func (e *Engine) applyCreate(
	ctx context.Context,
	repo store.MonitoredRepo,
	release *source.Release,
	project ticket.TicketProject,
	provider ticket.TicketProvider,
	repoWebURL string,
) (*RepoEvaluation, error) {
	input, err := e.ticketInput(ctx, repo, release, project, repoWebURL, nil)
	if err != nil {
		return e.recordError(ctx, repo, err)
	}

	externalID, err := provider.CreateTicket(ctx, input)
	if err != nil {
		return e.recordError(ctx, repo, fmt.Errorf("create ticket: %w", err))
	}

	now := pollNowUTC()
	tag := release.Tag
	update := basePollUpdate(repo)
	update.OpenTicketExternalID = &externalID
	update.OpenTicketTag = &tag
	update.LastKnownTag = &tag
	update.LastReleasePublishedAt = releasePublishedAtPtr(release)
	update.LastPolledAt = &now
	update.LastError = nil
	updated, err := e.pollRepo.UpdatePollState(ctx, repo.ID, update)
	if err != nil {
		return nil, err
	}
	return &RepoEvaluation{Actions: []string{ActionCreate}, Repo: updated}, nil
}

func (e *Engine) applySupersede(
	ctx context.Context,
	repo store.MonitoredRepo,
	release *source.Release,
	project ticket.TicketProject,
	provider ticket.TicketProvider,
	repoWebURL string,
) (*RepoEvaluation, error) {
	oldID := *repo.OpenTicketExternalID
	oldTag := supersedeOldTag(repo)

	if project.StatusMapping.Superseded == "" {
		return e.recordError(ctx, repo, fmt.Errorf("status_mapping.superseded is required for supersede policy"))
	}

	supersedeCtx := &tickettemplate.SupersedeContext{
		OldTag: oldTag,
		NewTag: release.Tag,
	}
	input, err := e.ticketInput(ctx, repo, release, project, repoWebURL, supersedeCtx)
	if err != nil {
		return e.recordError(ctx, repo, err)
	}

	externalID, err := provider.CreateTicket(ctx, input)
	if err != nil {
		return e.recordError(ctx, repo, fmt.Errorf("create ticket after supersede: %w", err))
	}

	newTicketURL, err := provider.TicketWebURL(externalID)
	if err != nil {
		return e.recordError(ctx, repo, fmt.Errorf("resolve new ticket web url: %w", err))
	}

	supersedeCtx.NewTicketURL = newTicketURL
	comment, err := e.renderSupersedeComment(repo, release, project, repoWebURL, supersedeCtx)
	if err != nil {
		return e.recordError(ctx, repo, fmt.Errorf("render supersede comment: %w", err))
	}

	if err := provider.UpdateTicketStatus(ctx, oldID, project.StatusMapping.Superseded); err != nil {
		return e.recordError(ctx, repo, fmt.Errorf("supersede ticket status: %w", err))
	}

	if err := provider.AddTicketComment(ctx, oldID, comment); err != nil {
		return e.recordError(ctx, repo, fmt.Errorf("supersede ticket comment: %w", err))
	}

	now := pollNowUTC()
	tag := release.Tag
	update := basePollUpdate(repo)
	update.OpenTicketExternalID = &externalID
	update.OpenTicketTag = &tag
	update.LastKnownTag = &tag
	update.LastReleasePublishedAt = releasePublishedAtPtr(release)
	update.LastPolledAt = &now
	update.LastError = nil
	updated, err := e.pollRepo.UpdatePollState(ctx, repo.ID, update)
	if err != nil {
		return nil, err
	}
	return &RepoEvaluation{Actions: []string{ActionSupersede, ActionCreate}, Repo: updated}, nil
}

func (e *Engine) applyMerge(
	ctx context.Context,
	repo store.MonitoredRepo,
	release *source.Release,
	project ticket.TicketProject,
	provider ticket.TicketProvider,
	repoWebURL string,
) (*RepoEvaluation, error) {
	oldID := *repo.OpenTicketExternalID

	input, err := e.ticketInput(ctx, repo, release, project, repoWebURL, nil)
	if err != nil {
		return e.recordError(ctx, repo, err)
	}

	if err := provider.UpdateTicket(ctx, oldID, input.Title, input.Description); err != nil {
		return e.recordError(ctx, repo, fmt.Errorf("merge ticket: %w", err))
	}

	now := pollNowUTC()
	tag := release.Tag
	update := basePollUpdate(repo)
	update.OpenTicketTag = &tag
	update.LastKnownTag = &tag
	update.LastReleasePublishedAt = releasePublishedAtPtr(release)
	update.LastPolledAt = &now
	update.LastError = nil
	updated, err := e.pollRepo.UpdatePollState(ctx, repo.ID, update)
	if err != nil {
		return nil, err
	}
	return &RepoEvaluation{Actions: []string{ActionMerge}, Repo: updated}, nil
}

func (e *Engine) applySkipIfOpen(
	ctx context.Context,
	repo store.MonitoredRepo,
	release *source.Release,
) (*RepoEvaluation, error) {
	now := pollNowUTC()
	tag := release.Tag
	update := basePollUpdate(repo)
	update.LastKnownTag = &tag
	update.LastReleasePublishedAt = releasePublishedAtPtr(release)
	update.LastPolledAt = &now
	update.LastError = nil
	updated, err := e.pollRepo.UpdatePollState(ctx, repo.ID, update)
	if err != nil {
		return nil, err
	}
	return &RepoEvaluation{Actions: []string{ActionSkipOpen}, Repo: updated}, nil
}

func (e *Engine) recordError(ctx context.Context, repo store.MonitoredRepo, err error) (*RepoEvaluation, error) {
	msg := err.Error()
	now := pollNowUTC()
	update := basePollUpdate(repo)
	update.LastPolledAt = &now
	update.LastError = &msg
	updated, updateErr := e.pollRepo.UpdatePollState(ctx, repo.ID, update)
	if updateErr != nil {
		return nil, updateErr
	}
	return &RepoEvaluation{
		Actions: []string{ActionError},
		Repo:    updated,
		Detail:  msg,
	}, nil
}

func (e *Engine) ticketInput(
	_ context.Context,
	repo store.MonitoredRepo,
	release *source.Release,
	project ticket.TicketProject,
	repoWebURL string,
	supersede *tickettemplate.SupersedeContext,
) (ticket.TicketInput, error) {
	renderer := newTemplateRenderer(project)
	if err := renderer.Validate(); err != nil {
		return ticket.TicketInput{}, err
	}

	tmplCtx := tickettemplate.BuildContext(repo, release, repoWebURL, supersede)
	title, err := renderer.RenderTitle(tmplCtx)
	if err != nil {
		return ticket.TicketInput{}, fmt.Errorf("render title template: %w", err)
	}
	description, err := renderer.RenderDescription(tmplCtx)
	if err != nil {
		return ticket.TicketInput{}, fmt.Errorf("render description template: %w", err)
	}

	return ticket.TicketInput{
		Title:       title,
		Description: description,
		Project:     project,
	}, nil
}

func (e *Engine) renderSupersedeComment(
	repo store.MonitoredRepo,
	release *source.Release,
	project ticket.TicketProject,
	repoWebURL string,
	supersede *tickettemplate.SupersedeContext,
) (string, error) {
	renderer := newTemplateRenderer(project)
	if err := renderer.Validate(); err != nil {
		return "", err
	}
	tmplCtx := tickettemplate.BuildContext(repo, release, repoWebURL, supersede)
	comment, err := renderer.RenderSupersedeComment(tmplCtx)
	if err != nil {
		return "", err
	}
	if comment == "" {
		return "", fmt.Errorf("supersede comment template rendered empty body")
	}
	return comment, nil
}

func newTemplateRenderer(project ticket.TicketProject) *tickettemplate.Renderer {
	return tickettemplate.NewRenderer(project.IntegrationKind, tickettemplate.ContentTemplates{
		Title:            project.ContentTemplates.Title,
		Description:      project.ContentTemplates.Description,
		SupersedeComment: project.ContentTemplates.SupersedeComment,
	})
}

func supersedeOldTag(repo store.MonitoredRepo) string {
	if repo.OpenTicketTag != nil && *repo.OpenTicketTag != "" {
		return *repo.OpenTicketTag
	}
	if repo.LastKnownTag != nil {
		return *repo.LastKnownTag
	}
	return ""
}

func pollNowUTC() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
}

// ResolveRepoWebURL derives the repository browser URL for template context (specs §5.4).
func ResolveRepoWebURL(repo store.MonitoredRepo, sourceIntegrationBaseURL *string) (string, error) {
	baseURL := ""
	if sourceIntegrationBaseURL != nil {
		baseURL = *sourceIntegrationBaseURL
	}
	return source.BuildRepoWebURL(repo.SourceKind, baseURL, repo.ProjectPath)
}
