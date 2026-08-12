package poll_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/sqlite"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/mdg-labs/release-ops/internal/crypto"
	"github.com/mdg-labs/release-ops/internal/poll"
	"github.com/mdg-labs/release-ops/internal/providers/source"
	"github.com/mdg-labs/release-ops/internal/providers/ticket"
	"github.com/mdg-labs/release-ops/internal/store"
	_ "modernc.org/sqlite"
)

const integrationTestKeyHex = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// sequentialSource returns tags from a fixed sequence on each GetLatestRelease call.
type sequentialSource struct {
	tags  []string
	index int
}

func (s *sequentialSource) GetLatestRelease(ctx context.Context, _ string, _ source.ReleaseOptions) (*source.Release, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.index >= len(s.tags) {
		return nil, fmt.Errorf("sequential source exhausted at index %d", s.index)
	}
	tag := s.tags[s.index]
	s.index++
	return integrationRelease(tag), nil
}

func integrationRelease(tag string) *source.Release {
	return &source.Release{
		Tag:         tag,
		Name:        "Release " + tag,
		URL:         "https://example.com/releases/" + tag,
		PublishedAt: time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC),
	}
}

// integrationTicketProvider records ticket operations and assigns sequential external IDs.
type integrationTicketProvider struct {
	statuses map[string]string

	createCalls  int
	createdIDs   []string
	createInputs []ticket.TicketInput
	updateStatus []statusUpdate
	comments     []ticketComment
}

func newIntegrationTicketProvider() *integrationTicketProvider {
	return &integrationTicketProvider{
		statuses: make(map[string]string),
	}
}

func (m *integrationTicketProvider) CreateTicket(_ context.Context, input ticket.TicketInput) (string, error) {
	m.createCalls++
	m.createInputs = append(m.createInputs, input)
	id := fmt.Sprintf("ticket-%d", m.createCalls)
	m.createdIDs = append(m.createdIDs, id)
	m.statuses[id] = "in-progress"
	return id, nil
}

func (m *integrationTicketProvider) GetTicketStatus(_ context.Context, externalID string) (string, error) {
	status, ok := m.statuses[externalID]
	if !ok {
		return "", fmt.Errorf("ticket %q not found", externalID)
	}
	return status, nil
}

func (m *integrationTicketProvider) UpdateTicketStatus(_ context.Context, externalID, status string) error {
	m.updateStatus = append(m.updateStatus, statusUpdate{externalID: externalID, status: status})
	m.statuses[externalID] = status
	return nil
}

func (m *integrationTicketProvider) AddTicketComment(_ context.Context, externalID, body string) error {
	m.comments = append(m.comments, ticketComment{externalID: externalID, body: body})
	return nil
}

func (m *integrationTicketProvider) UpdateTicket(_ context.Context, externalID, title, description string) error {
	return fmt.Errorf("UpdateTicket not expected in integration flow: %s", externalID)
}

func (m *integrationTicketProvider) TicketWebURL(externalID string) (string, error) {
	return "https://tickets.example/" + externalID, nil
}

type pollIntegrationEnv struct {
	store    *store.Store
	scheduler *poll.Scheduler
	source   *sequentialSource
	tickets  *integrationTicketProvider
	repoID   string
}

