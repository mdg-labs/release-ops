package logging_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/logging"
)

type clock struct{ now time.Time }

func (c *clock) Now() time.Time          { return c.now }
func (c *clock) Advance(d time.Duration) { c.now = c.now.Add(d) }

func removed(reg *logging.Registry, secret string) bool {
	l, out := newLogger(reg, false)
	l.Info("line " + secret)
	return !strings.Contains(out.String(), secret)
}

func TestAMintedSecretIsRemovedForItsLifetimeAndThenDropped(t *testing.T) {
	c := &clock{now: time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)}
	reg := logging.NewRegistry(c.Now)
	reg.AddFor(time.Hour, "opaque-minted-token-1")
	reg.Add("opaque-process-secret-1")

	if !removed(reg, "opaque-minted-token-1") || reg.Len() != 2 {
		t.Fatalf("the minted token is not removed while it lives (entries %d)", reg.Len())
	}
	c.Advance(30 * time.Minute)
	if !removed(reg, "opaque-minted-token-1") {
		t.Fatal("the minted token is not removed 30 minutes in")
	}
	c.Advance(time.Hour) // past the hour and the grace
	if removed(reg, "opaque-minted-token-1") {
		t.Fatal("an expired token is still registered")
	}
	if reg.Len() != 1 || !removed(reg, "opaque-process-secret-1") {
		t.Fatalf("the process secret was dropped with the token (entries %d)", reg.Len())
	}
}

// Minting tokens for as long as the process runs does not grow the registry without bound.
func TestTheRegistryDoesNotGrowWithTheNumberOfTokensMinted(t *testing.T) {
	c := &clock{now: time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)}
	reg := logging.NewRegistry(c.Now)
	reg.Add("opaque-process-secret-2")

	for i := range 10000 {
		reg.AddFor(time.Hour, fmt.Sprintf("opaque-token-%06d", i))
		c.Advance(time.Second)
	}
	if reg.Len() > 8300 {
		t.Fatalf("%d entries after 10000 tokens, want a bounded registry", reg.Len())
	}
	if !removed(reg, "opaque-process-secret-2") {
		t.Fatal("the process secret was evicted")
	}
	if !removed(reg, "opaque-token-009999") {
		t.Fatal("the newest token is not registered")
	}

	c.Advance(3 * time.Hour)
	removed(reg, "x") // a log line rebuilds the redactor and drops what ended
	if reg.Len() != 1 {
		t.Fatalf("%d entries after every token expired, want 1", reg.Len())
	}
}

// Traffic anyone can send fills the unauthenticated budget and nothing else: a credential, a
// signed-in session and a process secret stay registered however many entries it adds.
func TestAFloodOfUnauthenticatedEntriesDoesNotPushOutOtherSecrets(t *testing.T) {
	c := &clock{now: time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)}
	reg := logging.NewRegistry(c.Now)
	reg.Add("opaque-process-secret-3")
	reg.AddFor(time.Minute, "opaque-short-credential-3")
	reg.AddFor(8*time.Hour, "opaque-signed-in-session-3")

	for i := range 20000 {
		reg.AddUnauthenticatedFor(8*time.Hour, fmt.Sprintf("opaque-anonymous-%06d", i))
		c.Advance(time.Millisecond)
	}
	for _, s := range []string{"opaque-process-secret-3", "opaque-short-credential-3", "opaque-signed-in-session-3", "opaque-anonymous-019999"} {
		if !removed(reg, s) {
			t.Fatalf("%s was pushed out of the registry", s)
		}
	}
	if reg.Len() > 1100 {
		t.Fatalf("%d entries after the flood, want a bounded registry", reg.Len())
	}
}

// A budget that does fill drops live entries loudly: they are counted and a warning is logged.
func TestAFullBudgetCountsAndReportsTheLiveEntriesItDrops(t *testing.T) {
	c := &clock{now: time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)}
	reg := logging.NewRegistry(c.Now)
	for i := range 1100 {
		reg.AddUnauthenticatedFor(time.Hour, fmt.Sprintf("opaque-anonymous-%06d", i))
	}
	if reg.Dropped() == 0 {
		t.Fatal("live entries were dropped without being counted")
	}
}

func TestASecretRegisteredBothWaysStaysForever(t *testing.T) {
	c := &clock{now: time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)}
	reg := logging.NewRegistry(c.Now)
	reg.AddFor(time.Minute, "opaque-both-ways-secret")
	reg.Add("opaque-both-ways-secret")
	reg.AddFor(time.Minute, "opaque-both-ways-secret")
	c.Advance(24 * time.Hour)
	if !removed(reg, "opaque-both-ways-secret") {
		t.Fatal("a permanent secret was dropped when its temporary entry ended")
	}
}

func TestShortAndEmptySecretsAreNotRegisteredIntoText(t *testing.T) {
	reg := logging.NewRegistry(nil)
	reg.Add("", "abc")
	l, out := newLogger(reg, false)
	l.Info("abc stays readable")
	if !strings.Contains(out.String(), "abc stays readable") {
		t.Fatalf("a short secret shredded the line: %s", out.String())
	}
}

// A secret registered into a full budget is covered: it is the oldest entries that go.
func TestANewEntryIsKeptWhenItsBudgetIsFull(t *testing.T) {
	c := &clock{now: time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)}
	reg := logging.NewRegistry(c.Now)
	for i := range 1100 {
		reg.AddUnauthenticatedFor(8*time.Hour, fmt.Sprintf("opaque-anonymous-%06d", i))
		c.Advance(time.Second)
	}
	reg.AddUnauthenticatedFor(time.Minute, "opaque-short-lived-newest-1")
	if !removed(reg, "opaque-short-lived-newest-1") {
		t.Fatal("a secret registered into a full budget was dropped at once")
	}
}

// A secret registered again keeps covering the lifetime of the latest registration, however
// soon after the first one it was renewed.
func TestARenewedSecretIsCoveredUntilTheEndOfItsLatestRegistration(t *testing.T) {
	c := &clock{now: time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)}
	reg := logging.NewRegistry(c.Now)
	const secret = "opaque-renewed-token-1"
	reg.AddFor(time.Hour, secret)

	c.Advance(15 * time.Minute)
	reg.AddFor(time.Hour, secret)

	// Past the first registration's end (an hour and the grace) but not the second's.
	c.Advance(time.Hour + 10*time.Minute - 15*time.Minute + time.Minute)
	if !removed(reg, secret) {
		t.Fatal("a renewed secret is not removed after the end of its first registration")
	}
	if reg.Len() != 1 {
		t.Fatalf("entries = %d, want the renewed secret still registered", reg.Len())
	}
	c.Advance(20 * time.Minute)
	if removed(reg, secret) {
		t.Fatal("a renewed secret is still registered after the end of its latest registration")
	}
}

// An earlier end never replaces a later one.
func TestARegistrationForAShorterTimeDoesNotShortenTheEnd(t *testing.T) {
	c := &clock{now: time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)}
	reg := logging.NewRegistry(c.Now)
	const secret = "opaque-renewed-token-2"
	reg.AddFor(2*time.Hour, secret)
	reg.AddFor(time.Minute, secret)

	c.Advance(time.Hour)
	if !removed(reg, secret) {
		t.Fatal("a shorter registration shortened the end of a longer one")
	}
}
