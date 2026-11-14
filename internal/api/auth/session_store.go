package auth

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/alexedwards/scs/sqlite3store"
	"github.com/alexedwards/scs/v2"
	storedb "github.com/mdg-labs/release-ops/internal/store/db"
)

const sessionsTable = "sessions"

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

func (s *userBoundSQLiteStore) Find(token string) ([]byte, bool, error) {
	return s.inner.Find(token)
}

// Commit writes the session row and, when the session carries a repository scope
// (SessionScopeKey), replaces the stored list in the same transaction. The scope is removed
// from the blob: the table is the one place the list lives, so a later commit of the same
// session leaves it as it is. A session row is updated in place; replacing it would cascade
// away the list it owns.
func (s *userBoundSQLiteStore) Commit(token string, b []byte, expiry time.Time) error {
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
		"INSERT INTO %s (token, data, expiry, user_id) VALUES (?, ?, julianday(?), ?) "+
			"ON CONFLICT(token) DO UPDATE SET data = excluded.data, expiry = excluded.expiry, user_id = excluded.user_id",
		sessionsTable,
	)
	if _, err := tx.Exec(stmt, token, b, expiry.UTC().Format("2006-01-02T15:04:05.999"), userIDArg); err != nil {
		return err
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

func (s *userBoundSQLiteStore) Delete(token string) error {
	return s.inner.Delete(token)
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
