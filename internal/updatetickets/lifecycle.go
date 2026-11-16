package updatetickets

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/mdg-labs/release-ops/internal/providers/ticket"
	"github.com/mdg-labs/release-ops/internal/store"
	"github.com/mdg-labs/release-ops/internal/tickettemplate"
)

// SyncOutcome says what Sync decided for the ticket of a PR.
type SyncOutcome string

// The outcomes of Sync.
const (
	// SyncNothing: the PR has no ticket, or nothing is due for it.
	SyncNothing SyncOutcome = "nothing"
	// SyncUpdated: the ticket shows the PR's new version and carries a note about it.
	SyncUpdated SyncOutcome = "updated"
	// SyncTransitioned: the ticket was moved to done or won't do.
	SyncTransitioned SyncOutcome = "transitioned"
	// SyncSuperseded: the ticket was moved to the superseded status and linked to the
	// replacing PR.
	SyncSuperseded SyncOutcome = "superseded"
	// SyncWaiting: an earlier attempt failed and its retry is not due, or the replacing PR's
	// ticket is not created yet.
	SyncWaiting SyncOutcome = "waiting"
	// SyncHandedOff: the ticket was changed outside Release Ops, so it is left alone for good.
	SyncHandedOff SyncOutcome = "handed_off"
	// SyncFailed: the attempt failed; the error is recorded on the PR's ticket row.
	SyncFailed SyncOutcome = "failed"
)

type opKind int

const (
	opNone opKind = iota
	opUpdate
	opTransition
	opSupersede
)

// operation is what the PR's state asks of its ticket; state is the state a transition leads to.
type operation struct {
	kind  opKind
	state string
}

// plan reads the operation the PR's state asks of its ticket from the stored rows, without
// calling the ticket system (specs.html section 5.2.1). A ticket that is done or superseded
// gets none, and a won't-do ticket only the supersede, so a reopened PR leaves its ticket as it is.
func plan(c store.TicketCandidate) operation {
	t := c.Ticket
	switch c.State {
	case store.UpdatePROpen:
		if t.State == store.TicketStateOpen && len(c.Dependencies) > 0 && dependenciesHash(c.Dependencies) != t.SyncedHash {
			return operation{kind: opUpdate}
		}
	case store.UpdatePRMerged:
		if t.State == store.TicketStateOpen {
			return operation{kind: opTransition, state: store.TicketStateDone}
		}
	case store.UpdatePRClosed:
		if t.State == store.TicketStateOpen {
			return operation{kind: opTransition, state: store.TicketStateCancelled}
		}
	case store.UpdatePRSuperseded:
		if t.State == store.TicketStateOpen || t.State == store.TicketStateCancelled {
			return operation{kind: opSupersede, state: store.TicketStateSuperseded}
		}
	}
	return operation{}
}

// syncState is what one Sync works with.
type syncState struct {
	c        store.TicketCandidate
	t        *store.UpdateTicket
	project  *store.TicketProject
	mapping  ticket.StatusMapping
	provider ticket.TicketProvider
	kind     string
	// replacement is read for a supersede.
	replacement replacement
}

// replacement describes the PR that replaced the PR of a ticket and its ticket. pr is nil for a
// PR whose replacement is unknown or gone, and the supersede comment is left out. wait is set
// when the replacing PR has no ticket yet but is going to get one.
type replacement struct {
	pr        *tickettemplate.PullRequestContext
	ticketURL string
	wait      bool
}

