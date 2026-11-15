package main

import (
	"context"
	"sync"

	"github.com/mdg-labs/release-ops/internal/store"
)

// appIdentity reads the stored App: no App stored is store.ErrGitHubAppNotFound, and the bot
// user id is Known once it has been read from GitHub.
type appIdentity interface {
	BotIdentity(ctx context.Context) (store.GitHubAppBot, error)
}

// startupReconcile runs the PR reconcile of start-up (specs.html section 5.1) once, when both
// of its inputs are there: the App's bot user id, without which a reconcile skips every
// repository, and one successful repository sync, without which the repositories it would
// reconcile may not be recorded yet. The two arrive from independent loops in either order,
// and a failed pass of either is followed by a later one, so whichever completes last fires it.
type startupReconcile struct {
	trigger func()

	mu     sync.Mutex
	bot    bool
	synced bool
	fired  bool
}

// botPass records a pass of the bot user id resolver. A pass counts when it succeeded and the
// id is known: a pass before the App is stored succeeds without resolving anything.
func (s *startupReconcile) botPass(ctx context.Context, err error, app appIdentity) {
	if err != nil {
		return
	}
	if bot, err := app.BotIdentity(ctx); err != nil || !bot.Known {
		return
	}
	s.mu.Lock()
	s.bot = true
	s.mu.Unlock()
	s.fire()
}

// syncPass records a pass of the repository sync. A pass counts when it succeeded and the App
// is stored: before that the sync succeeds without reading any repository.
func (s *startupReconcile) syncPass(ctx context.Context, err error, app appIdentity) {
	if err != nil {
		return
	}
	if _, err := app.BotIdentity(ctx); err != nil {
		return
	}
	s.mu.Lock()
	s.synced = true
	s.mu.Unlock()
	s.fire()
}

func (s *startupReconcile) fire() {
	s.mu.Lock()
	ready := s.bot && s.synced && !s.fired
	if ready {
		s.fired = true
	}
	s.mu.Unlock()
	if ready {
		s.trigger()
	}
}
