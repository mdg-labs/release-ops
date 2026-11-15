package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/mdg-labs/release-ops/internal/api/auth"
	apimw "github.com/mdg-labs/release-ops/internal/api/middleware"
	"github.com/mdg-labs/release-ops/internal/queue"
	"github.com/mdg-labs/release-ops/internal/store"
)

// maxRepositoryPatchBytes bounds the body of a repository PATCH, which holds one flag.
const maxRepositoryPatchBytes = 4 << 10

// RepositoryHandlers serves the admin actions on a repository: run now and pause or enable.
// Both routes sit behind RequireAdmin; the scope check is the second line, so a repository
// outside the caller's scope or the installation answers 404, never 403.
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
}

func newRepositoryResponse(r store.Repository) repositoryResponse {
	return repositoryResponse{
		ID:            r.ID,
		FullName:      r.FullName,
		DefaultBranch: r.DefaultBranch,
		Enabled:       r.Enabled,
		ConfigState:   r.ConfigState,
	}
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

// Patch is PATCH /repositories/{id}: pause or enable the repository. A pause ends its queued
// job: one with no attempts is deleted, a queued retry with attempt history ends failed and
// keeps its runs, and a running job is aborted at its next heartbeat. The ticket-project
// override of the spec's body belongs to the repository mapping and is refused here instead of
// being ignored.
func (h *RepositoryHandlers) Patch(w http.ResponseWriter, r *http.Request) {
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
		switch key {
		case "enabled":
		case "ticketProjectId":
			auth.WriteError(w, "VALIDATION_ERROR", "ticketProjectId cannot be set yet", http.StatusBadRequest)
			return
		default:
			auth.WriteError(w, "VALIDATION_ERROR", "unknown field "+key, http.StatusBadRequest)
			return
		}
	}
	var enabled *bool
	if raw, present := body["enabled"]; present {
		if err := json.Unmarshal(raw, &enabled); err != nil {
			auth.WriteError(w, "VALIDATION_ERROR", "enabled must be true or false", http.StatusBadRequest)
			return
		}
	}
	if enabled == nil {
		auth.WriteError(w, "VALIDATION_ERROR", "enabled is required", http.StatusBadRequest)
		return
	}
	updated, err := h.Repositories.SetEnabled(r.Context(), repo.ID, *enabled)
	switch {
	case err == nil:
	case errors.Is(err, store.ErrRepositoryNotFound):
		auth.WriteError(w, "NOT_FOUND", "repository not found", http.StatusNotFound)
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
