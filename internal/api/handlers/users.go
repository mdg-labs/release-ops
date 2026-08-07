package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	netmail "net/mail"
	"strings"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/mdg-labs/release-ops/internal/api/auth"
	apimw "github.com/mdg-labs/release-ops/internal/api/middleware"
	"github.com/mdg-labs/release-ops/internal/mail"
	storedb "github.com/mdg-labs/release-ops/internal/store/db"
)

// UsersHandlers serves user management HTTP endpoints.
type UsersHandlers struct {
	Queries        *storedb.Queries
	SessionManager *scs.SessionManager
	TokenService   *auth.TokenService
	Mailer         mail.Mailer
	AppPublicURL   string
}

type userListItem struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	CreatedAt string `json:"createdAt"`
}

type listUsersResponse struct {
	Items []userListItem `json:"items"`
}

// List handles GET /api/v1/users.
func (h *UsersHandlers) List(w http.ResponseWriter, r *http.Request) {
	rows, err := h.Queries.ListUsers(r.Context())
	if err != nil {
		auth.WriteError(w, "INTERNAL_ERROR", "failed to list users", http.StatusInternalServerError)
		return
	}

	items := make([]userListItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, userListItem{
			ID:        row.ID,
			Email:     row.Email,
			CreatedAt: row.CreatedAt,
		})
	}
	writeUsersJSON(w, http.StatusOK, listUsersResponse{Items: items})
}

// Delete handles DELETE /api/v1/users/{id}.
func (h *UsersHandlers) Delete(w http.ResponseWriter, r *http.Request) {
	currentUserID, ok := apimw.UserIDFromContext(r.Context())
	if !ok {
		auth.WriteError(w, "unauthorized", "authentication required", http.StatusUnauthorized)
		return
	}

	targetID := chi.URLParam(r, "id")
	if targetID == "" {
		auth.WriteError(w, "VALIDATION_ERROR", "user id is required", http.StatusBadRequest)
		return
	}
	if targetID == currentUserID {
		auth.WriteError(w, "forbidden", "cannot delete your own account", http.StatusForbidden)
		return
	}

	count, err := h.Queries.CountUsers(r.Context())
	if err != nil {
		auth.WriteError(w, "INTERNAL_ERROR", "failed to count users", http.StatusInternalServerError)
		return
	}
	if count <= 1 {
		auth.WriteError(w, "conflict", "cannot delete the last user", http.StatusConflict)
		return
	}

	if _, err := h.Queries.GetUserByID(r.Context(), targetID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			auth.WriteError(w, "NOT_FOUND", "user not found", http.StatusNotFound)
			return
		}
		auth.WriteError(w, "INTERNAL_ERROR", "failed to load user", http.StatusInternalServerError)
		return
	}

	if err := h.Queries.DeleteSessionsByUserID(r.Context(), targetID); err != nil {
		auth.WriteError(w, "INTERNAL_ERROR", "failed to revoke sessions", http.StatusInternalServerError)
		return
	}
	if err := h.Queries.DeleteUser(r.Context(), targetID); err != nil {
		auth.WriteError(w, "INTERNAL_ERROR", "failed to delete user", http.StatusInternalServerError)
		return
	}

	writeUsersJSON(w, http.StatusOK, map[string]any{})
}

type emailChangeRequest struct {
	NewEmail        string `json:"newEmail"`
	CurrentPassword string `json:"currentPassword"`
}

// EmailChangeRequest handles POST /api/v1/users/me/email-change-request.
func (h *UsersHandlers) EmailChangeRequest(w http.ResponseWriter, r *http.Request) {
	currentUserID, ok := apimw.UserIDFromContext(r.Context())
	if !ok {
		auth.WriteError(w, "unauthorized", "authentication required", http.StatusUnauthorized)
		return
	}

	var req emailChangeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		auth.WriteError(w, "VALIDATION_ERROR", "invalid JSON body", http.StatusBadRequest)
		return
	}
	newEmail := strings.TrimSpace(strings.ToLower(req.NewEmail))
	if newEmail == "" || req.CurrentPassword == "" {
		auth.WriteError(w, "VALIDATION_ERROR", "newEmail and currentPassword are required", http.StatusBadRequest)
		return
	}
	if _, err := netmail.ParseAddress(newEmail); err != nil {
		auth.WriteError(w, "VALIDATION_ERROR", "invalid email address", http.StatusBadRequest)
		return
	}

	user, err := h.Queries.GetUserByID(r.Context(), currentUserID)
	if err != nil {
		auth.WriteError(w, "INTERNAL_ERROR", "failed to load user", http.StatusInternalServerError)
		return
	}
	if strings.EqualFold(user.Email, newEmail) {
		auth.WriteError(w, "VALIDATION_ERROR", "new email must differ from current email", http.StatusBadRequest)
		return
	}
	if err := auth.ComparePassword(user.PasswordHash, req.CurrentPassword); err != nil {
		auth.WriteError(w, "invalid_credentials", "invalid email or password", http.StatusUnauthorized)
		return
	}

	if _, err := h.Queries.GetUserByEmail(r.Context(), newEmail); err == nil {
		auth.WriteError(w, "conflict", "email already in use", http.StatusConflict)
		return
	} else if !errors.Is(err, sql.ErrNoRows) {
		auth.WriteError(w, "INTERNAL_ERROR", "failed to check email", http.StatusInternalServerError)
		return
	}

	settings, err := h.Queries.GetAppSettings(r.Context())
	if err != nil {
		auth.WriteError(w, "INTERNAL_ERROR", "failed to load settings", http.StatusInternalServerError)
		return
	}

	raw, _, err := h.TokenService.CreateEmailChange(r.Context(), user.Email, newEmail, settings.PasswordResetTokenExpiryMinutes)
	if err != nil {
		auth.WriteError(w, "INTERNAL_ERROR", "failed to create email change token", http.StatusInternalServerError)
		return
	}

	msg, err := mail.BuildEmailChangeConfirmation(newEmail, mail.EmailChangeData{
		ActionURL: buildActionURL(h.AppPublicURL, "confirm-email-change", raw),
		NewEmail:  newEmail,
	})
	if err != nil {
		auth.WriteError(w, "INTERNAL_ERROR", "failed to build email", http.StatusInternalServerError)
		return
	}
	if err := h.Mailer.Send(r.Context(), msg); err != nil {
		if errors.Is(err, mail.ErrSMTPNotConfigured) {
			auth.WriteError(w, "SERVICE_UNAVAILABLE", "smtp not configured", http.StatusServiceUnavailable)
			return
		}
		auth.WriteError(w, "INTERNAL_ERROR", "failed to send email", http.StatusInternalServerError)
		return
	}

	writeUsersJSON(w, http.StatusOK, map[string]any{})
}

func writeUsersJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func buildActionURL(appPublicURL, path, token string) string {
	base := strings.TrimRight(strings.TrimSpace(appPublicURL), "/")
	if base == "" {
		base = "http://localhost:3000"
	}
	return base + "/" + strings.TrimPrefix(path, "/") + "?token=" + token
}
