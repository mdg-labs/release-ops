package hooks_test

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/api/hooks"
	"github.com/mdg-labs/release-ops/internal/crypto"
	"github.com/mdg-labs/release-ops/internal/github"
	"github.com/mdg-labs/release-ops/internal/queue"
	"github.com/mdg-labs/release-ops/internal/store"
	"github.com/mdg-labs/release-ops/internal/store/storetest"
	"github.com/mdg-labs/release-ops/internal/updateprs"
)

// failingEnqueuer fails every enqueue while fail is set, and otherwise hands over to the real
// queue.
type failingEnqueuer struct {
	real hooks.RunEnqueuer
	fail *atomic.Bool
}

func (f failingEnqueuer) EnqueueTx(ctx context.Context, tx *sql.Tx, p queue.EnqueueParams) (queue.EnqueueResult, func(), error) {
	if f.fail.Load() {
		return queue.EnqueueResult{}, nil, errors.New("enqueue failed")
	}
	return f.real.EnqueueTx(ctx, tx, p)
}

// failingTracker applies the PR through the real tracker and then reports an error while
// failApply is set, as a store that fails after the row was written would.
type failingTracker struct {
	hooks.UpdatePRTracker
	failApply *atomic.Bool
}

func (f failingTracker) ApplyTx(ctx context.Context, tx *sql.Tx, repositoryID string, pr github.PullRequest, botUserID int64) (updateprs.Outcome, error) {
	out, err := f.UpdatePRTracker.ApplyTx(ctx, tx, repositoryID, pr, botUserID)
	if err == nil && f.failApply.Load() {
		return updateprs.Outcome{}, errors.New("upsert failed")
	}
	return out, err
}

type prRig struct {
	srv       *httptest.Server
	db        *store.DB
	wake      *updateprs.Notifier
	logs      *syncBuffer
	fail      *atomic.Bool
	failApply *atomic.Bool
	bot       *flakyBot
}

