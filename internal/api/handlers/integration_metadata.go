package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/mdg-labs/release-ops/internal/api/auth"
	"github.com/mdg-labs/release-ops/internal/providers/ticket/metadata"
	"github.com/mdg-labs/release-ops/internal/store"
)

var ticketIntegrationKinds = map[string]struct{}{
	"phasical": {},
	"jira":     {},
	"linear":   {},
}

// IntegrationMetadataHandlers serves ticket-metadata discovery endpoints.
type IntegrationMetadataHandlers struct {
	Integrations store.IntegrationRepository
}

// ListWorkspaces handles GET /api/v1/integrations/{id}/ticket-metadata/workspaces.
func (h *IntegrationMetadataHandlers) ListWorkspaces(w http.ResponseWriter, r *http.Request) {
	h.serveMetadata(w, r, func(ctx context.Context, provider metadata.Provider) metadata.ListResponse {
		items, err := provider.ListWorkspaces(ctx)
		return metadataResult(items, err)
	})
}

// ListProjects handles GET /api/v1/integrations/{id}/ticket-metadata/projects.
func (h *IntegrationMetadataHandlers) ListProjects(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.URL.Query().Get("workspaceId")
	h.serveMetadata(w, r, func(ctx context.Context, provider metadata.Provider) metadata.ListResponse {
		items, err := provider.ListProjects(ctx, workspaceID)
		return metadataResult(items, err)
	})
}

// ListStatuses handles GET /api/v1/integrations/{id}/ticket-metadata/statuses.
func (h *IntegrationMetadataHandlers) ListStatuses(w http.ResponseWriter, r *http.Request) {
	externalProjectID := r.URL.Query().Get("externalProjectId")
	h.serveMetadata(w, r, func(ctx context.Context, provider metadata.Provider) metadata.ListResponse {
		items, err := provider.ListStatuses(ctx, externalProjectID)
		return metadataResult(items, err)
	})
}

// ListPriorities handles GET /api/v1/integrations/{id}/ticket-metadata/priorities.
func (h *IntegrationMetadataHandlers) ListPriorities(w http.ResponseWriter, r *http.Request) {
	externalProjectID := r.URL.Query().Get("externalProjectId")
	h.serveMetadata(w, r, func(ctx context.Context, provider metadata.Provider) metadata.ListResponse {
		items, err := provider.ListPriorities(ctx, externalProjectID)
		return metadataResult(items, err)
	})
}

// ListIssueTypes handles GET /api/v1/integrations/{id}/ticket-metadata/issue-types.
func (h *IntegrationMetadataHandlers) ListIssueTypes(w http.ResponseWriter, r *http.Request) {
	externalProjectID := r.URL.Query().Get("externalProjectId")
	h.serveMetadata(w, r, func(ctx context.Context, provider metadata.Provider) metadata.ListResponse {
		items, err := provider.ListIssueTypes(ctx, externalProjectID)
		return metadataResult(items, err)
	})
}

type metadataFetch func(ctx context.Context, provider metadata.Provider) metadata.ListResponse

func (h *IntegrationMetadataHandlers) serveMetadata(
	w http.ResponseWriter,
	r *http.Request,
	fetch metadataFetch,
) {
	id := chi.URLParam(r, "id")
	if id == "" {
		auth.WriteError(w, "VALIDATION_ERROR", "integration id is required", http.StatusBadRequest)
		return
	}

	integration, err := h.Integrations.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			auth.WriteError(w, "NOT_FOUND", "integration not found", http.StatusNotFound)
			return
		}
		auth.WriteError(w, "INTERNAL_ERROR", "failed to load integration", http.StatusInternalServerError)
		return
	}
	if _, ok := ticketIntegrationKinds[integration.Kind]; !ok {
		auth.WriteError(w, "VALIDATION_ERROR", "integration kind does not support ticket metadata", http.StatusBadRequest)
		return
	}

	secret, err := h.Integrations.DecryptPayload(r.Context(), id)
	if err != nil {
		auth.WriteError(w, "INTERNAL_ERROR", "failed to decrypt integration credentials", http.StatusInternalServerError)
		return
	}

	provider, err := metadata.NewProvider(integration.Kind, integration.BaseURL, secret, nil)
	if err != nil {
		writeMetadataJSON(w, http.StatusOK, metadata.ListResponse{Message: err.Error()})
		return
	}

	writeMetadataJSON(w, http.StatusOK, fetch(r.Context(), provider))
}

func metadataResult(items []metadata.Item, err error) metadata.ListResponse {
	if err != nil {
		if errors.Is(err, metadata.ErrUnsupported) {
			return metadata.ListResponse{Items: []metadata.Item{}}
		}
		return metadata.ListResponse{Message: err.Error()}
	}
	if items == nil {
		items = []metadata.Item{}
	}
	return metadata.ListResponse{Items: items}
}

func writeMetadataJSON(w http.ResponseWriter, status int, payload metadata.ListResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
