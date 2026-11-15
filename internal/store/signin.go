package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/mdg-labs/release-ops/internal/store/db"
)

var (
	// ErrBootstrapAdminTaken is returned when a sign-in asked to become the bootstrap admin
	// but another user was pinned first. Nothing is written.
	ErrBootstrapAdminTaken = errors.New("a bootstrap admin is already pinned")
	// ErrNoBootstrapAdmin is returned when no bootstrap admin has been resolved yet.
	ErrNoBootstrapAdmin = errors.New("no bootstrap admin has been resolved")
	// ErrLoginLinkInvalid is returned for a login link that is unknown, expired or used.
	ErrLoginLinkInvalid = errors.New("login link is invalid, expired or used")
)

// GitHubUser is a user as GitHub identifies them: the numeric id is the identity, the login
// is display data that follows renames.
type GitHubUser struct {
	ID    int64
	Login string
}

// BootstrapAdmin is the pinned first admin.
type BootstrapAdmin struct {
	UserID string
	GitHubUser
}

// SignInRepository stores who signs in. Users exist only for identities that were let in:
// nothing here registers anyone else.
type SignInRepository interface {
	// BootstrapAdmin returns the pinned bootstrap admin, ok false while none is pinned.
	BootstrapAdmin(ctx context.Context) (admin BootstrapAdmin, ok bool, err error)
	// SignIn creates or updates the user of the GitHub identity in one transaction: found by
	// the numeric id, never the login, and a changed login updates the identity instead of
	// adding a user. With pinAdmin it also pins the user as the bootstrap admin, and returns
	// ErrBootstrapAdminTaken, writing nothing, when another one is pinned first. It returns
	// the user id.
	SignIn(ctx context.Context, u GitHubUser, pinAdmin bool) (userID string, err error)
	// CreateLoginLink stores the hash of a break-glass token, valid until expiresAt. It
	// returns ErrNoBootstrapAdmin, storing nothing, while no bootstrap admin is pinned.
	CreateLoginLink(ctx context.Context, tokenHash string, expiresAt time.Time) error
	// RedeemLoginLink consumes the link with a guarded write that matches only an unused,
	// unexpired token, and returns the bootstrap admin it signs in. A link that matches
	// nothing returns ErrLoginLinkInvalid. If no bootstrap admin is pinned the link is left
	// unconsumed and ErrNoBootstrapAdmin is returned.
	RedeemLoginLink(ctx context.Context, tokenHash string) (BootstrapAdmin, error)
}

// SignIn returns the sign-in repository.
func (s *Store) SignIn() SignInRepository {
	return signInRepo{store: s}
}

type signInRepo struct {
	store *Store
}

func bootstrapAdmin(ctx context.Context, q *db.Queries) (BootstrapAdmin, bool, error) {
	row, err := q.GetBootstrapAdminIdentity(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return BootstrapAdmin{}, false, nil
	}
	if err != nil {
		return BootstrapAdmin{}, false, fmt.Errorf("read bootstrap admin: %w", err)
	}
	id, err := strconv.ParseInt(row.Subject, 10, 64)
	if err != nil {
		return BootstrapAdmin{}, false, fmt.Errorf("bootstrap admin identity is not numeric: %w", err)
	}
	return BootstrapAdmin{UserID: row.UserID, GitHubUser: GitHubUser{ID: id, Login: row.Login}}, true, nil
}

func (r signInRepo) BootstrapAdmin(ctx context.Context) (BootstrapAdmin, bool, error) {
	return bootstrapAdmin(ctx, r.store.reads)
}

func (r signInRepo) SignIn(ctx context.Context, u GitHubUser, pinAdmin bool) (string, error) {
	if u.ID <= 0 || u.Login == "" {
		return "", errors.New("github user needs a numeric id and a login")
	}
	subject := strconv.FormatInt(u.ID, 10)

	tx, err := r.store.db.Writer.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("begin sign-in: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	q := r.store.writes.WithTx(tx)
	now := nowUTC()

	var userID string
	identity, err := q.GetGitHubIdentity(ctx, subject)
	switch {
	case err == nil:
		userID = identity.UserID
		if identity.Login != u.Login {
			if err := q.UpdateGitHubIdentityLogin(ctx, db.UpdateGitHubIdentityLoginParams{Login: u.Login, ID: identity.ID}); err != nil {
				return "", fmt.Errorf("update login: %w", err)
			}
		}
		if err := q.TouchUser(ctx, db.TouchUserParams{UpdatedAt: now, ID: userID}); err != nil {
			return "", fmt.Errorf("update user: %w", err)
		}
	case errors.Is(err, sql.ErrNoRows):
		userID = newID()
		if err := q.CreateGitHubUser(ctx, db.CreateGitHubUserParams{ID: userID, CreatedAt: now, UpdatedAt: now}); err != nil {
			return "", fmt.Errorf("create user: %w", err)
		}
		if err := q.CreateGitHubIdentity(ctx, db.CreateGitHubIdentityParams{
			ID: newID(), UserID: userID, Subject: subject, Login: u.Login, CreatedAt: now,
		}); err != nil {
			return "", fmt.Errorf("create identity: %w", err)
		}
	default:
		return "", fmt.Errorf("find identity: %w", err)
	}

	if pinAdmin {
		pinned, err := q.SetBootstrapAdmin(ctx, db.SetBootstrapAdminParams{
			BootstrapAdminUserID: sql.NullString{String: userID, Valid: true},
			UpdatedAt:            now,
		})
		if err != nil {
			return "", fmt.Errorf("pin bootstrap admin: %w", err)
		}
		if pinned == 0 {
			return "", ErrBootstrapAdminTaken
		}
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit sign-in: %w", err)
	}
	return userID, nil
}

func (r signInRepo) CreateLoginLink(ctx context.Context, tokenHash string, expiresAt time.Time) error {
	tx, err := r.store.db.Writer.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin login link: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	q := r.store.writes.WithTx(tx)

	if _, ok, err := bootstrapAdmin(ctx, q); err != nil {
		return err
	} else if !ok {
		return ErrNoBootstrapAdmin
	}
	if err := q.CreateLoginLinkToken(ctx, db.CreateLoginLinkTokenParams{
		ID:        newID(),
		TokenHash: tokenHash,
		ExpiresAt: expiresAt.UTC().Format(timeLayout),
		CreatedAt: nowUTC(),
	}); err != nil {
		return fmt.Errorf("store login link: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit login link: %w", err)
	}
	return nil
}

func (r signInRepo) RedeemLoginLink(ctx context.Context, tokenHash string) (BootstrapAdmin, error) {
	tx, err := r.store.db.Writer.BeginTx(ctx, nil)
	if err != nil {
		return BootstrapAdmin{}, fmt.Errorf("begin login link redemption: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	q := r.store.writes.WithTx(tx)

	consumed, err := q.ConsumeLoginLinkToken(ctx, db.ConsumeLoginLinkTokenParams{Now: nowUTC(), TokenHash: tokenHash})
	if err != nil {
		return BootstrapAdmin{}, fmt.Errorf("consume login link: %w", err)
	}
	if consumed == 0 {
		return BootstrapAdmin{}, ErrLoginLinkInvalid
	}
	admin, ok, err := bootstrapAdmin(ctx, q)
	if err != nil {
		return BootstrapAdmin{}, err
	}
	if !ok {
		return BootstrapAdmin{}, ErrNoBootstrapAdmin
	}
	if err := tx.Commit(); err != nil {
		return BootstrapAdmin{}, fmt.Errorf("commit login link redemption: %w", err)
	}
	return admin, nil
}
