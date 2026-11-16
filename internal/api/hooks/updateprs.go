package hooks

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/mdg-labs/release-ops/internal/github"
	"github.com/mdg-labs/release-ops/internal/store"
	"github.com/mdg-labs/release-ops/internal/updateprs"
)

// UpdatePRTracker applies a PR to its update_prs row and wakes the ticket engine.
type UpdatePRTracker interface {
	// ApplyTx is updateprs.Tracker.ApplyTx: it writes the PR, its dependencies and release notes
	// through the delivery's transaction and reports whether anything changed.
	ApplyTx(ctx context.Context, tx *sql.Tx, repositoryID string, pr github.PullRequest, botUserID int64) (updateprs.Outcome, error)
	// Wake is updateprs.Notifier.Wake, run once the transaction has committed.
	Wake()
	// Publish is updateprs.Tracker.Publish, run once the transaction has committed.
	Publish(events []updateprs.Event)
}

// UpdatePRs keeps update_prs current from pull_request deliveries (specs.html section 5.1).
// A delivery is applied inside its own transaction; no handler calls GitHub.
type UpdatePRs struct {
	Tracker      UpdatePRTracker
	Repositories RepositoryLookup
	Bot          BotIdentities
	// Logger defaults to slog.Default(). It receives the event, the delivery GUID, the
	// repository and the outcome, never a payload field.
	Logger *slog.Logger
}

// updatePRActions are the pull_request actions that change what update_prs stores.
var updatePRActions = map[string]bool{
	"opened": true, "reopened": true, "edited": true, "synchronize": true,
	"closed": true, "labeled": true, "unlabeled": true,
}

// RegisterUpdatePRHandler makes the pull_request handler that is registered at that point
// store the PR first and then do what it did. Both run in the delivery's transaction, so a
// failure of either rolls the delivery's record back. It is called after
// RegisterRenovateHandlers, which would replace the extended handler. Registering twice on one
// table changes nothing.
func RegisterUpdatePRHandler(r *Routes, u UpdatePRs) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.updatePRs {
		return
	}
	r.updatePRs = true
	if u.Logger == nil {
		u.Logger = slog.Default()
	}
	prev := r.handlers["pull_request"]
	if prev == nil {
		prev = Acknowledge
	}
	r.handlers["pull_request"] = func(ctx context.Context, tx *sql.Tx, d Delivery) error {
		if err := u.pullRequest(ctx, tx, d); err != nil {
			return err
		}
		return prev(ctx, tx, d)
	}
}

func (u UpdatePRs) pullRequest(ctx context.Context, tx *sql.Tx, d Delivery) error {
	var p struct {
		Action      string             `json:"action"`
		PullRequest github.PullRequest `json:"pull_request"`
		Repository  payloadRepository  `json:"repository"`
	}
	if err := json.Unmarshal(d.Payload, &p); err != nil {
		u.Logger.Warn("webhook: payload has an unexpected shape; ignored", "event", d.Event, "delivery", d.GUID, "error", err)
		return nil
	}
	if !updatePRActions[p.Action] {
		return nil
	}
	bot, ok, err := botUser(ctx, u.Bot, u.Logger, d)
	if err != nil || !ok {
		return err
	}
	repo, err := u.Repositories.RepositoryByGitHubID(ctx, tx, p.Repository.ID)
	if errors.Is(err, store.ErrRepositoryNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	out, err := u.Tracker.ApplyTx(ctx, tx, repo.ID, p.PullRequest, bot.UserID)
	if err != nil {
		return err
	}
	if len(out.Events) > 0 {
		d.AfterCommit(func() { u.Tracker.Publish(out.Events) })
	}
	if out.Changed {
		d.AfterCommit(u.Tracker.Wake)
		u.Logger.Info("webhook: update PR stored", "event", d.Event, "delivery", d.GUID, "repository", repo.FullName, "action", p.Action)
	}
	return nil
}