func TestIntegrationBaselineCreateSupersede(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newPollIntegrationEnv(t, []string{"v1.0.0", "v1.0.0", "v2.0.0", "v3.0.0"}, "")

	// Poll 1: baseline — first sight of v1.0.0, no ticket.
	run1 := env.runPollCycle(t, ctx)
	assertRunSuccess(t, ctx, env, run1, 0, 0)
	assertEventActions(t, ctx, env, run1, []string{poll.ActionBaseline})

	repo, err := env.store.Repos().Get(ctx, env.repoID)
	if err != nil {
		t.Fatalf("Get repo after baseline: %v", err)
	}
	if repo.LastKnownTag == nil || *repo.LastKnownTag != "v1.0.0" {
		t.Fatalf("LastKnownTag = %v, want v1.0.0", repo.LastKnownTag)
	}
	if repo.OpenTicketExternalID != nil {
		t.Fatalf("OpenTicketExternalID = %v, want nil after baseline", repo.OpenTicketExternalID)
	}
	if env.tickets.createCalls != 0 {
		t.Fatalf("CreateTicket calls = %d, want 0 after baseline", env.tickets.createCalls)
	}

	// Poll 2: skip — same tag, no ticket.
	run2 := env.runPollCycle(t, ctx)
	assertRunSuccess(t, ctx, env, run2, 0, 0)
	assertEventActions(t, ctx, env, run2, []string{poll.ActionSkip})

	// Poll 3: create — new tag v2.0.0.
	run3 := env.runPollCycle(t, ctx)
	assertRunSuccess(t, ctx, env, run3, 1, 0)
	assertEventActions(t, ctx, env, run3, []string{poll.ActionCreate})

	if env.tickets.createCalls != 1 {
		t.Fatalf("CreateTicket calls = %d, want 1 after create poll", env.tickets.createCalls)
	}
	if len(env.tickets.createdIDs) != 1 || env.tickets.createdIDs[0] != "ticket-1" {
		t.Fatalf("createdIDs = %v, want [ticket-1]", env.tickets.createdIDs)
	}

	repo, err = env.store.Repos().Get(ctx, env.repoID)
	if err != nil {
		t.Fatalf("Get repo after create: %v", err)
	}
	if repo.OpenTicketExternalID == nil || *repo.OpenTicketExternalID != "ticket-1" {
		t.Fatalf("OpenTicketExternalID = %v, want ticket-1", repo.OpenTicketExternalID)
	}
	if repo.LastKnownTag == nil || *repo.LastKnownTag != "v2.0.0" {
		t.Fatalf("LastKnownTag = %v, want v2.0.0", repo.LastKnownTag)
	}

	// Poll 4: supersede — open ticket on v2.0.0, new tag v3.0.0.
	run4 := env.runPollCycle(t, ctx)
	assertRunSuccess(t, ctx, env, run4, 1, 1)
	assertEventActions(t, ctx, env, run4, []string{poll.ActionSupersede, poll.ActionCreate})

	if env.tickets.createCalls != 2 {
		t.Fatalf("CreateTicket calls = %d, want 2 after supersede poll", env.tickets.createCalls)
	}
	if len(env.tickets.updateStatus) != 1 || env.tickets.updateStatus[0].status != "cancelled" {
		t.Fatalf("UpdateTicketStatus = %v, want superseded status cancelled", env.tickets.updateStatus)
	}
	if len(env.tickets.comments) != 1 {
		t.Fatalf("comments = %d, want 1 supersede comment", len(env.tickets.comments))
	}
	if !strings.Contains(env.tickets.comments[0].body, "v2.0.0 → v3.0.0") {
		t.Fatalf("supersede comment = %q, want v2.0.0 → v3.0.0", env.tickets.comments[0].body)
	}

	repo, err = env.store.Repos().Get(ctx, env.repoID)
	if err != nil {
		t.Fatalf("Get repo after supersede: %v", err)
	}
	if repo.OpenTicketExternalID == nil || *repo.OpenTicketExternalID != "ticket-2" {
		t.Fatalf("OpenTicketExternalID = %v, want ticket-2", repo.OpenTicketExternalID)
	}
	if repo.LastKnownTag == nil || *repo.LastKnownTag != "v3.0.0" {
		t.Fatalf("LastKnownTag = %v, want v3.0.0", repo.LastKnownTag)
	}
}

