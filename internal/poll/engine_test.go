package poll_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/poll"
	"github.com/mdg-labs/release-ops/internal/providers/source"
	"github.com/mdg-labs/release-ops/internal/providers/ticket"
	"github.com/mdg-labs/release-ops/internal/store"
)

type mockPollRepo struct {
	updates []store.PollStateUpdate
	repos   map[string]store.MonitoredRepo
}

func newMockPollRepo(repo store.MonitoredRepo) *mockPollRepo {
	return &mockPollRepo{
		repos: map[string]store.MonitoredRepo{repo.ID: repo},
	}
}

func (m *mockPollRepo) UpdatePollState(_ context.Context, repoID string, update store.PollStateUpdate) (*store.MonitoredRepo, error) {
	m.updates = append(m.updates, update)
	repo := m.repos[repoID]
	if update.OpenTicketExternalID != nil {
		if *update.OpenTicketExternalID == "" {
			repo.OpenTicketExternalID = nil
		} else {
			v := *update.OpenTicketExternalID
			repo.OpenTicketExternalID = &v
		}
	}
	if update.OpenTicketTag != nil {
		if *update.OpenTicketTag == "" {
			repo.OpenTicketTag = nil
		} else {
			v := *update.OpenTicketTag
			repo.OpenTicketTag = &v
		}
	}
	if update.LastKnownTag != nil {
		v := *update.LastKnownTag
		repo.LastKnownTag = &v
	}
	if update.LastReleasePublishedAt != nil {
		v := *update.LastReleasePublishedAt
		repo.LastReleasePublishedAt = &v
	}
	if update.LastPolledAt != nil {
		v := *update.LastPolledAt
		repo.LastPolledAt = &v
	}
	if update.LastError != nil {
		v := *update.LastError
		repo.LastError = &v
	} else {
		repo.LastError = nil
	}
	m.repos[repoID] = repo
	out := repo
	return &out, nil
}

func (m *mockPollRepo) InsertRun(context.Context, string) (*store.PollRun, error) {
	return nil, errors.New("not implemented")
}

func (m *mockPollRepo) FinishRun(context.Context, string, string, int64, int64, int64, string) (*store.PollRun, error) {
	return nil, errors.New("not implemented")
}

func (m *mockPollRepo) GetRun(context.Context, string) (*store.PollRun, error) {
	return nil, errors.New("not implemented")
}

func (m *mockPollRepo) ListRuns(context.Context, int64, int64) ([]store.PollRun, error) {
	return nil, errors.New("not implemented")
}

func (m *mockPollRepo) InsertEvent(context.Context, string, *string, string, *string) (*store.PollRunEvent, error) {
	return nil, errors.New("not implemented")
}

func (m *mockPollRepo) ListEventsByRunID(context.Context, string) ([]store.PollRunEvent, error) {
	return nil, errors.New("not implemented")
}

type mockTicketProvider struct {
	statuses map[string]string
	createID string
	webURL   string

	getStatusCalls []string
	updateStatus   []statusUpdate
	comments       []ticketComment
	updates        []ticketUpdate
	createCalls    int
	callLog        []string
	lastCreateInput ticket.TicketInput
}

type statusUpdate struct {
	externalID string
	status     string
}

type ticketComment struct {
	externalID string
	body       string
}

type ticketUpdate struct {
	externalID  string
	title       string
	description string
}

func (m *mockTicketProvider) CreateTicket(_ context.Context, input ticket.TicketInput) (string, error) {
	m.createCalls++
	m.callLog = append(m.callLog, "create")
	m.lastCreateInput = input
	if m.createID == "" {
		return "new-ticket-id", nil
	}
	return m.createID, nil
}

func (m *mockTicketProvider) GetTicketStatus(_ context.Context, externalID string) (string, error) {
	m.getStatusCalls = append(m.getStatusCalls, externalID)
	status, ok := m.statuses[externalID]
	if !ok {
		return "", errors.New("ticket not found")
	}
	return status, nil
}

func (m *mockTicketProvider) UpdateTicketStatus(_ context.Context, externalID, status string) error {
	m.callLog = append(m.callLog, "updateStatus")
	m.updateStatus = append(m.updateStatus, statusUpdate{externalID: externalID, status: status})
	return nil
}

