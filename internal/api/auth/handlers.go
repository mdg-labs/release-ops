package auth

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/alexedwards/scs/v2"
	"github.com/google/uuid"
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
	TokenService   *TokenService
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

type acceptInvitationRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

// AcceptInvitation handles POST /api/v1/auth/accept-invitation.
func (h *Handlers) AcceptInvitation(w http.ResponseWriter, r *http.Request) {
	if h.TokenService == nil {
		WriteError(w, "INTERNAL_ERROR", "token service not configured", http.StatusInternalServerError)
		return
	}

	var req acceptInvitationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, "VALIDATION_ERROR", "invalid JSON body", http.StatusBadRequest)
		return
	}
	if req.Token == "" || req.Password == "" {
		WriteError(w, "VALIDATION_ERROR", "token and password are required", http.StatusBadRequest)
		return
	}
	if len(req.Password) < minPasswordLength {
		WriteError(w, "VALIDATION_ERROR", "password must be at least 8 characters", http.StatusBadRequest)
		return
	}

	tokenRow, err := h.TokenService.Validate(r.Context(), req.Token, TokenKindInvitation)
	if err != nil {
		if errors.Is(err, ErrTokenInvalid) {
			WriteError(w, "VALIDATION_ERROR", "invalid or expired token", http.StatusBadRequest)
			return
		}
		WriteError(w, "INTERNAL_ERROR", "failed to validate token", http.StatusInternalServerError)
		return
	}

	if _, err := h.Queries.GetUserByEmail(r.Context(), tokenRow.Email); err == nil {
		WriteError(w, "VALIDATION_ERROR", "invalid or expired token", http.StatusBadRequest)
		return
	} else if !errors.Is(err, sql.ErrNoRows) {
		WriteError(w, "INTERNAL_ERROR", "failed to check email", http.StatusInternalServerError)
		return
	}

	hash, err := HashPassword(req.Password)
	if err != nil {
		WriteError(w, "INTERNAL_ERROR", "failed to hash password", http.StatusInternalServerError)
		return
	}

	now := nowUTC()
	user, err := h.Queries.CreateUser(r.Context(), storedb.CreateUserParams{
		ID:           newUserID(),
		Email:        tokenRow.Email,
		PasswordHash: hash,
		CreatedAt:    now,
		UpdatedAt:    now,
	})
	if err != nil {
		WriteError(w, "INTERNAL_ERROR", "failed to create user", http.StatusInternalServerError)
		return
	}
	if err := h.TokenService.Consume(r.Context(), tokenRow.ID); err != nil {
		WriteError(w, "INTERNAL_ERROR", "failed to consume token", http.StatusInternalServerError)
		return
	}

	h.SessionManager.Put(r.Context(), SessionUserIDKey, user.ID)
	h.SessionManager.Put(r.Context(), SessionUserEmailKey, user.Email)

	writeJSON(w, http.StatusOK, sessionResponse{
		User: &userResponse{ID: user.ID, Email: user.Email},
	})
}

type confirmEmailChangeRequest struct {
	Token string `json:"token"`
}

// ConfirmEmailChange handles POST /api/v1/auth/confirm-email-change.
func (h *Handlers) ConfirmEmailChange(w http.ResponseWriter, r *http.Request) {
	if h.TokenService == nil {
		WriteError(w, "INTERNAL_ERROR", "token service not configured", http.StatusInternalServerError)
		return
	}

	var req confirmEmailChangeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, "VALIDATION_ERROR", "invalid JSON body", http.StatusBadRequest)
		return
	}
	if req.Token == "" {
		WriteError(w, "VALIDATION_ERROR", "token is required", http.StatusBadRequest)
		return
	}

	tokenRow, err := h.TokenService.Validate(r.Context(), req.Token, TokenKindEmailChange)
	if err != nil {
		if errors.Is(err, ErrTokenInvalid) {
			WriteError(w, "VALIDATION_ERROR", "invalid or expired token", http.StatusBadRequest)
			return
		}
		WriteError(w, "INTERNAL_ERROR", "failed to validate token", http.StatusInternalServerError)
		return
	}
	if !tokenRow.NewEmail.Valid || tokenRow.NewEmail.String == "" {
		WriteError(w, "VALIDATION_ERROR", "invalid or expired token", http.StatusBadRequest)
		return
	}

	user, err := h.Queries.GetUserByEmail(r.Context(), tokenRow.Email)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			WriteError(w, "VALIDATION_ERROR", "invalid or expired token", http.StatusBadRequest)
			return
		}
		WriteError(w, "INTERNAL_ERROR", "failed to load user", http.StatusInternalServerError)
		return
	}

	if _, err := h.Queries.GetUserByEmail(r.Context(), tokenRow.NewEmail.String); err == nil {
		WriteError(w, "VALIDATION_ERROR", "invalid or expired token", http.StatusBadRequest)
		return
	} else if !errors.Is(err, sql.ErrNoRows) {
		WriteError(w, "INTERNAL_ERROR", "failed to check email", http.StatusInternalServerError)
		return
	}

	updated, err := h.Queries.UpdateUserEmail(r.Context(), storedb.UpdateUserEmailParams{
		Email:     tokenRow.NewEmail.String,
		UpdatedAt: nowUTC(),
		ID:        user.ID,
	})
	if err != nil {
		WriteError(w, "INTERNAL_ERROR", "failed to update email", http.StatusInternalServerError)
		return
	}
	if err := h.TokenService.Consume(r.Context(), tokenRow.ID); err != nil {
		WriteError(w, "INTERNAL_ERROR", "failed to consume token", http.StatusInternalServerError)
		return
	}

	currentToken := h.SessionManager.Token(r.Context())
	if currentToken != "" {
		if err := h.Queries.DeleteOtherSessionsByUserID(r.Context(), storedb.DeleteOtherSessionsByUserIDParams{
			UserID: user.ID,
			Token:  currentToken,
		}); err != nil {
			WriteError(w, "INTERNAL_ERROR", "failed to revoke sessions", http.StatusInternalServerError)
			return
		}
	} else if err := h.Queries.DeleteSessionsByUserID(r.Context(), user.ID); err != nil {
		WriteError(w, "INTERNAL_ERROR", "failed to revoke sessions", http.StatusInternalServerError)
		return
	}

	h.SessionManager.Put(r.Context(), SessionUserIDKey, updated.ID)
	h.SessionManager.Put(r.Context(), SessionUserEmailKey, updated.Email)

	writeJSON(w, http.StatusOK, sessionResponse{
		User: &userResponse{ID: updated.ID, Email: updated.Email},
	})
}

const minPasswordLength = 8

func newUserID() string {
	return uuid.NewString()
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
