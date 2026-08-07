package auth

import (
	"database/sql"
	"net/http"
	"time"

	"github.com/alexedwards/scs/v2"
)

const (
	// SessionCookieName is the HttpOnly session cookie used by the Go API.
	SessionCookieName = "release_ops_session"
	sessionLifetime   = 7 * 24 * time.Hour
)

// NewSessionManager configures scs with the SQLite sessions table store.
// sessionSecret is validated by config.Load before the server starts.
func NewSessionManager(db *sql.DB, sessionSecret string, secureCookies bool) *scs.SessionManager {
	_ = sessionSecret

	manager := scs.New()
	manager.Store = newUserBoundSQLiteStore(db, manager.Codec)
	manager.Lifetime = sessionLifetime
	manager.Cookie.Name = SessionCookieName
	manager.Cookie.HttpOnly = true
	manager.Cookie.Path = "/"
	manager.Cookie.SameSite = http.SameSiteLaxMode
	manager.Cookie.Secure = secureCookies
	manager.Cookie.Persist = true
	return manager
}
