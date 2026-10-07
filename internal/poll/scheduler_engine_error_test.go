package poll_test

import (
	"context"
	"database/sql"
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
// carries a new ticket link: a state write whose acknowledgement is lost after the
// row is committed. The store never produces this by itself, so the error is not the
// store's own post-write marker and the engine must treat it as a failed step.
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

// dropTargetsBeforeLinkPollRepo drops the repo-to-target link table just before the
// first write that carries a new ticket link, so the real store's UPDATE commits and
// its follow-up read of the repo's notification targets fails.
type dropTargetsBeforeLinkPollRepo struct {
	store.PollRepository
	sqlDB *sql.DB
	t     *testing.T
}

func (r dropTargetsBeforeLinkPollRepo) UpdatePollState(ctx context.Context, repoID string, update store.PollStateUpdate) (*store.MonitoredRepo, error) {
	if update.OpenTicketExternalID != nil {
		if _, err := r.sqlDB.ExecContext(ctx, `DROP TABLE IF EXISTS monitored_repo_notifications`); err != nil {
			r.t.Fatalf("drop monitored_repo_notifications: %v", err)
		}
	}
	return r.PollRepository.UpdatePollState(ctx, repoID, update)
}

// stateWriteFixture is a gitea repo that moved from v1.0.0 to v1.1.0, a kaneo ticket
// project, and two enabled notification targets of which the repo selects only one.
type stateWriteFixture struct {
	sqlDB   *sql.DB
	store   *store.Store
	repo    *store.MonitoredRepo
	creates *atomic.Int32
	sends   *sendRecorder
}

const (
	selectedTargetURL   = "generic://selected.invalid"
	unselectedTargetURL = "generic://unselected.invalid"
)

func newStateWriteFixture(t *testing.T) *stateWriteFixture {
	t.Helper()
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
	selected, err := s.Notifications().Create(ctx, store.CreateNotificationTargetInput{
		Name: "selected", ShoutrrrURL: selectedTargetURL, Events: []string{poll.EventCreate, poll.EventError}, Enabled: true,
	})
	if err != nil {
		t.Fatalf("Create selected target: %v", err)
	}
	if _, err := s.Notifications().Create(ctx, store.CreateNotificationTargetInput{
		Name: "unselected", ShoutrrrURL: unselectedTargetURL, Events: []string{poll.EventCreate, poll.EventError}, Enabled: true,
	}); err != nil {
		t.Fatalf("Create unselected target: %v", err)
	}
	repo, err := s.Repos().Create(ctx, store.CreateMonitoredRepoInput{
		SourceKind:            "gitea",
		ProjectPath:           "org/app",
		Enabled:               true,
		TicketProjectID:       project.ID,
		SourceIntegrationID:   &sourceIntegration.ID,
		NotificationTargetIDs: []string{selected.ID},
	})
	if err != nil {
		t.Fatalf("Create repo: %v", err)
	}
	oldTag := "v1.0.0"
	if _, err := s.Poll().UpdatePollState(ctx, repo.ID, store.PollStateUpdate{LastKnownTag: &oldTag}); err != nil {
		t.Fatalf("seed last_known_tag: %v", err)
	}
	return &stateWriteFixture{sqlDB: sqlDB, store: s, repo: repo, creates: &creates, sends: &sendRecorder{}}
}

// runAll polls once through the scheduler with pollRepo wrapped around the real store's
// poll repository, and returns the run row.
func (f *stateWriteFixture) runAll(t *testing.T, pollRepo store.PollRepository) *store.PollRun {
	t.Helper()
	ctx := context.Background()

	scheduler, err := poll.NewScheduler(poll.SchedulerConfig{
		Engine:         poll.NewEngine(pollRepo),
		Repos:          f.store.Repos(),
		TicketProjects: f.store.TicketProjects(),
		Integrations:   f.store.Integrations(),
		Poll:           f.store.Poll(),
		Notifier:       poll.NewNotifier(f.store.Notifications(), f.sends.Send),
	})
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}
	run, err := f.store.Poll().InsertRun(ctx, store.PollTriggerSourceManual)
	if err != nil {
		t.Fatalf("InsertRun: %v", err)
	}
	if err := scheduler.RunAll(ctx, run.ID); err != nil {
		t.Fatalf("RunAll: %v", err)
	}
	gotRun, err := f.store.Poll().GetRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	return gotRun
}

