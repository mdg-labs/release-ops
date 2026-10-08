package store_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/mdg-labs/release-ops/internal/store"
)

func TestUpdatePollStateStoresAndClearsPendingTicketTag(t *testing.T) {
	t.Parallel()

	s, _ := testStore(t)
	ctx := context.Background()
	fixture := seedRepoFixture(t, s, ctx)

	pending := "v1.1.0"
	got, err := s.Poll().UpdatePollState(ctx, fixture.repoID, store.PollStateUpdate{PendingTicketTag: &pending})
	if err != nil {
		t.Fatalf("UpdatePollState (set marker): %v", err)
	}
	if got.PendingTicketTag == nil || *got.PendingTicketTag != pending {
		t.Fatalf("returned PendingTicketTag = %v, want %q", got.PendingTicketTag, pending)
	}
	read, err := s.Repos().Get(ctx, fixture.repoID)
	if err != nil {
		t.Fatalf("Get repo: %v", err)
	}
	if read.PendingTicketTag == nil || *read.PendingTicketTag != pending {
		t.Fatalf("stored PendingTicketTag = %v, want %q", read.PendingTicketTag, pending)
	}
	enabled, err := s.Repos().ListEnabled(ctx)
	if err != nil {
		t.Fatalf("ListEnabled: %v", err)
	}
	if len(enabled) != 1 || enabled[0].PendingTicketTag == nil || *enabled[0].PendingTicketTag != pending {
		t.Fatalf("ListEnabled PendingTicketTag = %+v, want %q", enabled, pending)
	}

	got, err = s.Poll().UpdatePollState(ctx, fixture.repoID, store.PollStateUpdate{})
	if err != nil {
		t.Fatalf("UpdatePollState (clear marker): %v", err)
	}
	if got.PendingTicketTag != nil {
		t.Fatalf("PendingTicketTag = %q after a write without it, want cleared", *got.PendingTicketTag)
	}
}

func TestRepoUpdateClearsPendingTicketTagOnTargetChange(t *testing.T) {
	t.Parallel()

	s, _ := testStore(t)
	ctx := context.Background()
	fixture := seedRepoFixture(t, s, ctx)

	otherProject, err := s.TicketProjects().Create(ctx, store.CreateTicketProjectInput{
		IntegrationID:     fixture.integrationID,
		ExternalProjectID: "proj-2",
		Name:              "Other",
		CreateConfig:      `{"status":"ready"}`,
		StatusMapping:     `{"open":["To Do"],"done":["Done"],"cancelled":["Cancelled"],"superseded":"Cancelled"}`,
	})
	if err != nil {
		t.Fatalf("Create ticket project: %v", err)
	}

	setMarker := func() {
		t.Helper()
		pending := "v1.1.0"
		if _, err := s.Poll().UpdatePollState(ctx, fixture.repoID, store.PollStateUpdate{PendingTicketTag: &pending}); err != nil {
			t.Fatalf("UpdatePollState: %v", err)
		}
	}
	update := func(projectPath, ticketProjectID string, enabled bool) *store.MonitoredRepo {
		t.Helper()
		updated, err := s.Repos().Update(ctx, fixture.repoID, store.UpdateMonitoredRepoInput{
			SourceKind:      "github",
			ProjectPath:     projectPath,
			Enabled:         enabled,
			TicketProjectID: ticketProjectID,
		})
		if err != nil {
			t.Fatalf("Update repo: %v", err)
		}
		return updated
	}

	setMarker()
	if got := update("org/repo", fixture.ticketProjectID, false); got.PendingTicketTag == nil {
		t.Fatal("PendingTicketTag cleared by an update that changes no target")
	}
	if got := update("org/other", fixture.ticketProjectID, true); got.PendingTicketTag != nil {
		t.Fatalf("PendingTicketTag = %q after a source change, want cleared", *got.PendingTicketTag)
	}

	setMarker()
	if got := update("org/other", otherProject.ID, true); got.PendingTicketTag != nil {
		t.Fatalf("PendingTicketTag = %q after a ticket project change, want cleared", *got.PendingTicketTag)
	}
	read, err := s.Repos().Get(ctx, fixture.repoID)
	if err != nil {
		t.Fatalf("Get repo: %v", err)
	}
	if read.PendingTicketTag != nil {
		t.Fatalf("stored PendingTicketTag = %q, want cleared", *read.PendingTicketTag)
	}
}

func TestOpenPathAppliesBusyTimeout(t *testing.T) {
	t.Parallel()

	sqlDB, err := store.OpenPath(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("OpenPath: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	var millis int
	if err := sqlDB.QueryRow(`PRAGMA busy_timeout`).Scan(&millis); err != nil {
		t.Fatalf("PRAGMA busy_timeout: %v", err)
	}
	if millis != 5000 {
		t.Fatalf("busy_timeout = %d ms, want 5000", millis)
	}

	var foreignKeys int
	if err := sqlDB.QueryRow(`PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
		t.Fatalf("PRAGMA foreign_keys: %v", err)
	}
	if foreignKeys != 1 {
		t.Fatalf("foreign_keys = %d, want 1", foreignKeys)
	}
}
