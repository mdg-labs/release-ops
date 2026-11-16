package updatetickets

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/mdg-labs/release-ops/internal/providers/ticket"
	"github.com/mdg-labs/release-ops/internal/ratelimit"
	"github.com/mdg-labs/release-ops/internal/store"
	"github.com/mdg-labs/release-ops/internal/tickettemplate"
	"github.com/mdg-labs/release-ops/internal/updateprs"
)

// Outcome says what Process decided for a PR.
type Outcome string

// The outcomes of Process.
const (
	// OutcomeCreated: the ticket was created and its link stored.
	OutcomeCreated Outcome = "created"
	// OutcomeNotFound: the PR is not stored, or its repository left the installation.
	OutcomeNotFound Outcome = "not_found"
	// OutcomeNotOpen: only open PRs get a ticket.
	OutcomeNotOpen Outcome = "not_open"
	// OutcomeNoDependencies: the PR has no recorded dependency, as a config-migration PR, an
	// onboarding PR or a PR whose dependencies are not recorded yet; it is looked at again when
	// they are.
	OutcomeNoDependencies Outcome = "no_dependencies"
	// OutcomeNoTicketProject: neither the repository nor the installation has a ticket project.
	OutcomeNoTicketProject Outcome = "no_ticket_project"
	// OutcomeFiltered: the ticket project's filter keeps the PR out.
	OutcomeFiltered Outcome = "filtered"
	// OutcomeExists: the PR already has its ticket.
	OutcomeExists Outcome = "exists"
	// OutcomeWaiting: an earlier attempt failed and its retry is not due.
	OutcomeWaiting Outcome = "waiting"
	// OutcomeUnconfirmed: an earlier create left its pending marker, so nothing is created.
	OutcomeUnconfirmed Outcome = "unconfirmed"
	// OutcomeFailed: the attempt failed; the error is recorded on the PR's ticket row.
	OutcomeFailed Outcome = "failed"
)

// ErrUnconfirmed is returned by Process for a PR whose earlier create may have stored a ticket
// that Release Ops did not record. Nothing is created for it until the marker is resolved.
var ErrUnconfirmed = errors.New("a ticket for this update PR may already exist in the ticket system; Release Ops could not confirm it, so it does not create a second one")

const (
	// linkAttempts bounds the writes of a created ticket's link: the ticket exists in the
	// tracker already, so a failed write is retried a few times before the marker is left to
	// guard the PR.
	linkAttempts = 3
	// failureWriteTimeout bounds the write of a failure, which runs even when ctx ended.
	failureWriteTimeout = 5 * time.Second
	// keyLine precedes the row id in the description: the external key of specs.html section
	// 5.2.2, appended outside the template so a template cannot drop it.
	keyLine = "Release Ops key: "
	// maxErrorRunes bounds the error stored for the Problems view.
	maxErrorRunes = 512
	minBackoff    = time.Minute
	maxBackoff    = 30 * time.Minute
)

// linkBackoff is the pause between link writes; a variable so a test can shorten it.
var linkBackoff = 100 * time.Millisecond

// Target is the ticket system of an integration: its provider and its kind.
type Target struct {
	Provider ticket.TicketProvider
	Kind     string
}

// Targets builds the provider of an integration from its stored, decrypted credentials.
type Targets interface {
	Target(ctx context.Context, integrationID string) (Target, error)
}

// Projects reads ticket projects.
type Projects interface {
	Get(ctx context.Context, id string) (*store.TicketProject, error)
}

