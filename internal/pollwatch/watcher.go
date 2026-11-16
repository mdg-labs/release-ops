package pollwatch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/mdg-labs/release-ops/internal/api/hooks"
	"github.com/mdg-labs/release-ops/internal/github"
	"github.com/mdg-labs/release-ops/internal/queue"
	"github.com/mdg-labs/release-ops/internal/store"
)

const (
	// RepositoryTimeout bounds one repository's pass, GitHub calls included.
	RepositoryTimeout = 2 * time.Minute
	// maxPages bounds how many pages of one listing a pass follows.
	maxPages = 50
)

// API reads from GitHub.
type API interface {
	// OpenPullRequestsPage is github.Client.OpenPullRequestsPage.
	OpenPullRequestsPage(ctx context.Context, token, fullName, pageURL, etag string) (github.PullRequestPage, error)
	// OpenIssuesPage is github.Client.OpenIssuesPage.
	OpenIssuesPage(ctx context.Context, token, fullName, creator, pageURL, etag string) (github.IssuePage, error)
	// BranchHead is github.Client.BranchHead.
	BranchHead(ctx context.Context, token, fullName, branch, etag string) (github.BranchHead, error)
}

// Tokens mints the installation token a pass reads with.
type Tokens interface {
	// RepositoryToken is github.AppClient.RepositoryToken.
	RepositoryToken(ctx context.Context, installationID int64, target string, presets []string) (github.InstallationToken, error)
}

// BotIdentities reads the App's bot user.
type BotIdentities interface {
	BotIdentity(ctx context.Context) (store.GitHubAppBot, error)
}

// Repositories lists the repositories to watch and finds their installation.
type Repositories interface {
	// ReconcileRepositoryIDs is store.UpdatePRRepository.ReconcileRepositoryIDs: the enabled
	// repositories of an active installation.
	ReconcileRepositoryIDs(ctx context.Context) ([]string, error)
	// ReconcileRepository is store.UpdatePRRepository.ReconcileRepository.
	ReconcileRepository(ctx context.Context, id string) (store.ReconcileRepository, error)
}

// Branches reads the default branch of a repository.
type Branches interface {
	// Get is store.RepositoryRepository.Get.
	Get(ctx context.Context, id string) (store.Repository, error)
}

// Enqueuer is the queue's enqueue call.
type Enqueuer interface {
	Enqueue(ctx context.Context, p queue.EnqueueParams) (queue.EnqueueResult, error)
}

// Result counts what a pass did.
type Result struct {
	// Checked is the number of repositories read.
	Checked int
	// Requested is the number of repositories a run was asked for.
	Requested int
}

// Watcher polls the repositories. Its zero fields are not usable; Run and Poll are called from
// one goroutine at a time.
type Watcher struct {
	Repositories Repositories
	Branches     Branches
	Bot          BotIdentities
	Tokens       Tokens
	API          API
	Queue        Enqueuer
	// Logger defaults to slog.Default(). It receives the repository, counts and marker counts,
	// never a body.
	Logger *slog.Logger

	mu    sync.Mutex
	state map[string]*repoState // by repository id
}

func (w *Watcher) logger() *slog.Logger {
	if w.Logger != nil {
		return w.Logger
	}
	return slog.Default()
}

// Run polls at once, to take the baseline, and then on every tick until ctx ends; the caller
// owns the clock. report gets the result of every pass; a failed pass does not stop the next.
func (w *Watcher) Run(ctx context.Context, ticks <-chan time.Time, report func(Result, error)) {
	for {
		res, err := w.Poll(ctx)
		if err == nil || ctx.Err() == nil {
			report(res, err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticks:
		}
	}
}

// Poll reads every repository once and asks the queue for a normal-priority run of each one
// that changed. A failure on one repository does not stop the others; the errors of all are
// returned together. Before an App is stored, or while its bot user id is not known, nothing
// is a bot object and only the default branch is watched.
func (w *Watcher) Poll(ctx context.Context) (Result, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	ids, err := w.Repositories.ReconcileRepositoryIDs(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("list repositories: %w", err)
	}
	if w.state == nil {
		w.state = map[string]*repoState{}
	}
	listed := make(map[string]bool, len(ids))
	for _, id := range ids {
		listed[id] = true
	}
	for id := range w.state {
		if !listed[id] {
			delete(w.state, id)
		}
	}
	bot, botKnown, err := w.bot(ctx)
	if err != nil {
		return Result{}, err
	}

	var res Result
	var errs []error
	for _, id := range ids {
		rctx, cancel := context.WithTimeout(ctx, RepositoryTimeout)
		requested, err := w.poll(rctx, id, bot, botKnown)
		cancel()
		if ctx.Err() != nil {
			return res, ctx.Err()
		}
		res.Checked++
		if requested {
			res.Requested++
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("repository %s: %w", id, err))
		}
	}
	return res, errors.Join(errs...)
}

