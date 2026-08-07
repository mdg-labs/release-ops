package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	netmail "net/mail"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mdg-labs/release-ops/internal/api/auth"
	apimw "github.com/mdg-labs/release-ops/internal/api/middleware"
	"github.com/mdg-labs/release-ops/internal/mail"
	storedb "github.com/mdg-labs/release-ops/internal/store/db"
)

// InvitationHandlers serves invitation HTTP endpoints.
type InvitationHandlers struct {
	Queries        *storedb.Queries
	TokenService   *auth.TokenService
	Mailer         mail.Mailer
	AppPublicURL   string
}

type invitationItem struct {
	ID              string  `json:"id"`
	Email           string  `json:"email"`
	ExpiresAt       string  `json:"expiresAt"`
	CreatedAt       string  `json:"createdAt"`
	InvitedByUserID *string `json:"invitedByUserId"`
}

type listInvitationsResponse struct {
	Items []invitationItem `json:"items"`
}

type createInvitationRequest struct {
	Email string `json:"email"`
}

type createInvitationResponse struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	ExpiresAt string `json:"expiresAt"`
	InviteURL string `json:"inviteUrl"`
}

// List handles GET /api/v1/users/invitations.
func (h *InvitationHandlers) List(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	rows, err := h.Queries.ListPendingInvitations(r.Context(), now)
	if err != nil {
		auth.WriteError(w, "INTERNAL_ERROR", "failed to list invitations", http.StatusInternalServerError)
		return
	}

	items := make([]invitationItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, invitationItem{
			ID:              row.ID,
			Email:           row.Email,
			ExpiresAt:       row.ExpiresAt,
			CreatedAt:       row.CreatedAt,
			InvitedByUserID: nullStringToPtr(row.InvitedByUserID),
		})
	}
	writeInvitationsJSON(w, http.StatusOK, listInvitationsResponse{Items: items})
}

// Create handles POST /api/v1/users/invitations.
func (h *InvitationHandlers) Create(w http.ResponseWriter, r *http.Request) {
	inviterID, ok := apimw.UserIDFromContext(r.Context())
	if !ok {
		auth.WriteError(w, "unauthorized", "authentication required", http.StatusUnauthorized)
		return
	}

	var req createInvitationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		auth.WriteError(w, "VALIDATION_ERROR", "invalid JSON body", http.StatusBadRequest)
		return
	}
	email := strings.TrimSpace(strings.ToLower(req.Email))
	if email == "" {
		auth.WriteError(w, "VALIDATION_ERROR", "email is required", http.StatusBadRequest)
		return
	}
	if _, err := netmail.ParseAddress(email); err != nil {
		auth.WriteError(w, "VALIDATION_ERROR", "invalid email address", http.StatusBadRequest)
		return
	}

	if _, err := h.Queries.GetUserByEmail(r.Context(), email); err == nil {
		auth.WriteError(w, "conflict", "user already exists", http.StatusConflict)
		return
	} else if !errors.Is(err, sql.ErrNoRows) {
		auth.WriteError(w, "INTERNAL_ERROR", "failed to check email", http.StatusInternalServerError)
		return
	}

	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	if _, err := h.Queries.GetPendingInvitationByEmail(r.Context(), storedb.GetPendingInvitationByEmailParams{
		Email:     email,
		ExpiresAt: now,
	}); err == nil {
		auth.WriteError(w, "conflict", "pending invitation already exists", http.StatusConflict)
		return
	} else if !errors.Is(err, sql.ErrNoRows) {
		auth.WriteError(w, "INTERNAL_ERROR", "failed to check invitations", http.StatusInternalServerError)
		return
	}

	settings, err := h.Queries.GetAppSettings(r.Context())
	if err != nil {
		auth.WriteError(w, "INTERNAL_ERROR", "failed to load settings", http.StatusInternalServerError)
		return
	}

	raw, row, err := h.TokenService.CreateInvitation(r.Context(), email, inviterID, settings.InviteTokenExpiryHours)
	if err != nil {
		auth.WriteError(w, "INTERNAL_ERROR", "failed to create invitation", http.StatusInternalServerError)
		return
	}

	writeInvitationsJSON(w, http.StatusCreated, createInvitationResponse{
		ID:        row.ID,
		Email:     row.Email,
		ExpiresAt: row.ExpiresAt,
		InviteURL: buildActionURL(h.AppPublicURL, "accept-invitation", raw),
	})
}

