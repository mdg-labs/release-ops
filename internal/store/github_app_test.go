package store_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/mdg-labs/release-ops/internal/crypto"
	"github.com/mdg-labs/release-ops/internal/store"
	"github.com/mdg-labs/release-ops/internal/store/storetest"
)

const appStoreKeyHex = "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"

func newGitHubAppStore(t *testing.T) (*store.Store, *store.DB, *crypto.Cipher) {
	t.Helper()
	db := storetest.OpenMigrated(t)
	cipher, err := crypto.NewCipherFromHex(appStoreKeyHex)
	if err != nil {
		t.Fatalf("NewCipherFromHex: %v", err)
	}
	return store.New(db, cipher), db, cipher
}

func sampleApp() store.NewGitHubApp {
	return store.NewGitHubApp{
		AppID: 1234567, Slug: "release-ops-acme", ClientID: "fixture-client-id",
		HTMLURL: "https://github.com/apps/release-ops-acme", OwnerLogin: "acme", OwnerType: "Organization",
		WebhookActive: true,
		PrivateKey:    "-----BEGIN RSA PRIVATE KEY-----\nfixture-private-key-body\n-----END RSA PRIVATE KEY-----\n",
		ClientSecret:  "fixture-client-secret-value",
		WebhookSecret: "fixture-webhook-secret-value",
	}
}

func seedSetupToken(t *testing.T, db *store.DB) {
	t.Helper()
	if _, err := db.Writer.Exec(`INSERT INTO auth_tokens (id, kind, token_hash, expires_at, created_at)
		VALUES ('t1', 'setup', 'hash', '9999-12-31T23:59:59.000Z', '2026-11-14T12:00:00.000Z')`); err != nil {
		t.Fatalf("seed setup token: %v", err)
	}
}

