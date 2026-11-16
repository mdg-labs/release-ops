package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/mdg-labs/release-ops/internal/api/auth"
	apimw "github.com/mdg-labs/release-ops/internal/api/middleware"
	"github.com/mdg-labs/release-ops/internal/store"
)

const (
	defaultUpdatePRPageSize = 25
	maxUpdatePRPageSize     = 100
)

// UpdatePRHandlers serves the update PR list of the dashboard (specs.html section 7.6) to every
// signed-in role. A viewer reads only the repositories of the scope Authenticate stored; an
// admin reads all of them. A request that names a repository outside the scope is answered as
// one that names a repository that does not exist: an empty page.
type UpdatePRHandlers struct {
	PRs store.UpdatePRLister
}

type updatePRRepositoryResponse struct {
	ID       string `json:"id"`
	FullName string `json:"fullName"`
}

type updatePRRefResponse struct {
	ID     string `json:"id"`
	Number int64  `json:"number"`
	URL    string `json:"url"`
}

type updatePRDependencyResponse struct {
	Name       string  `json:"name"`
	From       *string `json:"from"`
	To         *string `json:"to"`
	UpdateType *string `json:"updateType"`
}

// updatePRTicketResponse carries no error text: HasError says only that an operation on the
// ticket is failing, to viewers and admins alike.
type updatePRTicketResponse struct {
	State      string  `json:"state"`
	ExternalID *string `json:"externalId"`
	URL        *string `json:"url"`
	HasError   bool    `json:"hasError"`
}

type updatePRResponse struct {
	ID           string                       `json:"id"`
	Repository   updatePRRepositoryResponse   `json:"repository"`
	Number       int64                        `json:"number"`
	URL          string                       `json:"url"`
	Title        string                       `json:"title"`
	State        string                       `json:"state"`
	OpenedAt     *string                      `json:"openedAt"`
	MergedAt     *string                      `json:"mergedAt"`
	ClosedAt     *string                      `json:"closedAt"`
	SupersededBy *updatePRRefResponse         `json:"supersededBy"`
	UpdateTypes  []string                     `json:"updateTypes"`
	Dependencies []updatePRDependencyResponse `json:"dependencies"`
	Ticket       *updatePRTicketResponse      `json:"ticket"`
}

func newUpdatePRResponse(it store.UpdatePRListItem) updatePRResponse {
	out := updatePRResponse{
		ID: it.ID, Repository: updatePRRepositoryResponse{ID: it.RepositoryID, FullName: it.RepositoryFullName},
		Number: it.Number, URL: it.URL, Title: it.Title, State: it.State,
		OpenedAt: emptyToNil(it.OpenedAt), MergedAt: emptyToNil(it.MergedAt), ClosedAt: emptyToNil(it.ClosedAt),
		UpdateTypes: it.UpdateTypes, Dependencies: make([]updatePRDependencyResponse, 0, len(it.Dependencies)),
	}
	if it.SupersededBy != nil {
		out.SupersededBy = &updatePRRefResponse{ID: it.SupersededBy.ID, Number: it.SupersededBy.Number, URL: it.SupersededBy.URL}
	}
	for _, d := range it.Dependencies {
		out.Dependencies = append(out.Dependencies, updatePRDependencyResponse{
			Name: d.Name, From: emptyToNil(d.From), To: emptyToNil(d.To), UpdateType: emptyToNil(d.UpdateType),
		})
	}
	if it.Ticket != nil {
		out.Ticket = &updatePRTicketResponse{
			State: it.Ticket.State, ExternalID: emptyToNil(it.Ticket.ExternalID), URL: emptyToNil(it.Ticket.ExternalURL), HasError: it.Ticket.HasError,
		}
	}
	return out
}

// List is GET /update-prs. ?state= (open by default, merged, closed or superseded),
// ?repositoryId=, ?updateType= (major, minor, patch, digest, lockfile or other), ?sort=
// (openedAt, -openedAt, mergedAt or -mergedAt; -openedAt by default), ?page= and ?pageSize=
// (25 by default, at most 100).
func (h *UpdatePRHandlers) List(w http.ResponseWriter, r *http.Request) {
	if _, ok := apimw.PrincipalFromContext(r.Context()); !ok {
		auth.WriteError(w, "unauthorized", "authentication required", http.StatusUnauthorized)
		return
	}
	q := r.URL.Query()
	filter := store.UpdatePRListFilter{
		State: store.UpdatePROpen, Sort: store.UpdatePRSortOpenedAtDesc,
		RepositoryID: q.Get("repositoryId"), UpdateType: q.Get("updateType"),
		Page: 1, PageSize: defaultUpdatePRPageSize,
	}
	if v := q.Get("state"); v != "" {
		filter.State = v
	}
	switch filter.State {
	case store.UpdatePROpen, store.UpdatePRMerged, store.UpdatePRClosed, store.UpdatePRSuperseded:
	default:
		auth.WriteError(w, "VALIDATION_ERROR", "state must be open, merged, closed or superseded", http.StatusBadRequest)
		return
	}
	switch filter.UpdateType {
	case "", "major", "minor", "patch", "digest", "lockfile", "other":
	default:
		auth.WriteError(w, "VALIDATION_ERROR", "updateType must be major, minor, patch, digest, lockfile or other", http.StatusBadRequest)
		return
	}
	if v := q.Get("sort"); v != "" {
		filter.Sort = v
	}
	switch filter.Sort {
	case store.UpdatePRSortOpenedAt, store.UpdatePRSortOpenedAtDesc, store.UpdatePRSortMergedAt, store.UpdatePRSortMergedAtDesc:
	default:
		auth.WriteError(w, "VALIDATION_ERROR", "sort must be openedAt, -openedAt, mergedAt or -mergedAt", http.StatusBadRequest)
		return
	}
	var ok bool
	if filter.Page, ok = positiveInt(q.Get("page"), 1, 1<<20); !ok {
		auth.WriteError(w, "VALIDATION_ERROR", "page must be a positive integer", http.StatusBadRequest)
		return
	}
	if filter.PageSize, ok = positiveInt(q.Get("pageSize"), defaultUpdatePRPageSize, maxUpdatePRPageSize); !ok {
		auth.WriteError(w, "VALIDATION_ERROR", "pageSize must be an integer from 1 to 100", http.StatusBadRequest)
		return
	}

	scope := apimw.ScopeFromContext(r.Context())
	if scope.All() {
		filter.AllRepositories = true
	} else {
		filter.RepositoryIDs, _ = scope.IDs()
	}
	items, total, err := h.PRs.List(r.Context(), filter)
	if err != nil {
		slog.Error("update PRs: listing failed", "err", err)
		auth.WriteError(w, "INTERNAL_ERROR", "failed to list update PRs", http.StatusInternalServerError)
		return
	}
	out := make([]updatePRResponse, 0, len(items))
	for _, item := range items {
		out = append(out, newUpdatePRResponse(item))
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{"items": out, "page": filter.Page, "pageSize": filter.PageSize, "total": total})
}
