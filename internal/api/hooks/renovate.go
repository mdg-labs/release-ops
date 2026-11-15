package hooks

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"regexp"
	"strings"

	"github.com/mdg-labs/release-ops/internal/queue"
	"github.com/mdg-labs/release-ops/internal/store"
)

// RunEnqueuer records a run request inside the delivery's transaction.
type RunEnqueuer interface {
	// EnqueueTx is store.JobQueue.EnqueueTx: the returned function wakes a waiting worker and
	// is run once the transaction has committed.
	EnqueueTx(ctx context.Context, tx *sql.Tx, p queue.EnqueueParams) (queue.EnqueueResult, func(), error)
}

// RepositoryLookup finds the repository a payload names.
type RepositoryLookup interface {
	RepositoryByGitHubID(ctx context.Context, tx *sql.Tx, githubRepositoryID int64) (store.WebhookRepository, error)
}

// BotIdentities reads the App's bot user.
type BotIdentities interface {
	BotIdentity(ctx context.Context) (store.GitHubAppBot, error)
}

// RenovateTriggers turns the deliveries that ask for a Renovate run into high-priority
// renovate_run jobs (specs.html § 2.9). A delivery is evaluated and enqueued inside its own
// transaction; no handler calls GitHub.
type RenovateTriggers struct {
	Queue        RunEnqueuer
	Repositories RepositoryLookup
	Bot          BotIdentities
	// Logger defaults to slog.Default(). It receives the event, the delivery GUID, the
	// repository and the outcome, never a payload field.
	Logger *slog.Logger
}

// RegisterRenovateHandlers registers the handlers of the issues, pull_request and push events
// and extends the installation_repositories handler that is registered at that point, so a
// repository added by a delivery is synced first and then gets its job in the same
// transaction. It is called after RegisterInstallationHandlers, which would replace the
// extended handler. Registering twice on one table changes nothing.
func RegisterRenovateHandlers(r *Routes, t RenovateTriggers) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.renovate {
		return
	}
	r.renovate = true
	if t.Logger == nil {
		t.Logger = slog.Default()
	}
	r.handlers["issues"] = t.issues
	r.handlers["pull_request"] = t.pullRequest
	r.handlers["push"] = t.push
	prev := r.handlers["installation_repositories"]
	if prev == nil {
		prev = Acknowledge
	}
	r.handlers["installation_repositories"] = func(ctx context.Context, tx *sql.Tx, d Delivery) error {
		if err := prev(ctx, tx, d); err != nil {
			return err
		}
		return t.repositoriesAdded(ctx, tx, d)
	}
}

type payloadUser struct {
	ID int64 `json:"id"`
}

type payloadRepository struct {
	ID            int64  `json:"id"`
	DefaultBranch string `json:"default_branch"`
}

// bodyChange is changes.body of an edited event; nil for an edit that did not touch the body.
type bodyChange struct {
	From *string `json:"from"`
}

type editedPayload struct {
	Action  string `json:"action"`
	Changes struct {
		Body *bodyChange `json:"body"`
	} `json:"changes"`
	Issue struct {
		Body *string     `json:"body"`
		User payloadUser `json:"user"`
	} `json:"issue"`
	PullRequest struct {
		Body *string     `json:"body"`
		User payloadUser `json:"user"`
	} `json:"pull_request"`
	Repository payloadRepository `json:"repository"`
	Sender     payloadUser       `json:"sender"`
}

func (t RenovateTriggers) issues(ctx context.Context, tx *sql.Tx, d Delivery) error {
	var p editedPayload
	if !t.decode(d, &p) || p.Action != "edited" {
		return nil
	}
	return t.edited(ctx, tx, d, queue.TriggerDashboardCheckbox, p, p.Issue.User, p.Issue.Body)
}

func (t RenovateTriggers) pullRequest(ctx context.Context, tx *sql.Tx, d Delivery) error {
	var p editedPayload
	if !t.decode(d, &p) {
		return nil
	}
	switch p.Action {
	case "edited":
		return t.edited(ctx, tx, d, queue.TriggerPRCheckbox, p, p.PullRequest.User, p.PullRequest.Body)
	case "closed":
		// The bot's own close is not filtered: an automerge frees a slot a follow-up run needs.
		bot, ok := t.bot(ctx, d)
		if !ok || p.PullRequest.User.ID != bot.UserID {
			return nil
		}
		return t.enqueue(ctx, tx, d, p.Repository.ID, queue.TriggerPRClosed, nil)
	}
	return nil
}

// edited enqueues the run of an edit that ticked a Renovate checkbox in an object the App's
// bot user authored, unless the bot made the edit itself. Objects are identified by author
// and by the checkbox markers they carry, never by head ref or title.
func (t RenovateTriggers) edited(ctx context.Context, tx *sql.Tx, d Delivery, trigger queue.Trigger, p editedPayload, author payloadUser, body *string) error {
	if p.Changes.Body == nil || p.Changes.Body.From == nil {
		return nil
	}
	current := ""
	if body != nil {
		current = *body
	}
	markers := tickedMarkers(*p.Changes.Body.From, current)
	if len(markers) == 0 {
		return nil
	}
	bot, ok := t.bot(ctx, d)
	if !ok || author.ID != bot.UserID || p.Sender.ID == bot.UserID {
		return nil
	}
	return t.enqueue(ctx, tx, d, p.Repository.ID, trigger, markers)
}

