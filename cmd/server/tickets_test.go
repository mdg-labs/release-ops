package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/github/githubtest"
	"github.com/mdg-labs/release-ops/internal/queue"
	"github.com/mdg-labs/release-ops/internal/queue/worker"
	"github.com/mdg-labs/release-ops/internal/store"
)

// fakeKaneo records the tasks created in it.
type fakeKaneo struct {
	srv *httptest.Server
	mu  sync.Mutex
	got []string
}

func newFakeKaneo(t *testing.T) *fakeKaneo {
	t.Helper()
	k := &fakeKaneo{}
	k.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body struct{ Title, Description string }
		_ = json.Unmarshal(raw, &body)
		k.mu.Lock()
		k.got = append(k.got, r.Method+" "+r.URL.Path+"\n"+body.Title+"\n"+body.Description)
		n := len(k.got)
		k.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]string{"id": fmt.Sprintf("task-%d", n), "status": "ready"})
	}))
	t.Cleanup(k.srv.Close)
	return k
}

func (k *fakeKaneo) created() []string {
	k.mu.Lock()
	defer k.mu.Unlock()
	return append([]string(nil), k.got...)
}

const updatesTable = "This PR contains the following updates:\n\n| Package | Update | Change |\n|---|---|---|\n" +
	"| [foo](https://foo.dev) | major | `1.4.0` -> `2.0.0` |\n\n---\n\n"

// A PR the server learns of from GitHub gets its ticket once its dependencies are recorded:
// the reconcile that runs after a job result stores the PR and its dependencies, publishes the
// event after it commits, and the ticket engine creates the ticket in the installation's default
// ticket project and links it to the PR's update_tickets row. A PR without dependencies, as
// Renovate's config-migration PR, gets none, and a repeated reconcile makes no second ticket.
func TestServerCreatesTheTicketOfAnUpdatePROnceItsDependenciesAreRecorded(t *testing.T) {
	kaneo := newFakeKaneo(t)
	s := startPRServer(t, func(t *testing.T, st *store.Store) {
		ctx := context.Background()
		base := kaneo.srv.URL
		integ, err := st.Integrations().Create(ctx, store.CreateIntegrationInput{Kind: "kaneo", Name: "Kaneo", BaseURL: &base, Secret: []byte(`{"api_key":"k"}`)})
		if err != nil {
			t.Fatal(err)
		}
		tp, err := st.TicketProjects().Create(ctx, store.CreateTicketProjectInput{
			IntegrationID: integ.ID, ExternalProjectID: "DEV", Name: "DEV",
			CreateConfig: `{"workspaceId":"ws-1","status":"ready","priority":"medium"}`, StatusMapping: `{"open":["ready"],"done":["done"],"cancelled":["cancelled"],"superseded":"cancelled"}`,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := st.InstallationSettings().SetDefaultTicketProject(ctx, "inst-1", tp.ID); err != nil {
			t.Fatal(err)
		}
	})
	// PRs 1 and 2 were stored at start without dependencies: neither has a ticket.
	s.waitStates(t, "the reconcile at start", "1:open 2:open 9:merged")

	// PR 2's body now carries an updates table; the next job result reconciles it.
	s.botPR(2, func(p *githubtest.PullRequest) { p.Body, p.UpdatedAt = updatesTable+prBody, prAt(10) })
	finishAJob := func() {
		t.Helper()
		jobs := store.NewJobQueue(s.db, store.JobQueueOptions{})
		if _, err := jobs.Enqueue(context.Background(), queue.EnqueueParams{RepositoryID: s.repo, Kind: queue.KindRenovateRun, Priority: queue.PriorityHigh, Trigger: queue.TriggerPush}); err != nil {
			t.Fatal(err)
		}
		w := worker.NewClient(s.internal, testWorkerSecret, nil)
		job, err := w.Claim(context.Background())
		if err != nil || job == nil {
			t.Fatalf("claim = %v, %v", job, err)
		}
		if err := w.Result(context.Background(), job.ID, queue.ResultRequest{LeaseOwner: job.LeaseOwner, Outcome: queue.OutcomeSucceeded}); err != nil {
			t.Fatalf("result: %v", err)
		}
	}
	finishAJob()

	deadline := time.Now().Add(20 * time.Second)
	for len(kaneo.created()) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for the ticket of PR 2")
		}
		time.Sleep(25 * time.Millisecond)
	}
	finishAJob()
	time.Sleep(500 * time.Millisecond)

	created := kaneo.created()
	if len(created) != 1 || !strings.HasPrefix(created[0], "POST /api/task/DEV\n") || !strings.Contains(created[0], "- foo: 1.4.0 → 2.0.0 (major)") {
		t.Fatalf("tickets created = %q, want one in DEV listing foo", created)
	}
	rows, err := s.db.Reader.Query(`SELECT p.number, t.external_id, t.state, t.pending_since FROM update_tickets t JOIN update_prs p ON p.id = t.update_pr_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var got []string
	for rows.Next() {
		var number int
		var external, state string
		var marker *string
		if err := rows.Scan(&number, &external, &state, &marker); err != nil {
			t.Fatal(err)
		}
		got = append(got, fmt.Sprintf("%d:%s:%s:%v", number, external, state, marker))
	}
	if len(got) != 1 || got[0] != "2:task-1:open:<nil>" {
		t.Fatalf("update_tickets = %v, want only PR 2, linked and unmarked", got)
	}
}
