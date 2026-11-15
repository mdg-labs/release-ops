package store

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/mdg-labs/release-ops/internal/store/db"
)

// Where an update PR's dependency list came from (update_pr_dependencies.source).
const (
	DependencySourceReport = "report"
	DependencySourceBody   = "body"
)

// UpdatePRDependency is one dependency an update PR changes. Every field but Name and Source
// may be empty, which is stored as NULL; UpdateType, when set, is one of major, minor, patch,
// digest, lockfile and other.
type UpdatePRDependency struct {
	Name, Manager, Datasource, From, To, UpdateType string
	// Source is DependencySourceReport or DependencySourceBody.
	Source string
}

// RenovateReport is the newest readable Renovate report stored for a repository.
type RenovateReport struct {
	// FullName is the repository's owner/name, the key of its entry in the report.
	FullName string
	// Report is the report as the worker posted it.
	Report string
}

// DependenciesInput is the derived state of one update PR: its dependencies and release notes.
type DependenciesInput struct {
	RepositoryID string
	Number       int64
	// EventAt is the EventAt of the report the state was derived from. A PR row that GitHub
	// changed after that time keeps what it has.
	EventAt time.Time
	// Dependencies replace the PR's stored list as a set.
	Dependencies []UpdatePRDependency
	// KeepEmpty keeps the stored list when Dependencies is empty, so a PR that is no longer
	// open does not lose the list it was recorded with.
	KeepEmpty bool
	// ReportSHA is the report branch's head commit the list was read for; empty when the list
	// is not from a report.
	ReportSHA string
	// HeadSHA is the PR's head commit. A list that is not from a report (ReportSHA empty)
	// leaves a stored report list alone while the stored report_sha is this commit, so a later
	// report without the PR does not replace what was read at the PR's current head.
	HeadSHA string
	// ReleaseNotes is the stored excerpt; empty stores NULL.
	ReleaseNotes string
	Now          time.Time
}

// DependenciesResult says what RecordDependencies did.
type DependenciesResult struct {
	// Applied is false when the PR is not stored or GitHub changed it after EventAt; nothing
	// was written then.
	Applied bool
	// UpdatePRID and State are the stored row's.
	UpdatePRID string
	State      string
	// Changed reports whether the dependency list or the release-notes excerpt changed.
	Changed bool
	// Dependencies is how many dependency rows the PR has after the call.
	Dependencies int
}

// Superseded is a closed PR that an open one replaced.
type Superseded struct {
	ID, ByID         string
	Number, ByNumber int64
}

// LatestReport returns the newest readable Renovate report of the repository, read through tx,
// or false when it has none.
func (r updatePRRepo) LatestReport(ctx context.Context, tx *sql.Tx, repositoryID string) (RenovateReport, bool, error) {
	row, err := r.store.writes.WithTx(tx).GetLatestRenovateReport(ctx, repositoryID)
	if errors.Is(err, sql.ErrNoRows) {
		return RenovateReport{}, false, nil
	}
	if err != nil {
		return RenovateReport{}, false, fmt.Errorf("read renovate report: %w", err)
	}
	return RenovateReport{FullName: row.FullName, Report: row.Report.String}, true, nil
}

// RecordDependencies stores the derived state of the PR through tx: its dependency list is
// replaced as a set, never row by row, and left untouched when the set is the same, so a replay
// changes nothing.
func (r updatePRRepo) RecordDependencies(ctx context.Context, tx *sql.Tx, in DependenciesInput) (DependenciesResult, error) {
	q := r.store.writes.WithTx(tx)
	row, err := q.GetUpdatePRForDependencies(ctx, db.GetUpdatePRForDependenciesParams{RepositoryID: in.RepositoryID, Number: in.Number})
	if errors.Is(err, sql.ErrNoRows) {
		return DependenciesResult{}, nil
	}
	if err != nil {
		return DependenciesResult{}, fmt.Errorf("read update PR: %w", err)
	}
	seen, err := time.Parse(timeLayout, row.LastSeenAt)
	if err != nil {
		return DependenciesResult{}, fmt.Errorf("stored last_seen_at %q: %w", row.LastSeenAt, err)
	}
	if seen.After(in.EventAt) {
		return DependenciesResult{}, nil
	}
	res := DependenciesResult{Applied: true, UpdatePRID: row.ID, State: row.State}

	stored, err := q.ListUpdatePRDependencies(ctx, row.ID)
	if err != nil {
		return DependenciesResult{}, fmt.Errorf("list update PR dependencies: %w", err)
	}
	have := make([]UpdatePRDependency, 0, len(stored))
	for _, d := range stored {
		have = append(have, UpdatePRDependency{
			Name: d.Name, Manager: d.Manager.String, Datasource: d.Datasource.String, From: d.FromVersion.String,
			To: d.ToVersion.String, UpdateType: d.UpdateType.String, Source: d.Source,
		})
	}
	keepReport := in.ReportSHA == "" && in.HeadSHA != "" && row.ReportSha.Valid && row.ReportSha.String == in.HeadSHA
	res.Dependencies = len(have)
	if !keepReport && (len(in.Dependencies) > 0 || !in.KeepEmpty) {
		res.Dependencies = len(in.Dependencies)
		if !sameDependencies(have, in.Dependencies) {
			if err := q.DeleteUpdatePRDependencies(ctx, row.ID); err != nil {
				return DependenciesResult{}, fmt.Errorf("delete update PR dependencies: %w", err)
			}
			for _, d := range in.Dependencies {
				err := q.InsertUpdatePRDependency(ctx, db.InsertUpdatePRDependencyParams{
					ID: newID(), UpdatePrID: row.ID, Name: d.Name, Manager: nullIfEmpty(d.Manager),
					Datasource: nullIfEmpty(d.Datasource), FromVersion: nullIfEmpty(d.From), ToVersion: nullIfEmpty(d.To),
					UpdateType: nullIfEmpty(d.UpdateType), Source: d.Source,
				})
				if err != nil {
					return DependenciesResult{}, fmt.Errorf("insert update PR dependency: %w", err)
				}
			}
			res.Changed = true
		}
	}

	notes, sha := nullIfEmpty(in.ReleaseNotes), nullIfEmpty(in.ReportSHA)
	if keepReport || (len(in.Dependencies) == 0 && in.KeepEmpty) {
		sha = row.ReportSha
	}
	if row.ReleaseNotes != notes || row.ReportSha != sha {
		if err := q.SetUpdatePRNotes(ctx, db.SetUpdatePRNotesParams{
			ReleaseNotes: notes, ReportSha: sha, UpdatedAt: formatTime(in.Now), ID: row.ID,
		}); err != nil {
			return DependenciesResult{}, fmt.Errorf("update release notes: %w", err)
		}
		res.Changed = res.Changed || row.ReleaseNotes != notes
	}
	return res, nil
}

