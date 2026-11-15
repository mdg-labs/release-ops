package updateprs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/mdg-labs/release-ops/internal/github"
	"github.com/mdg-labs/release-ops/internal/store"
)

const (
	// maxFetches bounds how many vanished open PRs one reconcile fetches one by one; the rest
	// are fetched by the next reconciles, which carry on after the last number fetched.
	maxFetches = 100
	// ReconcileTimeout bounds one repository's reconcile, GitHub calls included.
	ReconcileTimeout = 2 * time.Minute
)

// API reads pull requests from GitHub.
type API interface {
	// OpenPullRequestsPage is github.Client.OpenPullRequestsPage.
	OpenPullRequestsPage(ctx context.Context, token, fullName, pageURL, etag string) (github.PullRequestPage, error)
	// PullRequest is github.Client.PullRequest.
	PullRequest(ctx context.Context, token, fullName string, number int64) (github.PullRequest, error)
}

// Tokens mints the installation token a reconcile reads with.
type Tokens interface {
	// RepositoryToken is github.AppClient.RepositoryToken.
	RepositoryToken(ctx context.Context, installationID int64, target string, presets []string) (github.InstallationToken, error)
}

// BotIdentities reads the App's bot user.
type BotIdentities interface {
	BotIdentity(ctx context.Context) (store.GitHubAppBot, error)
}

