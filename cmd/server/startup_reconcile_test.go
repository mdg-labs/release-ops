package main

import (
	"context"
	"errors"
	"testing"

	"github.com/mdg-labs/release-ops/internal/store"
)

type fakeApp struct {
	bot store.GitHubAppBot
	err error
}

func (a *fakeApp) BotIdentity(context.Context) (store.GitHubAppBot, error) { return a.bot, a.err }

func newStartup() (*startupReconcile, *int) {
	fired := new(int)
	return &startupReconcile{trigger: func() { *fired++ }}, fired
}

var (
	errPass     = errors.New("pass failed")
	knownApp    = func() *fakeApp { return &fakeApp{bot: store.GitHubAppBot{Slug: "app", UserID: 7, Known: true}} }
	unknownApp  = func() *fakeApp { return &fakeApp{bot: store.GitHubAppBot{Slug: "app"}} }
	noStoredApp = func() *fakeApp { return &fakeApp{err: store.ErrGitHubAppNotFound} }
)

func TestStartupReconcileWaitsForTheBotAndTheSyncInEitherOrder(t *testing.T) {
	ctx := context.Background()

	s, fired := newStartup()
	s.botPass(ctx, nil, knownApp())
	if *fired != 0 {
		t.Fatalf("fired %d times with only the bot resolved", *fired)
	}
	s.syncPass(ctx, nil, knownApp())
	if *fired != 1 {
		t.Fatalf("fired %d times after bot then sync, want 1", *fired)
	}

	s, fired = newStartup()
	s.syncPass(ctx, nil, knownApp())
	if *fired != 0 {
		t.Fatalf("fired %d times with only the sync done", *fired)
	}
	s.botPass(ctx, nil, knownApp())
	if *fired != 1 {
		t.Fatalf("fired %d times after sync then bot, want 1", *fired)
	}
}

func TestStartupReconcileKeepsWaitingAfterAFailedBotLookupOrSync(t *testing.T) {
	ctx := context.Background()

	s, fired := newStartup()
	s.syncPass(ctx, nil, knownApp())
	s.botPass(ctx, errPass, unknownApp())
	s.botPass(ctx, errPass, unknownApp())
	if *fired != 0 {
		t.Fatalf("fired %d times while the bot lookup keeps failing", *fired)
	}
	s.botPass(ctx, nil, knownApp())
	if *fired != 1 {
		t.Fatalf("fired %d times once the bot lookup succeeded on a retry, want 1", *fired)
	}

	s, fired = newStartup()
	s.botPass(ctx, nil, knownApp())
	s.syncPass(ctx, errPass, knownApp())
	if *fired != 0 {
		t.Fatalf("fired %d times after a failed sync", *fired)
	}
	s.syncPass(ctx, nil, knownApp())
	if *fired != 1 {
		t.Fatalf("fired %d times once the sync succeeded on a retry, want 1", *fired)
	}
}

// A pass before the App is stored, or one that finds no bot user id yet, succeeds without
// doing the work the reconcile needs.
func TestStartupReconcileDoesNotCountAPassThatFoundNoAppOrNoBotID(t *testing.T) {
	ctx := context.Background()
	s, fired := newStartup()

	s.botPass(ctx, nil, noStoredApp())
	s.botPass(ctx, nil, unknownApp())
	s.syncPass(ctx, nil, noStoredApp())
	if *fired != 0 {
		t.Fatalf("fired %d times on passes that did nothing", *fired)
	}
	s.syncPass(ctx, nil, unknownApp())
	if *fired != 0 {
		t.Fatalf("fired %d times with the sync but no bot id", *fired)
	}
	s.botPass(ctx, nil, knownApp())
	if *fired != 1 {
		t.Fatalf("fired %d times, want 1", *fired)
	}
}

func TestStartupReconcileFiresOnce(t *testing.T) {
	ctx := context.Background()
	s, fired := newStartup()
	for range 3 {
		s.botPass(ctx, nil, knownApp())
		s.syncPass(ctx, nil, knownApp())
	}
	if *fired != 1 {
		t.Fatalf("fired %d times over repeated passes, want exactly 1", *fired)
	}
}
