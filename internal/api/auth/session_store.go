package auth

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/alexedwards/scs/sqlite3store"
	"github.com/alexedwards/scs/v2"
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

func (s *userBoundSQLiteStore) Commit(token string, b []byte, expiry time.Time) error {
	userID := extractSessionUserID(s.codec, b)
	stmt := fmt.Sprintf(
		"REPLACE INTO %s (token, data, expiry, user_id) VALUES (?, ?, julianday(?), ?)",
		sessionsTable,
	)
	var userIDArg any
	if userID != "" {
		userIDArg = userID
	}
	_, err := s.db.Exec(stmt, token, b, expiry.UTC().Format("2006-01-02T15:04:05.999"), userIDArg)
	return err
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
