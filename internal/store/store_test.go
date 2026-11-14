package store_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mdg-labs/release-ops/internal/crypto"
	"github.com/mdg-labs/release-ops/internal/store"
	"github.com/mdg-labs/release-ops/internal/store/db"
	"github.com/mdg-labs/release-ops/internal/store/storetest"
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

func openMigratedDB(t *testing.T, dbPath string) *store.DB {
	t.Helper()

	storetest.Migrate(t, dbPath)

	sqlDB, err := store.OpenPath(dbPath)
	if err != nil {
		t.Fatalf("OpenPath: %v", err)
	}
	return sqlDB
}

func seedAppSettings(t *testing.T, sqlDB *store.DB) {
	t.Helper()

	_, err := sqlDB.Writer.Exec(
		`INSERT INTO app_settings (id, poll_interval_minutes, updated_at) VALUES (1, 360, ?)`,
		"2026-08-06T12:00:00.000Z",
	)
	if err != nil {
		t.Fatalf("seed app_settings: %v", err)
	}
}

func TestEnsureDefaultAppSettings(t *testing.T) {
	t.Parallel()

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

	s := store.New(sqlDB, cipher)
	ctx := context.Background()

	if err := s.Settings().EnsureDefault(ctx); err != nil {
		t.Fatalf("EnsureDefault: %v", err)
	}

	got, err := s.Settings().Get(ctx)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != 1 || got.PollIntervalMinutes != 360 {
		t.Fatalf("settings = %+v, want id=1 poll_interval_minutes=360", got)
	}

	if err := s.Settings().EnsureDefault(ctx); err != nil {
		t.Fatalf("EnsureDefault second call: %v", err)
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

	secret := []byte(`{"api_key":"lin_test_secret"}`)
	created, err := s.Integrations().Create(ctx, store.CreateIntegrationInput{
		Kind:   "linear",
		Name:   "Linear",
		Secret: secret,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !created.HasSecret {
		t.Fatal("HasSecret = false, want true")
	}

	row, err := db.New(s.DB().Reader).GetIntegration(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetIntegration raw: %v", err)
	}
	if row.EncryptedPayload == string(secret) {
		t.Fatal("encrypted_payload stored as plaintext")
	}
	if strings.Contains(row.EncryptedPayload, "lin_test_secret") {
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

	secret := []byte(`{"api_key":"super-secret-value"}`)
	created, err := s.Integrations().Create(ctx, store.CreateIntegrationInput{
		Kind:   "linear",
		Name:   "Linear",
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

func TestRepositoryInterfacesMockable(t *testing.T) {
	t.Parallel()

	s, _ := testStore(t)

	var _ = s.Settings()
	var _ = s.Integrations()
	var _ = s.TicketProjects()
}

type ticketFixture struct {
	integrationID   string
	ticketProjectID string
}

func seedTicketFixture(t *testing.T, s *store.Store, ctx context.Context) ticketFixture {
	t.Helper()

	integration, err := s.Integrations().Create(ctx, store.CreateIntegrationInput{
		Kind:    "kaneo",
		Name:    "Kaneo",
		BaseURL: strPtr("https://api.kaneo.example"),
		Secret:  []byte(`{"api_key":"test"}`),
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

	return ticketFixture{integrationID: integration.ID, ticketProjectID: ticketProject.ID}
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