func (m *mockTicketProvider) AddTicketComment(_ context.Context, externalID, body string) error {
	m.callLog = append(m.callLog, "addComment")
	m.comments = append(m.comments, ticketComment{externalID: externalID, body: body})
	return nil
}

func (m *mockTicketProvider) UpdateTicket(_ context.Context, externalID, title, description string) error {
	m.updates = append(m.updates, ticketUpdate{externalID: externalID, title: title, description: description})
	return nil
}

func (m *mockTicketProvider) TicketWebURL(externalID string) (string, error) {
	if m.webURL != "" {
		return m.webURL, nil
	}
	return "https://tickets.example/" + externalID, nil
}

func testRelease(tag string) *source.Release {
	return &source.Release{
		Tag:         tag,
		Name:        "Release",
		URL:         "https://example.com/releases/" + tag,
		PublishedAt: time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC),
	}
}

func testTicketProject(policy string) ticket.TicketProject {
	return ticket.TicketProject{
		ID:                "tp-1",
		IntegrationID:     "int-1",
		IntegrationKind:   ticket.IntegrationKindPhasical,
		ExternalProjectID: "proj-1",
		CreateConfig:      map[string]any{"status": "ready"},
		StatusMapping: ticket.StatusMapping{
			Open:       []string{"ready", "in-progress"},
			Done:       []string{"done"},
			Cancelled:  []string{"cancelled"},
			Superseded: "cancelled",
		},
		OnOpenTicketPolicy: policy,
	}
}

func testRepoWebURL() string {
	return "https://github.com/org/repo"
}

func baseRepo() store.MonitoredRepo {
	return store.MonitoredRepo{
		ID:          "repo-1",
		SourceKind:  "github",
		ProjectPath: "org/repo",
		Enabled:     true,
	}
}

func TestEvaluateRepoBaselineOnFirstPoll(t *testing.T) {
	t.Parallel()

	repo := baseRepo()
	pollRepo := newMockPollRepo(repo)
	engine := poll.NewEngine(pollRepo)
	provider := &mockTicketProvider{}

	got, err := engine.EvaluateRepo(
		context.Background(),
		repo,
		testRelease("v1.0.0"),
		nil,
		testTicketProject(ticket.PolicySupersede),
		provider,
		testRepoWebURL(),
	)
	if err != nil {
		t.Fatalf("EvaluateRepo: %v", err)
	}
	if len(got.Actions) != 1 || got.Actions[0] != poll.ActionBaseline {
		t.Fatalf("actions = %v, want [baseline]", got.Actions)
	}
	if got.Repo.LastKnownTag == nil || *got.Repo.LastKnownTag != "v1.0.0" {
		t.Fatalf("LastKnownTag = %v, want v1.0.0", got.Repo.LastKnownTag)
	}
	wantPublished := "2026-08-06T12:00:00.000Z"
	if got.Repo.LastReleasePublishedAt == nil || *got.Repo.LastReleasePublishedAt != wantPublished {
		t.Fatalf("LastReleasePublishedAt = %v, want %s", got.Repo.LastReleasePublishedAt, wantPublished)
	}
	if provider.createCalls != 0 {
		t.Fatalf("CreateTicket calls = %d, want 0", provider.createCalls)
	}
}

func TestEvaluateRepoSkipWhenTagMatches(t *testing.T) {
	t.Parallel()

	repo := baseRepo()
	tag := "v1.0.0"
	repo.LastKnownTag = &tag
	pollRepo := newMockPollRepo(repo)
	engine := poll.NewEngine(pollRepo)

	got, err := engine.EvaluateRepo(
		context.Background(),
		repo,
		testRelease("v1.0.0"),
		nil,
		testTicketProject(ticket.PolicySupersede),
		&mockTicketProvider{},
		testRepoWebURL(),
	)
	if err != nil {
		t.Fatalf("EvaluateRepo: %v", err)
	}
	if got.Actions[0] != poll.ActionSkip {
		t.Fatalf("actions = %v, want [skip]", got.Actions)
	}
	if got.Repo.LastKnownTag == nil || *got.Repo.LastKnownTag != "v1.0.0" {
		t.Fatalf("LastKnownTag = %v, want unchanged v1.0.0", got.Repo.LastKnownTag)
	}
}