// newPRRig serves the receiver with the Renovate handlers and, on top of them, the update PR
// handler, as the router registers them. botID zero leaves the bot's user id unknown.
func newPRRig(t *testing.T, botID int64) *prRig {
	t.Helper()
	db := storetest.OpenMigrated(t)
	cipher, err := crypto.NewCipherFromHex(strings.Repeat("ab", 32))
	if err != nil {
		t.Fatal(err)
	}
	st := store.New(db, cipher)
	if err := st.GitHubApp().Create(context.Background(), store.NewGitHubApp{
		AppID: 1234567, Slug: "release-ops-acme", ClientID: "cid", HTMLURL: "https://github.com/apps/release-ops-acme",
		OwnerLogin: "acme", OwnerType: "Organization", PrivateKey: "pem", ClientSecret: "client-secret-value", WebhookSecret: secret,
	}); err != nil {
		t.Fatal(err)
	}
	if botID != 0 {
		if _, err := st.GitHubApp().RecordBotUserID(context.Background(), botID); err != nil {
			t.Fatal(err)
		}
	}
	r := &prRig{db: db, wake: updateprs.NewNotifier(), logs: &syncBuffer{}, fail: &atomic.Bool{}, failApply: &atomic.Bool{}, bot: &flakyBot{BotIdentities: st.GitHubApp()}}
	logger := slog.New(slog.NewTextHandler(r.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	routes := hooks.NewRoutes()
	hooks.RegisterInstallationHandlers(routes, st.InstallationSync(), nil)
	hooks.RegisterRenovateHandlers(routes, hooks.RenovateTriggers{
		Queue: failingEnqueuer{real: store.NewJobQueue(db, store.JobQueueOptions{}), fail: r.fail}, Repositories: st, Bot: r.bot, Logger: logger,
	})
	tracker := updateprs.NewTracker(st.UpdatePRs(), func() time.Time { return receivedAt }, r.wake)
	hooks.RegisterUpdatePRHandler(routes, hooks.UpdatePRs{Tracker: failingTracker{UpdatePRTracker: tracker, failApply: r.failApply}, Repositories: st, Bot: r.bot, Logger: logger})
	r.srv = httptest.NewServer(&hooks.Receiver{
		Secrets: secrets{}, Deliveries: st.WebhookDeliveries(), Routes: routes, Now: func() time.Time { return receivedAt },
	})
	t.Cleanup(r.srv.Close)

	rig := &trigRig{srv: r.srv}
	rig.mustDeliver(t, "installation", "seed-installation", fixture(t, "installation_created.json"))
	return r
}

func (r *prRig) deliver(t *testing.T, guid, file string) int {
	t.Helper()
	return (&trigRig{srv: r.srv}).post(t, "pull_request", guid, fixture(t, file))
}

type prState struct {
	State, Title, HeadSHA, Labels, URL, NodeID, HeadRef string
	Opened, Merged, Closed, Seen                        string
}

func (r *prRig) row(t *testing.T, number int64) (prState, bool) {
	t.Helper()
	var s prState
	var sha, opened, merged, closed sql.NullString
	err := r.db.Reader.QueryRow(`SELECT state, title, head_sha, labels, url, node_id, head_ref, opened_at, merged_at, closed_at, last_seen_at FROM update_prs WHERE number = ?`, number).
		Scan(&s.State, &s.Title, &sha, &s.Labels, &s.URL, &s.NodeID, &s.HeadRef, &opened, &merged, &closed, &s.Seen)
	if errors.Is(err, sql.ErrNoRows) {
		return prState{}, false
	}
	if err != nil {
		t.Fatal(err)
	}
	s.HeadSHA, s.Opened, s.Merged, s.Closed = sha.String, opened.String, merged.String, closed.String
	return s, true
}

func (r *prRig) rows(t *testing.T) int {
	t.Helper()
	var n int
	if err := r.db.Reader.QueryRow(`SELECT COUNT(*) FROM update_prs`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (r *prRig) woken() bool {
	select {
	case <-r.wake.C():
		return true
	default:
		return false
	}
}

func (r *prRig) must(t *testing.T, guid, file string) {
	t.Helper()
	if got := r.deliver(t, guid, file); got != http.StatusAccepted {
		t.Fatalf("%s = %d, want 202", file, got)
	}
}

const (
	originalTitle = "chore(deps): update dependency lodash to v4.17.21"
	editedTitle   = "chore(deps): update lodash to v4.17.21 (edited)"
	firstSHA      = "0123456789abcdef0123456789abcdef01234567"
	secondSHA     = "fedcba9876543210fedcba9876543210fedcba98"
)

// Each event of specs.html 5.1 upserts the one row of the PR with the state GitHub reports.
func TestPullRequestFixturesUpsertTheRowOfTheBotPR(t *testing.T) {
	r := newPRRig(t, botUserID)

	for i, step := range []struct {
		file string
		want prState
	}{
		{"update_pr_opened.json", prState{State: "open", Title: originalTitle, HeadSHA: firstSHA, Labels: `["dependencies"]`, Opened: "2026-11-15T10:00:00.000Z", Seen: "2026-11-15T10:00:00.000Z"}},
		{"update_pr_edited.json", prState{State: "open", Title: editedTitle, HeadSHA: firstSHA, Labels: `["dependencies"]`, Opened: "2026-11-15T10:00:00.000Z", Seen: "2026-11-15T10:05:00.000Z"}},
		{"update_pr_synchronize.json", prState{State: "open", Title: editedTitle, HeadSHA: secondSHA, Labels: `["dependencies"]`, Opened: "2026-11-15T10:00:00.000Z", Seen: "2026-11-15T10:10:00.000Z"}},
		{"update_pr_labeled.json", prState{State: "open", Title: editedTitle, HeadSHA: secondSHA, Labels: `["dependencies","security"]`, Opened: "2026-11-15T10:00:00.000Z", Seen: "2026-11-15T10:15:00.000Z"}},
		{"update_pr_unlabeled.json", prState{State: "open", Title: editedTitle, HeadSHA: secondSHA, Labels: `["dependencies"]`, Opened: "2026-11-15T10:00:00.000Z", Seen: "2026-11-15T10:20:00.000Z"}},
		{"update_pr_closed_merged.json", prState{State: "merged", Title: editedTitle, HeadSHA: secondSHA, Labels: `["dependencies"]`, Opened: "2026-11-15T10:00:00.000Z", Merged: "2026-11-15T10:30:00.000Z", Closed: "2026-11-15T10:30:00.000Z", Seen: "2026-11-15T10:30:00.000Z"}},
		{"update_pr_reopened.json", prState{State: "open", Title: editedTitle, HeadSHA: secondSHA, Labels: `["dependencies"]`, Opened: "2026-11-15T10:00:00.000Z", Seen: "2026-11-15T10:40:00.000Z"}},
	} {
		r.must(t, "g-"+step.file, step.file)
		got, ok := r.row(t, 21)
		step.want.URL, step.want.NodeID, step.want.HeadRef = "https://github.com/acme/widgets/pull/21", "PR_fixtureNodeId21", "renovate/lodash-4.x"
		if !ok || got != step.want {
			t.Fatalf("step %d (%s): row = %+v (found %v), want %+v", i, step.file, got, ok, step.want)
		}
		if !r.woken() {
			t.Errorf("step %d (%s): a changed row did not wake the engine", i, step.file)
		}
	}
	if r.rows(t) != 1 {
		t.Fatalf("%d rows, want one", r.rows(t))
	}

	// A close stamped 10:30 is older than the reopen at 10:40 stored above: nothing changes.
	r.must(t, "g-late-close", "update_pr_closed_unmerged.json")
	if got, _ := r.row(t, 21); got.State != "open" {
		t.Fatalf("a close older than the stored reopen changed the row: %+v", got)
	}
	if r.woken() {
		t.Fatal("a delivery older than the stored state woke the engine")
	}
}

func TestUnmergedCloseIsStoredAsClosed(t *testing.T) {
	r := newPRRig(t, botUserID)
	r.must(t, "g-1", "update_pr_opened.json")
	r.must(t, "g-2", "update_pr_closed_unmerged.json")
	if got, _ := r.row(t, 21); got.State != "closed" || got.Merged != "" || got.Closed != "2026-11-15T10:30:00.000Z" {
		t.Fatalf("row = %+v, want closed without a merge time", got)
	}
}

func TestDuplicateAndReplayedDeliveriesLeaveOneRowAndWakeOnce(t *testing.T) {
	r := newPRRig(t, botUserID)
	r.must(t, "g-1", "update_pr_opened.json")
	if !r.woken() {
		t.Fatal("the first delivery did not wake the engine")
	}
	r.must(t, "g-1", "update_pr_opened.json") // GitHub redelivers the same GUID
	r.must(t, "g-2", "update_pr_opened.json") // the same content under another GUID
	if r.woken() {
		t.Fatal("a replay woke the engine")
	}
	if r.rows(t) != 1 {
		t.Fatalf("%d rows, want one", r.rows(t))
	}
}

// An edit generated before the close but delivered after it must not reopen the row.
func TestStaleEditAfterCloseDoesNotReopenOrRewrite(t *testing.T) {
	r := newPRRig(t, botUserID)
	r.must(t, "g-1", "update_pr_opened.json")
	r.must(t, "g-2", "update_pr_synchronize.json")
	r.must(t, "g-3", "update_pr_closed_merged.json")
	r.woken()

	r.must(t, "g-4", "update_pr_edited_stale.json")
	got, _ := r.row(t, 21)
	if got.State != "merged" || got.Title != editedTitle || got.Seen != "2026-11-15T10:30:00.000Z" {
		t.Fatalf("row after the stale edit = %+v, want merged and untouched", got)
	}
	if r.woken() {
		t.Fatal("a stale delivery woke the engine")
	}
}

func TestOutOfOrderDeliveriesEndWithTheNewestState(t *testing.T) {
	r := newPRRig(t, botUserID)
	for _, file := range []string{"update_pr_synchronize.json", "update_pr_edited.json", "update_pr_opened.json"} {
		r.must(t, "g-"+file, file)
	}
	got, _ := r.row(t, 21)
	if got.HeadSHA != secondSHA || got.Seen != "2026-11-15T10:10:00.000Z" || got.Title != editedTitle {
		t.Fatalf("row = %+v, want the synchronize state", got)
	}
}

func TestOnlyBotAuthoredRenovatePRsAreStored(t *testing.T) {
	r := newPRRig(t, botUserID)
	for _, file := range []string{
		"update_pr_by_person.json",  // a Renovate-looking head ref, title and body, but a person's PR
		"update_pr_no_markers.json", // the bot's "Enable repo" PR
		"update_pr_assigned.json",   // an action that changes nothing stored
	} {
		r.must(t, "g-"+file, file)
	}
	if r.rows(t) != 0 {
		t.Fatalf("%d rows, want none", r.rows(t))
	}

	for number, file := range map[int64]string{
		22: "update_pr_non_default_prefix.json",
		30: "update_pr_config_migration.json",
		31: "update_pr_onboarding.json",
	} {
		r.must(t, "g-"+file, file)
		if _, ok := r.row(t, number); !ok {
			t.Errorf("%s: no row for PR %d", file, number)
		}
	}
	if got, _ := r.row(t, 22); got.HeadRef != "deps/lodash-update" {
		t.Fatalf("head ref = %q, want the non-default prefix kept as is", got.HeadRef)
	}
}

func TestNothingIsStoredWhileTheBotUserIdIsUnknown(t *testing.T) {
	r := newPRRig(t, 0)
	r.must(t, "g-1", "update_pr_opened.json")
	if r.rows(t) != 0 || r.woken() {
		t.Fatalf("rows = %d, want none while nothing can be told to be a bot PR", r.rows(t))
	}
}

func TestAFailedBotLookupFailsTheDeliveryAndItsRedeliveryStoresThePR(t *testing.T) {
	r := newPRRig(t, botUserID)

	r.bot.fail.Store(true)
	if got := r.deliver(t, "g-retry", "update_pr_opened.json"); got != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 so GitHub's redelivery is processed", got)
	}
	if r.rows(t) != 0 || r.woken() {
		t.Fatalf("rows = %d, want none from a delivery that failed", r.rows(t))
	}

	r.bot.fail.Store(false)
	r.must(t, "g-retry", "update_pr_opened.json")
	if r.rows(t) != 1 || !r.woken() {
		t.Fatalf("rows = %d, want the redelivery to store the PR", r.rows(t))
	}
}

func TestPullRequestOfAnUntrackedRepositoryIsAcceptedAndDropped(t *testing.T) {
	r := newPRRig(t, botUserID)
	body := mutate(t, "update_pr_opened.json", func(m map[string]any) { obj(m, "repository")["id"] = 424242 })
	if got := (&trigRig{srv: r.srv}).post(t, "pull_request", "g-other", body); got != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", got)
	}
	if r.rows(t) != 0 {
		t.Fatalf("%d rows, want none", r.rows(t))
	}
}

// The upsert and the delivery record commit together: when the rest of the delivery fails the
// row is rolled back with the record, the engine is not woken, and GitHub's redelivery applies
// the delivery once.
func TestAFailedDeliveryRollsBackTheUpsertAndItsRedeliveryAppliesOnce(t *testing.T) {
	r := newPRRig(t, botUserID)
	r.must(t, "g-open", "update_pr_opened.json")
	r.woken()
	deliveriesBefore := (&trigRig{db: r.db}).deliveryCount(t)

	r.fail.Store(true) // a close also asks for a run; its enqueue fails after the upsert ran
	if got := r.deliver(t, "g-close", "update_pr_closed_merged.json"); got != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", got)
	}
	if got, _ := r.row(t, 21); got.State != "open" {
		t.Fatalf("row after the failed delivery = %+v, want the upsert rolled back", got)
	}
	if n := (&trigRig{db: r.db}).deliveryCount(t); n != deliveriesBefore {
		t.Fatalf("%d delivery records, want %d: the dedupe row must roll back too", n, deliveriesBefore)
	}
	if r.woken() {
		t.Fatal("a rolled-back delivery woke the engine")
	}

	r.fail.Store(false)
	r.must(t, "g-close", "update_pr_closed_merged.json") // the redelivery, same GUID
	if got, _ := r.row(t, 21); got.State != "merged" {
		t.Fatalf("row after the redelivery = %+v, want merged", got)
	}
	if !r.woken() {
		t.Fatal("the redelivery did not wake the engine")
	}
	r.must(t, "g-close", "update_pr_closed_merged.json")
	if r.woken() {
		t.Fatal("a second redelivery woke the engine")
	}
}

// The upsert itself failing, after it wrote its row, fails the delivery the same way: the row
// and the delivery record roll back, nothing wakes the engine, and the redelivery of the same
// GUID applies once instead of being deduplicated away.
func TestAFailedUpsertRollsBackTheDeliveryAndItsRedeliveryAppliesOnce(t *testing.T) {
	r := newPRRig(t, botUserID)
	deliveriesBefore := (&trigRig{db: r.db}).deliveryCount(t)

	r.failApply.Store(true)
	if got := r.deliver(t, "g-open", "update_pr_opened.json"); got != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", got)
	}
	if _, ok := r.row(t, 21); ok {
		t.Fatal("a row survived the failed upsert")
	}
	if n := (&trigRig{db: r.db}).deliveryCount(t); n != deliveriesBefore {
		t.Fatalf("%d delivery records, want %d: the dedupe row must roll back too", n, deliveriesBefore)
	}
	if r.woken() {
		t.Fatal("a failed upsert woke the engine")
	}

	r.failApply.Store(false)
	r.must(t, "g-open", "update_pr_opened.json") // the redelivery, same GUID
	if got, ok := r.row(t, 21); !ok || got.State != "open" {
		t.Fatalf("row after the redelivery = %+v (found %v), want open", got, ok)
	}
	if !r.woken() {
		t.Fatal("the redelivery did not wake the engine")
	}
	r.must(t, "g-open", "update_pr_opened.json")
	if r.rows(t) != 1 || r.woken() {
		t.Fatal("a second redelivery applied again")
	}
}

