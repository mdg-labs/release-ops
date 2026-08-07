package poll

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/mdg-labs/release-ops/internal/providers/source"
	"github.com/mdg-labs/release-ops/internal/providers/ticket"
	"github.com/mdg-labs/release-ops/internal/store"
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
) (*RepoEvaluation, error) {
	if fetchErr != nil {
		return e.recordError(ctx, repo, fetchErr)
	}

	tagAction, tag := decideTagChange(repo.LastKnownTag, release)
	switch tagAction {
	case tagActionBaseline:
		return e.applyBaseline(ctx, repo, tag)
	case tagActionSkip:
		return e.applySkip(ctx, repo)
	case tagActionNewTag:
		return e.applyNewTag(ctx, repo, release, project, provider)
	default:
		return e.recordError(ctx, repo, fmt.Errorf("unexpected tag decision"))
	}
}

// TicketProjectFromStore converts a store row to ticket.TicketProject for EvaluateRepo.
func TicketProjectFromStore(row store.TicketProject) (ticket.TicketProject, error) {
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

	policy := row.OnOpenTicketPolicy
	if policy == "" {
		policy = ticket.PolicySupersede
	}

	return ticket.TicketProject{
		ID:                 row.ID,
		IntegrationID:      row.IntegrationID,
		ExternalProjectID:  row.ExternalProjectID,
		CreateConfig:       createConfig,
		StatusMapping:      mapping,
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
		OpenTicketExternalID: repo.OpenTicketExternalID,
		OpenTicketTag:        repo.OpenTicketTag,
		LastKnownTag:         repo.LastKnownTag,
		LastPolledAt:         repo.LastPolledAt,
		LastError:            repo.LastError,
	}
}

func (e *Engine) applyBaseline(ctx context.Context, repo store.MonitoredRepo, tag string) (*RepoEvaluation, error) {
	now := pollNowUTC()
	update := basePollUpdate(repo)
	update.LastPolledAt = &now
	update.LastError = nil
	if tag != "" {
		update.LastKnownTag = &tag
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
			return e.applyOpenTicketPolicy(ctx, repo, release, project, provider)
		default:
			return e.recordError(ctx, repo, fmt.Errorf("unknown ticket status %q", rawStatus))
		}
	}

	return e.applyCreate(ctx, repoForPolicy, release, project, provider)
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
) (*RepoEvaluation, error) {
	policy := project.OnOpenTicketPolicy
	if policy == "" {
		policy = ticket.PolicySupersede
	}

	switch policy {
	case ticket.PolicySupersede:
		return e.applySupersede(ctx, repo, release, project, provider)
	case ticket.PolicyMerge:
		return e.applyMerge(ctx, repo, release, project, provider)
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
) (*RepoEvaluation, error) {
	externalID, err := provider.CreateTicket(ctx, ticketInput(repo, release, project))
	if err != nil {
		return e.recordError(ctx, repo, fmt.Errorf("create ticket: %w", err))
	}

	now := pollNowUTC()
	tag := release.Tag
	update := basePollUpdate(repo)
	update.OpenTicketExternalID = &externalID
	update.OpenTicketTag = &tag
	update.LastKnownTag = &tag
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
) (*RepoEvaluation, error) {
	oldID := *repo.OpenTicketExternalID
	oldTag := supersedeOldTag(repo)

	if project.StatusMapping.Superseded == "" {
		return e.recordError(ctx, repo, fmt.Errorf("status_mapping.superseded is required for supersede policy"))
	}

	if err := provider.UpdateTicketStatus(ctx, oldID, project.StatusMapping.Superseded); err != nil {
		return e.recordError(ctx, repo, fmt.Errorf("supersede ticket status: %w", err))
	}

	comment := buildSupersedeComment(oldTag, release.Tag, release.URL)
	if err := provider.AddTicketComment(ctx, oldID, comment); err != nil {
		return e.recordError(ctx, repo, fmt.Errorf("supersede ticket comment: %w", err))
	}

	externalID, err := provider.CreateTicket(ctx, ticketInput(repo, release, project))
	if err != nil {
		return e.recordError(ctx, repo, fmt.Errorf("create ticket after supersede: %w", err))
	}

	now := pollNowUTC()
	tag := release.Tag
	update := basePollUpdate(repo)
	update.OpenTicketExternalID = &externalID
	update.OpenTicketTag = &tag
	update.LastKnownTag = &tag
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
) (*RepoEvaluation, error) {
	oldID := *repo.OpenTicketExternalID
	title := ticket.BuildTitle(repo.SourceKind, repo.ProjectPath, release.Tag)
	description := ticket.BuildDescription(*release)

	if err := provider.UpdateTicket(ctx, oldID, title, description); err != nil {
		return e.recordError(ctx, repo, fmt.Errorf("merge ticket: %w", err))
	}

	now := pollNowUTC()
	tag := release.Tag
	update := basePollUpdate(repo)
	update.OpenTicketTag = &tag
	update.LastKnownTag = &tag
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

func ticketInput(repo store.MonitoredRepo, release *source.Release, project ticket.TicketProject) ticket.TicketInput {
	return ticket.TicketInput{
		Title:       ticket.BuildTitle(repo.SourceKind, repo.ProjectPath, release.Tag),
		Description: ticket.BuildDescription(*release),
		Project:     project,
	}
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

func buildSupersedeComment(oldTag, newTag, releaseURL string) string {
	return fmt.Sprintf("Superseded: %s → %s\n%s", oldTag, newTag, releaseURL)
}

func pollNowUTC() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
}
