package updateprs

import (
	"context"
	"database/sql"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mdg-labs/release-ops/internal/github"
	"github.com/mdg-labs/release-ops/internal/store"
)

// Caps on the text of a PR that is stored. The text is data: it is never logged, rendered as
// markup or used as template source.
const (
	MaxTitleBytes = 1024
	maxURLBytes   = 2048
	maxRefBytes   = 512
	maxLabels     = 100
	maxLabelBytes = 100
)

// Tracker applies what GitHub reports about a PR to its update_prs row.
type Tracker struct {
	store store.UpdatePRRepository
	now   func() time.Time
	wake  *Notifier
	sink  func([]Event)
}

// NewTracker returns a Tracker over the update PR repository; now defaults to time.Now, and
// wake, which may be nil, is what Wake fires.
func NewTracker(s store.UpdatePRRepository, now func() time.Time, wake *Notifier) *Tracker {
	if now == nil {
		now = time.Now
	}
	return &Tracker{store: s, now: now, wake: wake}
}

// Wake fires the notifier. A caller that applied a PR through a transaction runs it after the
// commit, when the change is visible to the ticket engine.
func (t *Tracker) Wake() { t.wake.Wake() }

// Subscribe makes Publish hand the events it receives to sink. It is called once, before the
// Tracker is used.
func (t *Tracker) Subscribe(sink func([]Event)) { t.sink = sink }

// Publish hands the events of applied PRs to the subscriber. A caller that applied PRs through a
// transaction runs it after the commit, when the rows the events describe are final; with no
// subscriber, or no events, it does nothing. The sink only queues the events: the work they
// cause never runs in the caller.
func (t *Tracker) Publish(events []Event) {
	if t.sink != nil && len(events) > 0 {
		t.sink(events)
	}
}

// ApplyTx upserts a PR of the repository through tx when it is an update PR, then records its
// dependencies and release notes and applies the superseded rule, all in tx. A PR is an update
// PR when its author is the bot user, by numeric id, and its body carries Renovate's markers;
// branch name and title decide nothing. A PR that is already tracked stays tracked while the
// bot is its author, so an edit that drops the markers does not freeze its state. A PR that
// lacks what tracking needs, or whose bot id is not known (botUserID 0), is ignored. The
// Outcome says whether anything changed and carries the events of the step; a report the
// stored row outranks (an older one, or an open one at the EventAt of a close) changes nothing,
// records nothing and has no events.
func (t *Tracker) ApplyTx(ctx context.Context, tx *sql.Tx, repositoryID string, pr github.PullRequest, botUserID int64) (Outcome, error) {
	if botUserID == 0 || pr.User.ID != botUserID || !pr.Valid() {
		return Outcome{}, nil
	}
	if !HasRenovateMarkers(pr.Body) {
		tracked, err := t.store.Tracked(ctx, tx, repositoryID, pr.Number)
		if err != nil || !tracked {
			return Outcome{}, err
		}
	}
	in := input(repositoryID, pr, t.now())
	up, err := t.store.Upsert(ctx, tx, in)
	if err != nil || !up.Accepted {
		return Outcome{}, err
	}
	events, depsChanged, err := t.recordDependencies(ctx, tx, repositoryID, pr, in)
	if err != nil {
		return Outcome{}, err
	}
	return Outcome{Changed: up.Changed || depsChanged, Events: events}, nil
}

// input maps a valid PR to the row it is stored as.
func input(repositoryID string, pr github.PullRequest, now time.Time) store.UpdatePRInput {
	in := store.UpdatePRInput{
		RepositoryID: repositoryID,
		Number:       pr.Number,
		NodeID:       capBytes(pr.NodeID, maxRefBytes),
		URL:          capBytes(pr.HTMLURL, maxURLBytes),
		Title:        capBytes(pr.Title, MaxTitleBytes),
		HeadRef:      capBytes(pr.Head.Ref, maxRefBytes),
		HeadSHA:      capBytes(pr.Head.SHA, maxRefBytes),
		State:        store.UpdatePROpen,
		OpenedAt:     *pr.CreatedAt,
		EventAt:      *pr.UpdatedAt,
		Now:          now,
	}
	if pr.State == "closed" {
		in.State = store.UpdatePRClosed
		if pr.Merged || pr.MergedAt != nil {
			in.State = store.UpdatePRMerged
		}
		if pr.ClosedAt != nil {
			in.ClosedAt = *pr.ClosedAt
		}
		if pr.MergedAt != nil {
			in.MergedAt = *pr.MergedAt
		}
		if in.State == store.UpdatePRMerged && in.MergedAt.IsZero() {
			in.MergedAt = latest(in.ClosedAt, in.EventAt)
		}
		in.EventAt = latest(in.EventAt, in.ClosedAt, in.MergedAt)
	}
	for _, l := range pr.Labels {
		if len(in.Labels) == maxLabels {
			break
		}
		in.Labels = append(in.Labels, capBytes(l.Name, maxLabelBytes))
	}
	return in
}

// capBytes shortens s to at most n bytes without cutting a character.
func capBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// checkboxMarker matches a Renovate checkbox line, " - [ ] <!-- name -->", ticked or not.
var checkboxMarker = regexp.MustCompile(`^\s*[-*+]\s+\[[ xX]\]\s+<!--\s*\S.*?-->`)

// HasRenovateMarkers reports whether a PR body carries what Renovate puts in the body it
// generates: the renovate-debug comment, or a checkbox line with a marker comment.
func HasRenovateMarkers(body *string) bool {
	if body == nil {
		return false
	}
	if strings.Contains(*body, "<!--renovate-debug:") {
		return true
	}
	for line := range strings.SplitSeq(*body, "\n") {
		if checkboxMarker.MatchString(strings.TrimSuffix(line, "\r")) {
			return true
		}
	}
	return false
}

// latest returns the latest of the times.
func latest(first time.Time, rest ...time.Time) time.Time {
	for _, t := range rest {
		if t.After(first) {
			first = t
		}
	}
	return first
}