// Delete handles DELETE /api/v1/users/invitations/{id}.
func (h *InvitationHandlers) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		auth.WriteError(w, "VALIDATION_ERROR", "invitation id is required", http.StatusBadRequest)
		return
	}

	rows, err := h.Queries.DeletePendingInvitation(r.Context(), id)
	if err != nil {
		auth.WriteError(w, "INTERNAL_ERROR", "failed to revoke invitation", http.StatusInternalServerError)
		return
	}
	if rows == 0 {
		auth.WriteError(w, "NOT_FOUND", "invitation not found", http.StatusNotFound)
		return
	}

	writeInvitationsJSON(w, http.StatusOK, map[string]any{})
}

// SendEmail handles POST /api/v1/users/invitations/{id}/send-email.
func (h *InvitationHandlers) SendEmail(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		auth.WriteError(w, "VALIDATION_ERROR", "invitation id is required", http.StatusBadRequest)
		return
	}

	row, err := h.Queries.GetAuthTokenByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			auth.WriteError(w, "NOT_FOUND", "invitation not found", http.StatusNotFound)
			return
		}
		auth.WriteError(w, "INTERNAL_ERROR", "failed to load invitation", http.StatusInternalServerError)
		return
	}
	if row.Kind != auth.TokenKindInvitation || row.UsedAt.Valid {
		auth.WriteError(w, "NOT_FOUND", "invitation not found", http.StatusNotFound)
		return
	}
	if !invitationNotExpired(row.ExpiresAt) {
		auth.WriteError(w, "NOT_FOUND", "invitation not found", http.StatusNotFound)
		return
	}

	settings, err := h.Queries.GetAppSettings(r.Context())
	if err != nil {
		auth.WriteError(w, "INTERNAL_ERROR", "failed to load settings", http.StatusInternalServerError)
		return
	}

	raw, newRow, err := h.TokenService.CreateInvitation(r.Context(), row.Email, nullStringValue(row.InvitedByUserID), settings.InviteTokenExpiryHours)
	if err != nil {
		auth.WriteError(w, "INTERNAL_ERROR", "failed to refresh invitation token", http.StatusInternalServerError)
		return
	}

	inviteURL := buildActionURL(h.AppPublicURL, "accept-invitation", raw)
	msg, err := mail.BuildInvitation(row.Email, mail.InvitationData{ActionURL: inviteURL})
	if err != nil {
		_, _ = h.Queries.DeletePendingInvitation(r.Context(), newRow.ID)
		auth.WriteError(w, "INTERNAL_ERROR", "failed to build email", http.StatusInternalServerError)
		return
	}
	if err := h.Mailer.Send(r.Context(), msg); err != nil {
		_, _ = h.Queries.DeletePendingInvitation(r.Context(), newRow.ID)
		if errors.Is(err, mail.ErrSMTPNotConfigured) {
			auth.WriteError(w, "SERVICE_UNAVAILABLE", "smtp not configured", http.StatusServiceUnavailable)
			return
		}
		auth.WriteError(w, "INTERNAL_ERROR", "failed to send email", http.StatusInternalServerError)
		return
	}

	if _, err := h.Queries.DeletePendingInvitation(r.Context(), id); err != nil {
		auth.WriteError(w, "INTERNAL_ERROR", "failed to replace invitation", http.StatusInternalServerError)
		return
	}

	writeInvitationsJSON(w, http.StatusOK, map[string]any{})
}

func writeInvitationsJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func nullStringToPtr(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	s := v.String
	return &s
}

func nullStringValue(v sql.NullString) string {
	if !v.Valid {
		return ""
	}
	return v.String
}

func invitationNotExpired(expiresAt string) bool {
	expiry, err := time.Parse("2006-01-02T15:04:05.000Z", expiresAt)
	if err != nil {
		return false
	}
	return time.Now().UTC().Before(expiry)
}