// Engine creates the tickets of update PRs.
type Engine struct {
	Tickets  store.UpdateTicketRepository
	Projects Projects
	Targets  Targets
	// Comments, which may be nil, writes the ticket link comment on each ticketed PR (specs.html
	// section 5.2.3). Without it no comment is written.
	Comments PRComments
	// Wake, which may be nil, starts a pass over the PRs without a ticket when it fires.
	Wake *updateprs.Notifier
	// Now defaults to time.Now.
	Now func() time.Time
	// Rand returns a value in [0, 1) for the jitter of a retry the ticket system's rate limit
	// asked for; it defaults to math/rand.
	Rand func() float64
	// SweepInterval is how often the pass over the PRs without a ticket runs by itself;
	// it defaults to a minute.
	SweepInterval time.Duration
	// Logger defaults to slog.Default(). It receives ids, outcomes and errors, never
	// credentials, and PR text only through the redacted error message.
	Logger *slog.Logger

	mu sync.Mutex // serializes Process: one create at a time
	// cmu serializes the PR comment writes, so two passes never both post a comment, and guards
	// commentFailures, the pause after a failed write by ticket row id. It is separate from mu so
	// a slow GitHub never holds back the ticket system calls, and the pause is lost on a
	// restart, which only makes the next pass try at once.
	cmu             sync.Mutex
	commentFailures map[string]commentFailure
	qmu             sync.Mutex
	queued          []string
	queuedS         map[string]bool
	signal          chan struct{}
}

func (e *Engine) now() time.Time {
	if e.Now != nil {
		return e.Now().UTC()
	}
	return time.Now().UTC()
}

func (e *Engine) logger() *slog.Logger {
	if e.Logger != nil {
		return e.Logger
	}
	return slog.Default()
}

// Handle queues the PRs whose dependencies were recorded and the PRs that were replaced; Run
// processes them. It never blocks and does no work of its own, so a caller that holds a request
// is not delayed.
func (e *Engine) Handle(events []updateprs.Event) {
	e.qmu.Lock()
	if e.queuedS == nil {
		e.queuedS = map[string]bool{}
		e.signal = make(chan struct{}, 1)
	}
	for _, ev := range events {
		if (ev.Kind == updateprs.EventDependenciesRecorded || ev.Kind == updateprs.EventSuperseded) && !e.queuedS[ev.UpdatePRID] {
			e.queuedS[ev.UpdatePRID] = true
			e.queued = append(e.queued, ev.UpdatePRID)
		}
	}
	sig := e.signal
	e.qmu.Unlock()
	select {
	case sig <- struct{}{}:
	default:
	}
}

func (e *Engine) takeQueued() []string {
	e.qmu.Lock()
	defer e.qmu.Unlock()
	ids := e.queued
	e.queued, e.queuedS = nil, map[string]bool{}
	return ids
}

// Run processes queued events and, on a wake and on every sweep interval, the PRs that still lack
// a ticket and the tickets that may have an operation to run, until ctx ends. The pass over the
// stored PRs is what picks up a PR whose event was lost after its transaction committed, and the
// PRs whose retry has become due.
func (e *Engine) Run(ctx context.Context) {
	e.qmu.Lock()
	if e.queuedS == nil {
		e.queuedS = map[string]bool{}
		e.signal = make(chan struct{}, 1)
	}
	sig := e.signal
	e.qmu.Unlock()

	interval := e.SweepInterval
	if interval <= 0 {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	e.sweep(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-sig:
			for _, id := range e.takeQueued() {
				e.run(ctx, id)
			}
		case <-e.Wake.C():
			e.sweep(ctx)
		case <-ticker.C:
			e.sweep(ctx)
		}
	}
}

// sweep processes every PR that lacks a ticket and is not waiting on a retry or a marker, then
// the tickets that may have an operation to run.
func (e *Engine) sweep(ctx context.Context) {
	ids, err := e.Tickets.PendingPRs(ctx, e.now())
	if err != nil {
		if ctx.Err() == nil {
			e.logger().Error("update tickets: listing the PRs without a ticket failed", "error", err)
		}
		return
	}
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		e.run(ctx, id)
	}
	ids, err = e.Tickets.SyncablePRs(ctx, e.now())
	if err != nil {
		if ctx.Err() == nil {
			e.logger().Error("update tickets: listing the tickets to sync failed", "error", err)
		}
		return
	}
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		e.sync(ctx, id, false)
	}
	e.commentSweep(ctx)
}

