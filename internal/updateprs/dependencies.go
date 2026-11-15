package updateprs

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mdg-labs/release-ops/internal/github"
	"github.com/mdg-labs/release-ops/internal/store"
)

// Kinds of Event.
const (
	// EventDependenciesRecorded follows the dependency step of an update PR, whether or not it
	// found a dependency: a PR with none is one the ticket engine skips.
	EventDependenciesRecorded = "dependencies_recorded"
	// EventSuperseded is a closed PR that an open one replaced.
	EventSuperseded = "superseded"
)

// Event is a domain event of an update PR. It is produced inside the transaction that wrote
// the rows it describes and returned to the caller in Outcome.Events, so a consumer that acts on
// it after that transaction commits sees the rows final. The ticket engine that consumes the
// events is wired separately; until then the callers do not forward them.
type Event struct {
	Kind         string
	RepositoryID string
	// UpdatePRID and Number identify the PR; for EventSuperseded it is the replaced PR.
	UpdatePRID string
	Number     int64
	// Dependencies is how many dependencies the PR has (EventDependenciesRecorded).
	Dependencies int
	// ByUpdatePRID and ByNumber identify the replacing PR (EventSuperseded).
	ByUpdatePRID string
	ByNumber     int64
}

// Outcome is what ApplyTx did to a PR.
type Outcome struct {
	// Changed reports a change a ticket engine has to see: the PR row, its dependencies or
	// release notes, or a supersede.
	Changed bool
	// Events are the events of the step, in order; none when the PR was not applied.
	Events []Event
}

// recordDependencies derives the dependencies and release notes of the stored PR and records
// them. A PR is derived from the repository's newest Renovate report when it has an entry for
// the PR that was read at the PR's head commit, and otherwise from the updates table of its
// body; with neither it has no dependencies. A list already stored from a report read at the
// PR's current head is kept, so a newer report that lacks the PR does not replace it. A report that is missing, unreadable or of an
// unknown shape is no report. The PR's release notes are its body's section, whatever the
// source of the dependencies.
func (t *Tracker) recordDependencies(ctx context.Context, tx *sql.Tx, repositoryID string, pr github.PullRequest, in store.UpdatePRInput) ([]Event, bool, error) {
	var deps []store.UpdatePRDependency
	reportSHA := ""
	rep, haveReport, err := t.store.LatestReport(ctx, tx, repositoryID)
	if err != nil {
		return nil, false, err
	}
	if haveReport {
		if e, ok := fromReport(rep.Report, rep.FullName, pr.Number, pr.Head.Ref); ok && (e.SHA == "" || pr.Head.SHA == "" || e.SHA == pr.Head.SHA) {
			deps, reportSHA = e.Dependencies, e.SHA
		}
	}
	body := ""
	if pr.Body != nil {
		body = *pr.Body
	}
	if len(deps) == 0 {
		deps = bodyDependencies(body)
	}

	res, err := t.store.RecordDependencies(ctx, tx, store.DependenciesInput{
		RepositoryID: repositoryID, Number: pr.Number, EventAt: in.EventAt,
		Dependencies: deps, KeepEmpty: in.State != store.UpdatePROpen,
		ReportSHA: reportSHA, HeadSHA: pr.Head.SHA, ReleaseNotes: releaseNotes(body), Now: in.Now,
	})
	if err != nil || !res.Applied {
		return nil, false, err
	}
	changed := res.Changed
	events := []Event{{
		Kind: EventDependenciesRecorded, RepositoryID: repositoryID, UpdatePRID: res.UpdatePRID, Number: pr.Number, Dependencies: res.Dependencies,
	}}

	var replaced []store.Superseded
	switch res.State {
	case store.UpdatePROpen:
		replaced, err = t.store.SupersedeReplaced(ctx, tx, repositoryID, res.UpdatePRID, pr.Number, in.Now)
	case store.UpdatePRClosed:
		replaced, err = t.store.SupersedeIfReplaced(ctx, tx, repositoryID, res.UpdatePRID, pr.Number, in.Now)
	}
	if err != nil {
		return nil, false, fmt.Errorf("supersede: %w", err)
	}
	for _, s := range replaced {
		changed = true
		events = append(events, Event{
			Kind: EventSuperseded, RepositoryID: repositoryID, UpdatePRID: s.ID, Number: s.Number, ByUpdatePRID: s.ByID, ByNumber: s.ByNumber,
		})
	}
	return events, changed, nil
}
