package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/mdg-labs/release-ops/internal/api/auth"
	apimw "github.com/mdg-labs/release-ops/internal/api/middleware"
	"github.com/mdg-labs/release-ops/internal/github"
	"github.com/mdg-labs/release-ops/internal/queue"
	"github.com/mdg-labs/release-ops/internal/store"
)

// maxRepositoryPatchBytes bounds the body of a repository PATCH, which holds a flag and an id.
const maxRepositoryPatchBytes = 4 << 10

const (
	defaultRepositoryPageSize = 25
	maxRepositoryPageSize     = 100
)

// RenovateEnabler opens the pull request of "Enable repo".
type RenovateEnabler interface {
	// Enable opens a pull request adding a renovate.json that extends preset, or returns the
	// one an earlier call left open; github.ErrConfigExists when the default branch has a config;
	// github.ErrEnableBranchExists when the action's branch holds commits that are not its own.
	Enable(ctx context.Context, installationID int64, fullName, defaultBranch, preset string) (github.EnabledPullRequest, error)
}

// RepositoryHandlers serves the repositories: the list and one repository to every signed-in
// role, limited to the caller's scope, and for an admin run now, pause, enable, the
// ticket-project override and "Enable repo". The routes of the actions sit behind
// RequireAdmin and the ones that read or set a ticket project check the role again; the scope
// check is the next line, so a repository outside the caller's scope or the installation
// answers 404, never 403.
type RepositoryHandlers struct {
	Repositories store.RepositoryRepository
	Queue        queue.Queue
	// Enabler opens the "Enable repo" pull request; without one the route is not mounted.
	Enabler RenovateEnabler
	// PresetRepositories returns PRESET_REPOSITORIES; the first entry is the preset the
	// opened config extends.
	PresetRepositories func() []string
	// DashboardBaseURL is the GitHub web URL (GITHUB_BASE_URL) the repository and Dependency
	// Dashboard links are built on.
	DashboardBaseURL string
}

type repositoryJobResponse struct {
	ID         string  `json:"id"`
	State      string  `json:"state"`
	FinishedAt *string `json:"finishedAt"`
}

// repositoryResponse is what a viewer sees of a repository.
type repositoryResponse struct {
	ID            string `json:"id"`
	FullName      string `json:"fullName"`
	DefaultBranch string `json:"defaultBranch"`
	Enabled       bool   `json:"enabled"`
	ConfigState   string `json:"configState"`
	// HTMLURL is the repository's page on GitHub, which the full name links to.
	HTMLURL *string `json:"htmlUrl"`
	// DashboardURL is the Dependency Dashboard issue, null while its number is not known.
	DashboardURL  *string                `json:"dashboardUrl"`
	OpenUpdatePRs int                    `json:"openUpdatePrs"`
	LastJob       *repositoryJobResponse `json:"lastJob"`
}

// adminRepositoryResponse adds the ticket project fields, which only an admin sees.
type adminRepositoryResponse struct {
	repositoryResponse
	// TicketProjectID is the repository's override; the effective fields are the project its
	// PRs get tickets in. All are null when none is set, which means no tickets (specs.html
	// section 5.2).
	TicketProjectID            *string `json:"ticketProjectId"`
	EffectiveTicketProjectID   *string `json:"effectiveTicketProjectId"`
	EffectiveTicketProjectName *string `json:"effectiveTicketProjectName"`
}

func (h *RepositoryHandlers) newRepositoryResponse(r store.RepositoryListing) repositoryResponse {
	out := repositoryResponse{
		ID:            r.ID,
		FullName:      r.FullName,
		DefaultBranch: r.DefaultBranch,
		Enabled:       r.Enabled,
		ConfigState:   r.ConfigState,
		OpenUpdatePRs: r.OpenUpdatePRs,
	}
	if h.DashboardBaseURL != "" {
		page := h.DashboardBaseURL + "/" + r.FullName
		out.HTMLURL = &page
		if r.DashboardIssueNumber > 0 {
			issue := page + "/issues/" + strconv.FormatInt(r.DashboardIssueNumber, 10)
			out.DashboardURL = &issue
		}
	}
	if r.LastJob != nil {
		out.LastJob = &repositoryJobResponse{ID: r.LastJob.ID, State: r.LastJob.State, FinishedAt: emptyToNil(r.LastJob.FinishedAt)}
	}
	return out
}

// respond returns the response body for the caller's role: the ticket project fields are part
// of an admin's and absent from a viewer's.
func (h *RepositoryHandlers) respond(r *http.Request, item store.RepositoryListing) any {
	base := h.newRepositoryResponse(item)
	if p, ok := apimw.PrincipalFromContext(r.Context()); !ok || p.Role != auth.RoleAdmin {
		return base
	}
	return adminRepositoryResponse{
		repositoryResponse:         base,
		TicketProjectID:            emptyToNil(item.TicketProjectID),
		EffectiveTicketProjectID:   emptyToNil(item.EffectiveTicketProjectID),
		EffectiveTicketProjectName: emptyToNil(item.EffectiveTicketProjectName),
	}
}