// SupersedeReplaced marks the closed, unmerged PRs that the open PR with id and number replaces
// as superseded, through tx, and returns them. A closed PR is replaced by an open PR of the
// same repository with a higher number that carries a dependency of the same name, and of the
// same manager and datasource where both are known. A merged PR is never replaced, and a PR
// that is already superseded is left as it is.
func (r updatePRRepo) SupersedeReplaced(ctx context.Context, tx *sql.Tx, repositoryID, id string, number int64, now time.Time) ([]Superseded, error) {
	q := r.store.writes.WithTx(tx)
	old, err := q.ListSupersededByOpenPR(ctx, db.ListSupersededByOpenPRParams{OpenID: id, RepositoryID: repositoryID, OpenNumber: number})
	if err != nil {
		return nil, fmt.Errorf("list replaced update PRs: %w", err)
	}
	var out []Superseded
	for _, o := range old {
		n, err := q.SupersedeUpdatePR(ctx, db.SupersedeUpdatePRParams{
			SupersededByID: sql.NullString{String: id, Valid: true}, UpdatedAt: formatTime(now), ID: o.ID,
		})
		if err != nil {
			return nil, fmt.Errorf("supersede update PR: %w", err)
		}
		if n > 0 {
			out = append(out, Superseded{ID: o.ID, Number: o.Number, ByID: id, ByNumber: number})
		}
	}
	return out, nil
}

// SupersedeIfReplaced marks the closed PR with id and number as superseded through tx when an
// open PR replaces it (SupersedeReplaced), and returns what it did.
func (r updatePRRepo) SupersedeIfReplaced(ctx context.Context, tx *sql.Tx, repositoryID, id string, number int64, now time.Time) ([]Superseded, error) {
	q := r.store.writes.WithTx(tx)
	nw, err := q.GetReplacementOfClosedPR(ctx, db.GetReplacementOfClosedPRParams{ClosedID: id, RepositoryID: repositoryID, ClosedNumber: number})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find replacement of update PR: %w", err)
	}
	n, err := q.SupersedeUpdatePR(ctx, db.SupersedeUpdatePRParams{
		SupersededByID: sql.NullString{String: nw.ID, Valid: true}, UpdatedAt: formatTime(now), ID: id,
	})
	if err != nil {
		return nil, fmt.Errorf("supersede update PR: %w", err)
	}
	if n == 0 {
		return nil, nil
	}
	return []Superseded{{ID: id, Number: number, ByID: nw.ID, ByNumber: nw.Number}}, nil
}

// sameDependencies reports whether a and b hold the same dependencies, whatever their order.
func sameDependencies(a, b []UpdatePRDependency) bool {
	if len(a) != len(b) {
		return false
	}
	a, b = slices.Clone(a), slices.Clone(b)
	less := func(x, y UpdatePRDependency) int {
		return cmp.Or(cmp.Compare(x.Name, y.Name), cmp.Compare(x.Manager, y.Manager), cmp.Compare(x.Datasource, y.Datasource),
			cmp.Compare(x.From, y.From), cmp.Compare(x.To, y.To), cmp.Compare(x.UpdateType, y.UpdateType), cmp.Compare(x.Source, y.Source))
	}
	slices.SortFunc(a, less)
	slices.SortFunc(b, less)
	return slices.Equal(a, b)
}
