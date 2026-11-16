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

	"github.com/mdg-labs/release-ops/internal/logging"
	storedb "github.com/mdg-labs/release-ops/internal/store/db"
)

// TokenKindSetup is the auth_tokens.kind of the first-start setup token, the only kind.
const TokenKindSetup = "setup"

const tokenByteLength = 32

// rawTokenLogTTL is how long a one-time token is kept out of the logs.
const rawTokenLogTTL = 30 * 24 * time.Hour

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
	raw, err := newRawToken()
	if err != nil {
		return "", err
	}
	logging.RegisterFor(rawTokenLogTTL, raw)
	return raw, nil
}

// newRawToken returns a random one-time secret and registers nothing: the caller knows how long
// the token lives and who can make it be minted.
func newRawToken() (string, error) {
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

// Consume marks an unused token row as used. It returns ErrTokenInvalid when the token
// was already consumed, so only one of several concurrent redemptions succeeds.
func (s *TokenService) Consume(ctx context.Context, id string) error {
	now := nowUTC()
	_, err := s.Queries.MarkAuthTokenUsed(ctx, storedb.MarkAuthTokenUsedParams{
		UsedAt: sql.NullString{String: now, Valid: true},
		ID:     id,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return ErrTokenInvalid
	}
	return err
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
