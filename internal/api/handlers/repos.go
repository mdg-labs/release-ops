package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/mdg-labs/release-ops/internal/api/auth"
	"github.com/mdg-labs/release-ops/internal/store"
)

var validSourceKinds = map[string]struct{}{
	"github":   {},
	"gitlab":   {},
	"gitea":    {},
	"forgejo":  {},
	"codeberg": {},
}

var sourceKindsRequiringIntegration = map[string]struct{}{
	"gitlab":  {},
	"gitea":   {},
	"forgejo": {},
}

// RepoHandlers serves monitored repo HTTP endpoints.
type RepoHandlers struct {
	Repos store.MonitoredRepoRepository
}

type repoResponse struct {
	ID                    string   `json:"id"`
	SourceKind            string   `json:"sourceKind"`
	ProjectPath           string   `json:"projectPath"`
	Enabled               bool     `json:"enabled"`
	SourceIntegrationID   *string  `json:"sourceIntegrationId"`
	TicketProjectID       string   `json:"ticketProjectId"`
	NotificationTargetIDs []string `json:"notificationTargetIds"`
	OpenTicketExternalID  *string  `json:"openTicketExternalId"`
	OpenTicketTag         *string  `json:"openTicketTag"`
	LastKnownTag          *string  `json:"lastKnownTag"`
	LastPolledAt          *string  `json:"lastPolledAt"`
	LastError             *string  `json:"lastError"`
	CreatedAt             string   `json:"createdAt"`
	UpdatedAt             string   `json:"updatedAt"`
}

type createRepoRequest struct {
	SourceKind            string   `json:"sourceKind"`
	ProjectPath           string   `json:"projectPath"`
	Enabled               *bool    `json:"enabled"`
	SourceIntegrationID   *string  `json:"sourceIntegrationId"`
	TicketProjectID       string   `json:"ticketProjectId"`
	NotificationTargetIDs []string `json:"notificationTargetIds"`
}

type patchRepoRequest struct {
	SourceKind            string   `json:"sourceKind"`
	ProjectPath           string   `json:"projectPath"`
	Enabled               bool     `json:"enabled"`
	SourceIntegrationID   *string  `json:"sourceIntegrationId"`
	TicketProjectID       string   `json:"ticketProjectId"`
	NotificationTargetIDs []string `json:"notificationTargetIds"`
}

// List handles GET /api/v1/repos.
func (h *RepoHandlers) List(w http.ResponseWriter, r *http.Request) {
	items, err := h.Repos.List(r.Context())
	if err != nil {
		auth.WriteError(w, "INTERNAL_ERROR", "failed to list repos", http.StatusInternalServerError)
		return
	}

	resp := make([]repoResponse, len(items))
	for i := range items {
		resp[i] = repoFromStore(&items[i])
	}

	writeReposJSON(w, http.StatusOK, resp)
}

// Create handles POST /api/v1/repos.
func (h *RepoHandlers) Create(w http.ResponseWriter, r *http.Request) {
	var req createRepoRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		auth.WriteError(w, "VALIDATION_ERROR", "invalid JSON body", http.StatusBadRequest)
		return
	}

	if err := validateRepoSourceKind(req.SourceKind); err != nil {
		auth.WriteError(w, "VALIDATION_ERROR", err.Error(), http.StatusBadRequest)
		return
	}
	if req.ProjectPath == "" {
		auth.WriteError(w, "VALIDATION_ERROR", "projectPath is required", http.StatusBadRequest)
		return
	}
	if req.TicketProjectID == "" {
		auth.WriteError(w, "VALIDATION_ERROR", "ticketProjectId is required", http.StatusBadRequest)
		return
	}
	if err := validateSourceIntegrationID(req.SourceKind, req.SourceIntegrationID); err != nil {
		auth.WriteError(w, "VALIDATION_ERROR", err.Error(), http.StatusBadRequest)
		return
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	created, err := h.Repos.Create(r.Context(), store.CreateMonitoredRepoInput{
		SourceKind:            req.SourceKind,
		ProjectPath:           req.ProjectPath,
		Enabled:               enabled,
		SourceIntegrationID:   req.SourceIntegrationID,
		TicketProjectID:       req.TicketProjectID,
		NotificationTargetIDs: req.NotificationTargetIDs,
	})
	if err != nil {
		if isUniqueConstraintError(err) {
			auth.WriteError(w, "CONFLICT", "repo already exists for this source kind and project path", http.StatusConflict)
			return
		}
		auth.WriteError(w, "INTERNAL_ERROR", "failed to create repo", http.StatusInternalServerError)
		return
	}

	writeReposJSON(w, http.StatusCreated, repoFromStore(created))
}

