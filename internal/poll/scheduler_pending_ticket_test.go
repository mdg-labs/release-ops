package poll_test

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mdg-labs/release-ops/internal/poll"
	"github.com/mdg-labs/release-ops/internal/store"
)

// failingWritePollRepo rejects the state writes failWhen selects before they reach the
// store, so nothing of a rejected write is stored. attempts counts the rejected writes.
type failingWritePollRepo struct {
	store.PollRepository
	failWhen func(update store.PollStateUpdate) bool
	attempts atomic.Int32
}

func (r *failingWritePollRepo) UpdatePollState(ctx context.Context, repoID string, update store.PollStateUpdate) (*store.MonitoredRepo, error) {
	if r.failWhen(update) {
		r.attempts.Add(1)
		return nil, errors.New("database is locked")
	}
	return r.PollRepository.UpdatePollState(ctx, repoID, update)
}

func isLinkWrite(update store.PollStateUpdate) bool {
	return update.OpenTicketExternalID != nil && *update.OpenTicketExternalID == "task-1"
}

func isMarkerWrite(update store.PollStateUpdate) bool {
	return update.PendingTicketTag != nil
}

func (f *stateWriteFixture) pendingTag(t *testing.T) sql.NullString {
	t.Helper()
	var pending sql.NullString
	if err := f.sqlDB.QueryRow(`SELECT pending_ticket_tag FROM monitored_repos WHERE id = ?`, f.repo.ID).Scan(&pending); err != nil {
		t.Fatalf("read pending_ticket_tag: %v", err)
	}
	return pending
}

func (f *stateWriteFixture) setPendingTag(t *testing.T, tag string) {
	t.Helper()
	if _, err := f.sqlDB.Exec(`UPDATE monitored_repos SET pending_ticket_tag = ? WHERE id = ?`, tag, f.repo.ID); err != nil {
		t.Fatalf("set pending_ticket_tag: %v", err)
	}
}

// markerDuringCreate records what pending_ticket_tag holds when the tracker receives a create.
func (f *stateWriteFixture) markerDuringCreate(t *testing.T) func() string {
	t.Helper()
	var seen atomic.Pointer[string]
	hook := func() {
		var pending sql.NullString
		if err := f.sqlDB.QueryRow(`SELECT pending_ticket_tag FROM monitored_repos WHERE id = ?`, f.repo.ID).Scan(&pending); err != nil {
			return
		}
		value := "<none>"
		if pending.Valid {
			value = pending.String
		}
		seen.Store(&value)
	}
	f.onCreate.Store(&hook)
	return func() string {
		if value := seen.Load(); value != nil {
			return *value
		}
		return "<tracker not called>"
	}
}

func (f *stateWriteFixture) eventActions(t *testing.T, runID string) []string {
	t.Helper()
	events, err := f.store.Poll().ListEventsByRunID(context.Background(), runID)
	if err != nil {
		t.Fatalf("ListEventsByRunID: %v", err)
	}
	actions := make([]string, len(events))
	for i, event := range events {
		actions[i] = event.Action
	}
	return actions
}

func assertActions(t *testing.T, got []string, want ...string) {
	t.Helper()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("events = %v, want %v", got, want)
	}
}

const mayAlreadyExist = "may already exist"