func TestEvaluateRepoCreateWhenNoOpenTicket(t *testing.T) {
	t.Parallel()

	repo := baseRepo()
	last := "v1.0.0"
	repo.LastKnownTag = &last
	pollRepo := newMockPollRepo(repo)
	engine := poll.NewEngine(pollRepo)
	provider := &mockTicketProvider{createID: "ticket-v2"}

	got, err := engine.EvaluateRepo(
		context.Background(),
		repo,
		testRelease("v2.0.0"),
		nil,
		testTicketProject(ticket.PolicySupersede),
		provider,
		testRepoWebURL(),
	)
	if err != nil {
		t.Fatalf("EvaluateRepo: %v", err)
	}
	if got.Actions[0] != poll.ActionCreate {
		t.Fatalf("actions = %v, want [create]", got.Actions)
	}
	if provider.createCalls != 1 {
		t.Fatalf("CreateTicket calls = %d, want 1", provider.createCalls)
	}
	if got.Repo.OpenTicketExternalID == nil || *got.Repo.OpenTicketExternalID != "ticket-v2" {
		t.Fatalf("OpenTicketExternalID = %v, want ticket-v2", got.Repo.OpenTicketExternalID)
	}
	if got.Repo.LastKnownTag == nil || *got.Repo.LastKnownTag != "v2.0.0" {
		t.Fatalf("LastKnownTag = %v, want v2.0.0", got.Repo.LastKnownTag)
	}
}

func TestEvaluateRepoLiveGetTicketStatusBeforePolicy(t *testing.T) {
	t.Parallel()

	repo := baseRepo()
	last := "v1.0.0"
	openID := "old-ticket"
	openTag := "v1.0.0"
	repo.LastKnownTag = &last
	repo.OpenTicketExternalID = &openID
	repo.OpenTicketTag = &openTag

	pollRepo := newMockPollRepo(repo)
	engine := poll.NewEngine(pollRepo)
	provider := &mockTicketProvider{
		statuses: map[string]string{openID: "in-progress"},
	}

	_, err := engine.EvaluateRepo(
		context.Background(),
		repo,
		testRelease("v2.0.0"),
		nil,
		testTicketProject(ticket.PolicySkipIfOpen),
		provider,
		testRepoWebURL(),
	)
	if err != nil {
		t.Fatalf("EvaluateRepo: %v", err)
	}
	if len(provider.getStatusCalls) != 1 || provider.getStatusCalls[0] != openID {
		t.Fatalf("GetTicketStatus calls = %v, want [%s]", provider.getStatusCalls, openID)
	}
}

func TestEvaluateRepoSupersedePolicy(t *testing.T) {
	t.Parallel()

	repo := baseRepo()
	last := "v1.0.0"
	openID := "old-ticket"
	openTag := "v1.0.0"
	repo.LastKnownTag = &last
	repo.OpenTicketExternalID = &openID
	repo.OpenTicketTag = &openTag

	pollRepo := newMockPollRepo(repo)
	engine := poll.NewEngine(pollRepo)
	provider := &mockTicketProvider{
		statuses: map[string]string{openID: "in-progress"},
		createID: "new-ticket",
	}

	got, err := engine.EvaluateRepo(
		context.Background(),
		repo,
		testRelease("v2.0.0"),
		nil,
		testTicketProject(ticket.PolicySupersede),
		provider,
		testRepoWebURL(),
	)
	if err != nil {
		t.Fatalf("EvaluateRepo: %v", err)
	}
	if len(got.Actions) != 2 {
		t.Fatalf("actions = %v, want [supersede create]", got.Actions)
	}
	if got.Actions[0] != poll.ActionSupersede || got.Actions[1] != poll.ActionCreate {
		t.Fatalf("actions = %v, want [supersede create]", got.Actions)
	}
	if len(provider.updateStatus) != 1 || provider.updateStatus[0].status != "cancelled" {
		t.Fatalf("UpdateTicketStatus = %v, want superseded status cancelled", provider.updateStatus)
	}
	if len(provider.comments) != 1 {
		t.Fatalf("comments = %d, want 1", len(provider.comments))
	}
	if !strings.Contains(provider.comments[0].body, "v1.0.0 → v2.0.0") {
		t.Fatalf("comment = %q, want old→new tag", provider.comments[0].body)
	}
	if provider.createCalls != 1 {
		t.Fatalf("CreateTicket calls = %d, want 1", provider.createCalls)
	}
	if len(provider.callLog) < 3 || provider.callLog[0] != "create" {
		t.Fatalf("call order = %v, want create before status update", provider.callLog)
	}
	if got.Repo.OpenTicketExternalID == nil || *got.Repo.OpenTicketExternalID != "new-ticket" {
		t.Fatalf("OpenTicketExternalID = %v, want new-ticket", got.Repo.OpenTicketExternalID)
	}
}