// run creates the ticket of the PR when it qualifies, then keeps the ticket in step with it. A
// created ticket also releases the PRs that wait for it to be superseded.
func (e *Engine) run(ctx context.Context, updatePRID string) {
	outcome, err := e.Process(ctx, updatePRID)
	switch {
	case err != nil && ctx.Err() == nil:
		e.logger().Error("update tickets: creating the ticket failed", "update_pr", updatePRID, "outcome", outcome, "error", err)
	case outcome == OutcomeCreated:
		e.logger().Info("update tickets: ticket created", "update_pr", updatePRID)
	}
	e.sync(ctx, updatePRID, outcome == OutcomeCreated)
	if outcome != OutcomeCreated {
		return
	}
	replaced, err := e.Tickets.SupersededBy(ctx, updatePRID)
	if err != nil {
		if ctx.Err() == nil {
			e.logger().Error("update tickets: listing the PRs the new PR replaced failed", "update_pr", updatePRID, "error", err)
		}
		return
	}
	for _, id := range replaced {
		e.sync(ctx, id, false)
	}
}

// sync keeps the ticket in step with its PR, then its PR comment in step with the ticket. A
// ticket that was just created or changed gets its comment written at once, whatever pause an
// earlier failed write left; any other ticket waits for it.
func (e *Engine) sync(ctx context.Context, updatePRID string, created bool) {
	outcome, err := e.Sync(ctx, updatePRID)
	switch {
	case err != nil && ctx.Err() == nil:
		e.logger().Error("update tickets: syncing the ticket failed", "update_pr", updatePRID, "outcome", outcome, "error", err)
	case outcome != SyncNothing && outcome != SyncWaiting && err == nil:
		e.logger().Info("update tickets: ticket synced", "update_pr", updatePRID, "outcome", outcome)
	}
	changed := err == nil && (outcome == SyncUpdated || outcome == SyncTransitioned || outcome == SyncSuperseded)
	e.comment(ctx, updatePRID, created || changed)
}

// Process creates the ticket of the update PR when it qualifies: it is open, has dependencies, its
// repository has a ticket project and the project's filter lets it through (specs.html section
// 5.2). It is safe to call again for the same PR at any time: a PR that has its ticket, or whose
// earlier create may have stored one, gets no second create. A failure is recorded on the PR's
// update_tickets row and returned.
func (e *Engine) Process(ctx context.Context, updatePRID string) (Outcome, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	c, err := e.Tickets.Candidate(ctx, updatePRID)
	switch {
	case errors.Is(err, store.ErrUpdatePRNotFound):
		return OutcomeNotFound, nil
	case err != nil:
		return OutcomeFailed, err
	}
	if c.State != store.UpdatePROpen {
		return OutcomeNotOpen, e.dropStaleError(ctx, c)
	}
	if len(c.Dependencies) == 0 {
		return OutcomeNoDependencies, e.dropStaleError(ctx, c)
	}
	if c.Ticket != nil {
		switch {
		case c.Ticket.Created:
			return OutcomeExists, nil
		case c.Ticket.Unconfirmed:
			return e.refuse(ctx, c.Ticket.ID)
		case e.now().Before(c.Ticket.NextAttemptAt):
			return OutcomeWaiting, nil
		}
	}
	if c.TicketProjectID == "" {
		return OutcomeNoTicketProject, e.dropStaleError(ctx, c)
	}

	project, err := e.Projects.Get(ctx, c.TicketProjectID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return OutcomeNoTicketProject, e.dropStaleError(ctx, c)
	case err != nil:
		return OutcomeFailed, fmt.Errorf("read ticket project: %w", err)
	}
	filter, err := ParseFilter(project.Filter)
	if err != nil {
		return e.fail(ctx, c, project.ID, fmt.Errorf("ticket project %s: %w", project.ID, err), false)
	}
	if !filter.Allows(c) {
		return OutcomeFiltered, e.dropStaleError(ctx, c)
	}

	// Everything that can fail without touching the tracker comes before the marker, so a
	// failed render or credential never leaves one behind.
	target, err := e.Targets.Target(ctx, project.IntegrationID)
	if err != nil {
		return e.fail(ctx, c, project.ID, fmt.Errorf("ticket integration: %w", err), false)
	}
	input, err := ticketInput(c, project, target.Kind)
	if err != nil {
		return e.fail(ctx, c, project.ID, err, false)
	}

	key, err := e.Tickets.BeginCreate(ctx, store.BeginCreateInput{UpdatePRID: c.ID, TicketProjectID: project.ID, Now: e.now()})
	switch {
	case errors.Is(err, store.ErrTicketExists):
		return OutcomeExists, nil
	case errors.Is(err, store.ErrCreateUnconfirmed):
		return e.refuse(ctx, key)
	case err != nil:
		// The marker is not stored, so the tracker is not called.
		return OutcomeFailed, fmt.Errorf("record the pending ticket: %w", err)
	}
	input.Description += "\n\n" + keyLine + key

	externalID, err := target.Provider.CreateTicket(ctx, input)
	if err != nil {
		return e.fail(ctx, c, project.ID, fmt.Errorf("create ticket: %w", err), ticket.CreateDefinitelyNotStored(err))
	}

	externalURL, _ := target.Provider.TicketWebURL(input.Project, externalID)
	if err := e.link(ctx, store.LinkCreatedInput{ID: key, ExternalID: externalID, ExternalURL: externalURL, SyncedHash: dependenciesHash(c.Dependencies)}); err != nil {
		return OutcomeFailed, e.noteUnlinked(ctx, key, err)
	}
	return OutcomeCreated, nil
}

