package main

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/mdg-labs/release-ops/internal/config"
	"github.com/mdg-labs/release-ops/internal/github/githubtest"
)

const (
	pollWatchBotID     int64 = 424242
	pollWatchDashboard       = "## Dependency Dashboard\n\n - [ ] <!-- approve-branch=renovate/foo -->Approve foo\n"
)

func (r *recoveryRig) jobs(t *testing.T) (n int, priority, trigger, markers string) {
	t.Helper()
	if err := r.db.Reader.QueryRowContext(context.Background(),
		`SELECT COUNT(*), COALESCE(MIN(priority), ''), COALESCE(MIN("trigger"), ''), COALESCE(MIN(matched_markers), '') FROM jobs`).
		Scan(&n, &priority, &trigger, &markers); err != nil {
		t.Fatal(err)
	}
	return
}

func (r *recoveryRig) setDashboard(body string) {
	r.fake.SetIssue("acme/widgets", githubtest.Issue{Number: 1, Title: "Dependency Dashboard", Body: body, AuthorID: pollWatchBotID, State: "open"})
}

// Without a webhook URL the server reads the Dependency Dashboard of each repository at the
// polling interval: the first read is the baseline, a ticked checkbox then queues one
// normal-priority job, an unchanged dashboard answers 304 and queues nothing, and a further
// change coalesces into the queued job.
func TestServerQueuesARunForATickedDashboardCheckboxWithoutAWebhookURL(t *testing.T) {
	shortenIntervals(t)
	r := newRecoveryRig(t)
	r.fake.SetBotUser("release-ops-acme", pollWatchBotID)
	r.setDashboard(pollWatchDashboard)
	r.fake.SetBranchHead("acme/widgets", "main", "c1")

	startServer(t, &config.Config{
		AppDBPath:     r.dbPath,
		AppURL:        "https://release-ops.example.com",
		GitHubBaseURL: r.githubURL,
	})

	waitFor(t, "the baseline reads of the repository", func() bool {
		return r.count(http.MethodGet, "/repos/acme/widgets/issues") >= 1 && r.count(http.MethodGet, "/repos/acme/widgets/git/ref/heads/main") >= 1
	})
	waitFor(t, "an unchanged repository to answer 304", func() bool { return r.fake.WatchNotModifiedAnswers() >= 2 })
	if n, _, _, _ := r.jobs(t); n != 0 {
		t.Fatalf("%d jobs before anything changed", n)
	}

	r.setDashboard(strings.Replace(pollWatchDashboard, "[ ]", "[x]", 1))
	waitFor(t, "the tick to queue a job", func() bool { n, _, _, _ := r.jobs(t); return n > 0 })
	n, priority, trigger, markers := r.jobs(t)
	if n != 1 || priority != "normal" || trigger != "polling" || !strings.Contains(markers, "approve-branch=renovate/foo") {
		t.Fatalf("jobs = %d (%s %s %s), want one normal polling job with the ticked marker", n, priority, trigger, markers)
	}

	r.fake.SetBranchHead("acme/widgets", "main", "c2")
	before := r.count(http.MethodGet, "/repos/acme/widgets/git/ref/heads/main")
	waitFor(t, "the pass after the push", func() bool { return r.count(http.MethodGet, "/repos/acme/widgets/git/ref/heads/main") >= before+2 })
	if n, _, _, _ := r.jobs(t); n != 1 {
		t.Fatalf("%d jobs after a push while one is queued, want the request coalesced into it", n)
	}
}

// With a webhook URL the webhook reports these changes and nothing is polled.
func TestServerDoesNotWatchDashboardsWhileTheWebhookIsActive(t *testing.T) {
	shortenIntervals(t)
	r := newRecoveryRig(t)
	r.fake.SetBotUser("release-ops-acme", pollWatchBotID)
	r.setDashboard(pollWatchDashboard)
	r.fake.SetBranchHead("acme/widgets", "main", "c1")

	startServer(t, &config.Config{
		AppDBPath:     r.dbPath,
		AppURL:        "https://release-ops.example.com",
		WebhookURL:    "https://release-ops.example.com/hooks/github",
		GitHubBaseURL: r.githubURL,
	})

	waitFor(t, "the start-up sync", func() bool { return repositoryRow(t, r.db, 7001) == "acme/widgets main present" })
	waitFor(t, "several redelivery passes, which run at the shortened interval", func() bool {
		return r.count(http.MethodGet, "/app/hook/deliveries") >= 4
	})
	for _, prefix := range []string{"/repos/acme/widgets/issues", "/repos/acme/widgets/git/"} {
		if n := r.count(http.MethodGet, prefix); n != 0 {
			t.Errorf("%d reads of %s while the webhook is active, want none", n, prefix)
		}
	}
}