func TestEvaluateRepoSupersedeRendersOldTagInNewTicketTemplate(t *testing.T) {
	t.Parallel()

	repo := baseRepo()
	last := "v1.0.0"
	openID := "old-ticket"
	openTag := "v1.0.0"
	repo.LastKnownTag = &last
	repo.OpenTicketExternalID = &openID
	repo.OpenTicketTag = &openTag

	pollRepo := newMockPollRepo(repo)
	engine := poll.NewEngine(pollRepo)
	provider := &mockTicketProvider{
		statuses: map[string]string{openID: "in-progress"},
		createID: "new-ticket",
	}
	project := testTicketProject(ticket.PolicySupersede)
	project.ContentTemplates.Title = "Supersede from {{ .Supersede.OldTag }} to {{ .Release.Tag }}"

	_, err := engine.EvaluateRepo(
		context.Background(),
		repo,
		testRelease("v2.0.0"),
		nil,
		project,
		provider,
		testRepoWebURL(),
	)
	if err != nil {
		t.Fatalf("EvaluateRepo: %v", err)
	}
	if provider.createCalls != 1 {
		t.Fatalf("CreateTicket calls = %d, want 1", provider.createCalls)
	}
	wantTitle := "Supersede from v1.0.0 to v2.0.0"
	if provider.lastCreateInput.Title != wantTitle {
		t.Fatalf("CreateTicket title = %q, want %q", provider.lastCreateInput.Title, wantTitle)
	}
}

func TestEvaluateRepoMergePolicyUpdatesTicket(t *testing.T) {
	t.Parallel()

	repo := baseRepo()
	last := "v1.0.0"
	openID := "open-ticket"
	openTag := "v1.0.0"
	repo.LastKnownTag = &last
	repo.OpenTicketExternalID = &openID
	repo.OpenTicketTag = &openTag

	pollRepo := newMockPollRepo(repo)
	engine := poll.NewEngine(pollRepo)
	provider := &mockTicketProvider{
		statuses: map[string]string{openID: "ready"},
	}

	got, err := engine.EvaluateRepo(
		context.Background(),
		repo,
		testRelease("v2.0.0"),
		nil,
		testTicketProject(ticket.PolicyMerge),
		provider,
		testRepoWebURL(),
	)
	if err != nil {
		t.Fatalf("EvaluateRepo: %v", err)
	}
	if got.Actions[0] != poll.ActionMerge {
		t.Fatalf("actions = %v, want [merge]", got.Actions)
	}
	if provider.createCalls != 0 {
		t.Fatalf("CreateTicket calls = %d, want 0", provider.createCalls)
	}
	if len(provider.updates) != 1 {
		t.Fatalf("UpdateTicket calls = %d, want 1", len(provider.updates))
	}
	wantTitle := "Release: github org/repo v2.0.0"
	if provider.updates[0].title != wantTitle {
		t.Fatalf("UpdateTicket title = %q, want %q", provider.updates[0].title, wantTitle)
	}
	if got.Repo.OpenTicketExternalID == nil || *got.Repo.OpenTicketExternalID != openID {
		t.Fatalf("OpenTicketExternalID = %v, want unchanged %s", got.Repo.OpenTicketExternalID, openID)
	}
	if got.Repo.LastKnownTag == nil || *got.Repo.LastKnownTag != "v2.0.0" {
		t.Fatalf("LastKnownTag = %v, want v2.0.0", got.Repo.LastKnownTag)
	}
}