func TestIntegrationCustomContentTemplatesApplied(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	customTemplates := `{"title":"CUSTOM {{ .Release.Tag }}","description":"Repo: {{ .Repo.URL }}","supersedeComment":""}`
	env := newPollIntegrationEnv(t, []string{"v1.0.0", "v2.0.0"}, customTemplates)

	// Baseline first sight of v1.0.0.
	run1 := env.runPollCycle(t, ctx)
	assertRunSuccess(t, ctx, env, run1, 0, 0)
	assertEventActions(t, ctx, env, run1, []string{poll.ActionBaseline})

	// Create on v2.0.0 should use stored templates from ticket_projects.content_templates.
	run2 := env.runPollCycle(t, ctx)
	assertRunSuccess(t, ctx, env, run2, 1, 0)
	assertEventActions(t, ctx, env, run2, []string{poll.ActionCreate})

	if len(env.tickets.createInputs) != 1 {
		t.Fatalf("createInputs = %d, want 1", len(env.tickets.createInputs))
	}
	input := env.tickets.createInputs[0]
	if input.Title != "CUSTOM v2.0.0" {
		t.Fatalf("CreateTicket title = %q, want CUSTOM v2.0.0", input.Title)
	}
	if !strings.Contains(input.Description, "Repo: https://github.com/org/integration-repo") {
		t.Fatalf("CreateTicket description = %q, want repo URL from custom template", input.Description)
	}
}

func newPollIntegrationEnv(t *testing.T, tags []string, contentTemplates string) *pollIntegrationEnv {
	t.Helper()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	sqlDB := openIntegrationDB(t, dbPath)
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})

	cipher, err := crypto.NewCipherFromHex(integrationTestKeyHex)
	if err != nil {
		t.Fatalf("NewCipherFromHex: %v", err)
	}

	seedIntegrationAppSettings(t, sqlDB)
	s := store.New(sqlDB, cipher)
	ctx := context.Background()

	integration, err := s.Integrations().Create(ctx, store.CreateIntegrationInput{
		Kind:    "phasical",
		Name:    "Phasical Integration",
		BaseURL: integrationStrPtr("https://api.phasical.example"),
		Secret:  []byte(`{"api_key":"test"}`),
	})
	if err != nil {
		t.Fatalf("Create integration: %v", err)
	}

	ticketProject, err := s.TicketProjects().Create(ctx, store.CreateTicketProjectInput{
		IntegrationID:      integration.ID,
		ExternalProjectID:  "proj-integration",
		Name:               "Integration Project",
		CreateConfig:       `{"status":"ready"}`,
		StatusMapping:      `{"open":["ready","in-progress"],"done":["done"],"cancelled":["cancelled"],"superseded":"cancelled"}`,
		ContentTemplates:   contentTemplates,
		OnOpenTicketPolicy: ticket.PolicySupersede,
	})
	if err != nil {
		t.Fatalf("Create ticket project: %v", err)
	}

	repo, err := s.Repos().Create(ctx, store.CreateMonitoredRepoInput{
		SourceKind:      "github",
		ProjectPath:     "org/integration-repo",
		Enabled:         true,
		TicketProjectID: ticketProject.ID,
	})
	if err != nil {
		t.Fatalf("Create monitored repo: %v", err)
	}

	seqSource := &sequentialSource{tags: tags}
	ticketProvider := newIntegrationTicketProvider()
	engine := poll.NewEngine(s.Poll())

	scheduler, err := poll.NewScheduler(poll.SchedulerConfig{
		Engine: engine,
		Repos:  s.Repos(),
		Poll:   s.Poll(),
		PollRepo: func(ctx context.Context, _ string, repo store.MonitoredRepo) (*poll.RepoEvaluation, error) {
			release, fetchErr := seqSource.GetLatestRelease(ctx, repo.ProjectPath, source.ReleaseOptions{})
			if fetchErr != nil {
				return engine.EvaluateRepo(ctx, repo, nil, fetchErr, ticket.TicketProject{}, ticketProvider, "")
			}

			tpRow, err := s.TicketProjects().Get(ctx, repo.TicketProjectID)
			if err != nil {
				return nil, fmt.Errorf("load ticket project: %w", err)
			}
			ticketProject, err := poll.TicketProjectFromStore(*tpRow, integration.Kind)
			if err != nil {
				return nil, err
			}

			repoWebURL, err := poll.ResolveRepoWebURL(repo, nil)
			if err != nil {
				return nil, err
			}

			return engine.EvaluateRepo(ctx, repo, release, nil, ticketProject, ticketProvider, repoWebURL)
		},
	})
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}

	return &pollIntegrationEnv{
		store:     s,
		scheduler: scheduler,
		source:    seqSource,
		tickets:   ticketProvider,
		repoID:    repo.ID,
	}
}

