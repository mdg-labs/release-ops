package auth

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/alexedwards/scs/v2"
)

const (
	// SessionUserIDKey is the scs session key for the authenticated user ID.
	SessionUserIDKey = "userID"
)

// Handlers serves auth HTTP endpoints.
type Handlers struct {
	SessionManager *scs.SessionManager
	// Authorizer decides what GET /auth/session answers: the role as evaluated now, a session
	// whose user lost access or that has no GitHub identity reading as no session, and one
	// whose role cannot be evaluated a 503.
	Authorizer *Authorizer
}

// Logout handles POST /api/v1/auth/logout.
func (h *Handlers) Logout(w http.ResponseWriter, r *http.Request) {
	if err := h.SessionManager.Destroy(r.Context()); err != nil {
		WriteError(w, "INTERNAL_ERROR", "failed to logout", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

// Session handles GET /api/v1/auth/session.
func (h *Handlers) Session(w http.ResponseWriter, r *http.Request) {
	switch _, err := h.Authorizer.Authorize(r.Context()); {
	case err == nil:
	case errors.Is(err, ErrUnauthenticated):
		writeJSON(w, http.StatusOK, githubSessionResponse{User: nil})
		return
	case errors.Is(err, ErrRoleUnavailable):
		slog.Warn("session: the role could not be re-evaluated", "err", err)
		WriteError(w, "SERVICE_UNAVAILABLE", "the role could not be checked with GitHub", http.StatusServiceUnavailable)
		return
	default:
		slog.Error("session: authorizing failed", "err", err)
		WriteError(w, "INTERNAL_ERROR", "failed to read the session", http.StatusInternalServerError)
		return
	}
	user, ok := githubSessionUser(r.Context(), h.SessionManager)
	if !ok {
		writeJSON(w, http.StatusOK, githubSessionResponse{User: nil})
		return
	}
	writeJSON(w, http.StatusOK, githubSessionResponse{User: user})
}

// WriteError writes the standard JSON error envelope from specs §7.
func WriteError(w http.ResponseWriter, code, message string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
