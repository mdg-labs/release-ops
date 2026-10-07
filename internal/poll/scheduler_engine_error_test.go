package poll_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/mdg-labs/release-ops/internal/crypto"
	"github.com/mdg-labs/release-ops/internal/poll"
	"github.com/mdg-labs/release-ops/internal/providers/ticket"
	"github.com/mdg-labs/release-ops/internal/store"
)

// failAfterSavePollRepo stores the update, then reports an error when the update
// carries a new ticket link: the shape of a state write that commits and whose
// follow-up read fails.
type failAfterSavePollRepo struct {
	store.PollRepository
	saveErr error
}

func (r failAfterSavePollRepo) UpdatePollState(ctx context.Context, repoID string, update store.PollStateUpdate) (*store.MonitoredRepo, error) {
	updated, err := r.PollRepository.UpdatePollState(ctx, repoID, update)
	if err != nil {
		return nil, err
	}
	if update.OpenTicketExternalID != nil {
		return nil, r.saveErr
	}
	return updated, nil
}

// TestSchedulerKeepsStateSavedByEngineWhenEngineErrors drives the default per-repo path
// through a new-release ticket create whose state write commits and then returns an
// error. The error stays an engine error: the saved ticket link and tag are not
// rewritten, so the next poll does not create a second ticket.
func TestSchedulerKeepsStateSavedByEngineWhenEngineErrors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	var creates atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/org/app/releases/latest":
			_, _ = w.Write([]byte(`{"tag_name":"v1.1.0","name":"v1.1.0","html_url":"https://example.invalid/r","published_at":"2026-01-02T03:04:05Z"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/task/proj-1":
			creates.Add(1)
			_, _ = w.Write([]byte(`{"id":"task-1"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	sqlDB := openIntegrationDB(t, filepath.Join(t.TempDir(), "app.db"))
	t.Cleanup(func() { _ = sqlDB.Close() })
	cipher, err := crypto.NewCipherFromHex(integrationTestKeyHex)
	if err != nil {
		t.Fatalf("NewCipherFromHex: %v", err)
	}
	seedIntegrationAppSettings(t, sqlDB)
	s := store.New(sqlDB, cipher)

	ticketIntegration, err := s.Integrations().Create(ctx, store.CreateIntegrationInput{
		Kind:    "kaneo",
		Name:    "Tickets",
		BaseURL: integrationStrPtr(server.URL),
		Secret:  []byte(`{"api_key":"k"}`),
	})
	if err != nil {
		t.Fatalf("Create ticket integration: %v", err)
	}
	sourceIntegration, err := s.Integrations().Create(ctx, store.CreateIntegrationInput{
		Kind:    "gitea",
		Name:    "Source",
		BaseURL: integrationStrPtr(server.URL),
		Secret:  []byte(`{"token":""}`),
	})
	if err != nil {
		t.Fatalf("Create source integration: %v", err)
	}
	project, err := s.TicketProjects().Create(ctx, store.CreateTicketProjectInput{
		IntegrationID:      ticketIntegration.ID,
		ExternalProjectID:  "proj-1",
		Name:               "Project",
		CreateConfig:       `{}`,
		StatusMapping:      validStatusMapping,
		OnOpenTicketPolicy: ticket.PolicySupersede,
	})
	if err != nil {
		t.Fatalf("Create ticket project: %v", err)
	}
	repo, err := s.Repos().Create(ctx, store.CreateMonitoredRepoInput{
		SourceKind:          "gitea",
		ProjectPath:         "org/app",
		Enabled:             true,
		TicketProjectID:     project.ID,
		SourceIntegrationID: &sourceIntegration.ID,
	})
	if err != nil {
		t.Fatalf("Create repo: %v", err)
	}
	oldTag := "v1.0.0"
	if _, err := s.Poll().UpdatePollState(ctx, repo.ID, store.PollStateUpdate{LastKnownTag: &oldTag}); err != nil {
		t.Fatalf("seed last_known_tag: %v", err)
	}

	rec := &sendRecorder{}
	targets := &notifyMockRepo{
		enabled: []store.NotificationTarget{{ID: "t1", Name: "ops", Events: []string{poll.EventError}, Enabled: true}},
		urls:    map[string]string{"t1": "generic://example.invalid"},
	}
	scheduler, err := poll.NewScheduler(poll.SchedulerConfig{
		Engine:         poll.NewEngine(failAfterSavePollRepo{PollRepository: s.Poll(), saveErr: errors.New("list notification targets: database is locked")}),
		Repos:          s.Repos(),
		TicketProjects: s.TicketProjects(),
		Integrations:   s.Integrations(),
		Poll:           s.Poll(),
		Notifier:       poll.NewNotifier(targets, rec.Send),
	})
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}

	run, err := s.Poll().InsertRun(ctx, store.PollTriggerSourceManual)
	if err != nil {
		t.Fatalf("InsertRun: %v", err)
	}
	if err := scheduler.RunAll(ctx, run.ID); err != nil {
		t.Fatalf("RunAll: %v", err)
	}

	if got := creates.Load(); got != 1 {
		t.Fatalf("ticket creates = %d, want 1", got)
	}
	gotRun, err := s.Poll().GetRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if gotRun.Status != poll.RunStatusFailed || gotRun.ReposChecked != 1 {
		t.Fatalf("run status/repos_checked = %q/%d, want failed/1", gotRun.Status, gotRun.ReposChecked)
	}

	updated, err := s.Repos().Get(ctx, repo.ID)
	if err != nil {
		t.Fatalf("Get repo: %v", err)
	}
	if updated.LastKnownTag == nil || *updated.LastKnownTag != "v1.1.0" {
		t.Fatalf("last_known_tag = %v, want v1.1.0", updated.LastKnownTag)
	}
	if updated.OpenTicketExternalID == nil || *updated.OpenTicketExternalID != "task-1" {
		t.Fatalf("open_ticket_external_id = %v, want task-1", updated.OpenTicketExternalID)
	}
	if updated.OpenTicketTag == nil || *updated.OpenTicketTag != "v1.1.0" {
		t.Fatalf("open_ticket_tag = %v, want v1.1.0", updated.OpenTicketTag)
	}
}
