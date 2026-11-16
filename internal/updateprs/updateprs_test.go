package updateprs_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/crypto"
	"github.com/mdg-labs/release-ops/internal/github"
	"github.com/mdg-labs/release-ops/internal/github/githubtest"
	"github.com/mdg-labs/release-ops/internal/store"
	"github.com/mdg-labs/release-ops/internal/store/storetest"
	"github.com/mdg-labs/release-ops/internal/updateprs"
)

const botID int64 = 99887766

const renovateBody = "Updates.\n\n - [ ] <!-- rebase-check -->rebase\n\n<!--renovate-debug:eyJ4IjoxfQ==-->\n"

var epoch = time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)

func at(minutes int) time.Time { return epoch.Add(time.Duration(minutes) * time.Minute) }

type rig struct {
	db      *store.DB
	st      *store.Store
	fake    *githubtest.Fake
	rec     *updateprs.Reconciler
	tracker *updateprs.Tracker
	wake    *updateprs.Notifier
	repo    string
}

// newRig stores an App, its bot user id and one repository (acme/repo-1, installation 1) and
// serves a fake GitHub that knows the installation.
func newRig(t *testing.T, withBot bool) *rig {
	t.Helper()
	db := storetest.OpenMigrated(t)
	cipher, err := crypto.NewCipherFromHex(strings.Repeat("ab", 32))
	if err != nil {
		t.Fatal(err)
	}
	st := store.New(db, cipher)
	fake := githubtest.New(t, time.Now)
	fake.AddInstallation(1, githubtest.AppID)
	if err := st.GitHubApp().Create(context.Background(), store.NewGitHubApp{
		AppID: githubtest.AppID, Slug: "release-ops-acme", ClientID: "cid", HTMLURL: "https://github.com/apps/release-ops-acme",
		OwnerLogin: "acme", OwnerType: "Organization", PrivateKey: string(fake.PEM), ClientSecret: "client-secret-value", WebhookSecret: "ws",
	}); err != nil {
		t.Fatal(err)
	}
	if withBot {
		if _, err := st.GitHubApp().RecordBotUserID(context.Background(), botID); err != nil {
			t.Fatal(err)
		}
	}
	client := github.NewClient(fake.URL, 0)
	wake := updateprs.NewNotifier()
	tracker := updateprs.NewTracker(st.UpdatePRs(), func() time.Time { return epoch.Add(2 * time.Hour) }, wake)
	return &rig{
		db: db, st: st, fake: fake, wake: wake, tracker: tracker, repo: storetest.SeedRepositories(t, db, 1)[0],
		rec: &updateprs.Reconciler{
			Store: st.UpdatePRs(), Tracker: tracker, API: client,
			Tokens: github.NewAppClient(client, st.GitHubApp(), github.AppClientOptions{}),
			Bot:    st.GitHubApp(), DB: db.Writer, Wake: wake,
		},
	}
}

// bot adds an open bot PR to the fake GitHub.
func (r *rig) bot(number int64, mutate ...func(*githubtest.PullRequest)) {
	pr := githubtest.PullRequest{
		Number: number, Title: "chore(deps): update dependency", Body: renovateBody, AuthorID: botID,
		HeadRef: "renovate/dep", HeadSHA: "abc", State: "open", CreatedAt: at(0), UpdatedAt: at(1),
	}
	for _, m := range mutate {
		m(&pr)
	}
	r.fake.SetPullRequest("acme/repo-1", pr)
}

func (r *rig) states(t *testing.T) map[int64]string {
	t.Helper()
	rows, err := r.db.Reader.Query(`SELECT number, state FROM update_prs WHERE repository_id = ?`, r.repo)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	out := map[int64]string{}
	for rows.Next() {
		var n int64
		var s string
		if err := rows.Scan(&n, &s); err != nil {
			t.Fatal(err)
		}
		out[n] = s
	}
	return out
}

func (r *rig) woken() bool {
	select {
	case <-r.wake.C():
		return true
	default:
		return false
	}
}

func (r *rig) apply(t *testing.T, pr github.PullRequest, botUserID int64) bool {
	t.Helper()
	return r.applyOutcome(t, pr, botUserID).Changed
}

func (r *rig) applyOutcome(t *testing.T, pr github.PullRequest, botUserID int64) updateprs.Outcome {
	t.Helper()
	tx, err := r.db.Writer.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	out, err := r.tracker.ApplyTx(context.Background(), tx, r.repo, pr, botUserID)
	if err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return out
}

func prOf(number int64, user int64, body string, mutate ...func(*github.PullRequest)) github.PullRequest {
	created, updated := at(0), at(1)
	pr := github.PullRequest{Number: number, NodeID: "PR_n", HTMLURL: "https://github.com/acme/repo-1/pull/1", Title: "t", Body: &body, State: "open", CreatedAt: &created, UpdatedAt: &updated}
	pr.User.ID = user
	pr.Head.Ref = "renovate/x"
	pr.Head.SHA = "sha"
	for _, m := range mutate {
		m(&pr)
	}
	return pr
}