// noteUnlinked records that a created ticket's link could not be stored, so the PR that sits
// behind its pending marker shows the ticket's id in the Problems view. It returns cause, joined
// with the error of the write when that fails too.
func (e *Engine) noteUnlinked(ctx context.Context, rowID string, cause error) error {
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), failureWriteTimeout)
	defer cancel()
	if err := e.Tickets.NoteError(writeCtx, rowID, redact(cause), e.now()); err != nil {
		return errors.Join(cause, fmt.Errorf("record the failure: %w", err))
	}
	return cause
}

// dropStaleError clears the error of a failed create for a PR that is not going to be ticketed,
// so the Problems view does not keep showing it. A row that has its ticket or its marker keeps
// its error.
func (e *Engine) dropStaleError(ctx context.Context, c store.TicketCandidate) error {
	t := c.Ticket
	if t == nil || t.Created || t.Unconfirmed || t.LastError == "" {
		return nil
	}
	return e.Tickets.ClearCreateError(ctx, t.ID, e.now())
}

// refuse records that a create is not repeated while the pending marker is set.
func (e *Engine) refuse(ctx context.Context, rowID string) (Outcome, error) {
	if err := e.Tickets.NoteError(ctx, rowID, ErrUnconfirmed.Error(), e.now()); err != nil {
		return OutcomeUnconfirmed, errors.Join(ErrUnconfirmed, err)
	}
	return OutcomeUnconfirmed, ErrUnconfirmed
}

// fail records a failed attempt with its retry time. The pending marker is dropped only when
// clearMarker says the ticket system cannot have stored a ticket (ticket.CreateDefinitelyNotStored);
// after any other failure it stays, and so does a marker whose clearing write fails. The write
// ignores the cancellation of ctx, because an attempt cut short while the tracker call failed
// would otherwise lose the record.
func (e *Engine) fail(ctx context.Context, c store.TicketCandidate, projectID string, cause error, clearMarker bool) (Outcome, error) {
	var attempts int64
	if c.Ticket != nil {
		attempts = c.Ticket.Attempts
	}
	now := e.now()
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), failureWriteTimeout)
	defer cancel()
	err := e.Tickets.FailCreate(writeCtx, store.FailCreateInput{
		UpdatePRID: c.ID, TicketProjectID: projectID, Message: redact(cause), ClearMarker: clearMarker,
		NextAttemptAt: now.Add(e.retryDelay(attempts, cause)), Now: now,
	})
	if err != nil {
		return OutcomeFailed, errors.Join(cause, fmt.Errorf("record the failure: %w", err))
	}
	return OutcomeFailed, cause
}

// link stores the created ticket's link, retrying a failed write a bounded number of times and
// stopping when ctx is done. A write that still fails is returned: the marker written before the
// create keeps the PR from getting a second ticket, and the error names the ticket so it can be
// found.
func (e *Engine) link(ctx context.Context, in store.LinkCreatedInput) error {
	var lastErr error
	for attempt := 1; attempt <= linkAttempts; attempt++ {
		in.Now = e.now()
		lastErr = e.Tickets.LinkCreated(ctx, in)
		if lastErr == nil {
			return nil
		}
		if attempt == linkAttempts {
			break
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("ticket %s was created but its link was not stored: %w", in.ExternalID, errors.Join(lastErr, ctx.Err()))
		case <-time.After(time.Duration(attempt) * linkBackoff):
		}
	}
	return fmt.Errorf("ticket %s was created but its link was not stored: %w", in.ExternalID, lastErr)
}