// savedTicketState reads the repo's stored ticket link and last_known_tag straight from
// the table, so it works even after the link table is gone.
func (f *stateWriteFixture) savedTicketState(t *testing.T) (ticketID, openTag, lastKnownTag, lastError sql.NullString) {
	t.Helper()
	err := f.sqlDB.QueryRow(
		`SELECT open_ticket_external_id, open_ticket_tag, last_known_tag, last_error FROM monitored_repos WHERE id = ?`,
		f.repo.ID,
	).Scan(&ticketID, &openTag, &lastKnownTag, &lastError)
	if err != nil {
		t.Fatalf("read repo state: %v", err)
	}
	return ticketID, openTag, lastKnownTag, lastError
}

// TestSchedulerKeepsStateSavedByEngineWhenEngineErrors drives the default per-repo path
// through a new-release ticket create whose state write commits and then returns an
// error the store does not mark as post-write. The error stays an engine error: the
// saved ticket link and tag are not rewritten, so the next poll does not create a
// second ticket.
func TestSchedulerKeepsStateSavedByEngineWhenEngineErrors(t *testing.T) {
	t.Parallel()
	f := newStateWriteFixture(t)

	gotRun := f.runAll(t, failAfterSavePollRepo{PollRepository: f.store.Poll(), saveErr: errors.New("write acknowledgement lost")})

	if got := f.creates.Load(); got != 1 {
		t.Fatalf("ticket creates = %d, want 1", got)
	}
	if gotRun.Status != poll.RunStatusFailed || gotRun.ReposChecked != 1 {
		t.Fatalf("run status/repos_checked = %q/%d, want failed/1", gotRun.Status, gotRun.ReposChecked)
	}

	ticketID, openTag, lastKnownTag, _ := f.savedTicketState(t)
	if lastKnownTag.String != "v1.1.0" || ticketID.String != "task-1" || openTag.String != "v1.1.0" {
		t.Fatalf("saved state = ticket %q, open tag %q, last known %q; want task-1, v1.1.0, v1.1.0",
			ticketID.String, openTag.String, lastKnownTag.String)
	}
}

// TestSchedulerRecordsCreateWhenTargetLookupFailsAfterStateWrite drives a new-release
// ticket create through the real store whose follow-up read of the repo's notification
// targets fails after the state write committed. The step is done: the ticket link is
// stored, the Create ticket event is logged, the run reports no error, and the create
// notification goes only to the repo's selected target.
func TestSchedulerRecordsCreateWhenTargetLookupFailsAfterStateWrite(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newStateWriteFixture(t)

	gotRun := f.runAll(t, dropTargetsBeforeLinkPollRepo{PollRepository: f.store.Poll(), sqlDB: f.sqlDB, t: t})

	if got := f.creates.Load(); got != 1 {
		t.Fatalf("ticket creates = %d, want 1", got)
	}
	if gotRun.Status != poll.RunStatusSuccess || gotRun.ErrorsJSON != "[]" {
		t.Fatalf("run status/errors = %q/%s, want success/[]", gotRun.Status, gotRun.ErrorsJSON)
	}
	if gotRun.TicketsCreated != 1 {
		t.Fatalf("tickets_created = %d, want 1", gotRun.TicketsCreated)
	}

	ticketID, openTag, lastKnownTag, lastError := f.savedTicketState(t)
	if lastKnownTag.String != "v1.1.0" || ticketID.String != "task-1" || openTag.String != "v1.1.0" {
		t.Fatalf("saved state = ticket %q, open tag %q, last known %q; want task-1, v1.1.0, v1.1.0",
			ticketID.String, openTag.String, lastKnownTag.String)
	}
	if lastError.Valid {
		t.Fatalf("last_error = %q, want none", lastError.String)
	}

	events, err := f.store.Poll().ListEventsByRunID(ctx, gotRun.ID)
	if err != nil {
		t.Fatalf("ListEventsByRunID: %v", err)
	}
	if len(events) != 1 || events[0].Action != poll.ActionCreate {
		t.Fatalf("events = %+v, want one create event", events)
	}
	if events[0].TicketExternalID == nil || *events[0].TicketExternalID != "task-1" {
		t.Fatalf("event ticket = %v, want task-1", events[0].TicketExternalID)
	}

	calls := f.sends.callsSnapshot()
	if len(calls) != 1 || calls[0].url != selectedTargetURL {
		t.Fatalf("notifications = %+v, want one create notification to the selected target only", calls)
	}
}