func (t RenovateTriggers) push(ctx context.Context, tx *sql.Tx, d Delivery) error {
	var p struct {
		Ref        string            `json:"ref"`
		Deleted    bool              `json:"deleted"`
		After      string            `json:"after"`
		Repository payloadRepository `json:"repository"`
	}
	if !t.decode(d, &p) || p.Deleted || (p.After != "" && strings.Trim(p.After, "0") == "") {
		return nil
	}
	branch, ok := strings.CutPrefix(p.Ref, "refs/heads/")
	if !ok || branch == "" {
		return nil
	}
	defaultBranch := p.Repository.DefaultBranch
	if defaultBranch == "" {
		repo, err := t.Repositories.RepositoryByGitHubID(ctx, tx, p.Repository.ID)
		if errors.Is(err, store.ErrRepositoryNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		defaultBranch = repo.DefaultBranch
	}
	if branch != defaultBranch {
		return nil
	}
	return t.enqueue(ctx, tx, d, p.Repository.ID, queue.TriggerPush, nil)
}

func (t RenovateTriggers) repositoriesAdded(ctx context.Context, tx *sql.Tx, d Delivery) error {
	var p struct {
		Action string              `json:"action"`
		Added  []payloadRepository `json:"repositories_added"`
	}
	if !t.decode(d, &p) || p.Action != "added" {
		return nil
	}
	seen := map[int64]bool{}
	for _, r := range p.Added {
		if seen[r.ID] {
			continue
		}
		seen[r.ID] = true
		if err := t.enqueue(ctx, tx, d, r.ID, queue.TriggerRepositoryAdded, nil); err != nil {
			return err
		}
	}
	return nil
}

// decode reads the payload. One that cannot be read is dropped, not failed: a redelivery
// would carry the same body, and a failed delivery is redelivered again and again.
func (t RenovateTriggers) decode(d Delivery, into any) bool {
	if err := json.Unmarshal(d.Payload, into); err != nil {
		t.Logger.Warn("webhook: payload has an unexpected shape; ignored", "event", d.Event, "delivery", d.GUID, "error", err)
		return false
	}
	return true
}

// bot returns the App's bot user. While its numeric id is not known nobody counts as the
// bot: no object is bot-authored and no sender is the bot's own, so the caller enqueues
// nothing for an edit or a close.
func (t RenovateTriggers) bot(ctx context.Context, d Delivery) (store.GitHubAppBot, bool) {
	bot, err := t.Bot.BotIdentity(ctx)
	if err != nil || !bot.Known {
		t.Logger.Warn("webhook: the App's bot user id is not known; ignored", "event", d.Event, "delivery", d.GUID)
		return store.GitHubAppBot{}, false
	}
	return bot, true
}

// enqueue asks for a high-priority run of the repository inside the delivery's transaction.
// A repository that is not tracked or is paused is dropped without an error, so the delivery
// still commits as seen.
func (t RenovateTriggers) enqueue(ctx context.Context, tx *sql.Tx, d Delivery, githubRepositoryID int64, trigger queue.Trigger, markers []string) error {
	repo, err := t.Repositories.RepositoryByGitHubID(ctx, tx, githubRepositoryID)
	if errors.Is(err, store.ErrRepositoryNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	receivedAt := d.ReceivedAt
	res, wake, err := t.Queue.EnqueueTx(ctx, tx, queue.EnqueueParams{
		RepositoryID:      repo.ID,
		Kind:              queue.KindRenovateRun,
		Priority:          queue.PriorityHigh,
		Trigger:           trigger,
		MatchedMarkers:    markers,
		WebhookReceivedAt: &receivedAt,
	})
	if errors.Is(err, queue.ErrRepositoryPaused) || errors.Is(err, queue.ErrRepositoryNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	d.AfterCommit(wake)
	t.Logger.Info("webhook: run requested", "event", d.Event, "delivery", d.GUID, "repository", repo.FullName,
		"trigger", string(trigger), "outcome", string(res.Outcome), "markers", len(markers))
	return nil
}

const (
	// maxMarkerBytes and maxMarkers bound what one edit stores with a job. Renovate's markers
	// are short names and branch names; anything longer is not one.
	maxMarkerBytes = 256
	maxMarkers     = 32
)

// checkboxLine matches a Renovate checkbox line, " - [ ] <!-- marker -->", ticked or not.
var checkboxLine = regexp.MustCompile(`^\s*[-*+]\s+\[([ xX])\]\s+<!--\s*(.+?)\s*-->`)

// tickedMarkers returns the markers that sit on an unchecked line of from and on a checked
// line of to, in the order of to, matched by marker text so reordered lines do not matter.
// It makes no assumption about which markers exist.
func tickedMarkers(from, to string) []string {
	unchecked := map[string]bool{}
	for _, line := range strings.Split(from, "\n") {
		if state, marker, ok := checkbox(line); ok && state == ' ' {
			unchecked[marker] = true
		}
	}
	if len(unchecked) == 0 {
		return nil
	}
	var ticked []string
	seen := map[string]bool{}
	for _, line := range strings.Split(to, "\n") {
		state, marker, ok := checkbox(line)
		if !ok || state == ' ' || !unchecked[marker] || seen[marker] {
			continue
		}
		seen[marker] = true
		ticked = append(ticked, marker)
		if len(ticked) == maxMarkers {
			break
		}
	}
	return ticked
}

func checkbox(line string) (state byte, marker string, ok bool) {
	m := checkboxLine.FindStringSubmatch(strings.TrimSuffix(line, "\r"))
	if m == nil || len(m[2]) > maxMarkerBytes {
		return 0, "", false
	}
	return m[1][0], m[2], true
}