func (env *pollIntegrationEnv) runPollCycle(t *testing.T, ctx context.Context) string {
	t.Helper()

	run, err := env.store.Poll().InsertRun(ctx, store.PollTriggerSourceManual)
	if err != nil {
		t.Fatalf("InsertRun: %v", err)
	}
	if err := env.scheduler.RunAll(ctx, run.ID); err != nil {
		t.Fatalf("RunAll: %v", err)
	}
	return run.ID
}

func assertRunSuccess(
	t *testing.T,
	ctx context.Context,
	env *pollIntegrationEnv,
	runID string,
	wantCreated, wantSuperseded int64,
) {
	t.Helper()

	got, err := env.store.Poll().GetRun(ctx, runID)
	if err != nil {
		t.Fatalf("GetRun %s: %v", runID, err)
	}
	if got.Status != poll.RunStatusSuccess {
		t.Fatalf("run %s status = %q, want success", runID, got.Status)
	}
	if got.ReposChecked != 1 {
		t.Fatalf("run %s repos_checked = %d, want 1", runID, got.ReposChecked)
	}
	if got.TicketsCreated != wantCreated {
		t.Fatalf("run %s tickets_created = %d, want %d", runID, got.TicketsCreated, wantCreated)
	}
	if got.TicketsSuperseded != wantSuperseded {
		t.Fatalf("run %s tickets_superseded = %d, want %d", runID, got.TicketsSuperseded, wantSuperseded)
	}
}

func assertEventActions(t *testing.T, ctx context.Context, env *pollIntegrationEnv, runID string, want []string) {
	t.Helper()

	events, err := env.store.Poll().ListEventsByRunID(ctx, runID)
	if err != nil {
		t.Fatalf("ListEventsByRunID %s: %v", runID, err)
	}
	if len(events) != len(want) {
		t.Fatalf("run %s events = %d, want %d", runID, len(events), len(want))
	}
	for i, action := range want {
		if events[i].Action != action {
			t.Fatalf("run %s event[%d] action = %q, want %q", runID, i, events[i].Action, action)
		}
		if events[i].MonitoredRepoID == nil || *events[i].MonitoredRepoID != env.repoID {
			t.Fatalf("run %s event[%d] repoId = %v, want %s", runID, i, events[i].MonitoredRepoID, env.repoID)
		}
	}
}

func openIntegrationDB(t *testing.T, dbPath string) *sql.DB {
	t.Helper()

	migrationsURL := integrationMigrationSourceURL(t)
	m, err := newIntegrationMigrator(t, dbPath, migrationsURL)
	if err != nil {
		t.Fatalf("newIntegrationMigrator: %v", err)
	}
	t.Cleanup(func() {
		_, _ = m.Close()
	})
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		t.Fatalf("migrate up: %v", err)
	}

	sqlDB, err := store.OpenPath(dbPath)
	if err != nil {
		t.Fatalf("OpenPath: %v", err)
	}
	return sqlDB
}

func seedIntegrationAppSettings(t *testing.T, sqlDB *sql.DB) {
	t.Helper()

	_, err := sqlDB.Exec(
		`INSERT INTO app_settings (id, poll_interval_minutes, updated_at) VALUES (1, 360, ?)`,
		"2026-08-06T12:00:00.000Z",
	)
	if err != nil {
		t.Fatalf("seed app_settings: %v", err)
	}
}

func integrationMigrationSourceURL(t *testing.T) string {
	t.Helper()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	return "file://" + filepath.Join(root, "migrations")
}

func newIntegrationMigrator(t *testing.T, dbPath, migrationsURL string) (*migrate.Migrate, error) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", "file:"+dbPath+"?_foreign_keys=on")
	if err != nil {
		return nil, err
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	driver, err := sqlite.WithInstance(db, &sqlite.Config{})
	if err != nil {
		return nil, err
	}

	return migrate.NewWithDatabaseInstance(migrationsURL, "sqlite", driver)
}

func integrationStrPtr(s string) *string {
	return &s
}
