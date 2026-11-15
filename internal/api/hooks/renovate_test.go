package hooks_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/api/hooks"
	"github.com/mdg-labs/release-ops/internal/crypto"
	"github.com/mdg-labs/release-ops/internal/queue"
	"github.com/mdg-labs/release-ops/internal/store"
	"github.com/mdg-labs/release-ops/internal/store/storetest"
)

const (
	botUserID = 99887766
	botLogin  = "release-ops-acme[bot]"
)

var receivedAt = time.Date(2026, 11, 15, 9, 30, 0, 0, time.UTC)

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type trigRig struct {
	srv  *httptest.Server
	db   *store.DB
	st   *store.Store
	jobs *store.JobQueue
	logs *syncBuffer
}

type trigConfig struct {
	// botID is recorded as the bot user's id; zero leaves it unknown.
	botID int64
	// wrap replaces the enqueuer the handlers use.
	wrap func(r *trigRig, real hooks.RunEnqueuer) hooks.RunEnqueuer
	// bot replaces the source of the bot user the handlers use.
	bot func(real hooks.BotIdentities) hooks.BotIdentities
}

// flakyBot reads the bot user through the real store and fails the read while fail is set,
// as a database that errors transiently would.
type flakyBot struct {
	hooks.BotIdentities
	fail atomic.Bool
}

func (f *flakyBot) BotIdentity(ctx context.Context) (store.GitHubAppBot, error) {
	if f.fail.Load() {
		return store.GitHubAppBot{}, errors.New("database is locked")
	}
	return f.BotIdentities.BotIdentity(ctx)
}