// TestSchedulerCreatesOneTicketWhenFinalStateWriteKeepsFailing drives a new release whose
// ticket is created but whose link cannot be stored. Later polls must not create a second
// ticket: they report that one may already exist.
func TestSchedulerCreatesOneTicketWhenFinalStateWriteKeepsFailing(t *testing.T) {
	t.Parallel()
	f := newStateWriteFixture(t)

	failing := &failingWritePollRepo{PollRepository: f.store.Poll(), failWhen: isLinkWrite}
	first := f.runAll(t, failing)

	if got := f.creates.Load(); got != 1 {
		t.Fatalf("ticket creates after first poll = %d, want 1", got)
	}
	if got := failing.attempts.Load(); got != 3 {
		t.Fatalf("link write attempts = %d, want 3", got)
	}
	if first.Status != poll.RunStatusFailed || first.ErrorsJSON == "[]" || first.TicketsCreated != 0 {
		t.Fatalf("first run status/errors/created = %q/%s/%d, want failed/non-empty/0",
			first.Status, first.ErrorsJSON, first.TicketsCreated)
	}
	ticketID, _, lastKnownTag, _ := f.savedTicketState(t)
	if ticketID.Valid || lastKnownTag.String != "v1.0.0" {
		t.Fatalf("saved state = ticket %v, last known %q; want no ticket, v1.0.0", ticketID, lastKnownTag.String)
	}
	if pending := f.pendingTag(t); pending.String != "v1.1.0" {
		t.Fatalf("pending_ticket_tag = %v, want v1.1.0", pending)
	}

	// The write works again; the marker must still stop a second create.
	for i := 2; i <= 3; i++ {
		run := f.runAll(t, f.store.Poll())
		if got := f.creates.Load(); got != 1 {
			t.Fatalf("ticket creates after poll %d = %d, want 1", i, got)
		}
		if run.Status != poll.RunStatusFailed || !strings.Contains(run.ErrorsJSON, mayAlreadyExist) {
			t.Fatalf("poll %d status/errors = %q/%s, want failed with %q", i, run.Status, run.ErrorsJSON, mayAlreadyExist)
		}
		assertActions(t, f.eventActions(t, run.ID), poll.ActionError)
		_, _, _, lastError := f.savedTicketState(t)
		if !strings.Contains(lastError.String, mayAlreadyExist) {
			t.Fatalf("poll %d last_error = %q, want %q", i, lastError.String, mayAlreadyExist)
		}
		if pending := f.pendingTag(t); pending.String != "v1.1.0" {
			t.Fatalf("poll %d pending_ticket_tag = %v, want v1.1.0 kept", i, pending)
		}
	}

	calls := f.sends.callsSnapshot()
	if len(calls) != 2 {
		t.Fatalf("notifications = %+v, want one error notification per blocked poll", calls)
	}
	for _, call := range calls {
		if call.url != selectedTargetURL || !strings.Contains(call.message, mayAlreadyExist) {
			t.Fatalf("notification = %+v, want an error to the selected target naming %q", call, mayAlreadyExist)
		}
	}
}

// TestSchedulerDoesNotCallTrackerWhenMarkerWriteFails: with no durable marker, a create
// could be repeated on the next poll, so the tracker is never called.
func TestSchedulerDoesNotCallTrackerWhenMarkerWriteFails(t *testing.T) {
	t.Parallel()
	f := newStateWriteFixture(t)

	failing := &failingWritePollRepo{PollRepository: f.store.Poll(), failWhen: isMarkerWrite}
	run := f.runAll(t, failing)

	if got := f.creates.Load(); got != 0 {
		t.Fatalf("ticket creates = %d, want 0", got)
	}
	if run.Status != poll.RunStatusFailed || !strings.Contains(run.ErrorsJSON, f.repo.ID) {
		t.Fatalf("run status/errors = %q/%s, want failed naming the repo", run.Status, run.ErrorsJSON)
	}
	ticketID, _, lastKnownTag, _ := f.savedTicketState(t)
	if ticketID.Valid || lastKnownTag.String != "v1.0.0" || f.pendingTag(t).Valid {
		t.Fatalf("state changed: ticket %v, last known %q, pending %v", ticketID, lastKnownTag.String, f.pendingTag(t))
	}

	// Once the marker can be written, the release gets its one ticket.
	f.runAll(t, f.store.Poll())
	if got := f.creates.Load(); got != 1 {
		t.Fatalf("ticket creates after recovery = %d, want 1", got)
	}
}

// TestSchedulerRetriesFinalStateWrite: a final write that fails twice and then succeeds
// stores the link, clears the marker and logs the create like a clean poll.
func TestSchedulerRetriesFinalStateWrite(t *testing.T) {
	t.Parallel()
	f := newStateWriteFixture(t)

	failing := &failingWritePollRepo{PollRepository: f.store.Poll()}
	failing.failWhen = func(update store.PollStateUpdate) bool {
		return isLinkWrite(update) && failing.attempts.Load() < 2
	}
	run := f.runAll(t, failing)

	if got := f.creates.Load(); got != 1 {
		t.Fatalf("ticket creates = %d, want 1", got)
	}
	if got := failing.attempts.Load(); got != 2 {
		t.Fatalf("rejected link writes = %d, want 2", got)
	}
	if run.Status != poll.RunStatusSuccess || run.ErrorsJSON != "[]" || run.TicketsCreated != 1 {
		t.Fatalf("run status/errors/created = %q/%s/%d, want success/[]/1", run.Status, run.ErrorsJSON, run.TicketsCreated)
	}
	ticketID, openTag, lastKnownTag, lastError := f.savedTicketState(t)
	if ticketID.String != "task-1" || openTag.String != "v1.1.0" || lastKnownTag.String != "v1.1.0" || lastError.Valid {
		t.Fatalf("saved state = ticket %q, open tag %q, last known %q, last error %v",
			ticketID.String, openTag.String, lastKnownTag.String, lastError)
	}
	if f.pendingTag(t).Valid {
		t.Fatalf("pending_ticket_tag = %v, want cleared", f.pendingTag(t))
	}
	assertActions(t, f.eventActions(t, run.ID), poll.ActionCreate)
	if calls := f.sends.callsSnapshot(); len(calls) != 1 {
		t.Fatalf("notifications = %+v, want one", calls)
	}
}