func countRows(t *testing.T, db *store.DB, table string) int {
	t.Helper()
	var n int
	if err := db.Writer.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

type appRow struct {
	appID                                   int64
	slug, clientID, ownerLogin, ownerType   string
	webhookActive                           int64
	privateKey, clientSecret, webhookSecret string
	keyID                                   string
}

func readApp(t *testing.T, db *store.DB) appRow {
	t.Helper()
	var r appRow
	err := db.Writer.QueryRow(`SELECT app_id, slug, client_id, owner_login, owner_type, webhook_active,
		encrypted_private_key, encrypted_client_secret, encrypted_webhook_secret, key_id FROM github_app WHERE id = 1`).
		Scan(&r.appID, &r.slug, &r.clientID, &r.ownerLogin, &r.ownerType, &r.webhookActive,
			&r.privateKey, &r.clientSecret, &r.webhookSecret, &r.keyID)
	if err != nil {
		t.Fatalf("read github_app: %v", err)
	}
	return r
}

func TestGitHubAppCreateEncryptsSecretsAtRest(t *testing.T) {
	t.Parallel()

	s, db, cipher := newGitHubAppStore(t)
	seedSetupToken(t, db)
	app := sampleApp()
	if err := s.GitHubApp().Create(context.Background(), app); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got := readApp(t, db)
	if got.appID != app.AppID || got.slug != app.Slug || got.clientID != app.ClientID ||
		got.ownerLogin != "acme" || got.ownerType != "Organization" || got.webhookActive != 1 {
		t.Fatalf("plain columns = %+v", got)
	}
	if got.keyID != cipher.KeyID() {
		t.Errorf("key_id = %q, want %q", got.keyID, cipher.KeyID())
	}
	for column, pair := range map[string][2]string{
		"encrypted_private_key":    {got.privateKey, app.PrivateKey},
		"encrypted_client_secret":  {got.clientSecret, app.ClientSecret},
		"encrypted_webhook_secret": {got.webhookSecret, app.WebhookSecret},
	} {
		stored, plain := pair[0], pair[1]
		if stored == "" || strings.Contains(stored, plain) || strings.Contains(stored, "fixture") {
			t.Errorf("%s holds plaintext or nothing: %q", column, stored)
		}
		decrypted, err := cipher.Decrypt(stored)
		if err != nil || string(decrypted) != plain {
			t.Errorf("%s does not decrypt to the secret: %q, %v", column, decrypted, err)
		}
	}
	if n := countRows(t, db, "auth_tokens WHERE kind = 'setup'"); n != 0 {
		t.Errorf("%d setup tokens remain after the App is stored", n)
	}
}

// Once an App is stored a second Create is refused and changes nothing: not the App, and not
// the setup token, which is only invalidated by the Create that stores the App.
func TestGitHubAppCreateTwiceChangesNothing(t *testing.T) {
	t.Parallel()

	s, db, _ := newGitHubAppStore(t)
	ctx := context.Background()
	if err := s.GitHubApp().Create(ctx, sampleApp()); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	before := readApp(t, db)
	seedSetupToken(t, db)

	second := sampleApp()
	second.AppID, second.Slug, second.ClientSecret = 999, "other", "another-client-secret"
	if err := s.GitHubApp().Create(ctx, second); !errors.Is(err, store.ErrGitHubAppExists) {
		t.Fatalf("second Create err = %v, want ErrGitHubAppExists", err)
	}
	if after := readApp(t, db); after != before {
		t.Errorf("a refused Create changed the stored App: %+v -> %+v", before, after)
	}
	if n := countRows(t, db, "auth_tokens WHERE kind = 'setup'"); n != 1 {
		t.Errorf("a refused Create touched the setup token: %d remain, want 1", n)
	}
}

// A failure after the insert leaves no App row: the setup token delete is part of the same
// transaction.
func TestGitHubAppCreateRollsBackWhenTheTokenCannotBeInvalidated(t *testing.T) {
	t.Parallel()

	s, db, _ := newGitHubAppStore(t)
	if _, err := db.Writer.Exec(`CREATE TRIGGER block_setup_token_delete BEFORE DELETE ON auth_tokens
		BEGIN SELECT RAISE(ABORT, 'blocked'); END`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	seedSetupToken(t, db)

	if err := s.GitHubApp().Create(context.Background(), sampleApp()); err == nil {
		t.Fatal("Create succeeded although the setup token could not be deleted")
	}
	var n int
	if err := db.Writer.QueryRow(`SELECT COUNT(*) FROM github_app`).Scan(&n); err != nil && !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("a failed Create left %d github_app rows", n)
	}
}

func TestGitHubAppReplacePrivateKeyEncryptsAndRewritesTheKeyID(t *testing.T) {
	t.Parallel()

	s, db, cipher := newGitHubAppStore(t)
	seedSetupToken(t, db)
	app := sampleApp()
	if err := s.GitHubApp().Create(context.Background(), app); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := db.Writer.Exec(`UPDATE github_app SET key_id = 'stale'`); err != nil {
		t.Fatalf("set stale key id: %v", err)
	}
	before := readApp(t, db)
	version := s.GitHubApp().KeyVersion()

	newPEM := "-----BEGIN RSA PRIVATE KEY-----\nreplacement-key-body\n-----END RSA PRIVATE KEY-----\n"
	if err := s.GitHubApp().ReplacePrivateKey(context.Background(), app.AppID, []byte(newPEM)); err != nil {
		t.Fatalf("ReplacePrivateKey: %v", err)
	}

	row := readApp(t, db)
	if strings.Contains(row.privateKey, "replacement-key-body") || strings.Contains(row.privateKey, "PRIVATE KEY") {
		t.Error("the replacement key is stored in plaintext")
	}
	if plain, err := cipher.Decrypt(row.privateKey); err != nil || string(plain) != newPEM {
		t.Errorf("stored key decrypts to %q, %v; want the replacement", plain, err)
	}
	if row.keyID != cipher.KeyID() {
		t.Errorf("key_id = %q, want %q", row.keyID, cipher.KeyID())
	}
	if row.clientSecret != before.clientSecret || row.webhookSecret != before.webhookSecret {
		t.Error("replacing the private key touched the other secrets")
	}
	if _, pem, err := s.GitHubApp().SigningKey(context.Background()); err != nil || string(pem) != newPEM {
		t.Errorf("SigningKey = %q, %v; want the replacement", pem, err)
	}
	if got := s.GitHubApp().KeyVersion(); got == version {
		t.Error("KeyVersion did not change")
	}
}

func TestGitHubAppReplacePrivateKeyRefusesAnotherAppOrNone(t *testing.T) {
	t.Parallel()

	s, db, _ := newGitHubAppStore(t)
	err := s.GitHubApp().ReplacePrivateKey(context.Background(), 1, []byte("pem"))
	if !errors.Is(err, store.ErrGitHubAppNotFound) {
		t.Fatalf("with no App stored: %v, want ErrGitHubAppNotFound", err)
	}

	seedSetupToken(t, db)
	app := sampleApp()
	if err := s.GitHubApp().Create(context.Background(), app); err != nil {
		t.Fatalf("Create: %v", err)
	}
	before := readApp(t, db)
	version := s.GitHubApp().KeyVersion()

	err = s.GitHubApp().ReplacePrivateKey(context.Background(), app.AppID+1, []byte("pem"))
	if !errors.Is(err, store.ErrGitHubAppNotFound) {
		t.Fatalf("for another App id: %v, want ErrGitHubAppNotFound", err)
	}
	if after := readApp(t, db); after != before {
		t.Error("a refused replacement changed the row")
	}
	if s.GitHubApp().KeyVersion() != version {
		t.Error("a refused replacement changed KeyVersion")
	}
}