// List is GET /repositories: the repositories of the caller's scope, a viewer's without the
// ticket project fields. Optional ?search= (a substring of the full name),
// ?enabled=true|false, ?configState=unknown|present|missing, ?page= and ?pageSize=.
func (h *RepositoryHandlers) List(w http.ResponseWriter, r *http.Request) {
	if _, ok := apimw.PrincipalFromContext(r.Context()); !ok {
		auth.WriteError(w, "unauthorized", "authentication required", http.StatusUnauthorized)
		return
	}
	q := r.URL.Query()
	filter := store.RepositoryFilter{Search: q.Get("search"), ConfigState: q.Get("configState"), Page: 1, PageSize: defaultRepositoryPageSize}
	scope := apimw.ScopeFromContext(r.Context())
	if ids, limited := scope.IDs(); limited {
		filter.Restrict, filter.RepositoryIDs = true, ids
	}
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
	out := make([]any, 0, len(items))
	for _, item := range items {
		out = append(out, h.respond(r, item))
	}
	writeRepositoryJSON(w, http.StatusOK, map[string]any{"items": out, "page": filter.Page, "pageSize": filter.PageSize, "total": total})
}

// Get is GET /repositories/{id}: one repository of the caller's scope, 404 for any other.
func (h *RepositoryHandlers) Get(w http.ResponseWriter, r *http.Request) {
	if _, ok := apimw.PrincipalFromContext(r.Context()); !ok {
		auth.WriteError(w, "unauthorized", "authentication required", http.StatusUnauthorized)
		return
	}
	id := chi.URLParam(r, "id")
	if !apimw.ScopeFromContext(r.Context()).Allows(id) {
		auth.WriteError(w, "NOT_FOUND", "repository not found", http.StatusNotFound)
		return
	}
	item, err := h.Repositories.GetListing(r.Context(), id)
	switch {
	case err == nil:
	case errors.Is(err, store.ErrRepositoryNotFound):
		auth.WriteError(w, "NOT_FOUND", "repository not found", http.StatusNotFound)
		return
	default:
		slog.Error("repositories: reading a repository failed", "repository", id, "err", err)
		auth.WriteError(w, "INTERNAL_ERROR", "failed to read the repository", http.StatusInternalServerError)
		return
	}
	writeRepositoryJSON(w, http.StatusOK, h.respond(r, item))
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
	writeRepositoryJSON(w, http.StatusOK, h.respond(r, updated))
}

// EnableRenovate is POST /repositories/{id}/enable-renovate, the "Enable repo" action: a pull
// request that adds a renovate.json extending the configured preset (the first of
// PRESET_REPOSITORIES, else config:recommended). 201 with the pull request, or 200 with the
// one an earlier call left open; 409 CONFIG_EXISTS for a repository recorded as having a
// config or whose default branch has one; 409 ENABLE_BRANCH_EXISTS when the action's branch
// already exists with commits that are not the action's own (it is left as it is). A repository whose pull request GitHub refused is
// 502 and changes nothing here: the repository is neither enabled nor otherwise marked.
func (h *RepositoryHandlers) EnableRenovate(w http.ResponseWriter, r *http.Request) {
	if !requireAdminRole(w, r) {
		return
	}
	repo, ok := h.find(w, r)
	if !ok {
		return
	}
	if repo.ConfigState == "present" {
		writeConfigExists(w)
		return
	}
	var presets []string
	if h.PresetRepositories != nil {
		presets = h.PresetRepositories()
	}
	pr, err := h.Enabler.Enable(r.Context(), repo.GitHubInstallationID, repo.FullName, repo.DefaultBranch, github.RenovatePreset(presets))
	switch {
	case err == nil:
	case errors.Is(err, github.ErrConfigExists):
		writeConfigExists(w)
		return
	case errors.Is(err, github.ErrEnableBranchExists):
		auth.WriteError(w, "ENABLE_BRANCH_EXISTS", "the branch "+github.EnableRenovateBranch+" already exists with commits that Release Ops did not make; delete or merge it, then try again", http.StatusConflict)
		return
	case errors.Is(err, github.ErrAppRejected):
		slog.Warn("repositories: GitHub refused the enable pull request", "repository", repo.ID, "err", err)
		auth.WriteError(w, "GITHUB_UNAVAILABLE", "GitHub refused to open the pull request; check the App's contents and pull request permissions", http.StatusBadGateway)
		return
	default:
		slog.Error("repositories: opening the enable pull request failed", "repository", repo.ID, "err", err)
		auth.WriteError(w, "GITHUB_UNAVAILABLE", "GitHub could not open the pull request; try again", http.StatusBadGateway)
		return
	}
	status := http.StatusCreated
	if pr.Existing {
		status = http.StatusOK
	}
	writeRepositoryJSON(w, status, map[string]any{"pullRequestUrl": pr.URL, "number": pr.Number})
}

func writeConfigExists(w http.ResponseWriter) {
	auth.WriteError(w, "CONFIG_EXISTS", "the repository already has a Renovate config file", http.StatusConflict)
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