// TestSchedulerSetsMarkerBeforeCreateAndClearsItAfter: the happy path is unchanged apart
// from the marker, which is durable when the tracker is called and gone once the link is stored.
func TestSchedulerSetsMarkerBeforeCreateAndClearsItAfter(t *testing.T) {
	t.Parallel()
	f := newStateWriteFixture(t)
	seen := f.markerDuringCreate(t)

	run := f.runAll(t, f.store.Poll())

	if got := seen(); got != "v1.1.0" {
		t.Fatalf("pending_ticket_tag when the tracker was called = %q, want v1.1.0", got)
	}
	if f.pendingTag(t).Valid {
		t.Fatalf("pending_ticket_tag = %v, want cleared", f.pendingTag(t))
	}
	if run.Status != poll.RunStatusSuccess || run.TicketsCreated != 1 || f.creates.Load() != 1 {
		t.Fatalf("run status/created/creates = %q/%d/%d, want success/1/1", run.Status, run.TicketsCreated, f.creates.Load())
	}
	assertActions(t, f.eventActions(t, run.ID), poll.ActionCreate)
	if calls := f.sends.callsSnapshot(); len(calls) != 1 {
		t.Fatalf("notifications = %+v, want one", calls)
	}
}

// TestSchedulerNewerTagClearsOlderMarker: a marker for an older tag does not block the
// newer release, and the newer ticket's write clears it.
func TestSchedulerNewerTagClearsOlderMarker(t *testing.T) {
	t.Parallel()
	f := newStateWriteFixture(t)
	f.setPendingTag(t, "v1.0.5")

	run := f.runAll(t, f.store.Poll())

	if got := f.creates.Load(); got != 1 {
		t.Fatalf("ticket creates = %d, want 1", got)
	}
	if run.Status != poll.RunStatusSuccess {
		t.Fatalf("run status = %q, want success", run.Status)
	}
	if f.pendingTag(t).Valid {
		t.Fatalf("pending_ticket_tag = %v, want cleared by the newer ticket", f.pendingTag(t))
	}
}

// TestSchedulerCreateFailureMarkerRule: the marker is cleared, so the next poll tries
// again, only when the failure proves the tracker stored no ticket (a 4xx answer, or a
// request that never left). After any ambiguous failure it stays, and the next poll
// reports a ticket that may exist instead of creating a second one.
func TestSchedulerCreateFailureMarkerRule(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		status   int  // tracker answer to the create; 0 closes the tracker instead
		wantKept bool // the marker stays set after the failed create
	}{
		{name: "400 rejects", status: http.StatusBadRequest},
		{name: "401 rejects", status: http.StatusUnauthorized},
		{name: "408 rejects", status: http.StatusRequestTimeout},
		{name: "429 rejects", status: http.StatusTooManyRequests},
		{name: "connection refused never left", status: 0},
		{name: "500 is ambiguous", status: http.StatusInternalServerError, wantKept: true},
		{name: "502 is ambiguous", status: http.StatusBadGateway, wantKept: true},
		{name: "200 without an id is ambiguous", status: http.StatusOK, wantKept: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newStateWriteFixture(t)
			if tc.status == 0 {
				f.tracker.Close()
			} else {
				f.createStatus.Store(int32(tc.status))
			}

			run := f.runAll(t, f.store.Poll())

			if run.Status != poll.RunStatusFailed || f.creates.Load() != 0 {
				t.Fatalf("run status/creates = %q/%d, want failed/0", run.Status, f.creates.Load())
			}
			if got := f.pendingTag(t).Valid; got != tc.wantKept {
				t.Fatalf("pending_ticket_tag set = %v, want %v after the failed create", got, tc.wantKept)
			}
			_, _, _, lastError := f.savedTicketState(t)
			if !strings.Contains(lastError.String, "create ticket") || strings.Contains(lastError.String, mayAlreadyExist) {
				t.Fatalf("last_error = %q, want the create failure", lastError.String)
			}

			if tc.status == 0 {
				return // the tracker is gone; the marker state above is the whole claim
			}
			f.createStatus.Store(0)
			next := f.runAll(t, f.store.Poll())
			wantCreates := int32(1)
			if tc.wantKept {
				wantCreates = 0
			}
			if got := f.creates.Load(); got != wantCreates {
				t.Fatalf("ticket creates on the next poll = %d, want %d", got, wantCreates)
			}
			_, _, _, lastError = f.savedTicketState(t)
			if tc.wantKept {
				if !strings.Contains(lastError.String, mayAlreadyExist) || next.Status != poll.RunStatusFailed {
					t.Fatalf("next poll status/last_error = %q/%q, want failed with %q", next.Status, lastError.String, mayAlreadyExist)
				}
			}
		})
	}
}

