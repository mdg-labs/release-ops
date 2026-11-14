package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/mdg-labs/release-ops/internal/store/db"
)

// ErrGitHubAppExists is returned when the instance already has its GitHub App.
var ErrGitHubAppExists = errors.New("github app is already configured")

// ErrGitHubAppNotFound is returned when the instance has no GitHub App yet.
var ErrGitHubAppNotFound = errors.New("github app is not configured")

// NewGitHubApp is the GitHub App an instance stores at the end of the manifest flow. The
// three secrets are plaintext here and encrypted before they reach the database.
type NewGitHubApp struct {
	AppID         int64
	Slug          string
	ClientID      string
	HTMLURL       string
	OwnerLogin    string
	OwnerType     string
	WebhookActive bool
	PrivateKey    string
	ClientSecret  string
	WebhookSecret string
}

// GitHubAppRepository stores the instance's singleton GitHub App.
type GitHubAppRepository interface {
	// Create stores the App with its secrets encrypted and invalidates the setup token in
	// the same transaction. It returns ErrGitHubAppExists, changing nothing, once an App
	// is stored.
	Create(ctx context.Context, app NewGitHubApp) error
	// SigningKey returns the App id and the decrypted private key (PEM), which signs the
	// App's JWTs. The caller keeps the key in memory only, for as long as it signs, and
	// clears it afterwards. It returns ErrGitHubAppNotFound before an App is stored.
	SigningKey(ctx context.Context) (appID int64, privateKeyPEM []byte, err error)
	// WebhookSecret returns the decrypted webhook secret that signs the App's deliveries.
	// The caller uses it only to verify a signature and never logs or stores it. It returns
	// ErrGitHubAppNotFound before an App is stored.
	WebhookSecret(ctx context.Context) ([]byte, error)
}

// GitHubApp returns the GitHub App repository.
func (s *Store) GitHubApp() GitHubAppRepository {
	return githubAppRepo{store: s}
}

type githubAppRepo struct {
	store *Store
}

func (r githubAppRepo) Create(ctx context.Context, app NewGitHubApp) error {
	privateKey, err := r.store.cipher.Encrypt([]byte(app.PrivateKey))
	if err != nil {
		return fmt.Errorf("encrypt private key: %w", err)
	}
	clientSecret, err := r.store.cipher.Encrypt([]byte(app.ClientSecret))
	if err != nil {
		return fmt.Errorf("encrypt client secret: %w", err)
	}
	webhookSecret, err := r.store.cipher.Encrypt([]byte(app.WebhookSecret))
	if err != nil {
		return fmt.Errorf("encrypt webhook secret: %w", err)
	}

	var webhookActive int64
	if app.WebhookActive {
		webhookActive = 1
	}

	tx, err := r.store.db.Writer.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin github app: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	q := r.store.writes.WithTx(tx)

	now := nowUTC()
	inserted, err := q.CreateGitHubApp(ctx, db.CreateGitHubAppParams{
		AppID:                  app.AppID,
		Slug:                   app.Slug,
		ClientID:               app.ClientID,
		HtmlUrl:                app.HTMLURL,
		OwnerLogin:             app.OwnerLogin,
		OwnerType:              app.OwnerType,
		WebhookActive:          webhookActive,
		EncryptedPrivateKey:    privateKey,
		EncryptedClientSecret:  clientSecret,
		EncryptedWebhookSecret: webhookSecret,
		KeyID:                  r.store.cipher.KeyID(),
		CreatedAt:              now,
		UpdatedAt:              now,
	})
	if err != nil {
		return fmt.Errorf("store github app: %w", err)
	}
	if inserted == 0 {
		return ErrGitHubAppExists
	}
	if err := q.DeleteSetupTokens(ctx); err != nil {
		return fmt.Errorf("invalidate setup token: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit github app: %w", err)
	}
	return nil
}

func (r githubAppRepo) SigningKey(ctx context.Context) (int64, []byte, error) {
	row, err := r.store.reads.GetGitHubAppSigningKey(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil, ErrGitHubAppNotFound
	}
	if err != nil {
		return 0, nil, fmt.Errorf("read github app key: %w", err)
	}
	pem, err := r.store.cipher.Decrypt(row.EncryptedPrivateKey)
	if err != nil {
		return 0, nil, errors.New("decrypt github app private key: the stored key does not match APP_ENCRYPTION_KEY")
	}
	return row.AppID, pem, nil
}

func (r githubAppRepo) WebhookSecret(ctx context.Context) ([]byte, error) {
	encrypted, err := r.store.reads.GetGitHubAppWebhookSecret(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrGitHubAppNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read github app webhook secret: %w", err)
	}
	secret, err := r.store.cipher.Decrypt(encrypted)
	if err != nil {
		return nil, errors.New("decrypt github app webhook secret: the stored secret does not match APP_ENCRYPTION_KEY")
	}
	return secret, nil
}
