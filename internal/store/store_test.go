package store_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/mdg-labs/release-ops/internal/crypto"
	"github.com/mdg-labs/release-ops/internal/store"
	"github.com/mdg-labs/release-ops/internal/store/db"
)

const testKeyHex = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func testStore(t *testing.T) (*store.Store, *crypto.Cipher) {
	t.Helper()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	sqlDB := openMigratedDB(t, dbPath)
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})

	cipher, err := crypto.NewCipherFromHex(testKeyHex)
	if err != nil {
		t.Fatalf("NewCipherFromHex: %v", err)
	}

	seedAppSettings(t, sqlDB)
	return store.New(sqlDB, cipher), cipher
}

func openMigratedDB(t *testing.T, dbPath string) *sql.DB {
	t.Helper()

	migrationsURL := migrationSourceURL(t)
	m, err := newMigrator(t, dbPath, migrationsURL)
	if err != nil {
		t.Fatalf("newMigrator: %v", err)
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

func seedAppSettings(t *testing.T, sqlDB *sql.DB) {
	t.Helper()

	_, err := sqlDB.Exec(
		`INSERT INTO app_settings (id, poll_interval_minutes, updated_at) VALUES (1, 360, ?)`,
		"2026-08-06T12:00:00.000Z",
	)
	if err != nil {
		t.Fatalf("seed app_settings: %v", err)
	}
}

func TestSettingsGetReturnsIDOne(t *testing.T) {
	t.Parallel()

	s, _ := testStore(t)
	ctx := context.Background()

	got, err := s.Settings().Get(ctx)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != 1 {
		t.Fatalf("ID = %d, want 1", got.ID)
	}
	if got.PollIntervalMinutes < 5 {
		t.Fatalf("PollIntervalMinutes = %d, want >= 5", got.PollIntervalMinutes)
	}
}

func TestIntegrationCreateEncryptsPayload(t *testing.T) {
	t.Parallel()

	s, cipher := testStore(t)
	ctx := context.Background()

	secret := []byte(`{"token":"ghp_test_secret"}`)
	created, err := s.Integrations().Create(ctx, store.CreateIntegrationInput{
		Kind:   "github",
		Name:   "GitHub PAT",
		Secret: secret,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !created.HasSecret {
		t.Fatal("HasSecret = false, want true")
	}

	row, err := s.Queries().GetIntegration(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetIntegration raw: %v", err)
	}
	if row.EncryptedPayload == string(secret) {
		t.Fatal("encrypted_payload stored as plaintext")
	}
	if strings.Contains(row.EncryptedPayload, "ghp_test_secret") {
		t.Fatal("encrypted_payload contains plaintext secret")
	}

	decrypted, err := cipher.Decrypt(row.EncryptedPayload)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if string(decrypted) != string(secret) {
		t.Fatalf("decrypted = %q, want %q", decrypted, secret)
	}
}

func TestIntegrationGetNeverReturnsDecryptedSecret(t *testing.T) {
	t.Parallel()

	s, _ := testStore(t)
	ctx := context.Background()

	secret := []byte(`{"token":"super-secret-value"}`)
	created, err := s.Integrations().Create(ctx, store.CreateIntegrationInput{
		Kind:   "github",
		Name:   "GitHub",
		Secret: secret,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := s.Integrations().Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	b, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	body := string(b)
	if strings.Contains(body, "super-secret-value") {
		t.Fatalf("Get JSON leaks secret: %s", body)
	}
	if strings.Contains(body, "encrypted") || strings.Contains(body, "Encrypted") {
		t.Fatalf("Get JSON exposes encrypted field: %s", body)
	}
	if !got.HasSecret {
		t.Fatal("HasSecret = false, want true")
	}

	plaintext, err := s.Integrations().DecryptPayload(ctx, created.ID)
	if err != nil {
		t.Fatalf("DecryptPayload: %v", err)
	}
	if string(plaintext) != string(secret) {
		t.Fatalf("DecryptPayload = %q, want %q", plaintext, secret)
	}
}

func TestUpdatePollStateOnMonitoredRepo(t *testing.T) {
	t.Parallel()

	s, _ := testStore(t)
	ctx := context.Background()

	fixture := seedRepoFixture(t, s, ctx)

	tag := "v1.2.3"
	polledAt := "2026-08-06T12:00:00.000Z"
	ticketID := "task-abc"
	openTag := "v1.2.2"

	updated, err := s.Poll().UpdatePollState(ctx, fixture.repoID, store.PollStateUpdate{
		LastKnownTag:         &tag,
		LastPolledAt:         &polledAt,
		OpenTicketExternalID: &ticketID,
		OpenTicketTag:        &openTag,
		LastError:            nil,
	})
	if err != nil {
		t.Fatalf("UpdatePollState: %v", err)
	}
	if updated.LastKnownTag == nil || *updated.LastKnownTag != tag {
		t.Fatalf("LastKnownTag = %v, want %q", updated.LastKnownTag, tag)
	}
	if updated.OpenTicketExternalID == nil || *updated.OpenTicketExternalID != ticketID {
		t.Fatalf("OpenTicketExternalID = %v, want %q", updated.OpenTicketExternalID, ticketID)
	}
}

func TestRepoCreateUpdatePersistsNotificationTargetIDs(t *testing.T) {
	t.Parallel()

	s, _ := testStore(t)
	ctx := context.Background()

	fixture := seedRepoFixture(t, s, ctx)

	targetA, err := s.Notifications().Create(ctx, store.CreateNotificationTargetInput{
		Name:        "Slack",
		ShoutrrrURL: "slack://token@channel",
	})
	if err != nil {
		t.Fatalf("Create target A: %v", err)
	}
	targetB, err := s.Notifications().Create(ctx, store.CreateNotificationTargetInput{
		Name:        "Ntfy",
		ShoutrrrURL: "ntfy://topic",
	})
	if err != nil {
		t.Fatalf("Create target B: %v", err)
	}

	created, err := s.Repos().Create(ctx, store.CreateMonitoredRepoInput{
		SourceKind:            "github",
		ProjectPath:           "org/with-notifications",
		Enabled:               true,
		TicketProjectID:       fixture.ticketProjectID,
		NotificationTargetIDs: []string{targetA.ID, targetB.ID},
	})
	if err != nil {
		t.Fatalf("Create repo: %v", err)
	}
	if len(created.NotificationTargetIDs) != 2 {
		t.Fatalf("NotificationTargetIDs = %v, want 2 entries", created.NotificationTargetIDs)
	}

	got, err := s.Repos().Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get repo: %v", err)
	}
	if len(got.NotificationTargetIDs) != 2 {
		t.Fatalf("persisted NotificationTargetIDs = %v, want 2", got.NotificationTargetIDs)
	}

	updated, err := s.Repos().Update(ctx, created.ID, store.UpdateMonitoredRepoInput{
		SourceKind:            "github",
		ProjectPath:           "org/with-notifications",
		Enabled:               true,
		TicketProjectID:       fixture.ticketProjectID,
		NotificationTargetIDs: []string{targetB.ID},
	})
	if err != nil {
		t.Fatalf("Update repo: %v", err)
	}
	if len(updated.NotificationTargetIDs) != 1 || updated.NotificationTargetIDs[0] != targetB.ID {
		t.Fatalf("updated NotificationTargetIDs = %v, want [%s]", updated.NotificationTargetIDs, targetB.ID)
	}

	ids, err := s.Queries().ListNotificationTargetIDsForRepo(ctx, created.ID)
	if err != nil {
		t.Fatalf("ListNotificationTargetIDsForRepo: %v", err)
	}
	if len(ids) != 1 || ids[0] != targetB.ID {
		t.Fatalf("join rows = %v, want [%s]", ids, targetB.ID)
	}
}

func TestNotificationTargetEventsJSONValid(t *testing.T) {
	t.Parallel()

	s, _ := testStore(t)
	ctx := context.Background()

	target, err := s.Notifications().Create(ctx, store.CreateNotificationTargetInput{
		Name:        "Default events",
		ShoutrrrURL: "generic://default",
	})
	if err != nil {
		t.Fatalf("Create with default events: %v", err)
	}
	if len(target.Events) != 3 {
		t.Fatalf("default Events = %v, want 3 entries", target.Events)
	}

	target, err = s.Notifications().Create(ctx, store.CreateNotificationTargetInput{
		Name:        "Valid events",
		ShoutrrrURL: "generic://example",
		Events:      []string{"create", "error"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(target.Events) != 2 {
		t.Fatalf("Events = %v, want 2 entries", target.Events)
	}

	row, err := s.Queries().GetNotificationTarget(ctx, target.ID)
	if err != nil {
		t.Fatalf("GetNotificationTarget: %v", err)
	}
	if !json.Valid([]byte(row.EventsJson)) {
		t.Fatalf("events_json is not valid JSON: %q", row.EventsJson)
	}
}

func TestRepositoryInterfacesMockable(t *testing.T) {
	t.Parallel()

	s, _ := testStore(t)

	var _ store.SettingsRepository = s.Settings()
	var _ store.IntegrationRepository = s.Integrations()
	var _ store.TicketProjectRepository = s.TicketProjects()
	var _ store.MonitoredRepoRepository = s.Repos()
	var _ store.NotificationTargetRepository = s.Notifications()
	var _ store.PollRepository = s.Poll()
}

type repoFixture struct {
	integrationID   string
	ticketProjectID string
	repoID          string
}

func seedRepoFixture(t *testing.T, s *store.Store, ctx context.Context) repoFixture {
	t.Helper()

	integration, err := s.Integrations().Create(ctx, store.CreateIntegrationInput{
		Kind:   "phasical",
		Name:   "Phasical",
		BaseURL: strPtr("https://api.phasical.example"),
		Secret: []byte(`{"api_key":"test"}`),
	})
	if err != nil {
		t.Fatalf("Create integration: %v", err)
	}

	ticketProject, err := s.TicketProjects().Create(ctx, store.CreateTicketProjectInput{
		IntegrationID:     integration.ID,
		ExternalProjectID: "proj-1",
		Name:              "Release Ops",
		CreateConfig:      `{"status":"ready"}`,
		StatusMapping:     `{"open":["To Do"],"done":["Done"],"cancelled":["Cancelled"],"superseded":"Cancelled"}`,
	})
	if err != nil {
		t.Fatalf("Create ticket project: %v", err)
	}

	repo, err := s.Repos().Create(ctx, store.CreateMonitoredRepoInput{
		SourceKind:      "github",
		ProjectPath:     "org/repo",
		Enabled:         true,
		TicketProjectID: ticketProject.ID,
	})
	if err != nil {
		t.Fatalf("Create repo: %v", err)
	}

	return repoFixture{
		integrationID:   integration.ID,
		ticketProjectID: ticketProject.ID,
		repoID:          repo.ID,
	}
}

func strPtr(s string) *string {
	return &s
}

// Ensure db package import is used for raw query assertions.
var _ = db.Integration{}

func TestMain(m *testing.M) {
	if os.Getenv("APP_DB_PATH") != "" {
		_ = os.Unsetenv("APP_DB_PATH")
	}
	os.Exit(m.Run())
}
