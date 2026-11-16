package auth

import (
	"testing"
	"time"
)

func fixedClock(at time.Time) func() time.Time { return func() time.Time { return at } }

func TestClaimIsGrantedOnceInsideTheAttemptWindow(t *testing.T) {
	t.Parallel()
	var s spentStates
	t0 := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	expires := t0.Add(10 * time.Minute)
	if !s.claim("state-a", fixedClock(t0), expires) {
		t.Fatal("the first claim of a live state was refused")
	}
	if s.claim("state-a", fixedClock(t0.Add(time.Minute)), expires) {
		t.Fatal("a state was claimed twice")
	}
}

// A request that checked the expiry before the state lapsed and reaches the lock after it
// is refused by the clock read under the lock.
func TestClaimRefusesAStateThatExpiredBeforeTheLockWasTaken(t *testing.T) {
	t.Parallel()
	var s spentStates
	t0 := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	expires := t0.Add(10 * time.Minute)
	if s.claim("state-a", fixedClock(expires), expires) {
		t.Fatal("a state was claimed at its expiry")
	}
	if s.claim("state-a", fixedClock(expires.Add(time.Hour)), expires) {
		t.Fatal("an expired state was claimed")
	}
	if len(s.until) != 0 {
		t.Fatalf("a refused claim recorded a marker: %d entries", len(s.until))
	}
}

// Once a later claim has pruned the spent marker of an expired state, a request that is stale
// in time cannot claim that state a second time.
func TestClaimDoesNotGrantAnExpiredStateAgainAfterItsMarkerWasPruned(t *testing.T) {
	t.Parallel()
	var s spentStates
	t0 := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	expiresA := t0.Add(10 * time.Minute)
	if !s.claim("state-a", fixedClock(t0), expiresA) {
		t.Fatal("the first claim of state-a was refused")
	}
	later := expiresA.Add(time.Minute)
	if !s.claim("state-b", fixedClock(later), later.Add(10*time.Minute)) {
		t.Fatal("a claim of another live state was refused")
	}
	if len(s.until) != 1 {
		t.Fatalf("the marker of the expired state was not pruned: %d entries", len(s.until))
	}
	if s.claim("state-a", fixedClock(later), expiresA) {
		t.Fatal("a state whose marker was pruned was claimed again")
	}
}