// ticketInput renders the ticket of the PR in its project.
func ticketInput(c store.TicketCandidate, project *store.TicketProject, kind string) (ticket.TicketInput, error) {
	templates, err := tickettemplate.ParseContentTemplates(project.ContentTemplates)
	if err != nil {
		return ticket.TicketInput{}, err
	}
	renderer := tickettemplate.NewRenderer(kind, templates)
	ctx := templateContext(c)
	title, err := renderer.RenderTitle(ctx)
	if err != nil {
		return ticket.TicketInput{}, fmt.Errorf("render the title: %w", err)
	}
	description, err := renderer.RenderDescription(ctx)
	if err != nil {
		return ticket.TicketInput{}, fmt.Errorf("render the description: %w", err)
	}

	tp := ticket.TicketProject{
		ID: project.ID, IntegrationID: project.IntegrationID, IntegrationKind: kind, ExternalProjectID: project.ExternalProjectID,
	}
	if err := json.Unmarshal([]byte(project.CreateConfig), &tp.CreateConfig); err != nil {
		return ticket.TicketInput{}, fmt.Errorf("parse create_config: %w", err)
	}
	if tp.StatusMapping, err = ticket.ParseStatusMapping(project.StatusMapping); err != nil {
		return ticket.TicketInput{}, err
	}
	return ticket.TicketInput{Title: title, Description: description, Project: tp}, nil
}

func templateContext(c store.TicketCandidate) tickettemplate.Context {
	ctx := tickettemplate.Context{
		PullRequest:  tickettemplate.PullRequestContext{URL: c.URL, Number: c.Number, Title: c.Title},
		Repository:   c.FullName,
		ReleaseNotes: c.ReleaseNotes,
	}
	for _, d := range c.Dependencies {
		ctx.Dependencies = append(ctx.Dependencies, tickettemplate.DependencyContext{Name: d.Name, From: d.From, To: d.To, UpdateType: d.UpdateType})
	}
	return ctx
}

// dependenciesHash identifies the dependency list a ticket was rendered for: the names and
// versions, whatever their order.
func dependenciesHash(deps []store.UpdatePRDependency) string {
	lines := make([]string, 0, len(deps))
	for _, d := range deps {
		lines = append(lines, d.Name+"\x00"+d.From+"\x00"+d.To)
	}
	slices.Sort(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:])
}

// rateLimitJitter is the fraction a retry the ticket system's rate limit asked for is spread
// by, upward only, so the retries of several PRs held back by one limit do not return together.
const rateLimitJitter = 0.2

// retryDelay is the pause before a failed operation is tried again: the backoff, or, when the
// ticket system answered with a rate limit, at least the delay it asked for (Retry-After or the
// reset time), spread by the jitter. A rate-limited create stored nothing
// (ticket.CreateDefinitelyNotStored), so the retry cannot create a second ticket.
func (e *Engine) retryDelay(attempts int64, cause error) time.Duration {
	d := backoff(attempts)
	if asked, limited := ticket.RateLimitDelay(cause); limited {
		rnd := rand.Float64
		if e.Rand != nil {
			rnd = e.Rand
		}
		d = max(d, ratelimit.Jittered(asked, rateLimitJitter, rnd()))
	}
	return d
}

// backoff is the pause after the attempt-th failure of a PR: a minute, doubling, at most half an
// hour.
func backoff(attempts int64) time.Duration {
	d := minBackoff
	for i := int64(0); i < attempts && d < maxBackoff; i++ {
		d *= 2
	}
	return min(d, maxBackoff)
}

// redact makes an error fit the Problems view: one line, bounded. Provider errors carry the
// ticket system's answer, never the request, so no credential is in them.
func redact(err error) string {
	msg := strings.Join(strings.Fields(err.Error()), " ")
	if r := []rune(msg); len(r) > maxErrorRunes {
		msg = string(r[:maxErrorRunes-1]) + "…"
	}
	return msg
}
