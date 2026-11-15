package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/mdg-labs/release-ops/internal/api/hooks"
	"github.com/mdg-labs/release-ops/internal/store"
	"github.com/mdg-labs/release-ops/internal/updateprs"
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
