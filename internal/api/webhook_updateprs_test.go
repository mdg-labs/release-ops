package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/api/hooks"
	"github.com/mdg-labs/release-ops/internal/providers/ticket"
	"github.com/mdg-labs/release-ops/internal/store"
	"github.com/mdg-labs/release-ops/internal/updateprs"
	"github.com/mdg-labs/release-ops/internal/updatetickets"
)

// newUpdatePRFlow serves the router with the update PR tracking wired the way the server wires it.
func newUpdatePRFlow(t *testing.T, track bool, withJobs bool) (*webhookFlow, *updateprs.Notifier) {
	t.Helper()

	deps := newRoutesTestDeps(t)
	deps.WebhookURL = "https://release-ops.example.com/hooks/github"
	routes := hooks.NewRoutes()
	hooks.RegisterInstallationHandlers(routes, deps.Store.InstallationSync(), nil)
	deps.WebhookRoutes = routes
	if withJobs {
		deps.Queue = store.NewJobQueue(deps.Store.DB(), store.JobQueueOptions{})
	}
	wake := updateprs.NewNotifier()
	if track {
		deps.UpdatePRs = updateprs.NewTracker(deps.Store.UpdatePRs(), nil, wake)
	}
	if err := deps.Store.GitHubApp().Create(context.Background(), store.NewGitHubApp{
		AppID: 1234567, Slug: "release-ops-acme", ClientID: "cid", HTMLURL: "https://github.com/apps/release-ops-acme",
		OwnerLogin: "acme", OwnerType: "Organization", PrivateKey: "pem", ClientSecret: "client-secret-value", WebhookSecret: webhookTestSecret,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := deps.Store.GitHubApp().RecordBotUserID(context.Background(), renovateBotUserID); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(NewServerRouter(deps))
	t.Cleanup(srv.Close)
	f := &webhookFlow{srv: srv, deps: deps}
	if status, _ := f.post(t, signedHook("installation", "seed", fixture(t, "installation_created.json"))); status != http.StatusAccepted {
		t.Fatalf("seed installation = %d, want 202", status)
	}
	return f, wake
}

func (f *webhookFlow) updatePRStates(t *testing.T) []string {
	t.Helper()
	rows, err := f.deps.DB.Query(`SELECT p.number || ':' || p.state || ':' || r.full_name FROM update_prs p JOIN repositories r ON r.id = p.repository_id ORDER BY p.number`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}

// POST /hooks/github stores a bot PR through the public route, whether or not the queue that
// starts runs is wired, and drops a person's PR.
func TestWebhookPullRequestsReachUpdatePRsThroughThePublicRoute(t *testing.T) {
	t.Parallel()
	for _, withJobs := range []bool{true, false} {
		f, wake := newUpdatePRFlow(t, true, withJobs)
		for _, file := range []string{"update_pr_opened.json", "update_pr_by_person.json", "update_pr_closed_merged.json"} {
			if status, _ := f.post(t, signedHook("pull_request", file, fixture(t, file))); status != http.StatusAccepted {
				t.Fatalf("jobs %v, %s = %d, want 202", withJobs, file, status)
			}
		}
		if got := f.updatePRStates(t); len(got) != 1 || got[0] != "21:merged:acme/widgets" {
			t.Fatalf("jobs %v: update PRs = %v, want [21:merged:acme/widgets]", withJobs, got)
		}
		select {
		case <-wake.C():
		default:
			t.Fatalf("jobs %v: no wake after a stored PR", withJobs)
		}
	}
}

func TestWebhookPullRequestsStoreNothingWithoutTheTracker(t *testing.T) {
	t.Parallel()
	f, _ := newUpdatePRFlow(t, false, true)
	if status, _ := f.post(t, signedHook("pull_request", "g", fixture(t, "update_pr_opened.json"))); status != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", status)
	}
	if got := f.updatePRStates(t); len(got) != 0 {
		t.Fatalf("update PRs = %v, want none", got)
	}
}

// POST /hooks/github records the dependencies and release notes of the bot PR, and none for the
// config-migration and onboarding PRs.
func TestWebhookPullRequestsRecordDependenciesAndReleaseNotesThroughThePublicRoute(t *testing.T) {
	t.Parallel()
	f, _ := newUpdatePRFlow(t, true, true)
	for _, file := range []string{"update_pr_opened.json", "update_pr_edited_release_notes.json", "update_pr_config_migration.json", "update_pr_onboarding.json"} {
		if status, _ := f.post(t, signedHook("pull_request", file, fixture(t, file))); status != http.StatusAccepted {
			t.Fatalf("%s = %d, want 202", file, status)
		}
	}
	rows, err := f.deps.DB.Query(`SELECT p.number, COUNT(d.id), COALESCE(MIN(d.name), ''), COALESCE(MIN(d.source), ''), COALESCE(p.release_notes, '') != ''
		FROM update_prs p LEFT JOIN update_pr_dependencies d ON d.update_pr_id = p.id GROUP BY p.id ORDER BY p.number`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var got []string
	for rows.Next() {
		var number, count int
		var name, source string
		var notes bool
		if err := rows.Scan(&number, &count, &name, &source, &notes); err != nil {
			t.Fatal(err)
		}
		got = append(got, fmt.Sprintf("%d:%d:%s:%s:%v", number, count, name, source, notes))
	}
	want := []string{"21:1:lodash:body:true", "30:0:::false", "31:0:::false"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("update PRs = %v, want %v", got, want)
	}
}

// fakeKaneo keeps the tasks created in it and records the writes made to them.
type fakeKaneo struct {
	srv *httptest.Server
	mu  sync.Mutex
	// status and comments by task id; writes are "METHOD path" of every request that changes a task.
	status   map[string]string
	title    map[string]string
	comments map[string][]string
	writes   []string
}

func newFakeKaneo(t *testing.T) *fakeKaneo {
	t.Helper()
	k := &fakeKaneo{status: map[string]string{}, title: map[string]string{}, comments: map[string][]string{}}
	k.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body struct{ Title, Status, Content string }
		_ = json.Unmarshal(raw, &body)
		parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api"), "/"), "/")
		k.mu.Lock()
		defer k.mu.Unlock()
		resp := map[string]string{}
		switch {
		case r.Method == http.MethodPost && len(parts) == 2 && parts[0] == "task":
			id := fmt.Sprintf("task-%d", len(k.status)+1)
			k.status[id], k.title[id] = body.Status, body.Title
			resp = map[string]string{"id": id, "status": body.Status}
		case r.Method == http.MethodGet && len(parts) == 2 && parts[0] == "task":
			resp = map[string]string{"id": parts[1], "status": k.status[parts[1]]}
		case r.Method == http.MethodPut && len(parts) == 3 && parts[1] == "status":
			k.status[parts[2]] = body.Status
		case r.Method == http.MethodPut && len(parts) == 3 && parts[1] == "title":
			k.title[parts[2]] = body.Title
		case r.Method == http.MethodPost && len(parts) == 2 && parts[0] == "comment":
			k.comments[parts[1]] = append(k.comments[parts[1]], body.Content)
		}
		if r.Method != http.MethodGet {
			k.writes = append(k.writes, r.Method+" "+r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(k.srv.Close)
	return k
}

func (k *fakeKaneo) read(f func()) {
	k.mu.Lock()
	defer k.mu.Unlock()
	f()
}

type kaneoTargets struct{ k *fakeKaneo }

func (g kaneoTargets) Target(context.Context, string) (updatetickets.Target, error) {
	p, err := ticket.NewKaneoProvider(g.k.srv.URL, "k", g.k.srv.Client())
	return updatetickets.Target{Provider: p, Kind: ticket.IntegrationKindKaneo}, err
}

// newTicketFlow serves the update PR flow with the ticket engine wired as the server wires it:
// the tracker publishes its events to the engine, which creates and syncs tickets in a fake Kaneo.
func newTicketFlow(t *testing.T) (*webhookFlow, *fakeKaneo) {
	t.Helper()
	f, wake := newUpdatePRFlow(t, true, true)
	k := newFakeKaneo(t)
	ctx := context.Background()
	base := k.srv.URL
	integ, err := f.deps.Store.Integrations().Create(ctx, store.CreateIntegrationInput{Kind: "kaneo", Name: "Kaneo", BaseURL: &base, Secret: []byte(`{"api_key":"k"}`)})
	if err != nil {
		t.Fatal(err)
	}
	tp, err := f.deps.Store.TicketProjects().Create(ctx, store.CreateTicketProjectInput{
		IntegrationID: integ.ID, ExternalProjectID: "DEV", Name: "DEV",
		CreateConfig:  `{"workspaceId":"ws-1","status":"ready","priority":"medium"}`,
		StatusMapping: `{"open":["ready"],"done":["done"],"cancelled":["cancelled"],"superseded":"cancelled"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	var installationID string
	if err := f.deps.DB.QueryRow(`SELECT id FROM installations`).Scan(&installationID); err != nil {
		t.Fatal(err)
	}
	if err := f.deps.Store.InstallationSettings().SetDefaultTicketProject(ctx, installationID, tp.ID); err != nil {
		t.Fatal(err)
	}

	engine := &updatetickets.Engine{
		Tickets: f.deps.Store.UpdateTickets(), Projects: f.deps.Store.TicketProjects(), Targets: kaneoTargets{k},
		Wake: wake, SweepInterval: time.Hour,
	}
	f.deps.UpdatePRs.(*updateprs.Tracker).Subscribe(engine.Handle)
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { engine.Run(runCtx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	return f, k
}

func (f *webhookFlow) hook(t *testing.T, guid string, body []byte) {
	t.Helper()
	if status, _ := f.post(t, signedHook("pull_request", guid, body)); status != http.StatusAccepted {
		t.Fatalf("pull_request %s = %d, want 202", guid, status)
	}
}

func (f *webhookFlow) ticketState(t *testing.T) string {
	t.Helper()
	var state string
	if err := f.deps.DB.QueryRow(`SELECT COALESCE((SELECT state FROM update_tickets), '')`).Scan(&state); err != nil {
		t.Fatal(err)
	}
	return state
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// pull_request deliveries through POST /hooks/github create the ticket, update it when the PR
// gets a new version, and move it to done when the PR is merged.
func TestWebhookPullRequestsKeepTheTicketInStepWithThePRThroughThePublicRoute(t *testing.T) {
	t.Parallel()
	f, k := newTicketFlow(t)

	f.hook(t, "opened", fixture(t, "update_pr_opened.json"))
	waitFor(t, "the ticket", func() bool { return f.ticketState(t) == "open" })

	f.hook(t, "synchronize", bytes.ReplaceAll(fixture(t, "update_pr_synchronize.json"), []byte("4.17.21"), []byte("4.17.22")))
	waitFor(t, "the update note", func() bool {
		var n int
		k.read(func() { n = len(k.comments["task-1"]) })
		return n == 1
	})
	k.read(func() {
		if !strings.Contains(k.comments["task-1"][0], "lodash: 4.17.20 → 4.17.22") {
			t.Errorf("note = %q, want the new version", k.comments["task-1"][0])
		}
	})

	f.hook(t, "merged", fixture(t, "update_pr_closed_merged.json"))
	waitFor(t, "the ticket to be done", func() bool { return f.ticketState(t) == "done" })
	k.read(func() {
		if k.status["task-1"] != "done" {
			t.Errorf("task status = %q, want done", k.status["task-1"])
		}
	})
}

// A PR closed unmerged moves its ticket to won't do, and reopening the PR leaves it there.
func TestWebhookClosedUnmergedPRMovesItsTicketToWontDoAndAReopenLeavesItAlone(t *testing.T) {
	t.Parallel()
	f, k := newTicketFlow(t)

	f.hook(t, "opened", fixture(t, "update_pr_opened.json"))
	waitFor(t, "the ticket", func() bool { return f.ticketState(t) == "open" })
	f.hook(t, "closed", fixture(t, "update_pr_closed_unmerged.json"))
	waitFor(t, "the ticket to be won't do", func() bool { return f.ticketState(t) == "cancelled" })

	f.hook(t, "reopened", fixture(t, "update_pr_reopened.json"))
	time.Sleep(300 * time.Millisecond)
	if got := f.updatePRStates(t); len(got) != 1 || got[0] != "21:open:acme/widgets" {
		t.Fatalf("update PRs = %v, want the PR open again", got)
	}
	k.read(func() {
		if f.ticketState(t) != "cancelled" || k.status["task-1"] != "cancelled" || len(k.writes) != 2 {
			t.Errorf("state = %s, task = %q, writes = %v, want the ticket left in won't do with only its create and its move", f.ticketState(t), k.status["task-1"], k.writes)
		}
	})
}