func (w *Watcher) bot(ctx context.Context) (store.GitHubAppBot, bool, error) {
	bot, err := w.Bot.BotIdentity(ctx)
	if errors.Is(err, store.ErrGitHubAppNotFound) {
		return store.GitHubAppBot{}, false, nil
	}
	if err != nil {
		return store.GitHubAppBot{}, false, err
	}
	return bot, bot.Known, nil
}

// finding is what one of the three watches found in a repository.
type finding struct {
	// changed reports that a run is asked for.
	changed bool
	markers []string
	// commit makes what was read the baseline of the next pass. It runs only once the queue has
	// taken the request, so a change whose request failed is found again.
	commit func()
}

func (w *Watcher) poll(ctx context.Context, id string, bot store.GitHubAppBot, botKnown bool) (bool, error) {
	repo, err := w.Repositories.ReconcileRepository(ctx, id)
	if errors.Is(err, store.ErrRepositoryNotFound) {
		delete(w.state, id)
		return false, nil
	}
	if err != nil {
		return false, err
	}
	token, err := w.Tokens.RepositoryToken(ctx, repo.GitHubInstallationID, repo.FullName, nil)
	if err != nil {
		return false, fmt.Errorf("installation token: %w", err)
	}
	st := w.state[id]
	if st == nil {
		st = &repoState{}
		w.state[id] = st
	}
	if st.botUserID != bot.UserID {
		st.botUserID = bot.UserID
		st.issuePages, st.issues, st.prPages, st.prs = nil, bodies{}, nil, bodies{}
	}

	var errs []error
	var findings []finding
	add := func(f finding, err error) {
		if err != nil {
			errs = append(errs, err)
			return
		}
		findings = append(findings, f)
	}
	if botKnown {
		f, err := st.watchIssues(ctx, w.API, token.Value(), repo.FullName, bot)
		add(f, wrap("issues", err))
		f, err = st.watchPullRequests(ctx, w.API, token.Value(), repo.FullName, bot)
		add(f, wrap("pull requests", err))
	}
	if branch, err := w.Branches.Get(ctx, id); err != nil {
		errs = append(errs, fmt.Errorf("read repository: %w", err))
	} else {
		f, err := st.watchHead(ctx, w.API, token.Value(), repo.FullName, branch.DefaultBranch)
		add(f, wrap("default branch", err))
	}

	var markers []string
	var changed bool
	for _, f := range findings {
		if f.changed {
			changed = true
			markers = appendMarkers(markers, f.markers)
		}
	}
	if changed {
		if err := w.request(ctx, id, repo.FullName, markers); err != nil {
			// Nothing is committed: the next pass finds the same changes.
			return false, errors.Join(append(errs, err)...)
		}
	}
	for _, f := range findings {
		f.commit()
	}
	return changed, errors.Join(errs...)
}

func wrap(what string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", what, err)
}

// request asks for a normal-priority run. The queue coalesces it into a queued job of the
// repository and drops it while a run is in progress; a repository paused since it was listed
// takes none.
func (w *Watcher) request(ctx context.Context, id, fullName string, markers []string) error {
	res, err := w.Queue.Enqueue(ctx, queue.EnqueueParams{
		RepositoryID:   id,
		Kind:           queue.KindRenovateRun,
		Priority:       queue.PriorityNormal,
		Trigger:        queue.TriggerPolling,
		MatchedMarkers: markers,
	})
	if errors.Is(err, queue.ErrRepositoryPaused) || errors.Is(err, queue.ErrRepositoryNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("enqueue: %w", err)
	}
	w.logger().Info("polling: run requested", "repository", fullName, "outcome", string(res.Outcome), "markers", len(markers))
	return nil
}

// appendMarkers adds the markers not yet in dst, up to what one job stores.
func appendMarkers(dst, add []string) []string {
	for _, m := range add {
		if len(dst) == queue.MaxMarkers {
			break
		}
		if !slices.Contains(dst, m) {
			dst = append(dst, m)
		}
	}
	return dst
}

// repoState is what the watcher keeps of one repository.
type repoState struct {
	// botUserID is the bot the cached pages were filtered for.
	botUserID int64

	issuePages map[string]cachedPage[object]
	issues     bodies

	prPages map[string]cachedPage[object]
	prs     bodies

	branch   string
	headETag string
	// seen is the head the last read answered; head is the one the last acted-on pass saw.
	seen      string
	head      string
	headKnown bool
}

// object is the number and body of a bot-authored issue or PR.
type object struct {
	number int64
	body   string
}

// bodies are the bodies seen on the last pass that was acted on, by object number; known is
// false until a first pass has set them.
type bodies struct {
	known bool
	by    map[int64]string
}

type cachedPage[T any] struct {
	etag  string
	items []T
	next  string
}