// Sync keeps the ticket of the update PR in step with the PR (specs.html section 5.2.1): a new
// version updates it and adds a note, a merge moves it to done, a close to won't do, and a
// replacement supersedes it and links the replacing PR. A PR without a created ticket, and one
// with nothing due, call no ticket system. Before it changes a ticket it reads the live status:
// a ticket whose class differs from the state Release Ops last put it in was changed by hand and
// is left alone for good. A transition is recorded on the row before the ticket system is called,
// so a stop between the call and the write of the new state is recognised on the next look
// instead of being taken for a change by hand. A failure is recorded on the row, retried with
// backoff and returned.
func (e *Engine) Sync(ctx context.Context, updatePRID string) (SyncOutcome, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	c, err := e.Tickets.Candidate(ctx, updatePRID)
	switch {
	case errors.Is(err, store.ErrUpdatePRNotFound):
		return SyncNothing, nil
	case err != nil:
		return SyncFailed, err
	}
	t := c.Ticket
	if t == nil || !t.Created || t.HandsOff {
		return SyncNothing, nil
	}
	op := plan(c)
	if op.kind == opNone && t.TargetState == "" {
		return SyncNothing, nil
	}
	if e.now().Before(t.NextAttemptAt) {
		return SyncWaiting, nil
	}

	// What can fail without touching the ticket system comes first, so a bad mapping or
	// template never leaves a recorded transition behind.
	project, err := e.Projects.Get(ctx, t.TicketProjectID)
	if err != nil {
		return e.failSync(ctx, t, fmt.Errorf("read ticket project: %w", err))
	}
	mapping, err := ticket.ParseStatusMapping(project.StatusMapping)
	if err != nil {
		return e.failSync(ctx, t, err)
	}
	target, err := e.Targets.Target(ctx, project.IntegrationID)
	if err != nil {
		return e.failSync(ctx, t, fmt.Errorf("ticket integration: %w", err))
	}
	s := &syncState{c: c, t: t, project: project, mapping: mapping, provider: target.Provider, kind: target.Kind}
	if op.kind == opTransition || op.kind == opSupersede {
		if _, err := statusFor(mapping, op.state); err != nil {
			return e.failSync(ctx, t, err)
		}
	}
	if op.kind == opSupersede || t.TargetState == store.TicketStateSuperseded {
		if s.replacement, err = e.replacement(ctx, c); err != nil {
			return e.failSync(ctx, t, err)
		}
		if s.replacement.wait && t.TargetState == "" {
			return SyncWaiting, nil
		}
	}

	live, err := s.provider.GetTicketStatus(ctx, t.ExternalID)
	if err != nil {
		return e.failSync(ctx, t, fmt.Errorf("read ticket status: %w", err))
	}

	if t.TargetState != "" {
		value, err := statusFor(mapping, t.TargetState)
		if err != nil {
			return e.failSync(ctx, t, err)
		}
		if sameStatus(live, value) {
			// The recorded transition reached the ticket and its state was not stored. Finish
			// it; the next look applies what the PR needs from there.
			return e.finish(ctx, s, t.TargetState)
		}
	}
	class := ticket.ClassifyStatus(mapping, live)
	if class == ticket.StatusUnknown {
		return e.failSync(ctx, t, fmt.Errorf("the ticket's status %q is not in the status mapping of ticket project %s", live, project.ID))
	}
	if class != storedClass(t.State) {
		if err := e.Tickets.HandOff(ctx, t.ID, e.now()); err != nil {
			return SyncFailed, err
		}
		return SyncHandedOff, nil
	}

	switch op.kind {
	case opUpdate:
		return e.update(ctx, s)
	case opTransition:
		return e.transition(ctx, s, op.state, live)
	case opSupersede:
		return e.supersede(ctx, s, live, false)
	}
	// The recorded transition did not reach the ticket and the PR asks for nothing: forget it.
	if err := e.Tickets.CompleteTransition(ctx, t.ID, t.State, e.now()); err != nil {
		return SyncFailed, err
	}
	return SyncNothing, nil
}

// finish stores the state of a recorded transition that already reached the ticket system. A
// supersede still adds its comment, which a stop before it left out.
func (e *Engine) finish(ctx context.Context, s *syncState, state string) (SyncOutcome, error) {
	if state == store.TicketStateSuperseded {
		return e.supersede(ctx, s, "", true)
	}
	if err := e.Tickets.CompleteTransition(ctx, s.t.ID, state, e.now()); err != nil {
		return SyncFailed, err
	}
	return SyncTransitioned, nil
}

