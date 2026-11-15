package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/mdg-labs/release-ops/internal/api/auth"
	apimw "github.com/mdg-labs/release-ops/internal/api/middleware"
	"github.com/mdg-labs/release-ops/internal/queue"
	"github.com/mdg-labs/release-ops/internal/store"
)

// maxRepositoryPatchBytes bounds the body of a repository PATCH, which holds a flag and an id.
const maxRepositoryPatchBytes = 4 << 10

const (
	defaultRepositoryPageSize = 25
	maxRepositoryPageSize     = 100
)

// RepositoryHandlers serves the admin actions on a repository: the list with each one's ticket
// project, run now, and pause, enable or set the ticket-project override. The routes sit behind
// RequireAdmin and the ones that read or set a ticket project check the role again; the scope
// check is the next line, so a repository outside the caller's scope or the installation
// answers 404, never 403.
type RepositoryHandlers struct {
	Repositories store.RepositoryRepository
	Queue        queue.Queue
}

type repositoryResponse struct {
	ID            string `json:"id"`
	FullName      string `json:"fullName"`
	DefaultBranch string `json:"defaultBranch"`
	Enabled       bool   `json:"enabled"`
	ConfigState   string `json:"configState"`
	// TicketProjectID is the repository's override; the effective fields are the project its
	// PRs get tickets in. All are null when none is set, which means no tickets (specs.html
	// section 5.2).
	TicketProjectID            *string `json:"ticketProjectId"`
	EffectiveTicketProjectID   *string `json:"effectiveTicketProjectId"`
	EffectiveTicketProjectName *string `json:"effectiveTicketProjectName"`
}

func newRepositoryResponse(r store.RepositoryListing) repositoryResponse {
	return repositoryResponse{
		ID:                         r.ID,
		FullName:                   r.FullName,
		DefaultBranch:              r.DefaultBranch,
		Enabled:                    r.Enabled,
		ConfigState:                r.ConfigState,
		TicketProjectID:            emptyToNil(r.TicketProjectID),
		EffectiveTicketProjectID:   emptyToNil(r.EffectiveTicketProjectID),
		EffectiveTicketProjectName: emptyToNil(r.EffectiveTicketProjectName),
	}
}

// List is GET /repositories: the repositories with their ticket-project override and the
// project each effectively uses. Optional ?search= (a substring of the full name),
// ?enabled=true|false, ?configState=unknown|present|missing, ?page= and ?pageSize=.
func (h *RepositoryHandlers) List(w http.ResponseWriter, r *http.Request) {
	if !requireAdminRole(w, r) {
		return
	}
	q := r.URL.Query()
	filter := store.RepositoryFilter{Search: q.Get("search"), ConfigState: q.Get("configState"), Page: 1, PageSize: defaultRepositoryPageSize}
	switch q.Get("enabled") {
	case "":
	case "true", "false":
		v := q.Get("enabled") == "true"
		filter.Enabled = &v
	default:
		auth.WriteError(w, "VALIDATION_ERROR", "enabled must be true or false", http.StatusBadRequest)
		return
	}
	switch filter.ConfigState {
	case "", "unknown", "present", "missing":
	default:
		auth.WriteError(w, "VALIDATION_ERROR", "configState must be unknown, present or missing", http.StatusBadRequest)
		return
	}
	var ok bool
	if filter.Page, ok = positiveInt(q.Get("page"), 1, 1<<20); !ok {
		auth.WriteError(w, "VALIDATION_ERROR", "page must be a positive integer", http.StatusBadRequest)
		return
	}
	if filter.PageSize, ok = positiveInt(q.Get("pageSize"), defaultRepositoryPageSize, maxRepositoryPageSize); !ok {
		auth.WriteError(w, "VALIDATION_ERROR", "pageSize must be an integer from 1 to 100", http.StatusBadRequest)
		return
	}

	items, total, err := h.Repositories.List(r.Context(), filter)
	if err != nil {
		slog.Error("repositories: listing failed", "err", err)
		auth.WriteError(w, "INTERNAL_ERROR", "failed to list repositories", http.StatusInternalServerError)
		return
	}
	out := make([]repositoryResponse, 0, len(items))
	for _, item := range items {
		out = append(out, newRepositoryResponse(item))
	}
	writeRepositoryJSON(w, http.StatusOK, map[string]any{"items": out, "page": filter.Page, "pageSize": filter.PageSize, "total": total})
}

// positiveInt reads a query value of at least 1 and at most max; an empty one is def.
func positiveInt(raw string, def, max int) (int, bool) {
	if raw == "" {
		return def, true
	}
	n, err := strconv.Atoi(raw)
	return n, err == nil && n >= 1 && n <= max
}