// Patch handles PATCH /api/v1/repos/{id}.
func (h *RepoHandlers) Patch(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		auth.WriteError(w, "VALIDATION_ERROR", "repo id is required", http.StatusBadRequest)
		return
	}

	var req patchRepoRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		auth.WriteError(w, "VALIDATION_ERROR", "invalid JSON body", http.StatusBadRequest)
		return
	}

	if err := validateRepoSourceKind(req.SourceKind); err != nil {
		auth.WriteError(w, "VALIDATION_ERROR", err.Error(), http.StatusBadRequest)
		return
	}
	if req.ProjectPath == "" {
		auth.WriteError(w, "VALIDATION_ERROR", "projectPath is required", http.StatusBadRequest)
		return
	}
	if req.TicketProjectID == "" {
		auth.WriteError(w, "VALIDATION_ERROR", "ticketProjectId is required", http.StatusBadRequest)
		return
	}
	if err := validateSourceIntegrationID(req.SourceKind, req.SourceIntegrationID); err != nil {
		auth.WriteError(w, "VALIDATION_ERROR", err.Error(), http.StatusBadRequest)
		return
	}

	if _, err := h.Repos.Get(r.Context(), id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			auth.WriteError(w, "NOT_FOUND", "repo not found", http.StatusNotFound)
			return
		}
		auth.WriteError(w, "INTERNAL_ERROR", "failed to load repo", http.StatusInternalServerError)
		return
	}

	updated, err := h.Repos.Update(r.Context(), id, store.UpdateMonitoredRepoInput{
		SourceKind:            req.SourceKind,
		ProjectPath:           req.ProjectPath,
		Enabled:               req.Enabled,
		SourceIntegrationID:   req.SourceIntegrationID,
		TicketProjectID:       req.TicketProjectID,
		NotificationTargetIDs: req.NotificationTargetIDs,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			auth.WriteError(w, "NOT_FOUND", "repo not found", http.StatusNotFound)
			return
		}
		if isUniqueConstraintError(err) {
			auth.WriteError(w, "CONFLICT", "repo already exists for this source kind and project path", http.StatusConflict)
			return
		}
		auth.WriteError(w, "INTERNAL_ERROR", "failed to update repo", http.StatusInternalServerError)
		return
	}

	writeReposJSON(w, http.StatusOK, repoFromStore(updated))
}

// Delete handles DELETE /api/v1/repos/{id}.
func (h *RepoHandlers) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		auth.WriteError(w, "VALIDATION_ERROR", "repo id is required", http.StatusBadRequest)
		return
	}

	if _, err := h.Repos.Get(r.Context(), id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			auth.WriteError(w, "NOT_FOUND", "repo not found", http.StatusNotFound)
			return
		}
		auth.WriteError(w, "INTERNAL_ERROR", "failed to load repo", http.StatusInternalServerError)
		return
	}

	if err := h.Repos.Delete(r.Context(), id); err != nil {
		auth.WriteError(w, "INTERNAL_ERROR", "failed to delete repo", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func repoFromStore(item *store.MonitoredRepo) repoResponse {
	notificationTargetIDs := item.NotificationTargetIDs
	if notificationTargetIDs == nil {
		notificationTargetIDs = []string{}
	}
	return repoResponse{
		ID:                    item.ID,
		SourceKind:            item.SourceKind,
		ProjectPath:           item.ProjectPath,
		Enabled:               item.Enabled,
		SourceIntegrationID:   item.SourceIntegrationID,
		TicketProjectID:       item.TicketProjectID,
		NotificationTargetIDs: notificationTargetIDs,
		OpenTicketExternalID:  item.OpenTicketExternalID,
		OpenTicketTag:         item.OpenTicketTag,
		LastKnownTag:          item.LastKnownTag,
		LastPolledAt:          item.LastPolledAt,
		LastError:             item.LastError,
		CreatedAt:             item.CreatedAt,
		UpdatedAt:             item.UpdatedAt,
	}
}

func validateRepoSourceKind(kind string) error {
	if _, ok := validSourceKinds[kind]; !ok {
		return errors.New("sourceKind must be one of: github, gitlab, gitea, forgejo, codeberg")
	}
	return nil
}

func validateSourceIntegrationID(sourceKind string, sourceIntegrationID *string) error {
	_, requiresIntegration := sourceKindsRequiringIntegration[sourceKind]
	hasIntegration := sourceIntegrationID != nil && *sourceIntegrationID != ""
	if requiresIntegration && !hasIntegration {
		return errors.New("sourceIntegrationId is required for this source kind")
	}
	return nil
}

func writeReposJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