// update re-renders the title and description, sends them, and adds the note.
func (e *Engine) update(ctx context.Context, s *syncState) (SyncOutcome, error) {
	renderer, tctx, err := contentRenderer(s)
	if err != nil {
		return e.failSync(ctx, s.t, err)
	}
	title, err := renderer.RenderTitle(tctx)
	if err != nil {
		return e.failSync(ctx, s.t, fmt.Errorf("render the title: %w", err))
	}
	description, err := renderer.RenderDescription(tctx)
	if err != nil {
		return e.failSync(ctx, s.t, fmt.Errorf("render the description: %w", err))
	}
	note, err := renderer.RenderUpdateComment(tctx)
	if err != nil {
		return e.failSync(ctx, s.t, fmt.Errorf("render the update comment: %w", err))
	}
	description += "\n\n" + keyLine + s.t.ID

	if err := s.provider.UpdateTicket(ctx, s.t.ExternalID, title, description); err != nil {
		return e.failSync(ctx, s.t, fmt.Errorf("update ticket: %w", err))
	}
	if strings.TrimSpace(note) != "" {
		if err := s.provider.AddTicketComment(ctx, s.t.ExternalID, note); err != nil {
			return e.failSync(ctx, s.t, fmt.Errorf("add update note: %w", err))
		}
	}
	if err := e.Tickets.MarkSynced(ctx, s.t.ID, dependenciesHash(s.c.Dependencies), e.now()); err != nil {
		return SyncFailed, err
	}
	return SyncUpdated, nil
}

// transition moves the ticket to done or won't do. A ticket already in the target status gets
// no call, only the state.
func (e *Engine) transition(ctx context.Context, s *syncState, state, live string) (SyncOutcome, error) {
	value, err := statusFor(s.mapping, state)
	if err != nil {
		return e.failSync(ctx, s.t, err)
	}
	if err := e.Tickets.BeginTransition(ctx, s.t.ID, state, e.now()); err != nil {
		return SyncFailed, err
	}
	if !sameStatus(live, value) {
		if err := s.provider.UpdateTicketStatus(ctx, s.t.ExternalID, value); err != nil {
			return e.failSync(ctx, s.t, fmt.Errorf("move ticket to %s: %w", state, err))
		}
	}
	if err := e.Tickets.CompleteTransition(ctx, s.t.ID, state, e.now()); err != nil {
		return SyncFailed, err
	}
	return SyncTransitioned, nil
}

// supersede moves the ticket to the superseded status and comments with the replacing PR and
// its ticket. With statusApplied the status was moved already, and only the comment and the
// state follow. Sync has made it wait for the replacing PR's ticket before it gets here.
func (e *Engine) supersede(ctx context.Context, s *syncState, live string, statusApplied bool) (SyncOutcome, error) {
	value, err := statusFor(s.mapping, store.TicketStateSuperseded)
	if err != nil {
		return e.failSync(ctx, s.t, err)
	}
	comment := ""
	if s.replacement.pr != nil {
		renderer, tctx, err := contentRenderer(s)
		if err != nil {
			return e.failSync(ctx, s.t, err)
		}
		tctx.Supersede = tickettemplate.SupersedeContext{NewPullRequest: *s.replacement.pr, NewTicketURL: s.replacement.ticketURL}
		if comment, err = renderer.RenderSupersedeComment(tctx); err != nil {
			return e.failSync(ctx, s.t, fmt.Errorf("render the supersede comment: %w", err))
		}
	}

	if !statusApplied {
		if err := e.Tickets.BeginTransition(ctx, s.t.ID, store.TicketStateSuperseded, e.now()); err != nil {
			return SyncFailed, err
		}
		if !sameStatus(live, value) {
			if err := s.provider.UpdateTicketStatus(ctx, s.t.ExternalID, value); err != nil {
				return e.failSync(ctx, s.t, fmt.Errorf("move ticket to superseded: %w", err))
			}
		}
	}
	if strings.TrimSpace(comment) != "" {
		if err := s.provider.AddTicketComment(ctx, s.t.ExternalID, comment); err != nil {
			return e.failSync(ctx, s.t, fmt.Errorf("add supersede comment: %w", err))
		}
	}
	if err := e.Tickets.CompleteTransition(ctx, s.t.ID, store.TicketStateSuperseded, e.now()); err != nil {
		return SyncFailed, err
	}
	return SyncSuperseded, nil
}

