package auth

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/alexedwards/scs/sqlite3store"
	"github.com/alexedwards/scs/v2"
	"github.com/mdg-labs/release-ops/internal/logging"
	storedb "github.com/mdg-labs/release-ops/internal/store/db"
)

const sessionsTable = "sessions"

// sqliteTimeLayout is the layout the store hands to julianday() for an expiry.
const sqliteTimeLayout = "2006-01-02T15:04:05.999"

// userBoundSQLiteStore persists scs session data and mirrors userID into sessions.user_id.
type userBoundSQLiteStore struct {
	inner *sqlite3store.SQLite3Store
	db    *sql.DB
	codec scs.Codec
}

func newUserBoundSQLiteStore(db *sql.DB, codec scs.Codec) *userBoundSQLiteStore {
	return &userBoundSQLiteStore{
		inner: sqlite3store.New(db),
		db:    db,
		codec: codec,
	}
}

// Find registers a token it finds, so a session that outlives a restart is kept out of the
// logs before its first commit.
func (s *userBoundSQLiteStore) Find(token string) ([]byte, bool, error) {
	b, found, err := s.inner.Find(token)
	if found {
		s.registerToken(token, SessionLifetime, b)
	}
	return b, found, err
}

// registerToken keeps a session token out of the logs. A session that has no user is made by
// any request, sign-in starts included, so its token goes in the registry's unauthenticated
// budget: a flood of them can age out only other entries of that budget, never a signed-in
// user's session or a credential.
func (s *userBoundSQLiteStore) registerToken(token string, ttl time.Duration, data []byte) {
	if extractSessionUserID(s.codec, data) == "" {
		logging.RegisterUnauthenticatedFor(ttl, token)
		return
	}
	logging.RegisterFor(ttl, token)
}

// Commit writes the session row and, when the session carries a repository scope
// (SessionScopeKey), replaces the stored list in the same transaction. The scope is removed
// from the blob: the table is the one place the list lives, so a later commit of the same
// session leaves it as it is. A session row is updated in place; replacing it would cascade
// away the list it owns.
//
// A token that Delete removed is never written again: a request that loaded the session before
// it was destroyed or renewed commits nothing, without an error, because the session it holds
// no longer exists. The check and the write are one statement, so a Delete cannot land between
// them.
func (s *userBoundSQLiteStore) Commit(token string, b []byte, expiry time.Time) error {
	s.registerToken(token, time.Until(expiry), b)
	userID := extractSessionUserID(s.codec, b)
	scope, hasScope, b, err := s.splitScope(b)
	if err != nil {
		return err
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var userIDArg any
	if userID != "" {
		userIDArg = userID
	}
	stmt := fmt.Sprintf(
		"INSERT INTO %s (token, data, expiry, user_id) "+
			"SELECT ?, ?, julianday(?), ? WHERE NOT EXISTS (SELECT 1 FROM revoked_sessions WHERE token_hash = ?) "+
			"ON CONFLICT(token) DO UPDATE SET data = excluded.data, expiry = excluded.expiry, user_id = excluded.user_id",
		sessionsTable,
	)
	res, err := tx.Exec(stmt, token, b, expiry.UTC().Format(sqliteTimeLayout), userIDArg, tokenHash(token))
	if err != nil {
		return err
	}
	written, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if written == 0 {
		return nil
	}
	if hasScope {
		q := storedb.New(tx)
		ctx := context.Background()
		if err := q.DeleteSessionRepositories(ctx, token); err != nil {
			return err
		}
		for _, id := range scope {
			if err := q.AddSessionRepository(ctx, storedb.AddSessionRepositoryParams{SessionToken: token, RepositoryID: id}); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// splitScope takes the repository scope out of the session data. hasScope is false when the
// session carries none; the returned data is then the input, untouched.
func (s *userBoundSQLiteStore) splitScope(data []byte) (scope []string, hasScope bool, rest []byte, err error) {
	if len(data) == 0 {
		return nil, false, data, nil
	}
	deadline, values, err := s.codec.Decode(data)
	if err != nil {
		return nil, false, data, nil
	}
	raw, ok := values[SessionScopeKey]
	if !ok {
		return nil, false, data, nil
	}
	delete(values, SessionScopeKey)
	rest, err = s.codec.Encode(deadline, values)
	if err != nil {
		return nil, false, nil, err
	}
	scope, _ = raw.([]string)
	return scope, true, rest, nil
}

// Delete removes the session and records the token as revoked for as long as a session could
// still be live, so that Commit refuses it. Markers whose time has passed are dropped here.
// An empty token (a request without a session) has nothing to delete or revoke.
func (s *userBoundSQLiteStore) Delete(token string) error {
	if token == "" {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	const revoke = "INSERT INTO revoked_sessions (token_hash, expiry) VALUES (?, julianday(?)) " +
		"ON CONFLICT(token_hash) DO UPDATE SET expiry = max(expiry, excluded.expiry)"
	until := time.Now().Add(SessionLifetime).UTC().Format(sqliteTimeLayout)
	if _, err := tx.Exec(revoke, tokenHash(token), until); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM revoked_sessions WHERE expiry < julianday('now')"); err != nil {
		return err
	}
	if _, err := tx.Exec(fmt.Sprintf("DELETE FROM %s WHERE token = ?", sessionsTable), token); err != nil {
		return err
	}
	return tx.Commit()
}

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func extractSessionUserID(codec scs.Codec, data []byte) string {
	if len(data) == 0 {
		return ""
	}
	_, values, err := codec.Decode(data)
	if err != nil {
		return ""
	}
	raw, ok := values[SessionUserIDKey]
	if !ok {
		return ""
	}
	userID, ok := raw.(string)
	if !ok {
		return ""
	}
	return userID
}