// walk reads every page of a listing, asking for each page with the ETag of the last read, and
// returns the objects of all pages and the pages to ask with next time. A page that stays
// unchanged is served from the cache.
func walk[T any](old map[string]cachedPage[T], fetch func(pageURL, etag string) (items []T, next, newETag string, notModified bool, err error)) ([]T, map[string]cachedPage[T], error) {
	fresh := map[string]cachedPage[T]{}
	var out []T
	pageURL := ""
	for n := 0; ; n++ {
		if n == maxPages {
			return nil, nil, errors.New("too many pages")
		}
		cached, have := old[pageURL]
		etag := ""
		if have {
			etag = cached.etag
		}
		items, next, newETag, notModified, err := fetch(pageURL, etag)
		if err != nil {
			return nil, nil, err
		}
		entry := cachedPage[T]{etag: newETag, items: items, next: next}
		if notModified {
			if !have {
				return nil, nil, errors.New("page not modified without a cached page")
			}
			entry.items = cached.items
			if entry.next == "" {
				// A 304 need not repeat Link; the page is unchanged, so its next page is too.
				entry.next = cached.next
			}
		}
		fresh[pageURL] = entry
		out = append(out, entry.items...)
		if entry.next == "" {
			return out, fresh, nil
		}
		pageURL = entry.next
	}
}

func bodyOf(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func (s *repoState) watchIssues(ctx context.Context, api API, token, fullName string, bot store.GitHubAppBot) (finding, error) {
	objects, pages, err := walk(s.issuePages, func(pageURL, etag string) ([]object, string, string, bool, error) {
		page, err := api.OpenIssuesPage(ctx, token, fullName, bot.Slug+"[bot]", pageURL, etag)
		if err != nil {
			return nil, "", "", false, err
		}
		var items []object
		for _, issue := range page.Issues {
			if issue.PullRequest == nil && issue.State == "open" && issue.User.ID == bot.UserID {
				items = append(items, object{number: issue.Number, body: bodyOf(issue.Body)})
			}
		}
		return items, page.Next, page.ETag, page.NotModified, nil
	})
	if err != nil {
		return finding{}, err
	}
	s.issuePages = pages
	cur := toMap(objects)
	f := finding{commit: func() { s.issues = bodies{known: true, by: cur} }}
	if s.issues.known {
		f.markers = ticked(s.issues.by, cur)
		f.changed = len(f.markers) > 0
	}
	return f, nil
}

func (s *repoState) watchPullRequests(ctx context.Context, api API, token, fullName string, bot store.GitHubAppBot) (finding, error) {
	objects, pages, err := walk(s.prPages, func(pageURL, etag string) ([]object, string, string, bool, error) {
		page, err := api.OpenPullRequestsPage(ctx, token, fullName, pageURL, etag)
		if err != nil {
			return nil, "", "", false, err
		}
		var items []object
		for _, pr := range page.PullRequests {
			if pr.Valid() && pr.User.ID == bot.UserID {
				items = append(items, object{number: pr.Number, body: bodyOf(pr.Body)})
			}
		}
		return items, page.Next, page.ETag, page.NotModified, nil
	})
	if err != nil {
		return finding{}, err
	}
	s.prPages = pages
	cur := toMap(objects)
	f := finding{commit: func() { s.prs = bodies{known: true, by: cur} }}
	if s.prs.known {
		f.markers = ticked(s.prs.by, cur)
		// A bot PR that is no longer listed as open was closed or merged.
		closed := false
		for number := range s.prs.by {
			if _, open := cur[number]; !open {
				closed = true
				break
			}
		}
		f.changed = closed || len(f.markers) > 0
	}
	return f, nil
}

func (s *repoState) watchHead(ctx context.Context, api API, token, fullName, branch string) (finding, error) {
	if s.branch != branch {
		s.branch, s.headETag, s.seen, s.head, s.headKnown = branch, "", "", "", false
	}
	head, err := api.BranchHead(ctx, token, fullName, branch, s.headETag)
	absent := errors.Is(err, github.ErrBranchNotFound)
	if err != nil && !absent {
		return finding{}, err
	}
	if !head.NotModified {
		s.headETag, s.seen = head.ETag, head.SHA
	}
	sha := s.seen
	f := finding{commit: func() { s.head, s.headKnown = sha, true }}
	f.changed = s.headKnown && sha != "" && sha != s.head
	return f, nil
}

func toMap(objects []object) map[int64]string {
	out := make(map[int64]string, len(objects))
	for _, o := range objects {
		out[o.number] = o.body
	}
	return out
}

// ticked returns the markers ticked between the bodies of two passes, over the objects present
// in both, in the order of the object numbers.
func ticked(prev, cur map[int64]string) []string {
	numbers := make([]int64, 0, len(cur))
	for n := range cur {
		if _, ok := prev[n]; ok {
			numbers = append(numbers, n)
		}
	}
	slices.Sort(numbers)
	var out []string
	for _, n := range numbers {
		out = appendMarkers(out, hooks.TickedMarkers(prev[n], cur[n]))
	}
	return out
}
