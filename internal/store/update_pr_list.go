package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/mdg-labs/release-ops/internal/store/db"
)

// Sort orders of the update PR list (specs.html section 7.6).
const (
	UpdatePRSortOpenedAt     = "openedAt"
	UpdatePRSortOpenedAtDesc = "-openedAt"
	UpdatePRSortMergedAt     = "mergedAt"
	UpdatePRSortMergedAtDesc = "-mergedAt"
)

// UpdatePRListFilter selects and pages the update PRs List returns.
type UpdatePRListFilter struct {
	// AllRepositories reads every repository (the admin scope). Otherwise only the PRs of
	// RepositoryIDs are read, and an empty list reads none.
	AllRepositories bool
	RepositoryIDs   []string
	// State is open, merged, closed or superseded.
	State string
	// RepositoryID, when set, keeps the PRs of that repository; one outside the scope has none.
	RepositoryID string
	// UpdateType, when set, keeps the PRs with a dependency of that type.
	UpdateType string
	// Sort is one of the UpdatePRSort values.
	Sort string
	// Page counts from 1.
	Page, PageSize int
}

// UpdatePRListItem is an update PR as the dashboard lists it.
type UpdatePRListItem struct {
	ID, RepositoryID, RepositoryFullName string
	Number                               int64
	URL, Title, State                    string
	// OpenedAt, MergedAt and ClosedAt are the stored UTC timestamps; empty when GitHub reported none.
	OpenedAt, MergedAt, ClosedAt string
	// SupersededBy is the PR that replaced this one; nil while none did.
	SupersededBy *UpdatePRRef
	// UpdateTypes are the distinct update types among the dependencies, in the order they first
	// occur.
	UpdateTypes  []string
	Dependencies []UpdatePRListDependency
	// Ticket is nil while the PR has none.
	Ticket *UpdatePRListTicket
}

// UpdatePRRef names another update PR.
type UpdatePRRef struct {
	ID     string
	Number int64
	URL    string
}

// UpdatePRListDependency is one dependency of a listed PR; From, To and UpdateType are empty
// when unknown.
type UpdatePRListDependency struct {
	Name, From, To, UpdateType string
}

// UpdatePRListTicket is the ticket of a listed PR as Release Ops last recorded it. It never
// carries the error text, only whether an operation on the ticket is failing.
type UpdatePRListTicket struct {
	State string
	// ExternalID and ExternalURL are empty until the ticket is created.
	ExternalID, ExternalURL string
	HasError                bool
}

// UpdatePRLister reads the update PRs for the dashboard.
type UpdatePRLister interface {
	// List returns one page of the update PRs the filter selects and the number it selects in
	// all, both read in one transaction.
	List(ctx context.Context, filter UpdatePRListFilter) ([]UpdatePRListItem, int, error)
}

// UpdatePRList returns the update PR lister.
func (s *Store) UpdatePRList() UpdatePRLister {
	return updatePRLister{store: s}
}

type updatePRLister struct {
	store *Store
}

func (l updatePRLister) List(ctx context.Context, f UpdatePRListFilter) ([]UpdatePRListItem, int, error) {
	if !f.AllRepositories && len(f.RepositoryIDs) == 0 {
		return []UpdatePRListItem{}, 0, nil
	}
	ids := f.RepositoryIDs
	if ids == nil {
		ids = []string{}
	}
	scope, err := json.Marshal(ids)
	if err != nil {
		return nil, 0, fmt.Errorf("encode repository scope: %w", err)
	}
	all := int64(0)
	if f.AllRepositories {
		all = 1
	}
	page, size := max(f.Page, 1), max(f.PageSize, 1)

	tx, err := l.store.db.Reader.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, 0, fmt.Errorf("begin update PR list read: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	q := l.store.reads.WithTx(tx)

	rows, err := q.ListScopedUpdatePRs(ctx, db.ListScopedUpdatePRsParams{
		Sort: f.Sort, AllRepositories: all, ScopeIds: string(scope), State: f.State,
		RepositoryID: f.RepositoryID, UpdateType: f.UpdateType,
		PageSize: int64(size), PageOffset: int64((page - 1) * size),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("list update PRs: %w", err)
	}
	total, err := q.CountScopedUpdatePRs(ctx, db.CountScopedUpdatePRsParams{
		AllRepositories: all, ScopeIds: string(scope), State: f.State,
		RepositoryID: f.RepositoryID, UpdateType: f.UpdateType,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("count update PRs: %w", err)
	}

	items := make([]UpdatePRListItem, 0, len(rows))
	for _, row := range rows {
		item, err := updatePRListItem(row)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, int(total), nil
}

func updatePRListItem(row db.ListScopedUpdatePRsRow) (UpdatePRListItem, error) {
	item := UpdatePRListItem{
		ID: row.ID, RepositoryID: row.RepositoryID, RepositoryFullName: row.RepositoryFullName,
		Number: row.Number, URL: row.Url, Title: row.Title, State: row.State,
		OpenedAt: row.OpenedAt.String, MergedAt: row.MergedAt.String, ClosedAt: row.ClosedAt.String,
		UpdateTypes: []string{}, Dependencies: []UpdatePRListDependency{},
	}
	if row.SupersededByID.Valid {
		item.SupersededBy = &UpdatePRRef{ID: row.SupersededByID.String, Number: row.SupersededByNumber.Int64, URL: row.SupersededByUrl.String}
	}
	var deps []struct {
		Name       string  `json:"name"`
		From       *string `json:"from"`
		To         *string `json:"to"`
		UpdateType *string `json:"updateType"`
	}
	if err := json.Unmarshal([]byte(row.Dependencies), &deps); err != nil {
		return UpdatePRListItem{}, fmt.Errorf("dependencies of update PR %s: %w", row.ID, err)
	}
	seen := map[string]bool{}
	for _, d := range deps {
		dep := UpdatePRListDependency{Name: d.Name}
		if d.From != nil {
			dep.From = *d.From
		}
		if d.To != nil {
			dep.To = *d.To
		}
		if d.UpdateType != nil {
			dep.UpdateType = *d.UpdateType
			if !seen[dep.UpdateType] {
				seen[dep.UpdateType] = true
				item.UpdateTypes = append(item.UpdateTypes, dep.UpdateType)
			}
		}
		item.Dependencies = append(item.Dependencies, dep)
	}
	if row.TicketState.Valid {
		item.Ticket = &UpdatePRListTicket{
			State: row.TicketState.String, ExternalID: row.TicketExternalID.String,
			ExternalURL: row.TicketExternalUrl.String, HasError: row.TicketHasError == 1,
		}
	}
	return item, nil
}
