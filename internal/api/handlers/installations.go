package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sort"

	"github.com/go-chi/chi/v5"
	"github.com/mdg-labs/release-ops/internal/api/auth"
	apimw "github.com/mdg-labs/release-ops/internal/api/middleware"
	"github.com/mdg-labs/release-ops/internal/store"
)

// maxInstallationPatchBytes bounds the body of an installation PATCH, which holds one id.
const maxInstallationPatchBytes = 4 << 10

// InstallationHandlers serves the installation endpoints of specs.html section 7.2. They sit
// behind RequireAdmin, and each handler checks the role itself as well, so a route that is
// moved by mistake does not open them to a viewer.
type InstallationHandlers struct {
	Installations store.InstallationSettingsRepository
	// RequiredPermissions is what the App's manifest requests; a permission an installation
	// lacks or holds at a lower level is listed in its missingPermissions.
	RequiredPermissions map[string]string
}

type installationResponse struct {
	ID                     string          `json:"id"`
	AccountLogin           string          `json:"accountLogin"`
	AccountType            string          `json:"accountType"`
	Status                 string          `json:"status"`
	RepositorySelection    string          `json:"repositorySelection"`
	RepositoryCount        int64           `json:"repositoryCount"`
	Permissions            json.RawMessage `json:"permissions"`
	MissingPermissions     []string        `json:"missingPermissions"`
	RateLimit              *rateLimit      `json:"rateLimit"`
	DefaultTicketProjectID *string         `json:"defaultTicketProjectId"`
}

type rateLimit struct {
	Remaining *int64  `json:"remaining"`
	Limit     *int64  `json:"limit"`
	ResetAt   *string `json:"resetAt"`
}

func newInstallationResponse(i store.InstallationSummary, required map[string]string) installationResponse {
	out := installationResponse{
		ID: i.ID, AccountLogin: i.AccountLogin, AccountType: i.AccountType, Status: i.Status, RepositorySelection: i.RepositorySelection,
		RepositoryCount: i.RepositoryCount, Permissions: json.RawMessage(i.Permissions), DefaultTicketProjectID: emptyToNil(i.DefaultTicketProjectID),
	}
	if !json.Valid(out.Permissions) {
		out.Permissions = json.RawMessage(`{}`)
	}
	out.MissingPermissions = missingPermissions(out.Permissions, required)
	if i.RateLimitRemaining != nil || i.RateLimitLimit != nil || i.RateLimitResetAt != nil {
		out.RateLimit = &rateLimit{Remaining: i.RateLimitRemaining, Limit: i.RateLimitLimit, ResetAt: i.RateLimitResetAt}
	}
	return out
}

// permissionRank orders the levels GitHub grants; an unknown level ranks below read.
var permissionRank = map[string]int{"read": 1, "write": 2, "admin": 3}

// missingPermissions lists, sorted, the required permissions the granted object lacks or holds
// at a lower level. It is never nil, so the field is an empty array when nothing is missing.
func missingPermissions(granted json.RawMessage, required map[string]string) []string {
	var have map[string]string
	_ = json.Unmarshal(granted, &have)
	missing := []string{}
	for name, level := range required {
		if permissionRank[have[name]] < permissionRank[level] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	return missing
}

// List is GET /installations: the live installation and a removed one kept for history.
func (h *InstallationHandlers) List(w http.ResponseWriter, r *http.Request) {
	if !requireAdminRole(w, r) {
		return
	}
	items, err := h.Installations.List(r.Context())
	if err != nil {
		slog.Error("installations: listing failed", "err", err)
		auth.WriteError(w, "INTERNAL_ERROR", "failed to list installations", http.StatusInternalServerError)
		return
	}
	out := make([]installationResponse, 0, len(items))
	for _, i := range items {
		out = append(out, newInstallationResponse(i, h.RequiredPermissions))
	}
	writeRepositoryJSON(w, http.StatusOK, map[string]any{"items": out})
}

// Patch is PATCH /installations/{id}: set the installation's default ticket project to a ticket
// project id, or clear it with null. It applies to tickets created afterwards. An unknown
// installation is 404 and an unknown ticket project 422.
func (h *InstallationHandlers) Patch(w http.ResponseWriter, r *http.Request) {
	if !requireAdminRole(w, r) {
		return
	}
	id := chi.URLParam(r, "id")
	var body map[string]json.RawMessage
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxInstallationPatchBytes)).Decode(&body); err != nil {
		auth.WriteError(w, "VALIDATION_ERROR", "invalid JSON body", http.StatusBadRequest)
		return
	}
	for key := range body {
		if key != "defaultTicketProjectId" {
			auth.WriteError(w, "VALIDATION_ERROR", "unknown field "+key, http.StatusBadRequest)
			return
		}
	}
	raw, present := body["defaultTicketProjectId"]
	if !present {
		auth.WriteError(w, "VALIDATION_ERROR", "defaultTicketProjectId is required", http.StatusBadRequest)
		return
	}
	projectID, ok := ticketProjectIDOf(raw)
	if !ok {
		auth.WriteError(w, "VALIDATION_ERROR", "defaultTicketProjectId must be a ticket project id or null", http.StatusBadRequest)
		return
	}

	switch err := h.Installations.SetDefaultTicketProject(r.Context(), id, projectID); {
	case err == nil:
	case errors.Is(err, store.ErrInstallationNotFound):
		auth.WriteError(w, "NOT_FOUND", "installation not found", http.StatusNotFound)
		return
	case errors.Is(err, store.ErrTicketProjectNotFound):
		auth.WriteError(w, "VALIDATION_ERROR", "defaultTicketProjectId names no ticket project", http.StatusUnprocessableEntity)
		return
	default:
		slog.Error("installations: setting the default ticket project failed", "installation", id, "err", err)
		auth.WriteError(w, "INTERNAL_ERROR", "failed to update the installation", http.StatusInternalServerError)
		return
	}

	items, err := h.Installations.List(r.Context())
	if err != nil {
		slog.Error("installations: reading back failed", "installation", id, "err", err)
		auth.WriteError(w, "INTERNAL_ERROR", "failed to read the installation", http.StatusInternalServerError)
		return
	}
	for _, i := range items {
		if i.ID == id {
			writeRepositoryJSON(w, http.StatusOK, newInstallationResponse(i, h.RequiredPermissions))
			return
		}
	}
	auth.WriteError(w, "NOT_FOUND", "installation not found", http.StatusNotFound)
}

// requireAdminRole answers 403 unless the caller's role is admin. The router's RequireAdmin
// already does; the handlers that set ticket project mappings check again.
func requireAdminRole(w http.ResponseWriter, r *http.Request) bool {
	p, ok := apimw.PrincipalFromContext(r.Context())
	if !ok {
		auth.WriteError(w, "unauthorized", "authentication required", http.StatusUnauthorized)
		return false
	}
	if p.Role != auth.RoleAdmin {
		auth.WriteError(w, "FORBIDDEN", "this action needs the admin role", http.StatusForbidden)
		return false
	}
	return true
}

// ticketProjectIDOf reads a ticket project id or null (empty id). Anything else, including an
// empty string, is not one.
func ticketProjectIDOf(raw json.RawMessage) (id string, ok bool) {
	var value *string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", false
	}
	if value == nil {
		return "", true
	}
	return *value, *value != ""
}

func emptyToNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
