package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/mdg-labs/release-ops/internal/api/auth"
	"github.com/mdg-labs/release-ops/internal/poll"
	"github.com/mdg-labs/release-ops/internal/store"
)

// ErrPollAlreadyRunning is returned when a manual poll is requested while one is active.
var ErrPollAlreadyRunning = errors.New("poll already running")

// PollRunner starts manual poll runs and reports whether a poll is active.
type PollRunner interface {
	Trigger(ctx context.Context) (runID string, err error)
	IsPolling() bool
}

// PollHandlers serves poll trigger and run history HTTP endpoints.
type PollHandlers struct {
	Poll   store.PollRepository
	Runner PollRunner
}

type pollRunError struct {
	RepoID  string `json:"repoId"`
	Message string `json:"message"`
}

type pollRunResponse struct {
	ID                string           `json:"id"`
	StartedAt         string           `json:"startedAt"`
	FinishedAt        *string          `json:"finishedAt"`
	Status            string           `json:"status"`
	ReposChecked      int64            `json:"reposChecked"`
	TicketsCreated    int64            `json:"ticketsCreated"`
	TicketsSuperseded int64            `json:"ticketsSuperseded"`
	Errors            []pollRunError   `json:"errors"`
	Events            []pollRunEventResponse `json:"events,omitempty"`
}

type pollRunEventResponse struct {
	ID              string  `json:"id"`
	PollRunID       string  `json:"pollRunId"`
	MonitoredRepoID *string `json:"monitoredRepoId"`
	Action          string  `json:"action"`
	Detail          *string `json:"detail"`
	CreatedAt       string  `json:"createdAt"`
}

type triggerPollResponse struct {
	RunID string `json:"runId"`
}

const (
	defaultPollRunsLimit = 20
	maxPollRunsLimit     = 100
)

// Trigger handles POST /api/v1/poll/trigger.
func (h *PollHandlers) Trigger(w http.ResponseWriter, r *http.Request) {
	runID, err := h.Runner.Trigger(r.Context())
	if errors.Is(err, ErrPollAlreadyRunning) || errors.Is(err, poll.ErrAlreadyRunning) {
		auth.WriteError(w, "CONFLICT", "poll already running", http.StatusConflict)
		return
	}
	if err != nil {
		auth.WriteError(w, "INTERNAL_ERROR", "failed to start poll", http.StatusInternalServerError)
		return
	}

	writePollJSON(w, http.StatusAccepted, triggerPollResponse{RunID: runID})
}

// ListRuns handles GET /api/v1/poll/runs.
func (h *PollHandlers) ListRuns(w http.ResponseWriter, r *http.Request) {
	limit, offset, err := parsePollRunsPagination(r)
	if err != nil {
		auth.WriteError(w, "VALIDATION_ERROR", err.Error(), http.StatusBadRequest)
		return
	}

	runs, err := h.Poll.ListRuns(r.Context(), limit, offset)
	if err != nil {
		auth.WriteError(w, "INTERNAL_ERROR", "failed to list poll runs", http.StatusInternalServerError)
		return
	}

	resp := make([]pollRunResponse, len(runs))
	for i := range runs {
		resp[i] = *pollRunFromStore(&runs[i])
	}

	writePollJSON(w, http.StatusOK, resp)
}

// GetRun handles GET /api/v1/poll/runs/{id}.
func (h *PollHandlers) GetRun(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		auth.WriteError(w, "VALIDATION_ERROR", "poll run id is required", http.StatusBadRequest)
		return
	}

	run, err := h.Poll.GetRun(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			auth.WriteError(w, "NOT_FOUND", "poll run not found", http.StatusNotFound)
			return
		}
		auth.WriteError(w, "INTERNAL_ERROR", "failed to load poll run", http.StatusInternalServerError)
		return
	}

	events, err := h.Poll.ListEventsByRunID(r.Context(), id)
	if err != nil {
		auth.WriteError(w, "INTERNAL_ERROR", "failed to load poll run events", http.StatusInternalServerError)
		return
	}

	resp := pollRunFromStore(run)
	resp.Events = make([]pollRunEventResponse, len(events))
	for i := range events {
		resp.Events[i] = pollRunEventFromStore(&events[i])
	}

	writePollJSON(w, http.StatusOK, resp)
}

func parsePollRunsPagination(r *http.Request) (limit, offset int64, err error) {
	limit = defaultPollRunsLimit
	offset = 0

	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || limit < 1 {
			return 0, 0, errors.New("limit must be a positive integer")
		}
		if limit > maxPollRunsLimit {
			limit = maxPollRunsLimit
		}
	}

	if raw := r.URL.Query().Get("offset"); raw != "" {
		offset, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || offset < 0 {
			return 0, 0, errors.New("offset must be a non-negative integer")
		}
	}

	return limit, offset, nil
}

func pollRunFromStore(run *store.PollRun) *pollRunResponse {
	return &pollRunResponse{
		ID:                run.ID,
		StartedAt:         run.StartedAt,
		FinishedAt:        run.FinishedAt,
		Status:            run.Status,
		ReposChecked:      run.ReposChecked,
		TicketsCreated:    run.TicketsCreated,
		TicketsSuperseded: run.TicketsSuperseded,
		Errors:            parsePollRunErrors(run.ErrorsJSON),
	}
}

func pollRunEventFromStore(event *store.PollRunEvent) pollRunEventResponse {
	return pollRunEventResponse{
		ID:              event.ID,
		PollRunID:       event.PollRunID,
		MonitoredRepoID: event.MonitoredRepoID,
		Action:          event.Action,
		Detail:          event.Detail,
		CreatedAt:       event.CreatedAt,
	}
}

func parsePollRunErrors(raw string) []pollRunError {
	if raw == "" {
		return []pollRunError{}
	}
	var out []pollRunError
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return []pollRunError{}
	}
	if out == nil {
		return []pollRunError{}
	}
	return out
}

func writePollJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