// replacement reads the PR that replaced c and its ticket.
func (e *Engine) replacement(ctx context.Context, c store.TicketCandidate) (replacement, error) {
	if c.SupersededByID == "" {
		return replacement{}, nil
	}
	rc, err := e.Tickets.Candidate(ctx, c.SupersededByID)
	switch {
	case errors.Is(err, store.ErrUpdatePRNotFound):
		return replacement{}, nil
	case err != nil:
		return replacement{}, err
	}
	r := replacement{pr: &tickettemplate.PullRequestContext{URL: rc.URL, Number: rc.Number, Title: rc.Title}}
	if rc.Ticket != nil && rc.Ticket.Created {
		r.ticketURL = rc.Ticket.ExternalURL
		return r, nil
	}
	r.wait, err = e.wouldTicket(ctx, rc)
	return r, err
}

// wouldTicket reports whether Process is going to create a ticket for the PR: it is open, has
// dependencies, its repository has a ticket project, and the project's filter lets it through.
func (e *Engine) wouldTicket(ctx context.Context, c store.TicketCandidate) (bool, error) {
	if c.State != store.UpdatePROpen || len(c.Dependencies) == 0 || c.TicketProjectID == "" {
		return false, nil
	}
	project, err := e.Projects.Get(ctx, c.TicketProjectID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("read ticket project: %w", err)
	}
	filter, err := ParseFilter(project.Filter)
	if err != nil {
		return false, fmt.Errorf("ticket project %s: %w", project.ID, err)
	}
	return filter.Allows(c), nil
}

// failSync records a failed operation on an existing ticket with its retry time. The write
// ignores the cancellation of ctx, because an attempt cut short while the ticket system call
// failed would otherwise lose the record.
func (e *Engine) failSync(ctx context.Context, t *store.UpdateTicket, cause error) (SyncOutcome, error) {
	now := e.now()
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), failureWriteTimeout)
	defer cancel()
	err := e.Tickets.FailSync(writeCtx, store.FailSyncInput{ID: t.ID, Message: redact(cause), NextAttemptAt: now.Add(backoff(t.Attempts)), Now: now})
	if err != nil {
		return SyncFailed, errors.Join(cause, fmt.Errorf("record the failure: %w", err))
	}
	return SyncFailed, cause
}

// contentRenderer builds the renderer of the ticket's project and the template context of its PR.
func contentRenderer(s *syncState) (*tickettemplate.Renderer, tickettemplate.Context, error) {
	templates, err := tickettemplate.ParseContentTemplates(s.project.ContentTemplates)
	if err != nil {
		return nil, tickettemplate.Context{}, err
	}
	return tickettemplate.NewRenderer(s.kind, templates), templateContext(s.c), nil
}

// statusFor is the status a ticket is moved to for the state: the first entry of the done and
// cancelled lists, and the single superseded value. An empty mapping is an error that says so.
func statusFor(m ticket.StatusMapping, state string) (string, error) {
	switch state {
	case store.TicketStateDone:
		if len(m.Done) > 0 && strings.TrimSpace(m.Done[0]) != "" {
			return strings.TrimSpace(m.Done[0]), nil
		}
		return "", errors.New("status_mapping.done has no entry, so a merged PR's ticket cannot be moved to done")
	case store.TicketStateCancelled:
		if len(m.Cancelled) > 0 && strings.TrimSpace(m.Cancelled[0]) != "" {
			return strings.TrimSpace(m.Cancelled[0]), nil
		}
		return "", errors.New("status_mapping.cancelled has no entry, so a closed PR's ticket cannot be moved to won't do")
	case store.TicketStateSuperseded:
		if strings.TrimSpace(m.Superseded) != "" {
			return strings.TrimSpace(m.Superseded), nil
		}
		return "", errors.New("status_mapping.superseded is empty, so a replaced PR's ticket cannot be superseded")
	}
	return "", fmt.Errorf("no status for ticket state %q", state)
}

// storedClass is the class of live status a ticket in the stored state has: a superseded ticket
// classifies as cancelled (ticket.ClassifyStatus).
func storedClass(state string) string {
	switch state {
	case store.TicketStateDone:
		return ticket.StatusDone
	case store.TicketStateCancelled, store.TicketStateSuperseded:
		return ticket.StatusCancelled
	}
	return ticket.StatusOpen
}

func sameStatus(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}
