package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/mdg-labs/release-ops/internal/api/auth"
	"github.com/mdg-labs/release-ops/internal/store"
)

var validOnOpenTicketPolicies = map[string]struct{}{
	"supersede":    {},
	"merge":        {},
	"skip_if_open": {},
}

const defaultContentTemplatesJSON = `{"title":"","description":"","supersedeComment":""}`

var requiredContentTemplateKeys = []string{"title", "description", "supersedeComment"}

// TicketProjectHandlers serves ticket project HTTP endpoints.
type TicketProjectHandlers struct {
	TicketProjects store.TicketProjectRepository
}

type contentTemplatesResponse struct {
	Title            string `json:"title"`
	Description      string `json:"description"`
	SupersedeComment string `json:"supersedeComment"`
}

type ticketProjectResponse struct {
	ID                 string                  `json:"id"`
	IntegrationID      string                  `json:"integrationId"`
	ExternalProjectID  string                  `json:"externalProjectId"`
	Name               string                  `json:"name"`
	CreateConfig       json.RawMessage         `json:"createConfig"`
	StatusMapping      json.RawMessage         `json:"statusMapping"`
	ContentTemplates   contentTemplatesResponse `json:"contentTemplates"`
	OnOpenTicketPolicy string                  `json:"onOpenTicketPolicy"`
	CreatedAt          string                  `json:"createdAt"`
	UpdatedAt          string                  `json:"updatedAt"`
}

type createTicketProjectRequest struct {
	IntegrationID      string          `json:"integrationId"`
	ExternalProjectID  string          `json:"externalProjectId"`
	Name               string          `json:"name"`
	CreateConfig       json.RawMessage `json:"createConfig"`
	StatusMapping      json.RawMessage `json:"statusMapping"`
	ContentTemplates   json.RawMessage `json:"contentTemplates"`
	OnOpenTicketPolicy string          `json:"onOpenTicketPolicy"`
}

type patchTicketProjectRequest struct {
	Name               string          `json:"name"`
	CreateConfig       json.RawMessage `json:"createConfig"`
	StatusMapping      json.RawMessage `json:"statusMapping"`
	ContentTemplates   json.RawMessage `json:"contentTemplates"`
	OnOpenTicketPolicy string          `json:"onOpenTicketPolicy"`
}

// List handles GET /api/v1/ticket-projects.
func (h *TicketProjectHandlers) List(w http.ResponseWriter, r *http.Request) {
	integrationID := r.URL.Query().Get("integrationId")

	var (
		items []store.TicketProject
		err   error
	)
	if integrationID != "" {
		items, err = h.TicketProjects.ListByIntegration(r.Context(), integrationID)
	} else {
		items, err = h.TicketProjects.List(r.Context())
	}
	if err != nil {
		auth.WriteError(w, "INTERNAL_ERROR", "failed to list ticket projects", http.StatusInternalServerError)
		return
	}

	resp := make([]ticketProjectResponse, len(items))
	for i := range items {
		resp[i] = ticketProjectFromStore(&items[i])
	}

	writeTicketProjectsJSON(w, http.StatusOK, resp)
}

// Create handles POST /api/v1/ticket-projects.
func (h *TicketProjectHandlers) Create(w http.ResponseWriter, r *http.Request) {
	var req createTicketProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		auth.WriteError(w, "VALIDATION_ERROR", "invalid JSON body", http.StatusBadRequest)
		return
	}

	if req.IntegrationID == "" {
		auth.WriteError(w, "VALIDATION_ERROR", "integrationId is required", http.StatusBadRequest)
		return
	}
	if req.ExternalProjectID == "" {
		auth.WriteError(w, "VALIDATION_ERROR", "externalProjectId is required", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		auth.WriteError(w, "VALIDATION_ERROR", "name is required", http.StatusBadRequest)
		return
	}

	createConfig, err := validateJSONField(req.CreateConfig, "createConfig")
	if err != nil {
		auth.WriteError(w, "VALIDATION_ERROR", err.Error(), http.StatusBadRequest)
		return
	}
	statusMapping, err := validateJSONField(req.StatusMapping, "statusMapping")
	if err != nil {
		auth.WriteError(w, "VALIDATION_ERROR", err.Error(), http.StatusBadRequest)
		return
	}
	contentTemplates, err := validateContentTemplates(req.ContentTemplates, true)
	if err != nil {
		auth.WriteError(w, "VALIDATION_ERROR", err.Error(), http.StatusBadRequest)
		return
	}

	policy := req.OnOpenTicketPolicy
	if policy == "" {
		policy = "supersede"
	}
	if err := validateOnOpenTicketPolicy(policy); err != nil {
		auth.WriteError(w, "VALIDATION_ERROR", err.Error(), http.StatusBadRequest)
		return
	}

	created, err := h.TicketProjects.Create(r.Context(), store.CreateTicketProjectInput{
		IntegrationID:      req.IntegrationID,
		ExternalProjectID:  req.ExternalProjectID,
		Name:               req.Name,
		CreateConfig:       createConfig,
		StatusMapping:      statusMapping,
		ContentTemplates:   contentTemplates,
		OnOpenTicketPolicy: policy,
	})
	if err != nil {
		if isUniqueConstraintError(err) {
			auth.WriteError(w, "CONFLICT", "ticket project already exists for this integration and external project", http.StatusConflict)
			return
		}
		auth.WriteError(w, "INTERNAL_ERROR", "failed to create ticket project", http.StatusInternalServerError)
		return
	}

	writeTicketProjectsJSON(w, http.StatusCreated, ticketProjectFromStore(created))
}

