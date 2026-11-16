package handlers

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/mdg-labs/release-ops/internal/api/auth"
	apimw "github.com/mdg-labs/release-ops/internal/api/middleware"
	"github.com/mdg-labs/release-ops/internal/store"
)

const (
	defaultJobPageSize = 25
	maxJobPageSize     = 100
)

// JobHandlers serves the job history (specs.html section 7.7) to every signed-in role. A viewer
// reads the jobs of the repositories of the scope Authenticate stored; an admin reads all of
// them. A job of another repository is answered as one that does not exist.
type JobHandlers struct {
	Jobs store.JobLister
}

type jobRepositoryResponse struct {
	ID       string `json:"id"`
	FullName string `json:"fullName"`
}

type jobResponse struct {
	ID             string                `json:"id"`
	Repository     jobRepositoryResponse `json:"repository"`
	Kind           string                `json:"kind"`
	State          string                `json:"state"`
	Priority       string                `json:"priority"`
	Trigger        string                `json:"trigger"`
	MatchedMarkers []string              `json:"matchedMarkers"`
	Attempts       int64                 `json:"attempts"`
	CreatedAt      string                `json:"createdAt"`
	StartedAt      *string               `json:"startedAt"`
	FinishedAt     *string               `json:"finishedAt"`
	StartLatencyMs *int64                `json:"startLatencyMs"`
}

type jobRunResponse struct {
	Attempt      int64   `json:"attempt"`
	StartedAt    string  `json:"startedAt"`
	FinishedAt   *string `json:"finishedAt"`
	Outcome      string  `json:"outcome"`
	ExitCode     *int64  `json:"exitCode"`
	ErrorCode    *string `json:"errorCode"`
	ErrorMessage *string `json:"errorMessage"`
	ReportStatus *string `json:"reportStatus"`
	LogExcerpt   *string `json:"logExcerpt"`
}

type jobEventResponse struct {
	At      string  `json:"at"`
	Kind    string  `json:"kind"`
	Message *string `json:"message"`
}

type jobDetailResponse struct {
	jobResponse
	Runs   []jobRunResponse   `json:"runs"`
	Events []jobEventResponse `json:"events"`
}

func newJobResponse(it store.JobListItem) jobResponse {
	out := jobResponse{
		ID: it.ID, Repository: jobRepositoryResponse{ID: it.RepositoryID, FullName: it.RepositoryFullName},
		Kind: it.Kind, State: it.State, Priority: it.Priority, Trigger: it.Trigger,
		MatchedMarkers: it.MatchedMarkers, Attempts: it.Attempts,
		CreatedAt: it.CreatedAt, StartedAt: emptyToNil(it.StartedAt), FinishedAt: emptyToNil(it.FinishedAt),
	}
	if out.MatchedMarkers == nil {
		out.MatchedMarkers = []string{}
	}
	if it.StartLatency != nil {
		ms := it.StartLatency.Milliseconds()
		out.StartLatencyMs = &ms
	}
	return out
}

// List is GET /jobs, newest first. ?repositoryId=, ?state= (queued, running, succeeded or
// failed), ?page= and ?pageSize= (25 by default, at most 100).
func (h *JobHandlers) List(w http.ResponseWriter, r *http.Request) {
	if _, ok := apimw.PrincipalFromContext(r.Context()); !ok {
		auth.WriteError(w, "unauthorized", "authentication required", http.StatusUnauthorized)
		return
	}
	q := r.URL.Query()
	filter := store.JobListFilter{RepositoryID: q.Get("repositoryId"), State: q.Get("state"), Page: 1, PageSize: defaultJobPageSize}
	switch filter.State {
	case "", "queued", "running", "succeeded", "failed":
	default:
		auth.WriteError(w, "VALIDATION_ERROR", "state must be queued, running, succeeded or failed", http.StatusBadRequest)
		return
	}
	var ok bool
	if filter.Page, ok = positiveInt(q.Get("page"), 1, 1<<20); !ok {
		auth.WriteError(w, "VALIDATION_ERROR", "page must be a positive integer", http.StatusBadRequest)
		return
	}
	if filter.PageSize, ok = positiveInt(q.Get("pageSize"), defaultJobPageSize, maxJobPageSize); !ok {
		auth.WriteError(w, "VALIDATION_ERROR", "pageSize must be an integer from 1 to 100", http.StatusBadRequest)
		return
	}
	scope := apimw.ScopeFromContext(r.Context())
	if scope.All() {
		filter.AllRepositories = true
	} else {
		filter.RepositoryIDs, _ = scope.IDs()
	}
	items, total, err := h.Jobs.List(r.Context(), filter)
	if err != nil {
		slog.Error("jobs: listing failed", "err", err)
		auth.WriteError(w, "INTERNAL_ERROR", "failed to list jobs", http.StatusInternalServerError)
		return
	}
	out := make([]jobResponse, 0, len(items))
	for _, item := range items {
		out = append(out, newJobResponse(item))
	}
	writeRepositoryJSON(w, http.StatusOK, map[string]any{"items": out, "page": filter.Page, "pageSize": filter.PageSize, "total": total})
}

// Get is GET /jobs/{id}: the job with its attempts, each with its stored log excerpt, and its
// events. The excerpt is returned as stored: the app redacts it when the worker's result
// arrives.
func (h *JobHandlers) Get(w http.ResponseWriter, r *http.Request) {
	if _, ok := apimw.PrincipalFromContext(r.Context()); !ok {
		auth.WriteError(w, "unauthorized", "authentication required", http.StatusUnauthorized)
		return
	}
	id := chi.URLParam(r, "id")
	scope := apimw.ScopeFromContext(r.Context())
	ids, _ := scope.IDs()
	detail, err := h.Jobs.Get(r.Context(), id, scope.All(), ids)
	switch {
	case err == nil:
	case errors.Is(err, store.ErrJobNotFound):
		auth.WriteError(w, "NOT_FOUND", "job not found", http.StatusNotFound)
		return
	default:
		slog.Error("jobs: reading a job failed", "job", id, "err", err)
		auth.WriteError(w, "INTERNAL_ERROR", "failed to read the job", http.StatusInternalServerError)
		return
	}
	out := jobDetailResponse{jobResponse: newJobResponse(detail.JobListItem), Runs: make([]jobRunResponse, 0, len(detail.Runs)), Events: make([]jobEventResponse, 0, len(detail.Events))}
	for _, run := range detail.Runs {
		out.Runs = append(out.Runs, jobRunResponse{
			Attempt: run.Attempt, StartedAt: run.StartedAt, FinishedAt: emptyToNil(run.FinishedAt), Outcome: run.Outcome,
			ExitCode: run.ExitCode, ErrorCode: emptyToNil(run.ErrorCode), ErrorMessage: emptyToNil(run.ErrorMessage),
			ReportStatus: emptyToNil(run.ReportStatus), LogExcerpt: emptyToNil(run.LogExcerpt),
		})
	}
	for _, e := range detail.Events {
		out.Events = append(out.Events, jobEventResponse{At: e.At, Kind: e.Kind, Message: emptyToNil(e.Message)})
	}
	writeRepositoryJSON(w, http.StatusOK, out)
}
