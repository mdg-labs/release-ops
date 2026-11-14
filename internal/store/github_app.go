package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/mdg-labs/release-ops/internal/store/db"
)

// ErrGitHubAppExists is returned when the instance already has its GitHub App.
var ErrGitHubAppExists = errors.New("github app is already configured")

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
