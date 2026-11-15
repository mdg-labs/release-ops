package github

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/mdg-labs/release-ops/internal/store"
)

// BotRetryInterval is how often a bot user id that could not be read is tried again.
const BotRetryInterval = 5 * time.Minute

// BotUserLooker reads the numeric id of an App's bot user.
type BotUserLooker interface {
	BotUserID(ctx context.Context, slug string) (int64, error)
}

// BotStore is where the App's bot user id is kept.
type BotStore interface {
	BotIdentity(ctx context.Context) (store.GitHubAppBot, error)
	RecordBotUserID(ctx context.Context, id int64) (bool, error)
}

// BotResolver keeps the App's bot user id, the identity of every bot comparison, filled in.
type BotResolver struct {
	api   BotUserLooker
	store BotStore
}

// NewBotResolver returns a resolver that reads the id through api and keeps it in store.
func NewBotResolver(api BotUserLooker, store BotStore) *BotResolver {
	return &BotResolver{api: api, store: store}
}

// Ensure stores the bot user's id when the App is stored and its id is not. An id already
// stored is never read from GitHub again, and before an App is stored it does nothing. A
// lookup that fails or answers with anything but the bot user stores nothing and returns the
// error; the next call tries again.
func (r *BotResolver) Ensure(ctx context.Context) error {
	bot, err := r.store.BotIdentity(ctx)
	if errors.Is(err, store.ErrGitHubAppNotFound) || (err == nil && bot.Known) {
		return nil
	}
	if err != nil {
		return err
	}
	id, err := r.api.BotUserID(ctx, bot.Slug)
	if err != nil {
		return err
	}
	if _, err := r.store.RecordBotUserID(ctx, id); err != nil {
		return fmt.Errorf("record the bot user id: %w", err)
	}
	return nil
}

// Run ensures the id at once and then on every tick, until ctx ends. report gets the result
// of every pass, nil when it succeeded; a failed pass does not stop the next.
func (r *BotResolver) Run(ctx context.Context, ticks <-chan time.Time, report func(error)) {
	for {
		if err := r.Ensure(ctx); err == nil || ctx.Err() == nil {
			report(err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticks:
		}
	}
}
