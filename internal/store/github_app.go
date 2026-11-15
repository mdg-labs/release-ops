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
	// SignIn returns what the OAuth sign-in needs: the App's client id and owner, and the
	// decrypted client secret. The caller sends the secret to GitHub's token endpoint and
	// never logs or stores it. It returns ErrGitHubAppNotFound before an App is stored.
	SignIn(ctx context.Context) (GitHubAppSignIn, error)
	// BotIdentity returns the App's slug and, once it has been read from GitHub, the numeric id
	// of its bot user. It returns ErrGitHubAppNotFound before an App is stored.
	BotIdentity(ctx context.Context) (GitHubAppBot, error)
	// RecordBotUserID stores the bot user's numeric id when none is stored yet and reports
	// whether it did. An id already stored is never replaced, and an id that is not positive
	// is refused. It stores nothing before an App is stored.
	RecordBotUserID(ctx context.Context, id int64) (bool, error)
}

// GitHubAppBot is the App's bot user <slug>[bot]. Its numeric id, not its login, is its
// identity: author comparisons and RENOVATE_GIT_AUTHOR use the id.
type GitHubAppBot struct {
	Slug string
	// UserID is the bot user's numeric id; Known is false until it has been read from GitHub.
	UserID int64
	Known  bool
}

// GitHubAppSignIn is the part of the stored App the OAuth sign-in uses.
type GitHubAppSignIn struct {
	ClientID     string
	ClientSecret []byte
	OwnerLogin   string
	// OwnerType is User or Organization.
	OwnerType string
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

func (r githubAppRepo) SignIn(ctx context.Context) (GitHubAppSignIn, error) {
	row, err := r.store.reads.GetGitHubAppSignIn(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return GitHubAppSignIn{}, ErrGitHubAppNotFound
	}
	if err != nil {
		return GitHubAppSignIn{}, fmt.Errorf("read github app sign-in: %w", err)
	}
	secret, err := r.store.cipher.Decrypt(row.EncryptedClientSecret)
	if err != nil {
		return GitHubAppSignIn{}, errors.New("decrypt github app client secret: the stored secret does not match APP_ENCRYPTION_KEY")
	}
	return GitHubAppSignIn{
		ClientID:     row.ClientID,
		ClientSecret: secret,
		OwnerLogin:   row.OwnerLogin,
		OwnerType:    row.OwnerType,
	}, nil
}

func (r githubAppRepo) BotIdentity(ctx context.Context) (GitHubAppBot, error) {
	row, err := r.store.reads.GetBotIdentity(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return GitHubAppBot{}, ErrGitHubAppNotFound
	}
	if err != nil {
		return GitHubAppBot{}, fmt.Errorf("read github app bot user: %w", err)
	}
	bot := GitHubAppBot{Slug: row.Slug}
	if row.BotUserID.Valid && row.BotUserID.Int64 > 0 {
		bot.UserID, bot.Known = row.BotUserID.Int64, true
	}
	return bot, nil
}

func (r githubAppRepo) RecordBotUserID(ctx context.Context, id int64) (bool, error) {
	if id <= 0 {
		return false, errors.New("record github app bot user: the id must be positive")
	}
	changed, err := r.store.writes.SetGitHubAppBotUserID(ctx, db.SetGitHubAppBotUserIDParams{
		BotUserID: sql.NullInt64{Int64: id, Valid: true},
		UpdatedAt: nowUTC(),
	})
	if err != nil {
		return false, fmt.Errorf("store github app bot user: %w", err)
	}
	return changed > 0, nil
}
