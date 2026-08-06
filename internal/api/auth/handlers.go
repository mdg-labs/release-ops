package auth

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/alexedwards/scs/v2"
	storedb "github.com/mdg-labs/release-ops/internal/store/db"
)

const (
	// SessionUserIDKey is the scs session key for the authenticated user ID.
	SessionUserIDKey = "userID"
	// SessionUserEmailKey is the scs session key for the authenticated user email.
	SessionUserEmailKey = "userEmail"
)

// Handlers serves auth HTTP endpoints.
type Handlers struct {
	SessionManager *scs.SessionManager
	Queries        *storedb.Queries
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type userResponse struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

type sessionResponse struct {
	User *userResponse `json:"user"`
}

// Login handles POST /api/v1/auth/login.
func (h *Handlers) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, "VALIDATION_ERROR", "invalid JSON body", http.StatusBadRequest)
		return
	}
	if req.Email == "" || req.Password == "" {
		WriteError(w, "VALIDATION_ERROR", "email and password are required", http.StatusBadRequest)
		return
	}

	user, err := h.Queries.GetUserByEmail(r.Context(), req.Email)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			WriteError(w, "invalid_credentials", "invalid email or password", http.StatusUnauthorized)
			return
		}
		WriteError(w, "INTERNAL_ERROR", "failed to authenticate", http.StatusInternalServerError)
		return
	}

	if err := ComparePassword(user.PasswordHash, req.Password); err != nil {
		WriteError(w, "invalid_credentials", "invalid email or password", http.StatusUnauthorized)
		return
	}

	h.SessionManager.Put(r.Context(), SessionUserIDKey, user.ID)
	h.SessionManager.Put(r.Context(), SessionUserEmailKey, user.Email)

	writeJSON(w, http.StatusOK, sessionResponse{
		User: &userResponse{ID: user.ID, Email: user.Email},
	})
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
	userID := h.SessionManager.GetString(r.Context(), SessionUserIDKey)
	email := h.SessionManager.GetString(r.Context(), SessionUserEmailKey)
	if userID == "" || email == "" {
		writeJSON(w, http.StatusOK, sessionResponse{User: nil})
		return
	}

	writeJSON(w, http.StatusOK, sessionResponse{
		User: &userResponse{ID: userID, Email: email},
	})
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
