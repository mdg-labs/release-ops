package setup

import (
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/mdg-labs/release-ops/internal/api/auth"
	storedb "github.com/mdg-labs/release-ops/internal/store/db"
)

const (
	// SessionExpiresKey is the scs session key holding the end of a setup session as Unix
	// seconds. A session without it is not a setup session.
	SessionExpiresKey = "setupSessionExpiresAt"
	// SessionTTL is how long a setup session lasts.
	SessionTTL = 15 * time.Minute

	maxBodyBytes = 4 << 10
)

// Handlers serves the setup endpoints.
type Handlers struct {
	Sessions *scs.SessionManager
	Queries  *storedb.Queries
	// Apps stores the GitHub App and Converter exchanges the manifest code for it; both are
	// needed by Manifest and Callback only.
	Apps      AppStore
	Converter ManifestConverter
	// Installations and Installer record and confirm the App's installation; both are needed
	// by Installed only.
	Installations InstallationStore
	Installer     InstallationFetcher
	// AppURL, WebhookURL and GitHubBaseURL are the configured APP_URL, WEBHOOK_URL (empty when
	// unset) and GITHUB_BASE_URL; every URL of the manifest and of the redirects comes from them.
	AppURL        string
	WebhookURL    string
	GitHubBaseURL string
	// TTL is the lifetime of a setup session; zero means SessionTTL.
	TTL time.Duration
	// Now is the clock; nil means time.Now.
	Now func() time.Time
}

func (h *Handlers) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

func (h *Handlers) ttl() time.Duration {
	if h.TTL > 0 {
		return h.TTL
	}
	return SessionTTL
}

type statusResponse struct {
	SetupRequired bool `json:"setupRequired"`
	// InstallURL is the App's installation page, present while an App is stored and has no
	// installation, so the operator can finish when the redirect after creation was missed.
	InstallURL string `json:"installUrl,omitempty"`
}

// Status answers GET /setup/status: whether the instance still needs setup. It stays
// available once an App exists so the sign-in page can tell the two states apart.
func (h *Handlers) Status(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	required, err := Required(ctx, h.Queries)
	if err != nil {
		writeInternalError(w)
		return
	}
	resp := statusResponse{SetupRequired: required}
	if !required {
		installs, err := h.Queries.CountLiveInstallations(ctx)
		if err != nil {
			writeInternalError(w)
			return
		}
		if installs == 0 {
			slug, err := h.Queries.GetGitHubAppSlug(ctx)
			if err != nil {
				writeInternalError(w)
				return
			}
			resp.InstallURL = InstallURL(h.GitHubBaseURL, slug)
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

type createSessionRequest struct {
	Token string `json:"token"`
}

// CreateSession answers POST /setup/session: it redeems the setup token, which can be
// redeemed once, and opens a setup session. It answers 404 once an App exists.
func (h *Handlers) CreateSession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	required, err := Required(ctx, h.Queries)
	if err != nil {
		writeInternalError(w)
		return
	}
	if !required {
		writeNotFound(w)
		return
	}

	var req createSessionRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes)).Decode(&req); err != nil {
		auth.WriteError(w, "VALIDATION_ERROR", "Invalid request body.", http.StatusBadRequest)
		return
	}

	// With no unused token the supplied one is compared with an empty hash, so a refused
	// request does the same work whether or not a token is outstanding.
	var storedHash, tokenID string
	row, err := h.Queries.GetUnusedSetupToken(ctx)
	switch {
	case err == nil:
		storedHash, tokenID = row.TokenHash, row.ID
	case errors.Is(err, sql.ErrNoRows):
	default:
		writeInternalError(w)
		return
	}
	hashMatches := subtle.ConstantTimeCompare([]byte(auth.HashToken(req.Token)), []byte(storedHash)) == 1
	if tokenID == "" || !hashMatches {
		writeInvalidToken(w)
		return
	}

	if err := auth.NewTokenService(h.Queries).Consume(ctx, tokenID); err != nil {
		if errors.Is(err, auth.ErrTokenInvalid) {
			writeInvalidToken(w)
			return
		}
		writeInternalError(w)
		return
	}

	expires := h.now().Add(h.ttl())
	if err := h.Sessions.RenewToken(ctx); err != nil {
		writeInternalError(w)
		return
	}
	h.Sessions.Put(ctx, SessionExpiresKey, expires.Unix())
	h.Sessions.SetDeadline(ctx, expires)
	w.WriteHeader(http.StatusNoContent)
}

type sessionResponse struct {
	ExpiresAt string `json:"expiresAt"`
}

// GetSession answers GET /setup/session behind RequireSession: the end of the current
// setup session.
func (h *Handlers) GetSession(w http.ResponseWriter, r *http.Request) {
	expires := h.Sessions.GetInt64(r.Context(), SessionExpiresKey)
	writeJSON(w, http.StatusOK, sessionResponse{
		ExpiresAt: time.Unix(expires, 0).UTC().Format(isoTimeFormat),
	})
}

// RequireSession lets a request through only with an unexpired setup session, and only
// while no App is configured. It answers 404 once an App exists and 401 otherwise; an
// expired session is destroyed. A session without a setup marker, such as a user session,
// is not a setup session.
func (h *Handlers) RequireSession(next http.Handler) http.Handler {
	return h.requireSession(next, func(w http.ResponseWriter, _ *http.Request) {
		writeSetupSessionRequired(w)
	})
}

// RequireCallbackSession is RequireSession for GET /setup/callback, a top-level browser
// redirect from GitHub: a missing or expired setup session sends the browser to
// /setup?error=session_expired instead of answering 401 JSON the person would read raw.
func (h *Handlers) RequireCallbackSession(next http.Handler) http.Handler {
	return h.requireSession(next, func(w http.ResponseWriter, r *http.Request) {
		h.redirectToSetup(w, r, errSessionExpired)
	})
}

func (h *Handlers) requireSession(next http.Handler, noSession func(http.ResponseWriter, *http.Request)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		required, err := Required(ctx, h.Queries)
		if err != nil {
			writeInternalError(w)
			return
		}
		if !required {
			writeNotFound(w)
			return
		}

		expires := h.Sessions.GetInt64(ctx, SessionExpiresKey)
		if expires == 0 {
			noSession(w, r)
			return
		}
		if !h.now().Before(time.Unix(expires, 0)) {
			if err := h.Sessions.Destroy(ctx); err != nil {
				writeInternalError(w)
				return
			}
			noSession(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeInvalidToken(w http.ResponseWriter) {
	auth.WriteError(w, "INVALID_TOKEN", "Invalid setup token.", http.StatusUnauthorized)
}

func writeSetupSessionRequired(w http.ResponseWriter) {
	auth.WriteError(w, "UNAUTHORIZED", "A setup session is required.", http.StatusUnauthorized)
}

func writeNotFound(w http.ResponseWriter) {
	auth.WriteError(w, "NOT_FOUND", "not found", http.StatusNotFound)
}

func writeInternalError(w http.ResponseWriter) {
	auth.WriteError(w, "INTERNAL_ERROR", "Internal server error.", http.StatusInternalServerError)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
