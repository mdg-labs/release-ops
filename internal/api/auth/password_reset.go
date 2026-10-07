package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	netmail "net/mail"
	"strings"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/google/uuid"
	"github.com/mdg-labs/release-ops/internal/mail"
	storedb "github.com/mdg-labs/release-ops/internal/store/db"
)

const passwordResetSendTimeout = 30 * time.Second

const forgotPasswordResponseMessage = "If an account exists for that email, a reset link has been sent."

// PasswordResetHandlers serves forgot-password and reset-password endpoints.
type PasswordResetHandlers struct {
	SessionManager *scs.SessionManager
	Queries        *storedb.Queries
	TokenService   *TokenService
	Mailer         mail.Mailer
	AppPublicURL   string
}

type forgotPasswordRequest struct {
	Email string `json:"email"`
}

type forgotPasswordResponse struct {
	Message string `json:"message"`
}

type resetPasswordRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

// ForgotPassword handles POST /api/v1/auth/forgot-password.
func (h *PasswordResetHandlers) ForgotPassword(w http.ResponseWriter, r *http.Request) {
	if h.TokenService == nil {
		WriteError(w, "INTERNAL_ERROR", "token service not configured", http.StatusInternalServerError)
		return
	}

	var req forgotPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, "VALIDATION_ERROR", "invalid JSON body", http.StatusBadRequest)
		return
	}
	email := NormalizeEmail(req.Email)
	if email == "" {
		WriteError(w, "VALIDATION_ERROR", "email is required", http.StatusBadRequest)
		return
	}
	if _, err := netmail.ParseAddress(email); err != nil {
		WriteError(w, "VALIDATION_ERROR", "invalid email address", http.StatusBadRequest)
		return
	}

	user, err := h.Queries.GetUserByEmail(r.Context(), email)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusOK, forgotPasswordResponse{Message: forgotPasswordResponseMessage})
			return
		}
		WriteError(w, "INTERNAL_ERROR", "failed to process request", http.StatusInternalServerError)
		return
	}

	settings, err := h.Queries.GetAppSettings(r.Context())
	if err != nil {
		WriteError(w, "INTERNAL_ERROR", "failed to load settings", http.StatusInternalServerError)
		return
	}

	raw, err := h.TokenService.CreatePasswordReset(r.Context(), user.Email, settings.PasswordResetTokenExpiryMinutes)
	if err != nil {
		WriteError(w, "INTERNAL_ERROR", "failed to create reset token", http.StatusInternalServerError)
		return
	}

	if h.Mailer != nil {
		msg, err := mail.BuildPasswordReset(user.Email, mail.PasswordResetData{
			ActionURL: buildPasswordResetActionURL(h.AppPublicURL, raw),
		})
		if err != nil {
			WriteError(w, "INTERNAL_ERROR", "failed to build email", http.StatusInternalServerError)
			return
		}
		// Send in the background so neither response time nor an SMTP failure reveals
		// whether the address has an account. Failures are logged server-side only.
		go h.sendResetEmail(context.WithoutCancel(r.Context()), msg)
	}

	writeJSON(w, http.StatusOK, forgotPasswordResponse{Message: forgotPasswordResponseMessage})
}

// ResetPassword handles POST /api/v1/auth/reset-password.
func (h *PasswordResetHandlers) ResetPassword(w http.ResponseWriter, r *http.Request) {
	if h.TokenService == nil {
		WriteError(w, "INTERNAL_ERROR", "token service not configured", http.StatusInternalServerError)
		return
	}

	var req resetPasswordRequest
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

	tokenRow, err := h.TokenService.Validate(r.Context(), req.Token, TokenKindPasswordReset)
	if err != nil {
		if errors.Is(err, ErrTokenInvalid) {
			WriteError(w, "VALIDATION_ERROR", "invalid or expired token", http.StatusBadRequest)
			return
		}
		WriteError(w, "INTERNAL_ERROR", "failed to validate token", http.StatusInternalServerError)
		return
	}
	// Consume before any side effect so a concurrent request with the same token fails.
	if !consumeOneTimeToken(w, r, h.TokenService, tokenRow.ID) {
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

	hash, err := HashPassword(req.Password)
	if err != nil {
		WriteError(w, "INTERNAL_ERROR", "failed to hash password", http.StatusInternalServerError)
		return
	}

	updated, err := h.Queries.UpdateUserPassword(r.Context(), storedb.UpdateUserPasswordParams{
		PasswordHash: hash,
		UpdatedAt:    nowUTC(),
		ID:           user.ID,
	})
	if err != nil {
		WriteError(w, "INTERNAL_ERROR", "failed to update password", http.StatusInternalServerError)
		return
	}

	// A reset usually follows a compromise: sign out every existing session of this user.
	if err := h.Queries.DeleteSessionsByUserID(r.Context(), updated.ID); err != nil {
		WriteError(w, "INTERNAL_ERROR", "failed to revoke sessions", http.StatusInternalServerError)
		return
	}
	if err := StartUserSession(r.Context(), h.SessionManager, updated.ID, updated.Email); err != nil {
		WriteError(w, "INTERNAL_ERROR", "failed to start session", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, sessionResponse{
		User: &userResponse{ID: updated.ID, Email: updated.Email},
	})
}

func (h *PasswordResetHandlers) sendResetEmail(ctx context.Context, msg mail.Message) {
	ctx, cancel := context.WithTimeout(ctx, passwordResetSendTimeout)
	defer cancel()
	if err := h.Mailer.Send(ctx, msg); err != nil && !errors.Is(err, mail.ErrSMTPNotConfigured) {
		slog.Error("send password reset email", "error", err)
	}
}

// CreatePasswordReset invalidates prior reset tokens and stores a new one.
func (s *TokenService) CreatePasswordReset(ctx context.Context, email string, expiryMinutes int64) (string, error) {
	raw, err := GenerateRawToken()
	if err != nil {
		return "", err
	}

	now := nowUTC()
	if err := s.Queries.InvalidatePasswordResetTokensForEmail(ctx, storedb.InvalidatePasswordResetTokensForEmailParams{
		UsedAt: sql.NullString{String: now, Valid: true},
		Email:  email,
	}); err != nil {
		return "", err
	}

	expiresAt := time.Now().UTC().Add(time.Duration(expiryMinutes) * time.Minute).Format(isoTimeFormat)
	_, err = s.Queries.CreateAuthToken(ctx, storedb.CreateAuthTokenParams{
		ID:              uuid.NewString(),
		Kind:            TokenKindPasswordReset,
		Email:           email,
		TokenHash:       HashToken(raw),
		NewEmail:        sql.NullString{},
		InvitedByUserID: sql.NullString{},
		ExpiresAt:       expiresAt,
		CreatedAt:       now,
	})
	if err != nil {
		return "", err
	}
	return raw, nil
}

func buildPasswordResetActionURL(appPublicURL, token string) string {
	base := strings.TrimRight(strings.TrimSpace(appPublicURL), "/")
	if base == "" {
		base = "http://localhost:3000"
	}
	return base + "/reset-password?token=" + token
}