// TxBeginner opens the transaction a reconcile applies its findings in.
type TxBeginner interface {
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

// Reconciler compares the stored open PRs of a repository with GitHub and repairs the drift
// (specs.html section 5.1): a bot PR GitHub lists and the store lacks is created, and a stored
// open PR that is no longer listed is fetched and set to merged or closed as GitHub reports.
// A stored open PR that GitHub answers 404 for, after the listing succeeded with the same token,
// is gone and is stored closed. Each PR it applies goes through Tracker.ApplyTx, so the
// dependency and supersede rules run for it as they do for a webhook.
type Reconciler struct {
	Store   store.UpdatePRRepository
	Tracker *Tracker
	API     API
	Tokens  Tokens
	Bot     BotIdentities
	DB      TxBeginner
	// Wake is fired after a reconcile commits a change; it may be nil.
	Wake *Notifier
	// Logger defaults to slog.Default(). It receives the repository and counts, never PR
	// text.
	Logger *slog.Logger

	mu      sync.Mutex
	pages   map[string]*pageCache // by repository full name
	cursors map[string]int64      // by repository id: the last stored PR number fetched one by one
}

// pageCache holds the last listing of a repository: the pages by URL, for conditional
// requests. It is only valid for the bot user it was filtered for.
type pageCache struct {
	botUserID int64
	pages     map[string]cachedPage
}

type cachedPage struct {
	etag string
	prs  []github.PullRequest // the bot PRs of the page
	next string
}

func (r *Reconciler) logger() *slog.Logger {
	if r.Logger != nil {
		return r.Logger
	}
	return slog.Default()
}

// Reconcile reads the open bot PRs of the repository from GitHub and applies them, with the
// PRs it had to fetch, in one transaction: a failing call or write leaves the stored rows as
// they were and returns the error. A repository that is removed or paused, and an App whose
// bot user id is not known yet, are skipped without an error, since nothing is a bot PR then.
// Concurrent reconciles are serialized.
func (r *Reconciler) Reconcile(ctx context.Context, repositoryID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	repo, err := r.Store.ReconcileRepository(ctx, repositoryID)
	if errors.Is(err, store.ErrRepositoryNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	bot, err := r.Bot.BotIdentity(ctx)
	if errors.Is(err, store.ErrGitHubAppNotFound) || (err == nil && !bot.Known) {
		r.logger().Debug("update PR reconcile skipped: the App's bot user id is not known", "repository", repo.FullName)
		return nil
	}
	if err != nil {
		return err
	}
	token, err := r.Tokens.RepositoryToken(ctx, repo.GitHubInstallationID, repo.FullName, nil)
	if err != nil {
		return fmt.Errorf("installation token: %w", err)
	}

	listed, err := r.list(ctx, token.Value(), repo.FullName, bot.UserID)
	if err != nil {
		return fmt.Errorf("list open pull requests: %w", err)
	}
	stored, err := r.Store.OpenNumbers(ctx, repo.ID)
	if err != nil {
		return err
	}
	seen := make(map[int64]bool, len(listed))
	for _, pr := range listed {
		seen[pr.Number] = true
	}
	var vanished []int64
	for _, number := range stored {
		if !seen[number] {
			vanished = append(vanished, number)
		}
	}
	vanished = r.nextFetches(repo.ID, vanished)
	listedCount := len(listed)
	var gone []int64
	for _, number := range vanished {
		pr, err := r.API.PullRequest(ctx, token.Value(), repo.FullName, number)
		if errors.Is(err, github.ErrPullRequestNotFound) {
			gone = append(gone, number)
			continue
		}
		if err != nil {
			return fmt.Errorf("get pull request %d: %w", number, err)
		}
		listed = append(listed, pr)
	}

	changed, events, err := r.apply(ctx, repo.ID, bot.UserID, listed, gone)
	if err != nil {
		return err
	}
	r.Tracker.Publish(events)
	if len(vanished) > 0 {
		r.cursors[repo.ID] = vanished[len(vanished)-1]
	}
	if changed > 0 {
		r.Wake.Wake()
	}
	r.logger().Debug("update PR reconcile done", "repository", repo.FullName, "listed", listedCount, "fetched", len(vanished), "gone", len(gone), "changed", changed)
	return nil
}

// nextFetches picks at most maxFetches of the vanished PR numbers (ascending) to fetch now: it
// starts after the last number the repository's previous reconcile fetched and wraps around,
// so that when more than maxFetches stay unresolved every one of them is reached in turn.
func (r *Reconciler) nextFetches(repositoryID string, vanished []int64) []int64 {
	if r.cursors == nil {
		r.cursors = map[string]int64{}
	}
	if len(vanished) <= maxFetches {
		return vanished
	}
	start := slices.IndexFunc(vanished, func(n int64) bool { return n > r.cursors[repositoryID] })
	if start < 0 {
		start = 0
	}
	picked := make([]int64, 0, maxFetches)
	for i := range maxFetches {
		picked = append(picked, vanished[(start+i)%len(vanished)])
	}
	return picked
}

// apply upserts the PRs and closes the gone ones in one transaction and returns how many rows
// changed and the events of the PRs it applied; the caller publishes them once the commit
// succeeded.
func (r *Reconciler) apply(ctx context.Context, repositoryID string, botUserID int64, prs []github.PullRequest, gone []int64) (int, []Event, error) {
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, nil, fmt.Errorf("begin reconcile: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	changed := 0
	var events []Event
	for _, pr := range prs {
		out, err := r.Tracker.ApplyTx(ctx, tx, repositoryID, pr, botUserID)
		if err != nil {
			return 0, nil, err
		}
		if out.Changed {
			changed++
		}
		events = append(events, out.Events...)
	}
	for _, number := range gone {
		c, err := r.Store.CloseGone(ctx, tx, repositoryID, number, r.Tracker.now())
		if err != nil {
			return 0, nil, err
		}
		if c {
			r.logger().Warn("update PR reconcile: a stored open pull request is not known to GitHub, stored closed", "repository", repositoryID, "number", number)
			changed++
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, nil, fmt.Errorf("commit reconcile: %w", err)
	}
	return changed, events, nil
}

// list returns the open PRs of the repository that the bot authored and that carry Renovate's
// markers, every page. A page whose ETag GitHub still honours is served from the last listing.
func (r *Reconciler) list(ctx context.Context, token, fullName string, botUserID int64) ([]github.PullRequest, error) {
	if r.pages == nil {
		r.pages = map[string]*pageCache{}
	}
	old := r.pages[fullName]
	if old == nil || old.botUserID != botUserID {
		old = &pageCache{botUserID: botUserID}
	}
	fresh := &pageCache{botUserID: botUserID, pages: map[string]cachedPage{}}
	delete(r.pages, fullName)

	var out []github.PullRequest
	pageURL := ""
	for n := 0; ; n++ {
		if n == 1000 {
			return nil, errors.New("too many pages")
		}
		cached, haveCached := old.pages[pageURL]
		etag := ""
		if haveCached {
			etag = cached.etag
		}
		page, err := r.API.OpenPullRequestsPage(ctx, token, fullName, pageURL, etag)
		if err != nil {
			return nil, err
		}
		entry := cachedPage{etag: page.ETag, next: page.Next}
		if page.NotModified && haveCached {
			entry.prs = cached.prs
			if entry.next == "" {
				// A 304 need not repeat Link; the page is unchanged, so its next page is too.
				entry.next = cached.next
			}
		} else if page.NotModified {
			return nil, errors.New("page not modified without a cached page")
		} else {
			for _, pr := range page.PullRequests {
				if pr.Valid() && pr.User.ID == botUserID && HasRenovateMarkers(pr.Body) {
					entry.prs = append(entry.prs, pr)
				}
			}
		}
		fresh.pages[pageURL] = entry
		out = append(out, entry.prs...)
		if entry.next == "" {
			break
		}
		pageURL = entry.next
	}
	r.pages[fullName] = fresh
	return out, nil
}

// Runner runs reconciles one at a time, off the request that asked for one: a job result, an
// hourly tick or the start-up. A repository asked for while its reconcile waits is reconciled
// once.
type Runner struct {
	Reconciler *Reconciler
	// Repositories lists the repositories TriggerAll asks for.
	Repositories interface {
		ReconcileRepositoryIDs(ctx context.Context) ([]string, error)
	}
	// Report gets the outcome of every reconcile, nil when it succeeded; it may be nil.
	Report func(repositoryID string, err error)

	mu      sync.Mutex
	pending []string
	wake    chan struct{}
	once    sync.Once
}

func (r *Runner) init() {
	r.once.Do(func() { r.wake = make(chan struct{}, 1) })
}

// Trigger asks for a reconcile of the repository and returns at once.
func (r *Runner) Trigger(repositoryID string) {
	r.init()
	r.mu.Lock()
	if !slices.Contains(r.pending, repositoryID) {
		r.pending = append(r.pending, repositoryID)
	}
	r.mu.Unlock()
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

// TriggerAll asks for a reconcile of every repository that takes part.
func (r *Runner) TriggerAll(ctx context.Context) error {
	ids, err := r.Repositories.ReconcileRepositoryIDs(ctx)
	if err != nil {
		return err
	}
	for _, id := range ids {
		r.Trigger(id)
	}
	return nil
}

func (r *Runner) next() (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.pending) == 0 {
		return "", false
	}
	id := r.pending[0]
	r.pending = r.pending[1:]
	return id, true
}

// Run reconciles what was asked for until ctx ends. A failed reconcile does not stop the
// next; the hourly one repairs what it left.
func (r *Runner) Run(ctx context.Context) {
	r.init()
	for {
		for {
			id, ok := r.next()
			if !ok {
				break
			}
			rctx, cancel := context.WithTimeout(ctx, ReconcileTimeout)
			err := r.Reconciler.Reconcile(rctx, id)
			cancel()
			if ctx.Err() != nil {
				return
			}
			if r.Report != nil {
				r.Report(id, err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-r.wake:
		}
	}
}