func TestEvaluateRepoSkipIfOpenPolicy(t *testing.T) {
	t.Parallel()

	repo := baseRepo()
	last := "v1.0.0"
	openID := "open-ticket"
	openTag := "v1.0.0"
	repo.LastKnownTag = &last
	repo.OpenTicketExternalID = &openID
	repo.OpenTicketTag = &openTag

	pollRepo := newMockPollRepo(repo)
	engine := poll.NewEngine(pollRepo)
	provider := &mockTicketProvider{
		statuses: map[string]string{openID: "ready"},
	}

	got, err := engine.EvaluateRepo(
		context.Background(),
		repo,
		testRelease("v2.0.0"),
		nil,
		testTicketProject(ticket.PolicySkipIfOpen),
		provider,
		testRepoWebURL(),
	)
	if err != nil {
		t.Fatalf("EvaluateRepo: %v", err)
	}
	if got.Actions[0] != poll.ActionSkipOpen {
		t.Fatalf("actions = %v, want [skip_open]", got.Actions)
	}
	if provider.createCalls != 0 {
		t.Fatalf("CreateTicket calls = %d, want 0", provider.createCalls)
	}
	if len(provider.updates) != 0 {
		t.Fatalf("UpdateTicket calls = %d, want 0", len(provider.updates))
	}
	if got.Repo.OpenTicketExternalID == nil || *got.Repo.OpenTicketExternalID != openID {
		t.Fatalf("OpenTicketExternalID = %v, want unchanged", got.Repo.OpenTicketExternalID)
	}
	if got.Repo.LastKnownTag == nil || *got.Repo.LastKnownTag != "v2.0.0" {
		t.Fatalf("LastKnownTag = %v, want v2.0.0", got.Repo.LastKnownTag)
	}
}

func TestEvaluateRepoCreateWhenTicketClosedExternally(t *testing.T) {
	t.Parallel()

	repo := baseRepo()
	last := "v1.0.0"
	openID := "done-ticket"
	repo.LastKnownTag = &last
	repo.OpenTicketExternalID = &openID

	pollRepo := newMockPollRepo(repo)
	engine := poll.NewEngine(pollRepo)
	provider := &mockTicketProvider{
		statuses: map[string]string{openID: "done"},
		createID: "fresh-ticket",
	}

	got, err := engine.EvaluateRepo(
		context.Background(),
		repo,
		testRelease("v2.0.0"),
		nil,
		testTicketProject(ticket.PolicySupersede),
		provider,
		testRepoWebURL(),
	)
	if err != nil {
		t.Fatalf("EvaluateRepo: %v", err)
	}
	if got.Actions[0] != poll.ActionCreate {
		t.Fatalf("actions = %v, want [create]", got.Actions)
	}
	if len(provider.updateStatus) != 0 {
		t.Fatal("closed ticket should not trigger supersede status update")
	}
}

func TestEvaluateRepoInvalidTemplateRecordsError(t *testing.T) {
	t.Parallel()

	repo := baseRepo()
	last := "v1.0.0"
	repo.LastKnownTag = &last
	pollRepo := newMockPollRepo(repo)
	engine := poll.NewEngine(pollRepo)
	project := testTicketProject(ticket.PolicySupersede)
	project.ContentTemplates.Title = "{{ .Release.Tag "

	got, err := engine.EvaluateRepo(
		context.Background(),
		repo,
		testRelease("v2.0.0"),
		nil,
		project,
		&mockTicketProvider{},
		testRepoWebURL(),
	)
	if err != nil {
		t.Fatalf("EvaluateRepo: %v", err)
	}
	if got.Actions[0] != poll.ActionError {
		t.Fatalf("actions = %v, want [error]", got.Actions)
	}
}

func TestEvaluateRepoFetchError(t *testing.T) {
	t.Parallel()

	repo := baseRepo()
	pollRepo := newMockPollRepo(repo)
	engine := poll.NewEngine(pollRepo)

	got, err := engine.EvaluateRepo(
		context.Background(),
		repo,
		nil,
		errors.New("network down"),
		testTicketProject(ticket.PolicySupersede),
		&mockTicketProvider{},
		testRepoWebURL(),
	)
	if err != nil {
		t.Fatalf("EvaluateRepo: %v", err)
	}
	if got.Actions[0] != poll.ActionError {
		t.Fatalf("actions = %v, want [error]", got.Actions)
	}
	if got.Repo.LastError == nil || !strings.Contains(*got.Repo.LastError, "network down") {
		t.Fatalf("LastError = %v, want network down", got.Repo.LastError)
	}
}