// TestSchedulerSupersedeCreatesOneTicketWhenFinalStateWriteKeepsFailing: the supersede
// path sets the marker before the new ticket, never touches the old ticket while the new
// one is unsaved, and blocks a second create on later polls.
func TestSchedulerSupersedeCreatesOneTicketWhenFinalStateWriteKeepsFailing(t *testing.T) {
	t.Parallel()
	f := newStateWriteFixture(t)
	f.seedOpenTicket(t)
	seen := f.markerDuringCreate(t)

	failing := &failingWritePollRepo{PollRepository: f.store.Poll(), failWhen: isLinkWrite}
	first := f.runAll(t, failing)

	if got := seen(); got != "v1.1.0" {
		t.Fatalf("pending_ticket_tag when the tracker was called = %q, want v1.1.0", got)
	}
	if f.creates.Load() != 1 || f.oldTouches.Load() != 0 {
		t.Fatalf("creates/old ticket writes = %d/%d, want 1/0", f.creates.Load(), f.oldTouches.Load())
	}
	if first.Status != poll.RunStatusFailed || failing.attempts.Load() != 3 {
		t.Fatalf("first run status/link attempts = %q/%d, want failed/3", first.Status, failing.attempts.Load())
	}

	second := f.runAll(t, f.store.Poll())
	if f.creates.Load() != 1 || f.oldTouches.Load() != 0 {
		t.Fatalf("after the second poll creates/old ticket writes = %d/%d, want 1/0", f.creates.Load(), f.oldTouches.Load())
	}
	if !strings.Contains(second.ErrorsJSON, mayAlreadyExist) {
		t.Fatalf("second run errors = %s, want %q", second.ErrorsJSON, mayAlreadyExist)
	}
	assertActions(t, f.eventActions(t, second.ID), poll.ActionError)
}

// TestSchedulerSupersedeClearsMarkerWithTheNewTicketWrite: on the happy path the marker is
// durable when the new ticket is created and gone after the write that stores the link.
func TestSchedulerSupersedeClearsMarkerWithTheNewTicketWrite(t *testing.T) {
	t.Parallel()
	f := newStateWriteFixture(t)
	f.seedOpenTicket(t)
	seen := f.markerDuringCreate(t)

	run := f.runAll(t, f.store.Poll())

	if got := seen(); got != "v1.1.0" {
		t.Fatalf("pending_ticket_tag when the tracker was called = %q, want v1.1.0", got)
	}
	if f.pendingTag(t).Valid {
		t.Fatalf("pending_ticket_tag = %v, want cleared", f.pendingTag(t))
	}
	if run.Status != poll.RunStatusSuccess || f.creates.Load() != 1 || f.oldTouches.Load() == 0 {
		t.Fatalf("run status/creates/old ticket writes = %q/%d/%d, want success/1/>0", run.Status, f.creates.Load(), f.oldTouches.Load())
	}
	ticketID, _, _, _ := f.savedTicketState(t)
	if ticketID.String != "task-1" {
		t.Fatalf("saved ticket = %q, want task-1", ticketID.String)
	}
}

// TestSchedulerSupersedeKeepsMarkerAfterAmbiguousCreateFailure: when the new ticket's
// create fails in a way that may have stored it, the old ticket is left alone, the
// marker stays and the next poll creates nothing.
func TestSchedulerSupersedeKeepsMarkerAfterAmbiguousCreateFailure(t *testing.T) {
	t.Parallel()
	f := newStateWriteFixture(t)
	f.seedOpenTicket(t)
	f.createStatus.Store(http.StatusInternalServerError)

	f.runAll(t, f.store.Poll())
	if got := f.pendingTag(t); !got.Valid || got.String != "v1.1.0" {
		t.Fatalf("pending_ticket_tag = %v, want v1.1.0 kept after a 500", got)
	}

	f.createStatus.Store(0)
	run := f.runAll(t, f.store.Poll())
	_, _, _, lastError := f.savedTicketState(t)
	if f.creates.Load() != 0 || f.oldTouches.Load() != 0 || run.Status != poll.RunStatusFailed ||
		!strings.Contains(lastError.String, mayAlreadyExist) {
		t.Fatalf("creates/oldTouches/status/last_error = %d/%d/%q/%q, want 0/0/failed/%q",
			f.creates.Load(), f.oldTouches.Load(), run.Status, lastError.String, mayAlreadyExist)
	}
}