// Run is POST /repositories/{id}/run: enqueue a high-priority job with trigger run_now. A
// queued job of the repository takes the trigger and its id is returned; a running one is
// flagged for one follow-up run and its id is returned. A paused repository is 409. The
// config state is not looked at: an admin's run starts a job so a newly added config is noticed.
func (h *RepositoryHandlers) Run(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.find(w, r)
	if !ok {
		return
	}
	if !repo.Enabled {
		writeRepositoryPaused(w)
		return
	}
	res, err := h.Queue.Enqueue(r.Context(), queue.EnqueueParams{
		RepositoryID: repo.ID,
		Kind:         queue.KindRenovateRun,
		Priority:     queue.PriorityHigh,
		Trigger:      queue.TriggerRunNow,
	})
	switch {
	case err == nil:
	case errors.Is(err, queue.ErrRepositoryPaused):
		writeRepositoryPaused(w)
		return
	case errors.Is(err, queue.ErrRepositoryNotFound):
		auth.WriteError(w, "NOT_FOUND", "repository not found", http.StatusNotFound)
		return
	default:
		slog.Error("repositories: enqueueing a run failed", "repository", repo.ID, "err", err)
		auth.WriteError(w, "INTERNAL_ERROR", "failed to start the run", http.StatusInternalServerError)
		return
	}
	writeRepositoryJSON(w, http.StatusAccepted, map[string]string{"jobId": res.JobID})
}

// Patch is PATCH /repositories/{id}: pause or enable the repository and set (a ticket project
// id) or clear (null) its ticket-project override, in one transaction. A pause ends its queued
// job: one with no attempts is deleted, a queued retry with attempt history ends failed and
// keeps its runs, and a running job is aborted at its next heartbeat. An override that names no
// ticket project is 422 and changes nothing.
func (h *RepositoryHandlers) Patch(w http.ResponseWriter, r *http.Request) {
	if !requireAdminRole(w, r) {
		return
	}
	repo, ok := h.find(w, r)
	if !ok {
		return
	}
	var body map[string]json.RawMessage
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRepositoryPatchBytes)).Decode(&body); err != nil {
		auth.WriteError(w, "VALIDATION_ERROR", "invalid JSON body", http.StatusBadRequest)
		return
	}
	for key := range body {
		if key != "enabled" && key != "ticketProjectId" {
			auth.WriteError(w, "VALIDATION_ERROR", "unknown field "+key, http.StatusBadRequest)
			return
		}
	}
	var change store.RepositoryChange
	if raw, present := body["enabled"]; present {
		if err := json.Unmarshal(raw, &change.Enabled); err != nil || change.Enabled == nil {
			auth.WriteError(w, "VALIDATION_ERROR", "enabled must be true or false", http.StatusBadRequest)
			return
		}
	}
	if raw, present := body["ticketProjectId"]; present {
		id, valid := ticketProjectIDOf(raw)
		if !valid {
			auth.WriteError(w, "VALIDATION_ERROR", "ticketProjectId must be a ticket project id or null", http.StatusBadRequest)
			return
		}
		change.SetTicketProject, change.TicketProjectID = true, id
	}
	if change.Enabled == nil && !change.SetTicketProject {
		auth.WriteError(w, "VALIDATION_ERROR", "enabled or ticketProjectId is required", http.StatusBadRequest)
		return
	}

	updated, err := h.Repositories.Update(r.Context(), repo.ID, change)
	switch {
	case err == nil:
	case errors.Is(err, store.ErrRepositoryNotFound):
		auth.WriteError(w, "NOT_FOUND", "repository not found", http.StatusNotFound)
		return
	case errors.Is(err, store.ErrTicketProjectNotFound):
		auth.WriteError(w, "VALIDATION_ERROR", "ticketProjectId names no ticket project", http.StatusUnprocessableEntity)
		return
	default:
		slog.Error("repositories: updating a repository failed", "repository", repo.ID, "err", err)
		auth.WriteError(w, "INTERNAL_ERROR", "failed to update the repository", http.StatusInternalServerError)
		return
	}
	writeRepositoryJSON(w, http.StatusOK, newRepositoryResponse(updated))
}

// find loads the repository of the {id} path value. A repository that is unknown, removed, of
// a removed installation or outside the caller's scope is 404; a failed read is never one.
func (h *RepositoryHandlers) find(w http.ResponseWriter, r *http.Request) (store.Repository, bool) {
	id := chi.URLParam(r, "id")
	if !apimw.ScopeFromContext(r.Context()).Allows(id) {
		auth.WriteError(w, "NOT_FOUND", "repository not found", http.StatusNotFound)
		return store.Repository{}, false
	}
	repo, err := h.Repositories.Get(r.Context(), id)
	switch {
	case err == nil:
		return repo, true
	case errors.Is(err, store.ErrRepositoryNotFound):
		auth.WriteError(w, "NOT_FOUND", "repository not found", http.StatusNotFound)
	default:
		slog.Error("repositories: reading a repository failed", "repository", id, "err", err)
		auth.WriteError(w, "INTERNAL_ERROR", "failed to read the repository", http.StatusInternalServerError)
	}
	return store.Repository{}, false
}

func writeRepositoryPaused(w http.ResponseWriter) {
	auth.WriteError(w, "REPOSITORY_PAUSED", "the repository is paused; enable it to run it", http.StatusConflict)
}

func writeRepositoryJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
