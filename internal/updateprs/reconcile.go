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
	// are fetched by the next reconcile.
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
// It never marks a PR superseded.
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

	mu    sync.Mutex
	pages map[string]*pageCache // by repository full name
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
	fetched := 0
	for _, number := range stored {
		if seen[number] || fetched == maxFetches {
			continue
		}
		fetched++
		pr, err := r.API.PullRequest(ctx, token.Value(), repo.FullName, number)
		if errors.Is(err, github.ErrPullRequestNotFound) {
			r.logger().Warn("update PR reconcile: a stored open pull request is not known to GitHub", "repository", repo.FullName, "number", number)
			continue
		}
		if err != nil {
			return fmt.Errorf("get pull request %d: %w", number, err)
		}
		listed = append(listed, pr)
	}

	changed, err := r.apply(ctx, repo.ID, bot.UserID, listed)
	if err != nil {
		return err
	}
	if changed > 0 {
		r.Wake.Wake()
	}
	r.logger().Debug("update PR reconcile done", "repository", repo.FullName, "listed", len(listed)-fetched, "fetched", fetched, "changed", changed)
	return nil
}

// apply upserts the PRs in one transaction and returns how many rows changed.
func (r *Reconciler) apply(ctx context.Context, repositoryID string, botUserID int64, prs []github.PullRequest) (int, error) {
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin reconcile: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	changed := 0
	for _, pr := range prs {
		c, err := r.Tracker.ApplyTx(ctx, tx, repositoryID, pr, botUserID)
		if err != nil {
			return 0, err
		}
		if c {
			changed++
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit reconcile: %w", err)
	}
	return changed, nil
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
