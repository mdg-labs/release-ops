package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/mdg-labs/release-ops/internal/store/db"
)

// States of an update PR (specs.html section 5.1). Tracking sets open, merged and closed; the
// supersede check sets superseded.
const (
	UpdatePROpen       = "open"
	UpdatePRMerged     = "merged"
	UpdatePRClosed     = "closed"
	UpdatePRSuperseded = "superseded"
)

// UpdatePRInput is what GitHub reports about a bot PR, from a webhook payload or from the pulls
// API.
type UpdatePRInput struct {
	RepositoryID string
	Number       int64
	NodeID       string
	URL          string
	Title        string
	HeadRef      string
	HeadSHA      string
	// State is open, merged or closed.
	State  string
	Labels []string
	// OpenedAt, MergedAt and ClosedAt are as GitHub reports them; zero means none.
	OpenedAt, MergedAt, ClosedAt time.Time
	// EventAt is when GitHub last changed the PR (its updated_at). It orders deliveries: a
	// report older than the stored one never overwrites it.
	EventAt time.Time
	// Now stamps created_at and updated_at.
	Now time.Time
}

// ReconcileRepository is a repository whose PRs the reconcile reads from GitHub.
type ReconcileRepository struct {
	ID       string
	FullName string
	// GitHubInstallationID is the id of its installation on GitHub.
	GitHubInstallationID int64
}

// UpsertResult says what Upsert did with a report.
type UpsertResult struct {
	// Accepted is false for a report the row's state outranks, a stale one or one that would
	// reopen a PR GitHub closed at the same EventAt; nothing was written then, and nothing
	// derived from that report may be either.
	Accepted bool
	// Changed reports whether the row changed in anything but its last_seen_at.
	Changed bool
}

// UpdatePRRepository reads and writes the update_prs rows.
type UpdatePRRepository interface {
	// Tracked reports whether the repository has a row for the PR number, read through tx.
	Tracked(ctx context.Context, tx *sql.Tx, repositoryID string, number int64) (bool, error)
	// Upsert stores the PR through tx and reports whether the report was accepted and whether
	// the row changed in anything but its last_seen_at, so a caller wakes the ticket engine only
	// for a change. The row's last_seen_at
	// holds the EventAt of the newest report applied, and a report older than it changes
	// nothing; at the same EventAt a report that reopens a merged, closed or superseded PR
	// changes nothing either, because GitHub stamps a close and an earlier edit alike. A PR
	// closed by CloseGone carries no such stamp, so an open report at its EventAt reopens it. A
	// closed report keeps the superseded state of a row, and an open one clears its
	// replacement. A report that changes nothing for either reason is not Accepted.
	Upsert(ctx context.Context, tx *sql.Tx, in UpdatePRInput) (UpsertResult, error)
	// CloseGone stores the repository's open PR with the number as closed, stamped closed at
	// now, through tx, for a PR GitHub no longer knows, and reports whether it did. A row that
	// is not open, or absent, is left as it is. It keeps last_seen_at, and a later report that
	// lists the PR open reopens it.
	CloseGone(ctx context.Context, tx *sql.Tx, repositoryID string, number int64, now time.Time) (changed bool, err error)
	// LatestReport returns the newest readable Renovate report of the repository through tx, and
	// false when it has none.
	LatestReport(ctx context.Context, tx *sql.Tx, repositoryID string) (RenovateReport, bool, error)
	// RecordDependencies stores a PR's derived dependency list and release-notes excerpt
	// through tx. See DependenciesInput.
	RecordDependencies(ctx context.Context, tx *sql.Tx, in DependenciesInput) (DependenciesResult, error)
	// SupersedeReplaced marks the closed PRs that the open PR with the id and number replaces
	// as superseded, through tx, and returns them; a replay returns none.
	SupersedeReplaced(ctx context.Context, tx *sql.Tx, repositoryID, id string, number int64, now time.Time) ([]Superseded, error)
	// SupersedeIfReplaced marks the closed PR with the id and number as superseded through tx
	// when an open PR replaces it, and returns it; a replay returns none.
	SupersedeIfReplaced(ctx context.Context, tx *sql.Tx, repositoryID, id string, number int64, now time.Time) ([]Superseded, error)
	// OpenNumbers returns the numbers of the repository's open PRs, ascending.
	OpenNumbers(ctx context.Context, repositoryID string) ([]int64, error)
	// OpenCounts returns the number of open PRs of every repository that is not removed, by
	// full name; a repository without any is present with 0.
	OpenCounts(ctx context.Context) (map[string]int, error)
	// ReconcileRepository returns the repository, or ErrRepositoryNotFound when it is removed,
	// paused or of an installation that is not active.
	ReconcileRepository(ctx context.Context, id string) (ReconcileRepository, error)
	// ReconcileRepositoryIDs returns the ids of every repository ReconcileRepository finds.
	ReconcileRepositoryIDs(ctx context.Context) ([]string, error)
}