// Patch handles PATCH /api/v1/ticket-projects/{id}.
func (h *TicketProjectHandlers) Patch(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		auth.WriteError(w, "VALIDATION_ERROR", "ticket project id is required", http.StatusBadRequest)
		return
	}

	var req patchTicketProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		auth.WriteError(w, "VALIDATION_ERROR", "invalid JSON body", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		auth.WriteError(w, "VALIDATION_ERROR", "name is required", http.StatusBadRequest)
		return
	}

	existing, err := h.TicketProjects.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			auth.WriteError(w, "NOT_FOUND", "ticket project not found", http.StatusNotFound)
			return
		}
		auth.WriteError(w, "INTERNAL_ERROR", "failed to load ticket project", http.StatusInternalServerError)
		return
	}

	createConfig, err := validateJSONField(req.CreateConfig, "createConfig")
	if err != nil {
		auth.WriteError(w, "VALIDATION_ERROR", err.Error(), http.StatusBadRequest)
		return
	}
	statusMapping, err := validateJSONField(req.StatusMapping, "statusMapping")
	if err != nil {
		auth.WriteError(w, "VALIDATION_ERROR", err.Error(), http.StatusBadRequest)
		return
	}

	contentTemplates := existing.ContentTemplates
	if len(req.ContentTemplates) > 0 {
		contentTemplates, err = mergeContentTemplates(existing.ContentTemplates, req.ContentTemplates)
		if err != nil {
			auth.WriteError(w, "VALIDATION_ERROR", err.Error(), http.StatusBadRequest)
			return
		}
	}

	if req.OnOpenTicketPolicy == "" {
		auth.WriteError(w, "VALIDATION_ERROR", "onOpenTicketPolicy is required", http.StatusBadRequest)
		return
	}
	if err := validateOnOpenTicketPolicy(req.OnOpenTicketPolicy); err != nil {
		auth.WriteError(w, "VALIDATION_ERROR", err.Error(), http.StatusBadRequest)
		return
	}

	updated, err := h.TicketProjects.Update(r.Context(), id, store.UpdateTicketProjectInput{
		Name:               req.Name,
		CreateConfig:       createConfig,
		StatusMapping:      statusMapping,
		ContentTemplates:   contentTemplates,
		OnOpenTicketPolicy: req.OnOpenTicketPolicy,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			auth.WriteError(w, "NOT_FOUND", "ticket project not found", http.StatusNotFound)
			return
		}
		auth.WriteError(w, "INTERNAL_ERROR", "failed to update ticket project", http.StatusInternalServerError)
		return
	}

	writeTicketProjectsJSON(w, http.StatusOK, ticketProjectFromStore(updated))
}

