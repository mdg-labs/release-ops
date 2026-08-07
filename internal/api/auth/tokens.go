package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	storedb "github.com/mdg-labs/release-ops/internal/store/db"
)

// Token kinds stored in auth_tokens.kind.
const (
	TokenKindInvitation    = "invitation"
	TokenKindPasswordReset = "password_reset"
	TokenKindEmailChange   = "email_change"
)

const tokenByteLength = 32

var (
	// ErrTokenInvalid is returned when a token is missing, wrong kind, used, or expired.
	ErrTokenInvalid = errors.New("invalid or expired token")
)

// TokenService manages one-time auth tokens.
type TokenService struct {
	Queries *storedb.Queries
}

// NewTokenService returns a token service backed by sqlc queries.
func NewTokenService(queries *storedb.Queries) *TokenService {
	return &TokenService{Queries: queries}
}

// GenerateRawToken returns a cryptographically random one-time secret.
func GenerateRawToken() (string, error) {
	buf := make([]byte, tokenByteLength)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// HashToken returns the SHA-256 hex digest of raw.
func HashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// Validate looks up raw by hash and checks kind, expiry, and used state.
func (s *TokenService) Validate(ctx context.Context, raw, kind string) (storedb.AuthToken, error) {
	if raw == "" {
		return storedb.AuthToken{}, ErrTokenInvalid
	}

	row, err := s.Queries.GetAuthTokenByHash(ctx, HashToken(raw))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storedb.AuthToken{}, ErrTokenInvalid
		}
		return storedb.AuthToken{}, err
	}
	if row.Kind != kind {
		return storedb.AuthToken{}, ErrTokenInvalid
	}
	if row.UsedAt.Valid {
		return storedb.AuthToken{}, ErrTokenInvalid
	}
	if !tokenNotExpired(row.ExpiresAt) {
		return storedb.AuthToken{}, ErrTokenInvalid
	}
	return row, nil
}

// Consume marks a token row as used.
func (s *TokenService) Consume(ctx context.Context, id string) error {
	now := nowUTC()
	_, err := s.Queries.MarkAuthTokenUsed(ctx, storedb.MarkAuthTokenUsedParams{
		UsedAt: sql.NullString{String: now, Valid: true},
		ID:     id,
	})
	return err
}

// CreateInvitation stores a pending invitation token and returns the raw secret.
func (s *TokenService) CreateInvitation(ctx context.Context, email, invitedByUserID string, expiryHours int64) (string, storedb.AuthToken, error) {
	raw, err := GenerateRawToken()
	if err != nil {
		return "", storedb.AuthToken{}, err
	}

	now := nowUTC()
	expiresAt := time.Now().UTC().Add(time.Duration(expiryHours) * time.Hour).Format(isoTimeFormat)

	row, err := s.Queries.CreateAuthToken(ctx, storedb.CreateAuthTokenParams{
		ID:        uuid.NewString(),
		Kind:      TokenKindInvitation,
		Email:     email,
		TokenHash: HashToken(raw),
		NewEmail:  sql.NullString{},
		InvitedByUserID: sql.NullString{
			String: invitedByUserID,
			Valid:  invitedByUserID != "",
		},
		ExpiresAt: expiresAt,
		CreatedAt: now,
	})
	if err != nil {
		return "", storedb.AuthToken{}, err
	}
	return raw, row, nil
}

// CreateEmailChange stores a pending email-change token and returns the raw secret.
func (s *TokenService) CreateEmailChange(ctx context.Context, currentEmail, newEmail string, expiryMinutes int64) (string, storedb.AuthToken, error) {
	raw, err := GenerateRawToken()
	if err != nil {
		return "", storedb.AuthToken{}, err
	}

	now := nowUTC()
	if err := s.Queries.InvalidateEmailChangeTokensForEmail(ctx, storedb.InvalidateEmailChangeTokensForEmailParams{
		UsedAt: sql.NullString{String: now, Valid: true},
		Email:  currentEmail,
	}); err != nil {
		return "", storedb.AuthToken{}, err
	}

	expiresAt := time.Now().UTC().Add(time.Duration(expiryMinutes) * time.Minute).Format(isoTimeFormat)
	row, err := s.Queries.CreateAuthToken(ctx, storedb.CreateAuthTokenParams{
		ID:        uuid.NewString(),
		Kind:      TokenKindEmailChange,
		Email:     currentEmail,
		TokenHash: HashToken(raw),
		NewEmail: sql.NullString{
			String: newEmail,
			Valid:  true,
		},
		InvitedByUserID: sql.NullString{},
		ExpiresAt:       expiresAt,
		CreatedAt:       now,
	})
	if err != nil {
		return "", storedb.AuthToken{}, err
	}
	return raw, row, nil
}

const isoTimeFormat = "2006-01-02T15:04:05.000Z"

func nowUTC() string {
	return time.Now().UTC().Format(isoTimeFormat)
}

func tokenNotExpired(expiresAt string) bool {
	expiry, err := time.Parse(isoTimeFormat, expiresAt)
	if err != nil {
		return false
	}
	return time.Now().UTC().Before(expiry)
}
