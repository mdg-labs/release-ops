package main

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/github/githubtest"
	"github.com/mdg-labs/release-ops/internal/queue"
	"github.com/mdg-labs/release-ops/internal/queue/worker"
	"github.com/mdg-labs/release-ops/internal/store"
)

// An update PR whose ticket was created gets one comment of the App's bot on the PR through the
// server's own wiring: the marker, the ticket's key and link and its state. When the PR is
// merged, the ticket moves to done and the same comment is edited, never a second one posted, and
// nothing is ever written to the PR itself.
func TestServerWritesAndEditsTheTicketCommentOnTheUpdatePR(t *testing.T) {
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
	s.fake.SetCommentAuthor(prBotID)
	s.waitStates(t, "the reconcile at start", "1:open 2:open 9:merged")

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
	waitComment := func(what, contains string) githubtest.Comment {
		t.Helper()
		deadline := time.Now().Add(20 * time.Second)
		for {
			for _, c := range s.fake.Comments("acme/repo-1", 2) {
				if strings.Contains(c.Body, contains) {
					return c
				}
			}
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s: comments = %+v", what, s.fake.Comments("acme/repo-1", 2))
			}
			time.Sleep(25 * time.Millisecond)
		}
	}

	s.botPR(2, func(p *githubtest.PullRequest) { p.Body, p.UpdatedAt = updatesTable+prBody, prAt(10) })
	finishAJob()
	first := waitComment("the ticket comment", "Tracked in")
	link := kaneo.srv.URL + "/dashboard/workspace/ws-1/project/DEV/task/task-1"
	for _, want := range []string{"<!-- release-ops:ticket -->", "task-1", link, "state: open"} {
		if !strings.Contains(first.Body, want) {
			t.Fatalf("comment = %q, want it to contain %q", first.Body, want)
		}
	}
	if first.UserID != prBotID {
		t.Fatalf("comment author = %d, want the bot %d", first.UserID, prBotID)
	}

	// A repeated reconcile of an unchanged PR posts nothing more.
	finishAJob()
	time.Sleep(500 * time.Millisecond)
	if n := len(s.fake.Comments("acme/repo-1", 2)); n != 1 {
		t.Fatalf("%d comments after a repeated reconcile, want 1", n)
	}

	// The PR is merged: the ticket is done and the same comment says so.
	s.botPR(2, func(p *githubtest.PullRequest) {
		p.Body, p.State, p.Merged, p.MergedAt, p.ClosedAt, p.UpdatedAt = updatesTable+prBody, "closed", true, prAt(20), prAt(20), prAt(20)
	})
	finishAJob()
	done := waitComment("the comment to show the merge", "state: done")
	if done.ID != first.ID {
		t.Fatalf("the comment id changed from %d to %d, want it edited in place", first.ID, done.ID)
	}
	if got := s.fake.Comments("acme/repo-1", 2); len(got) != 1 {
		t.Fatalf("comments = %+v, want exactly one", got)
	}
	for _, r := range s.fake.Requests() {
		if strings.Contains(r.Path, "/pulls") && r.Method != http.MethodGet {
			t.Fatalf("%s %s: the PR itself must never be written", r.Method, r.Path)
		}
	}
}