// UpdatePRs returns the update PR repository.
func (s *Store) UpdatePRs() UpdatePRRepository {
	return updatePRRepo{store: s}
}

type updatePRRepo struct {
	store *Store
}

func (r updatePRRepo) Tracked(ctx context.Context, tx *sql.Tx, repositoryID string, number int64) (bool, error) {
	_, err := r.store.writes.WithTx(tx).GetUpdatePR(ctx, db.GetUpdatePRParams{RepositoryID: repositoryID, Number: number})
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read update PR: %w", err)
	}
	return true, nil
}

func (r updatePRRepo) Upsert(ctx context.Context, tx *sql.Tx, in UpdatePRInput) (UpsertResult, error) {
	labels, err := json.Marshal(append([]string{}, in.Labels...))
	if err != nil {
		return UpsertResult{}, fmt.Errorf("encode labels: %w", err)
	}
	q := r.store.writes.WithTx(tx)
	row, err := q.GetUpdatePR(ctx, db.GetUpdatePRParams{RepositoryID: in.RepositoryID, Number: in.Number})
	if errors.Is(err, sql.ErrNoRows) {
		err := q.InsertUpdatePR(ctx, db.InsertUpdatePRParams{
			ID: newID(), RepositoryID: in.RepositoryID, Number: in.Number, NodeID: in.NodeID, Url: in.URL,
			Title: in.Title, HeadRef: in.HeadRef, HeadSha: nullIfEmpty(in.HeadSHA), State: in.State, Labels: string(labels),
			OpenedAt: nullTime(in.OpenedAt), MergedAt: nullTime(in.MergedAt), ClosedAt: nullTime(in.ClosedAt),
			LastSeenAt: formatTime(in.EventAt), CreatedAt: formatTime(in.Now), UpdatedAt: formatTime(in.Now),
		})
		if err != nil {
			return UpsertResult{}, fmt.Errorf("insert update PR: %w", err)
		}
		return UpsertResult{Accepted: true, Changed: true}, nil
	}
	if err != nil {
		return UpsertResult{}, fmt.Errorf("read update PR: %w", err)
	}

	seen, err := time.Parse(timeLayout, row.LastSeenAt)
	if err != nil {
		return UpsertResult{}, fmt.Errorf("stored last_seen_at %q: %w", row.LastSeenAt, err)
	}
	if in.EventAt.Before(seen) {
		return UpsertResult{}, nil
	}
	if in.EventAt.Equal(seen) && in.State == UpdatePROpen && row.State != UpdatePROpen && !closedAsGone(row, seen) {
		return UpsertResult{}, nil
	}

	state, supersededBy := in.State, row.SupersededByID
	switch {
	case in.State == UpdatePRClosed && row.State == UpdatePRSuperseded:
		state = UpdatePRSuperseded
	case in.State == UpdatePROpen:
		supersededBy = sql.NullString{}
	}
	next := db.UpdateUpdatePRParams{
		NodeID: in.NodeID, Url: in.URL, Title: in.Title, HeadRef: in.HeadRef, HeadSha: nullIfEmpty(in.HeadSHA),
		State: state, SupersededByID: supersededBy, Labels: string(labels),
		OpenedAt: nullTime(in.OpenedAt), MergedAt: nullTime(in.MergedAt), ClosedAt: nullTime(in.ClosedAt),
		LastSeenAt: formatTime(in.EventAt), UpdatedAt: formatTime(in.Now), ID: row.ID,
	}
	changed := row.NodeID != next.NodeID || row.Url != next.Url || row.Title != next.Title || row.HeadRef != next.HeadRef ||
		row.HeadSha != next.HeadSha || row.State != next.State || row.SupersededByID != next.SupersededByID ||
		row.Labels != next.Labels || row.OpenedAt != next.OpenedAt || row.MergedAt != next.MergedAt || row.ClosedAt != next.ClosedAt
	if !changed && row.LastSeenAt == next.LastSeenAt {
		return UpsertResult{Accepted: true}, nil
	}
	if err := q.UpdateUpdatePR(ctx, next); err != nil {
		return UpsertResult{}, fmt.Errorf("update update PR: %w", err)
	}
	return UpsertResult{Accepted: true, Changed: changed}, nil
}