// The update PR handler composes with the Renovate trigger: one close delivery stores the PR
// and asks for a run.
func TestAClosedBotPRIsStoredAndStillStartsARun(t *testing.T) {
	r := newPRRig(t, botUserID)
	r.must(t, "g-1", "update_pr_opened.json")
	r.must(t, "g-2", "update_pr_closed_merged.json")
	var jobs int
	if err := r.db.Reader.QueryRow(`SELECT COUNT(*) FROM jobs WHERE "trigger" = 'pr_closed' AND priority = 'high'`).Scan(&jobs); err != nil || jobs != 1 {
		t.Fatalf("pr_closed jobs = %d, %v, want one", jobs, err)
	}
	if got, _ := r.row(t, 21); got.State != "merged" {
		t.Fatalf("row = %+v", got)
	}
}

func TestUpdatePRLogsCarryNoPRText(t *testing.T) {
	r := newPRRig(t, botUserID)
	r.must(t, "g-1", "update_pr_opened.json")
	r.must(t, "g-2", "update_pr_edited.json")
	logs := r.logs.String()
	for _, secretText := range []string{originalTitle, editedTitle, "renovate/lodash-4.x", "renovate-debug", "rebase-check"} {
		if strings.Contains(logs, secretText) {
			t.Errorf("the log holds PR text %q:\n%s", secretText, logs)
		}
	}
	if !strings.Contains(logs, "update PR stored") {
		t.Errorf("the log does not record the stored PR:\n%s", logs)
	}
}
