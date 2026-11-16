package handlers

import (
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/mdg-labs/release-ops/internal/api/auth"
	apimw "github.com/mdg-labs/release-ops/internal/api/middleware"
	"github.com/mdg-labs/release-ops/internal/store"
)

// Problem kinds (specs.html section 7.7).
const (
	problemJobFailed       = "job_failed"
	problemTicketFailed    = "ticket_failed"
	problemRateLimited     = "rate_limited"
	problemPermissionsLost = "permissions_lost"
)

const problemTimeLayout = "2006-01-02T15:04:05.000Z"

// ProblemHandlers serves the Problems view (specs.html section 7.7). The problems are derived
// from stored state when asked for. An admin sees every kind; a viewer sees job_failed and
// ticket_failed for the repositories of their scope, a ticket_failed without its error text, and
// never an installation-level problem, which has no repository.
type ProblemHandlers struct {
	Problems store.ProblemReader
	// RequiredPermissions is what the App's manifest requests; an installation that lacks one
	// or holds it at a lower level has lost a permission.
	RequiredPermissions map[string]string
	// Now defaults to time.Now.
	Now func() time.Time
}

type problemRefResponse struct {
	JobID      string `json:"jobId,omitempty"`
	UpdatePRID string `json:"updatePrId,omitempty"`
	// UpdatePRURL links a ticket problem to its pull request.
	UpdatePRURL string `json:"updatePrUrl,omitempty"`
	// UpdateTicketID is the id the clear action takes; given to an admin for a row whose pending
	// marker may be cleared.
	UpdateTicketID string `json:"updateTicketId,omitempty"`
}

type problemResponse struct {
	Kind       string                 `json:"kind"`
	Repository *jobRepositoryResponse `json:"repository"`
	Since      string                 `json:"since"`
	Message    *string                `json:"message"`
	Ref        *problemRefResponse    `json:"ref"`
}

// List is GET /problems, newest first. ?page= and ?pageSize= (25 by default, at most 100).
func (h *ProblemHandlers) List(w http.ResponseWriter, r *http.Request) {
	principal, ok := apimw.PrincipalFromContext(r.Context())
	if !ok {
		auth.WriteError(w, "unauthorized", "authentication required", http.StatusUnauthorized)
		return
	}
	q := r.URL.Query()
	page, ok := positiveInt(q.Get("page"), 1, 1<<20)
	if !ok {
		auth.WriteError(w, "VALIDATION_ERROR", "page must be a positive integer", http.StatusBadRequest)
		return
	}
	pageSize, ok := positiveInt(q.Get("pageSize"), defaultJobPageSize, maxJobPageSize)
	if !ok {
		auth.WriteError(w, "VALIDATION_ERROR", "pageSize must be an integer from 1 to 100", http.StatusBadRequest)
		return
	}
	now := time.Now()
	if h.Now != nil {
		now = h.Now()
	}
	now = now.UTC()

	filter := store.ProblemFilter{Admin: principal.Role == auth.RoleAdmin}
	scope := apimw.ScopeFromContext(r.Context())
	if scope.All() {
		filter.AllRepositories = true
	} else {
		filter.RepositoryIDs, _ = scope.IDs()
	}
	set, err := h.Problems.Read(r.Context(), filter)
	if err != nil {
		slog.Error("problems: reading failed", "err", err)
		auth.WriteError(w, "INTERNAL_ERROR", "failed to list problems", http.StatusInternalServerError)
		return
	}

	problems := make([]problemResponse, 0, len(set.Jobs)+len(set.Tickets)+len(set.Installations))
	for _, j := range set.Jobs {
		problems = append(problems, problemResponse{
			Kind: problemJobFailed, Repository: &jobRepositoryResponse{ID: j.RepositoryID, FullName: j.RepositoryFullName},
			Since: j.Since, Message: emptyToNil(j.Message), Ref: &problemRefResponse{JobID: j.JobID},
		})
	}
	for _, t := range set.Tickets {
		problems = append(problems, problemResponse{
			Kind: problemTicketFailed, Repository: &jobRepositoryResponse{ID: t.RepositoryID, FullName: t.RepositoryFullName},
			Since: t.Since, Message: emptyToNil(t.Message),
			Ref: &problemRefResponse{UpdatePRID: t.UpdatePRID, UpdatePRURL: t.UpdatePRURL, UpdateTicketID: t.UpdateTicketID},
		})
	}
	if filter.Admin {
		for _, i := range set.Installations {
			problems = append(problems, h.installationProblems(i, now)...)
		}
	}
	sort.SliceStable(problems, func(a, b int) bool { return problems[a].Since > problems[b].Since })

	total := len(problems)
	start := min((page-1)*pageSize, total)
	end := min(start+pageSize, total)
	writeRepositoryJSON(w, http.StatusOK, map[string]any{"items": problems[start:end], "page": page, "pageSize": pageSize, "total": total})
}

// installationProblems derives the rate_limited and permissions_lost problems of an
// installation. Neither has a repository, and neither carries anything but names and times.
func (h *ProblemHandlers) installationProblems(i store.InstallationProblemInput, now time.Time) []problemResponse {
	var out []problemResponse
	stamp := now.Format(problemTimeLayout)

	if resetAt, err := time.Parse(problemTimeLayout, i.RateLimitResetAt); err == nil && i.RateLimitRemaining != nil && *i.RateLimitRemaining <= 0 && resetAt.After(now) {
		msg := fmt.Sprintf("GitHub's rate limit is used up; jobs wait until %s", i.RateLimitResetAt)
		out = append(out, problemResponse{Kind: problemRateLimited, Since: stamp, Message: &msg})
	} else if i.LatestAttemptErrorCode == "rate_limited" {
		msg := "The latest job attempt was stopped by GitHub's rate limit"
		out = append(out, problemResponse{Kind: problemRateLimited, Since: i.LatestAttemptFinishedAt, Message: &msg})
	}

	var msg string
	switch i.Status {
	case "suspended":
		msg = "The GitHub App installation is suspended"
	case "removed":
		msg = "The GitHub App installation was removed"
	default:
		if missing := missingPermissions([]byte(i.Permissions), h.RequiredPermissions); len(missing) > 0 {
			msg = "The GitHub App installation lacks permissions: " + strings.Join(missing, ", ")
		}
	}
	if msg != "" {
		out = append(out, problemResponse{Kind: problemPermissionsLost, Since: i.UpdatedAt, Message: &msg})
	}
	return out
}