// closedAsGone reports whether the row was closed by CloseGone and not by a report from GitHub:
// a close from GitHub never carries a closed_at later than the row's last_seen_at, CloseGone's
// is stamped with the time it ran.
func closedAsGone(row db.GetUpdatePRRow, seen time.Time) bool {
	if row.State != UpdatePRClosed || !row.ClosedAt.Valid {
		return false
	}
	closed, err := time.Parse(timeLayout, row.ClosedAt.String)
	return err == nil && closed.After(seen)
}

func (r updatePRRepo) CloseGone(ctx context.Context, tx *sql.Tx, repositoryID string, number int64, now time.Time) (bool, error) {
	n, err := r.store.writes.WithTx(tx).CloseGoneUpdatePR(ctx, db.CloseGoneUpdatePRParams{
		ClosedAt: sql.NullString{String: formatTime(now), Valid: true}, UpdatedAt: formatTime(now),
		RepositoryID: repositoryID, Number: number,
	})
	if err != nil {
		return false, fmt.Errorf("close update PR: %w", err)
	}
	return n > 0, nil
}

func (r updatePRRepo) OpenNumbers(ctx context.Context, repositoryID string) ([]int64, error) {
	numbers, err := r.store.reads.ListOpenUpdatePRNumbers(ctx, repositoryID)
	if err != nil {
		return nil, fmt.Errorf("list open update PRs: %w", err)
	}
	return numbers, nil
}

func (r updatePRRepo) OpenCounts(ctx context.Context) (map[string]int, error) {
	rows, err := r.store.reads.CountOpenUpdatePRsByRepository(ctx)
	if err != nil {
		return nil, fmt.Errorf("count open update PRs: %w", err)
	}
	out := make(map[string]int, len(rows))
	for _, row := range rows {
		out[row.FullName] = int(row.OpenCount)
	}
	return out, nil
}

func (r updatePRRepo) ReconcileRepository(ctx context.Context, id string) (ReconcileRepository, error) {
	row, err := r.store.reads.GetReconcileRepository(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return ReconcileRepository{}, ErrRepositoryNotFound
	}
	if err != nil {
		return ReconcileRepository{}, fmt.Errorf("read repository: %w", err)
	}
	return ReconcileRepository{ID: row.ID, FullName: row.FullName, GitHubInstallationID: row.GithubInstallationID}, nil
}

func (r updatePRRepo) ReconcileRepositoryIDs(ctx context.Context) ([]string, error) {
	ids, err := r.store.reads.ListReconcileRepositoryIDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("list repositories: %w", err)
	}
	return ids, nil
}

func nullTime(t time.Time) sql.NullString {
	if t.IsZero() {
		return sql.NullString{}
	}
	return sql.NullString{String: formatTime(t), Valid: true}
}

func nullIfEmpty(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}