func newTrigRig(t *testing.T, cfg trigConfig) *trigRig {
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
		t.Fatalf("store app: %v", err)
	}
	if cfg.botID != 0 {
		if _, err := st.GitHubApp().RecordBotUserID(context.Background(), cfg.botID); err != nil {
			t.Fatal(err)
		}
	}

	r := &trigRig{db: db, st: st, jobs: store.NewJobQueue(db, store.JobQueueOptions{}), logs: &syncBuffer{}}
	var enqueuer hooks.RunEnqueuer = r.jobs
	if cfg.wrap != nil {
		enqueuer = cfg.wrap(r, enqueuer)
	}
	var bots hooks.BotIdentities = st.GitHubApp()
	if cfg.bot != nil {
		bots = cfg.bot(bots)
	}
	routes := hooks.NewRoutes()
	hooks.RegisterInstallationHandlers(routes, st.InstallationSync(), nil)
	hooks.RegisterRenovateHandlers(routes, hooks.RenovateTriggers{
		Queue: enqueuer, Repositories: st, Bot: bots,
		Logger: slog.New(slog.NewTextHandler(r.logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	})
	r.srv = httptest.NewServer(&hooks.Receiver{
		Secrets: secrets{}, Deliveries: st.WebhookDeliveries(), Routes: routes,
		Now: func() time.Time { return receivedAt },
	})
	t.Cleanup(r.srv.Close)

	// The repositories come from the real installation sync, as they do in production.
	r.mustDeliver(t, "installation", "seed-installation", fixture(t, "installation_created.json"))
	return r
}

func (r *trigRig) post(t *testing.T, event, guid string, body []byte) int {
	t.Helper()
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	req, err := http.NewRequest(http.MethodPost, r.srv.URL, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	req.Header.Set("X-GitHub-Event", event)
	req.Header.Set("X-GitHub-Delivery", guid)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("deliver: %v", err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode
}

func (r *trigRig) mustDeliver(t *testing.T, event, guid string, body []byte) {
	t.Helper()
	if got := r.post(t, event, guid, body); got != http.StatusAccepted {
		t.Fatalf("%s %s = %d, want 202", event, guid, got)
	}
}

type jobRow struct {
	ID, Repository, Priority, State, Trigger, Markers, WebhookReceivedAt string
	Rerun                                                                int
}

func (r *trigRig) jobRows(t *testing.T) []jobRow {
	t.Helper()
	rows, err := r.db.Reader.Query(`
		SELECT j.id, repo.full_name, j.priority, j.state, j."trigger", COALESCE(j.matched_markers, ''), COALESCE(j.webhook_received_at, ''), j.rerun_requested
		FROM jobs j JOIN repositories repo ON repo.id = j.repository_id ORDER BY j.created_at, j.id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []jobRow
	for rows.Next() {
		var j jobRow
		if err := rows.Scan(&j.ID, &j.Repository, &j.Priority, &j.State, &j.Trigger, &j.Markers, &j.WebhookReceivedAt, &j.Rerun); err != nil {
			t.Fatal(err)
		}
		out = append(out, j)
	}
	return out
}

func (r *trigRig) deliveryCount(t *testing.T) int {
	t.Helper()
	var n int
	if err := r.db.Reader.QueryRow(`SELECT COUNT(*) FROM webhook_deliveries`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// mutate loads a fixture and lets f change its decoded JSON.
func mutate(t *testing.T, name string, f func(m map[string]any)) []byte {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(fixture(t, name), &m); err != nil {
		t.Fatal(err)
	}
	f(m)
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func obj(m map[string]any, keys ...string) map[string]any {
	for _, k := range keys {
		m = m[k].(map[string]any)
	}
	return m
}

func (r *trigRig) wantJobs(t *testing.T, n int) []jobRow {
	t.Helper()
	got := r.jobRows(t)
	if len(got) != n {
		t.Fatalf("jobs = %+v, want %d", got, n)
	}
	return got
}

// originalAndFollowUp tells the claimed job from its follow-up by id: both can share a
// millisecond, so their order in the rows says nothing about which came first.
func originalAndFollowUp(t *testing.T, jobs []jobRow, claimedID string) (original, followUp jobRow) {
	t.Helper()
	if len(jobs) != 2 {
		t.Fatalf("jobs = %+v, want the claimed job and one follow-up", jobs)
	}
	for _, j := range jobs {
		if j.ID == claimedID {
			original = j
		} else {
			followUp = j
		}
	}
	if original.ID == "" || followUp.ID == "" {
		t.Fatalf("jobs = %+v, want the claimed job %s and a follow-up with another id", jobs, claimedID)
	}
	return original, followUp
}

func TestDashboardTickEnqueuesAHighPriorityRunAndKeepsTheMarker(t *testing.T) {
	t.Parallel()
	r := newTrigRig(t, trigConfig{botID: botUserID})

	r.mustDeliver(t, "issues", "g-dash", fixture(t, "issues_edited_dashboard_tick.json"))

	j := r.wantJobs(t, 1)[0]
	if j.Repository != "acme/widgets" || j.Priority != "high" || j.State != "queued" || j.Trigger != "dashboard_checkbox" {
		t.Errorf("job = %+v, want a queued high-priority dashboard_checkbox job of acme/widgets", j)
	}
	if j.Markers != `["approve-branch=renovate/vite-7.x"]` {
		t.Errorf("markers = %s, want the ticked marker only", j.Markers)
	}
	if j.WebhookReceivedAt != "2026-11-15T09:30:00.000Z" {
		t.Errorf("webhook_received_at = %q, want the delivery's arrival", j.WebhookReceivedAt)
	}
}

func TestPullRequestRebaseTickEnqueuesAHighPriorityRun(t *testing.T) {
	t.Parallel()
	r := newTrigRig(t, trigConfig{botID: botUserID})

	r.mustDeliver(t, "pull_request", "g-pr", fixture(t, "pull_request_edited_rebase_tick.json"))

	j := r.wantJobs(t, 1)[0]
	if j.Priority != "high" || j.Trigger != "pr_checkbox" || j.Markers != `["rebase-check"]` {
		t.Errorf("job = %+v, want a high-priority pr_checkbox job for rebase-check", j)
	}
}

func TestATickOnAnyMarkerEnqueues(t *testing.T) {
	t.Parallel()

	for _, marker := range []string{
		"rebase-check", "approve-branch=renovate/major-react", "unschedule-branch=renovate/lodash-4.x",
		"rebase-all-open-prs", "approve-all-pending-prs", "create-all-rate-limited-prs",
		"create-config-migration-pr", "some-future-checkbox=1",
	} {
		t.Run(marker, func(t *testing.T) {
			t.Parallel()
			r := newTrigRig(t, trigConfig{botID: botUserID})
			from := "intro\n\n - [ ] <!-- " + marker + " -->do the thing\n - [ ] <!-- other -->untouched\n"
			to := strings.Replace(from, " - [ ] <!-- "+marker, " - [x] <!-- "+marker, 1)
			r.mustDeliver(t, "issues", "g-1", mutate(t, "issues_edited_dashboard_tick.json", func(m map[string]any) {
				obj(m, "issue")["body"] = to
				obj(m, "changes", "body")["from"] = from
			}))

			j := r.wantJobs(t, 1)[0]
			if j.Priority != "high" || j.Markers != fmt.Sprintf("[%q]", marker) {
				t.Errorf("job = %+v, want a high-priority job carrying %q", j, marker)
			}
		})
	}
}

func TestTickDetectionIsByMarkerTextNotByLine(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		from, to string
		want     string
	}{
		{"reordered lines", " - [ ] <!-- a -->A\n - [ ] <!-- b -->B\n", " - [x] <!-- b -->B\n - [ ] <!-- a -->A\n", `["b"]`},
		{"capital X", " - [ ] <!-- a -->A\n", " - [X] <!-- a -->A\n", `["a"]`},
		{"CRLF line ends", " - [ ] <!-- a -->A\r\n - [ ] <!-- b -->B\r\n", " - [ ] <!-- a -->A\r\n - [x] <!-- b -->B\r\n", `["b"]`},
		{"several ticks", " - [ ] <!-- a -->A\n - [ ] <!-- b -->B\n", " - [x] <!-- a -->A\n - [x] <!-- b -->B\n", `["a","b"]`},
		{"a new box already ticked is no transition", " - [ ] <!-- a -->A\n", " - [ ] <!-- a -->A\n - [x] <!-- b -->B\n", ""},
		{"an already ticked box stays ticked", " - [x] <!-- a -->A\n", " - [x] <!-- a -->A\ntext\n", ""},
		{"renovate unticks after it acted", " - [x] <!-- a -->A\n", " - [ ] <!-- a -->A\n", ""},
		{"a text edit", "Hello\n - [ ] <!-- a -->A\n", "Hello world\n - [ ] <!-- a -->A\n", ""},
		{"a plain checkbox with no marker", " - [ ] buy milk\n", " - [x] buy milk\n", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := newTrigRig(t, trigConfig{botID: botUserID})
			r.mustDeliver(t, "issues", "g-1", mutate(t, "issues_edited_dashboard_tick.json", func(m map[string]any) {
				obj(m, "issue")["body"] = tc.to
				obj(m, "changes", "body")["from"] = tc.from
			}))
			jobs := r.jobRows(t)
			if tc.want == "" {
				if len(jobs) != 0 {
					t.Fatalf("jobs = %+v, want none", jobs)
				}
				return
			}
			if len(jobs) != 1 || jobs[0].Markers != tc.want {
				t.Fatalf("jobs = %+v, want one with markers %s", jobs, tc.want)
			}
		})
	}
}

func TestEditsThatMustNotStartARun(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		event string
		file  string
		edit  func(m map[string]any)
	}{
		{"the dashboard edit of the App's own bot (Renovate rewriting its body)", "issues", "issues_edited_dashboard_tick.json", func(m map[string]any) {
			m["sender"] = map[string]any{"login": botLogin, "id": botUserID, "type": "Bot"}
		}},
		{"the PR edit of the App's own bot", "pull_request", "pull_request_edited_rebase_tick.json", func(m map[string]any) {
			m["sender"] = map[string]any{"login": botLogin, "id": botUserID, "type": "Bot"}
		}},
		{"a title edit without changes.body", "issues", "issues_edited_dashboard_tick.json", func(m map[string]any) {
			m["changes"] = map[string]any{"title": map[string]any{"from": "Old title"}}
		}},
		{"an issue authored by a person with Renovate-looking checkboxes", "issues", "issues_edited_dashboard_tick.json", func(m map[string]any) {
			obj(m, "issue")["user"] = map[string]any{"login": "octocat", "id": 1, "type": "User"}
		}},
		{"a PR authored by another bot", "pull_request", "pull_request_edited_rebase_tick.json", func(m map[string]any) {
			obj(m, "pull_request")["user"] = map[string]any{"login": "dependabot[bot]", "id": 49699333, "type": "Bot"}
		}},
		{"an author with the bot's login but another id", "issues", "issues_edited_dashboard_tick.json", func(m map[string]any) {
			obj(m, "issue")["user"] = map[string]any{"login": botLogin, "id": botUserID + 1, "type": "Bot"}
		}},
		{"an author without an id", "issues", "issues_edited_dashboard_tick.json", func(m map[string]any) {
			obj(m, "issue")["user"] = map[string]any{"login": botLogin}
		}},
		{"an issue that was only opened", "issues", "issues_edited_dashboard_tick.json", func(m map[string]any) { m["action"] = "opened" }},
		{"a PR that was only synchronized", "pull_request", "pull_request_edited_rebase_tick.json", func(m map[string]any) { m["action"] = "synchronize" }},
		{"a PR closed by a person that the bot did not author", "pull_request", "pull_request_closed_merged.json", func(m map[string]any) {
			obj(m, "pull_request")["user"] = map[string]any{"login": "octocat", "id": 1, "type": "User"}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := newTrigRig(t, trigConfig{botID: botUserID})
			r.mustDeliver(t, tc.event, "g-1", mutate(t, tc.file, tc.edit))
			if jobs := r.jobRows(t); len(jobs) != 0 {
				t.Fatalf("jobs = %+v, want none", jobs)
			}
			if got := r.deliveryCount(t); got != 2 {
				t.Errorf("deliveries = %d, want the seed and this one recorded", got)
			}
		})
	}
}

func TestBotObjectsAreIdentifiedByAuthorNotByBranchPrefixOrTitle(t *testing.T) {
	t.Parallel()
	r := newTrigRig(t, trigConfig{botID: botUserID})

	r.mustDeliver(t, "pull_request", "g-pr", mutate(t, "pull_request_edited_rebase_tick.json", func(m map[string]any) {
		pr := obj(m, "pull_request")
		pr["title"] = "A title nobody would call Renovate's"
		obj(pr, "head")["ref"] = "bots/deps/lodash"
	}))
	r.wantJobs(t, 1)

	r2 := newTrigRig(t, trigConfig{botID: botUserID})
	r2.mustDeliver(t, "issues", "g-dash", mutate(t, "issues_edited_dashboard_tick.json", func(m map[string]any) {
		obj(m, "issue")["title"] = "Update board"
	}))
	r2.wantJobs(t, 1)
}

func TestNothingCountsAsTheBotWhileItsIdIsUnknown(t *testing.T) {
	t.Parallel()
	r := newTrigRig(t, trigConfig{})

	r.mustDeliver(t, "issues", "g-1", fixture(t, "issues_edited_dashboard_tick.json"))
	r.mustDeliver(t, "pull_request", "g-2", fixture(t, "pull_request_edited_rebase_tick.json"))
	r.mustDeliver(t, "pull_request", "g-3", fixture(t, "pull_request_closed_merged.json"))
	r.wantJobs(t, 0)

	// A sender with id 0 is not "the bot" either: the unknown id never matches everyone.
	r.mustDeliver(t, "issues", "g-4", mutate(t, "issues_edited_dashboard_tick.json", func(m map[string]any) {
		obj(m, "issue")["user"] = map[string]any{"login": "x"}
		m["sender"] = map[string]any{"login": "y"}
	}))
	r.wantJobs(t, 0)

	// Events that name no author still start a run.
	r.mustDeliver(t, "push", "g-5", fixture(t, "push_default_branch.json"))
	r.wantJobs(t, 1)
}

func TestAFailedBotLookupFailsTheDeliveryAndItsRedeliveryIsProcessed(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name, event, file string
		trigger           string
	}{
		{"dashboard checkbox", "issues", "issues_edited_dashboard_tick.json", "dashboard_checkbox"},
		{"pull request checkbox", "pull_request", "pull_request_edited_rebase_tick.json", "pr_checkbox"},
		{"pull request close", "pull_request", "pull_request_closed_merged.json", "pr_closed"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			var bot *flakyBot
			r := newTrigRig(t, trigConfig{botID: botUserID, bot: func(real hooks.BotIdentities) hooks.BotIdentities {
				bot = &flakyBot{BotIdentities: real}
				return bot
			}})
			body := fixture(t, c.file)

			bot.fail.Store(true)
			if got := r.post(t, c.event, "g-retry", body); got != http.StatusInternalServerError {
				t.Fatalf("status = %d, want 500 so GitHub's redelivery is processed", got)
			}
			r.wantJobs(t, 0)
			if got := r.deliveryCount(t); got != 1 {
				t.Fatalf("deliveries = %d, want only the seed: a failed bot lookup leaves no dedupe row", got)
			}

			bot.fail.Store(false)
			r.mustDeliver(t, c.event, "g-retry", body)
			if j := r.wantJobs(t, 1)[0]; j.Trigger != c.trigger {
				t.Errorf("job = %+v, want a %s job", j, c.trigger)
			}
		})
	}
}

func TestClosedPullRequestEnqueuesWhetherMergedOrNotAndEvenFromTheBot(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		edit func(m map[string]any)
	}{
		{"merged", func(map[string]any) {}},
		{"closed unmerged", func(m map[string]any) { obj(m, "pull_request")["merged"] = false }},
		{"merged by the bot itself", func(m map[string]any) {
			m["sender"] = map[string]any{"login": botLogin, "id": botUserID, "type": "Bot"}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := newTrigRig(t, trigConfig{botID: botUserID})
			r.mustDeliver(t, "pull_request", "g-1", mutate(t, "pull_request_closed_merged.json", tc.edit))
			j := r.wantJobs(t, 1)[0]
			if j.Priority != "high" || j.Trigger != "pr_closed" || j.Markers != "" {
				t.Errorf("job = %+v, want a high-priority pr_closed job without markers", j)
			}
		})
	}
}

func TestPushEnqueuesOnlyForTheDefaultBranchAndNotForItsDeletion(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		edit func(m map[string]any)
		want int
	}{
		{"default branch", func(map[string]any) {}, 1},
		{"another branch", func(m map[string]any) { m["ref"] = "refs/heads/feature" }, 0},
		{"a branch that merely starts with the default's name", func(m map[string]any) { m["ref"] = "refs/heads/main-2" }, 0},
		{"a tag", func(m map[string]any) { m["ref"] = "refs/tags/main" }, 0},
		{"deletion of the default branch", func(m map[string]any) {
			m["deleted"] = true
			m["after"] = strings.Repeat("0", 40)
		}, 0},
		{"deletion marked only by the zero id", func(m map[string]any) { m["after"] = strings.Repeat("0", 40) }, 0},
		{"the repository's default branch is not main", func(m map[string]any) {
			obj(m, "repository")["default_branch"] = "trunk"
			m["ref"] = "refs/heads/trunk"
		}, 1},
		{"main after the default branch moved to trunk", func(m map[string]any) { obj(m, "repository")["default_branch"] = "trunk" }, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := newTrigRig(t, trigConfig{botID: botUserID})
			r.mustDeliver(t, "push", "g-1", mutate(t, "push_default_branch.json", tc.edit))
			jobs := r.jobRows(t)
			if len(jobs) != tc.want {
				t.Fatalf("jobs = %+v, want %d", jobs, tc.want)
			}
			if tc.want == 1 && (jobs[0].Trigger != "push" || jobs[0].Priority != "high") {
				t.Errorf("job = %+v, want a high-priority push job", jobs[0])
			}
		})
	}
}

func TestRepositoriesAddedGetOneJobEachAfterTheSyncInTheSameTransaction(t *testing.T) {
	t.Parallel()
	r := newTrigRig(t, trigConfig{botID: botUserID})

	body := mutate(t, "installation_repositories_added.json", func(m map[string]any) {
		m["repositories_added"] = []any{
			map[string]any{"id": 7002, "full_name": "acme/gadgets"},
			map[string]any{"id": 7003, "full_name": "acme/gizmos"},
			map[string]any{"id": 7002, "full_name": "acme/gadgets"},
		}
	})
	r.mustDeliver(t, "installation_repositories", "g-1", body)

	jobs := r.wantJobs(t, 2)
	got := map[string]jobRow{}
	for _, j := range jobs {
		got[j.Repository] = j
	}
	for _, name := range []string{"acme/gadgets", "acme/gizmos"} {
		if j, ok := got[name]; !ok || j.Priority != "high" || j.Trigger != "repository_added" {
			t.Errorf("job of %s = %+v (found %v), want a high-priority repository_added job", name, j, ok)
		}
	}
}

func TestRepositoriesRemovedStartNothing(t *testing.T) {
	t.Parallel()
	r := newTrigRig(t, trigConfig{botID: botUserID})
	r.mustDeliver(t, "installation_repositories", "g-1", fixture(t, "installation_repositories_added.json"))
	r.wantJobs(t, 1)
	r.mustDeliver(t, "installation_repositories", "g-2", fixture(t, "installation_repositories_removed.json"))
	r.wantJobs(t, 1)
}

func TestEventsOfUntrackedPausedOrRemovedRepositoriesAreAcceptedAndDropped(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		prep func(t *testing.T, r *trigRig)
		edit func(m map[string]any)
	}{
		{"not tracked", func(*testing.T, *trigRig) {}, func(m map[string]any) { obj(m, "repository")["id"] = 424242 }},
		{"paused", func(t *testing.T, r *trigRig) {
			if _, err := r.db.Writer.Exec(`UPDATE repositories SET enabled = 0`); err != nil {
				t.Fatal(err)
			}
		}, func(map[string]any) {}},
		{"removed", func(t *testing.T, r *trigRig) {
			if _, err := r.db.Writer.Exec(`UPDATE repositories SET removed_at = '2026-11-15T08:00:00.000Z'`); err != nil {
				t.Fatal(err)
			}
		}, func(map[string]any) {}},
	}
	for _, tc := range cases {
		for _, ev := range []struct{ event, file string }{
			{"issues", "issues_edited_dashboard_tick.json"},
			{"pull_request", "pull_request_edited_rebase_tick.json"},
			{"pull_request", "pull_request_closed_merged.json"},
			{"push", "push_default_branch.json"},
		} {
			t.Run(tc.name+"/"+ev.file, func(t *testing.T) {
				t.Parallel()
				r := newTrigRig(t, trigConfig{botID: botUserID})
				tc.prep(t, r)
				r.mustDeliver(t, ev.event, "g-1", mutate(t, ev.file, tc.edit))
				r.wantJobs(t, 0)
				if got := r.deliveryCount(t); got != 2 {
					t.Errorf("deliveries = %d, want this one recorded as seen", got)
				}
			})
		}
	}

	t.Run("a repository added while paused", func(t *testing.T) {
		t.Parallel()
		r := newTrigRig(t, trigConfig{botID: botUserID})
		r.mustDeliver(t, "installation_repositories", "g-0", fixture(t, "installation_repositories_added.json"))
		if _, err := r.db.Writer.Exec(`UPDATE jobs SET state = 'succeeded'; UPDATE repositories SET enabled = 0 WHERE github_repository_id = 7002`); err != nil {
			t.Fatal(err)
		}
		r.mustDeliver(t, "installation_repositories", "g-1", fixture(t, "installation_repositories_added.json"))
		if got := r.jobRows(t); len(got) != 1 {
			t.Errorf("jobs = %+v, want only the first delivery's", got)
		}
	})
}

func TestATickDuringARunningJobGivesExactlyOneFollowUp(t *testing.T) {
	t.Parallel()
	r := newTrigRig(t, trigConfig{botID: botUserID})
	ctx := context.Background()

	r.mustDeliver(t, "push", "g-push", fixture(t, "push_default_branch.json"))
	claimed, err := r.jobs.Claim(ctx)
	if err != nil {
		t.Fatal(err)
	}

	r.mustDeliver(t, "issues", "g-tick-1", fixture(t, "issues_edited_dashboard_tick.json"))
	r.mustDeliver(t, "pull_request", "g-tick-2", fixture(t, "pull_request_edited_rebase_tick.json"))
	r.mustDeliver(t, "issues", "g-tick-3", mutate(t, "issues_edited_dashboard_tick.json", func(m map[string]any) {
		obj(m, "issue")["body"] = " - [x] <!-- rebase-all-open-prs -->all\n"
		obj(m, "changes", "body")["from"] = " - [ ] <!-- rebase-all-open-prs -->all\n"
	}))

	jobs := r.wantJobs(t, 1)
	if jobs[0].State != "running" || jobs[0].Rerun != 1 {
		t.Fatalf("jobs = %+v, want the running job flagged for one rerun and nothing else queued", jobs)
	}

	if err := r.jobs.Complete(ctx, claimed.ID, claimed.LeaseOwner); err != nil {
		t.Fatal(err)
	}
	jobs = r.wantJobs(t, 2)
	finished, followUp := originalAndFollowUp(t, jobs, claimed.ID)
	if finished.State != "succeeded" || followUp.State != "queued" || followUp.Priority != "high" || followUp.Rerun != 0 {
		t.Fatalf("jobs = %+v, want the finished job and exactly one queued high-priority follow-up", jobs)
	}

	next, err := r.jobs.Claim(ctx)
	if err != nil || next.ID != followUp.ID {
		t.Fatalf("Claim = %+v, %v, want the follow-up", next, err)
	}
	if err := r.jobs.Complete(ctx, next.ID, next.LeaseOwner); err != nil {
		t.Fatal(err)
	}
	r.wantJobs(t, 2)
}

func TestTicksWhileAJobIsQueuedCoalesceIntoItAndKeepEveryMarker(t *testing.T) {
	t.Parallel()
	r := newTrigRig(t, trigConfig{botID: botUserID})

	r.mustDeliver(t, "issues", "g-1", fixture(t, "issues_edited_dashboard_tick.json"))
	r.mustDeliver(t, "pull_request", "g-2", fixture(t, "pull_request_edited_rebase_tick.json"))

	j := r.wantJobs(t, 1)[0]
	if j.Rerun != 0 || j.Markers != `["approve-branch=renovate/vite-7.x","rebase-check"]` {
		t.Errorf("job = %+v, want one queued job with both markers and no rerun flag", j)
	}
}

func TestADuplicateDeliveryGivesNoSecondJob(t *testing.T) {
	t.Parallel()
	r := newTrigRig(t, trigConfig{botID: botUserID})
	ctx := context.Background()

	body := fixture(t, "issues_edited_dashboard_tick.json")
	r.mustDeliver(t, "issues", "g-same", body)
	job, err := r.jobs.Claim(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.jobs.Complete(ctx, job.ID, job.LeaseOwner); err != nil {
		t.Fatal(err)
	}

	// The job is gone from the queue, so only the dedupe record can stop a second one.
	r.mustDeliver(t, "issues", "g-same", body)
	r.wantJobs(t, 1)
	if got := r.deliveryCount(t); got != 2 {
		t.Errorf("deliveries = %d, want the seed and one record of g-same", got)
	}
}

type failingOnce struct {
	hooks.RunEnqueuer
	mu     sync.Mutex
	failed bool
}

// EnqueueTx records the job and then fails the first time, as a failure after the write.
func (f *failingOnce) EnqueueTx(ctx context.Context, tx *sql.Tx, p queue.EnqueueParams) (queue.EnqueueResult, func(), error) {
	res, wake, err := f.RunEnqueuer.EnqueueTx(ctx, tx, p)
	f.mu.Lock()
	defer f.mu.Unlock()
	if err == nil && !f.failed {
		f.failed = true
		return queue.EnqueueResult{}, nil, errors.New("enqueue failed after its write")
	}
	return res, wake, err
}

func TestAFailedEnqueueRollsBackTheDedupeRowAndARedeliveryEnqueuesOnce(t *testing.T) {
	t.Parallel()
	r := newTrigRig(t, trigConfig{botID: botUserID, wrap: func(_ *trigRig, real hooks.RunEnqueuer) hooks.RunEnqueuer {
		return &failingOnce{RunEnqueuer: real}
	}})
	body := fixture(t, "issues_edited_dashboard_tick.json")

	if got := r.post(t, "issues", "g-retry", body); got != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 so GitHub's redelivery is processed", got)
	}
	r.wantJobs(t, 0)
	if got := r.deliveryCount(t); got != 1 {
		t.Fatalf("deliveries = %d, want only the seed: a failed enqueue leaves no dedupe row", got)
	}

	r.mustDeliver(t, "issues", "g-retry", body)
	r.wantJobs(t, 1)
	r.mustDeliver(t, "issues", "g-retry", body)
	r.wantJobs(t, 1)
	if got := r.deliveryCount(t); got != 2 {
		t.Errorf("deliveries = %d, want the seed and g-retry", got)
	}
}

type wakeProbe struct {
	hooks.RunEnqueuer
	r *trigRig
	// seen holds the number of committed jobs each wake-up saw.
	mu   sync.Mutex
	seen []int
}

func (w *wakeProbe) EnqueueTx(ctx context.Context, tx *sql.Tx, p queue.EnqueueParams) (queue.EnqueueResult, func(), error) {
	res, wake, err := w.RunEnqueuer.EnqueueTx(ctx, tx, p)
	if err != nil {
		return res, wake, err
	}
	return res, func() {
		// The read pool sees committed rows only, as a woken claim would.
		var n int
		if err := w.r.db.Reader.QueryRow(`SELECT COUNT(*) FROM jobs`).Scan(&n); err != nil {
			n = -1
		}
		w.mu.Lock()
		w.seen = append(w.seen, n)
		w.mu.Unlock()
		wake()
	}, nil
}

func TestTheWorkerIsWokenOnlyAfterTheDeliveryCommitted(t *testing.T) {
	t.Parallel()
	var probe *wakeProbe
	r := newTrigRig(t, trigConfig{botID: botUserID, wrap: func(r *trigRig, real hooks.RunEnqueuer) hooks.RunEnqueuer {
		probe = &wakeProbe{RunEnqueuer: real, r: r}
		return probe
	}})

	r.mustDeliver(t, "issues", "g-1", fixture(t, "issues_edited_dashboard_tick.json"))

	probe.mu.Lock()
	defer probe.mu.Unlock()
	if len(probe.seen) != 1 || probe.seen[0] != 1 {
		t.Errorf("wake-ups saw %v committed jobs, want one wake-up that already finds the job", probe.seen)
	}
}

func TestNoWakeUpWhenTheDeliveryRollsBack(t *testing.T) {
	t.Parallel()
	var probe *wakeProbe
	r := newTrigRig(t, trigConfig{botID: botUserID, wrap: func(r *trigRig, real hooks.RunEnqueuer) hooks.RunEnqueuer {
		probe = &wakeProbe{RunEnqueuer: &failingOnce{RunEnqueuer: real}, r: r}
		return probe
	}})

	if got := r.post(t, "issues", "g-1", fixture(t, "issues_edited_dashboard_tick.json")); got != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", got)
	}
	probe.mu.Lock()
	defer probe.mu.Unlock()
	if len(probe.seen) != 0 {
		t.Errorf("wake-ups = %v, want none for a rolled-back delivery", probe.seen)
	}
}

func TestAPayloadOfAnUnexpectedShapeIsDroppedNotFailed(t *testing.T) {
	t.Parallel()
	r := newTrigRig(t, trigConfig{botID: botUserID})

	r.mustDeliver(t, "issues", "g-1", mutate(t, "issues_edited_dashboard_tick.json", func(m map[string]any) {
		obj(m, "issue")["user"] = map[string]any{"id": "not a number"}
	}))
	r.wantJobs(t, 0)
	if got := r.deliveryCount(t); got != 2 {
		t.Errorf("deliveries = %d, want it recorded so a redelivery does not repeat it forever", got)
	}
}

func TestNoPayloadTextOrTokenIsLogged(t *testing.T) {
	t.Parallel()
	r := newTrigRig(t, trigConfig{botID: botUserID})

	const token = "ghs_FIXTURE0000000000000000000000000000"
	const prose = "private-prose-of-the-dashboard"
	body := mutate(t, "issues_edited_dashboard_tick.json", func(m map[string]any) {
		from := "intro " + prose + " " + token + "\n - [ ] <!-- approve-branch=renovate/secret-branch -->x\n"
		obj(m, "issue")["body"] = strings.Replace(from, "[ ]", "[x]", 1)
		obj(m, "changes", "body")["from"] = from
	})
	r.mustDeliver(t, "issues", "g-1", body)
	r.mustDeliver(t, "issues", "g-2", mutate(t, "issues_edited_dashboard_tick.json", func(m map[string]any) {
		obj(m, "issue")["user"] = map[string]any{"id": prose + token}
	}))
	r.wantJobs(t, 1)

	logs := r.logs.String()
	if !strings.Contains(logs, "run requested") {
		t.Fatalf("logs = %q, want the enqueue logged", logs)
	}
	for _, leaked := range []string{token, prose, "secret-branch", "approve-branch", "Dependency Dashboard", "octocat"} {
		if strings.Contains(logs, leaked) {
			t.Errorf("logs contain payload text %q:\n%s", leaked, logs)
		}
	}
}