// Delete handles DELETE /api/v1/ticket-projects/{id}.
func (h *TicketProjectHandlers) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		auth.WriteError(w, "VALIDATION_ERROR", "ticket project id is required", http.StatusBadRequest)
		return
	}

	if _, err := h.TicketProjects.Get(r.Context(), id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			auth.WriteError(w, "NOT_FOUND", "ticket project not found", http.StatusNotFound)
			return
		}
		auth.WriteError(w, "INTERNAL_ERROR", "failed to load ticket project", http.StatusInternalServerError)
		return
	}

	refs, err := h.TicketProjects.CountMonitoredRepos(r.Context(), id)
	if err != nil {
		auth.WriteError(w, "INTERNAL_ERROR", "failed to check ticket project references", http.StatusInternalServerError)
		return
	}
	if refs > 0 {
		auth.WriteError(w, "CONFLICT", "ticket project is referenced by monitored repos", http.StatusConflict)
		return
	}

	if err := h.TicketProjects.Delete(r.Context(), id); err != nil {
		auth.WriteError(w, "INTERNAL_ERROR", "failed to delete ticket project", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func ticketProjectFromStore(item *store.TicketProject) ticketProjectResponse {
	contentTemplates := contentTemplatesResponse{}
	raw := item.ContentTemplates
	if raw == "" {
		raw = defaultContentTemplatesJSON
	}
	_ = json.Unmarshal([]byte(raw), &contentTemplates)

	return ticketProjectResponse{
		ID:                 item.ID,
		IntegrationID:      item.IntegrationID,
		ExternalProjectID:  item.ExternalProjectID,
		Name:               item.Name,
		CreateConfig:       json.RawMessage(item.CreateConfig),
		StatusMapping:      json.RawMessage(item.StatusMapping),
		ContentTemplates:   contentTemplates,
		OnOpenTicketPolicy: item.OnOpenTicketPolicy,
		CreatedAt:          item.CreatedAt,
		UpdatedAt:          item.UpdatedAt,
	}
}

func validateJSONField(raw json.RawMessage, fieldName string) (string, error) {
	if len(raw) == 0 {
		return "", errors.New(fieldName + " is required")
	}
	if !json.Valid(raw) {
		return "", errors.New(fieldName + " must be valid JSON")
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return "", errors.New(fieldName + " must be a JSON object")
	}
	return string(raw), nil
}

func validateContentTemplates(raw json.RawMessage, requireAllKeys bool) (string, error) {
	if len(raw) == 0 {
		return "", errors.New("contentTemplates is required")
	}
	if !json.Valid(raw) {
		return "", errors.New("contentTemplates must be valid JSON")
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return "", errors.New("contentTemplates must be a JSON object")
	}

	if requireAllKeys {
		for _, key := range requiredContentTemplateKeys {
			if _, ok := obj[key]; !ok {
				return "", errors.New("contentTemplates." + key + " is required")
			}
		}
	}

	for key, value := range obj {
		if !isContentTemplateKey(key) {
			continue
		}
		if _, err := parseContentTemplateStringValue(key, value); err != nil {
			return "", err
		}
	}

	if requireAllKeys {
		return string(raw), nil
	}

	merged, err := mergeContentTemplates(defaultContentTemplatesJSON, raw)
	if err != nil {
		return "", err
	}
	return merged, nil
}

func mergeContentTemplates(existingJSON string, patch json.RawMessage) (string, error) {
	base := contentTemplatesResponse{}
	if existingJSON == "" {
		existingJSON = defaultContentTemplatesJSON
	}
	if err := json.Unmarshal([]byte(existingJSON), &base); err != nil {
		return "", errors.New("contentTemplates must be valid JSON")
	}

	patchObj := map[string]json.RawMessage{}
	if err := json.Unmarshal(patch, &patchObj); err != nil {
		return "", errors.New("contentTemplates must be a JSON object")
	}

	for key, value := range patchObj {
		if !isContentTemplateKey(key) {
			continue
		}
		strVal, err := parseContentTemplateStringValue(key, value)
		if err != nil {
			return "", err
		}
		switch key {
		case "title":
			base.Title = strVal
		case "description":
			base.Description = strVal
		case "supersedeComment":
			base.SupersedeComment = strVal
		}
	}

	out, err := json.Marshal(base)
	if err != nil {
		return "", errors.New("contentTemplates must be valid JSON")
	}
	return string(out), nil
}

func isContentTemplateKey(key string) bool {
	switch key {
	case "title", "description", "supersedeComment":
		return true
	default:
		return false
	}
}

func parseContentTemplateStringValue(key string, raw json.RawMessage) (string, error) {
	var str string
	if err := json.Unmarshal(raw, &str); err != nil {
		return "", errors.New("contentTemplates." + key + " must be a string")
	}
	return str, nil
}

func validateOnOpenTicketPolicy(policy string) error {
	if _, ok := validOnOpenTicketPolicies[policy]; !ok {
		return errors.New("onOpenTicketPolicy must be one of: supersede, merge, skip_if_open")
	}
	return nil
}

func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "UNIQUE")
}

func writeTicketProjectsJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