func TestHasRenovateMarkers(t *testing.T) {
	for _, tc := range []struct {
		name string
		body *string
		want bool
	}{
		{"debug comment", ptr("text\n<!--renovate-debug:abc-->\n"), true},
		{"checkbox", ptr("text\n - [ ] <!-- rebase-check -->rebase\n"), true},
		{"ticked checkbox with CRLF", ptr("text\r\n - [x] <!-- approve-branch=renovate/foo -->go\r\n"), true},
		{"star bullet", ptr("* [ ] <!-- unlimit-branch=x -->\n"), true},
		{"plain text", ptr("Enable Renovate by merging this PR."), false},
		{"a comment that is not a checkbox", ptr("<!-- some comment -->\n"), false},
		{"empty marker", ptr(" - [ ] <!-- -->\n"), false},
		{"nil body", nil, false},
	} {
		if got := updateprs.HasRenovateMarkers(tc.body); got != tc.want {
			t.Errorf("%s: HasRenovateMarkers = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func ptr(s string) *string { return &s }

func TestApplyStoresABotPRWhateverItsBranchOrTitleAndNothingElse(t *testing.T) {
	r := newRig(t, true)
	for _, tc := range []struct {
		name   string
		number int64
		pr     github.PullRequest
		bot    int64
		stored bool
	}{
		{"default prefix", 1, prOf(1, botID, renovateBody), botID, true},
		{"non-default prefix and a plain title", 2, prOf(2, botID, renovateBody, func(p *github.PullRequest) { p.Head.Ref = "deps/lodash"; p.Title = "Bump lodash" }), botID, true},
		{"config migration", 3, prOf(3, botID, "Migrate.\n<!--renovate-debug:eyJ4IjoyfQ==-->\n", func(p *github.PullRequest) { p.Head.Ref = "renovate/migrate-config" }), botID, true},
		{"onboarding with a checkbox only", 4, prOf(4, botID, " - [ ] <!-- create-config-migration-pr -->Select.\n"), botID, true},
		{"a person with a Renovate branch, title and body", 5, prOf(5, 1, renovateBody, func(p *github.PullRequest) { p.Title = "chore(deps): update x" }), botID, false},
		{"the bot without markers", 6, prOf(6, botID, "Enable Renovate for this repository."), botID, false},
		{"the bot while its id is not known", 7, prOf(7, botID, renovateBody), 0, false},
		{"a PR without its node id", 8, prOf(8, botID, renovateBody, func(p *github.PullRequest) { p.NodeID = "" }), botID, false},
	} {
		changed := r.apply(t, tc.pr, tc.bot)
		_, stored := r.states(t)[tc.number]
		if stored != tc.stored || changed != tc.stored {
			t.Errorf("%s: stored = %v, changed = %v, want %v", tc.name, stored, changed, tc.stored)
		}
	}
}

func TestApplyKeepsTrackingAStoredBotPRWhoseEditDroppedTheMarkers(t *testing.T) {
	r := newRig(t, true)
	r.apply(t, prOf(1, botID, renovateBody), botID)
	closed := at(30)
	r.apply(t, prOf(1, botID, "no markers any more", func(p *github.PullRequest) {
		p.State, p.ClosedAt = "closed", &closed
		u := at(30)
		p.UpdatedAt = &u
	}), botID)
	if got := r.states(t)[1]; got != "closed" {
		t.Fatalf("state = %q, want closed", got)
	}
	// A person's PR with the same number never takes the row over.
	r.apply(t, prOf(1, 1, renovateBody, func(p *github.PullRequest) { p.Title = "hijack" }), botID)
	var title string
	if err := r.db.Reader.QueryRow(`SELECT title FROM update_prs WHERE number = 1`).Scan(&title); err != nil || title == "hijack" {
		t.Fatalf("title = %q, %v", title, err)
	}
}

func TestApplyMapsMergedAndClosedAndCapsTheText(t *testing.T) {
	r := newRig(t, true)
	long := strings.Repeat("é", updateprs.MaxTitleBytes) // two bytes each
	merged, closedAt := at(30), at(30)
	r.apply(t, prOf(1, botID, renovateBody, func(p *github.PullRequest) {
		p.State, p.Merged, p.MergedAt, p.ClosedAt, p.Title = "closed", true, &merged, &closedAt, long
		p.UpdatedAt = &closedAt
	}), botID)
	r.apply(t, prOf(2, botID, renovateBody, func(p *github.PullRequest) {
		p.State, p.ClosedAt = "closed", &closedAt
		p.UpdatedAt = &closedAt
	}), botID)
	// The list API has no merged flag; merged_at tells it.
	r.apply(t, prOf(3, botID, renovateBody, func(p *github.PullRequest) {
		p.State, p.MergedAt, p.ClosedAt = "closed", &merged, &closedAt
		p.UpdatedAt = &closedAt
	}), botID)
	if got := r.states(t); got[1] != "merged" || got[2] != "closed" || got[3] != "merged" {
		t.Fatalf("states = %v", got)
	}
	var title string
	var mergedAt sql.NullString
	if err := r.db.Reader.QueryRow(`SELECT title, merged_at FROM update_prs WHERE number = 1`).Scan(&title, &mergedAt); err != nil {
		t.Fatal(err)
	}
	if len(title) != updateprs.MaxTitleBytes || !strings.HasSuffix(title, "é") || mergedAt.String != "2026-11-15T10:30:00.000Z" {
		t.Fatalf("title has %d bytes, merged_at = %v", len(title), mergedAt)
	}
}

func TestNotifierMergesWakesAndIsNilSafe(t *testing.T) {
	n := updateprs.NewNotifier()
	n.Wake()
	n.Wake()
	select {
	case <-n.C():
	default:
		t.Fatal("no wake after Wake")
	}
	select {
	case <-n.C():
		t.Fatal("two wakes were delivered separately")
	default:
	}
	var none *updateprs.Notifier
	none.Wake()
	if none.C() != nil {
		t.Fatal("a nil notifier has a channel")
	}
}
