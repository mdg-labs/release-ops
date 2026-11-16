package renovate_test

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/queue"
	"github.com/mdg-labs/release-ops/internal/queue/internalapi"
	"github.com/mdg-labs/release-ops/internal/queue/worker"
	"github.com/mdg-labs/release-ops/internal/store"
	"github.com/mdg-labs/release-ops/internal/store/storetest"
)

// Renovate skips a repository that has no config (requireConfig=required) before it extracts
// anything: it exits 0 and its report holds the repository's entry with status "disabled", the
// entry shape Renovate's report writer creates for a repository that ended before extraction.
// Such an attempt is reported as skipped for a missing config (specs.html sections 2.2 and
// 2.7); an attempt whose entry has another status, that has no entry for the repository, or that
// is not a clean exit with a readable report, is not.
func TestAnAttemptRenovateSkippedForAMissingConfigIsReportedAsNoConfig(t *testing.T) {
	cases := []struct {
		name   string
		script string
		want   bool
	}{
		{"entry disabled", `echo '{"problems":[],"repositories":{"acme/repo-1":{"problems":[],"branches":[],"packageFiles":{},"status":"disabled"}}}' > "$RENOVATE_REPORT_PATH"`, true},
		{"entry disabled under another case", `echo '{"repositories":{"Acme/Repo-1":{"problems":[],"branches":[],"packageFiles":{},"status":"disabled"}}}' > "$RENOVATE_REPORT_PATH"`, true},
		{"entry activated", `echo '{"repositories":{"acme/repo-1":{"problems":[],"branches":[],"packageFiles":{},"status":"activated"}}}' > "$RENOVATE_REPORT_PATH"`, false},
		{"entry without a status", `echo '{"repositories":{"acme/repo-1":{"branches":[],"packageFiles":{}}}}' > "$RENOVATE_REPORT_PATH"`, false},
		{"entry null", `echo '{"repositories":{"acme/repo-1":null}}' > "$RENOVATE_REPORT_PATH"`, false},
		{"only another repository disabled", `echo '{"repositories":{"acme/other":{"status":"disabled"}}}' > "$RENOVATE_REPORT_PATH"`, false},
		{"empty repositories", `echo '{"problems":[],"repositories":{}}' > "$RENOVATE_REPORT_PATH"`, false},
		{"no report", `echo done`, false},
		{"unreadable report", `echo '{"repositories": ' > "$RENOVATE_REPORT_PATH"`, false},
		{"report of another shape", `echo '{"x":1}' > "$RENOVATE_REPORT_PATH"`, false},
		{"repositories not an object", `echo '{"repositories":[]}' > "$RENOVATE_REPORT_PATH"`, false},
		{"non-zero exit", `echo '{"repositories":{"acme/repo-1":{"status":"disabled"}}}' > "$RENOVATE_REPORT_PATH"; exit 2`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(t, c.script)
			res := e.h.Run(context.Background(), job(0))
			if res.Run == nil {
				t.Fatalf("no run record: %+v", res)
			}
			if c.want && res.Err != nil {
				t.Fatalf("a skipped repository failed the attempt: %+v", res.Err)
			}
			if res.Run.NoConfig != c.want {
				t.Errorf("NoConfig = %v, want %v (run %+v)", res.Run.NoConfig, c.want, res.Run)
			}
		})
	}
}

// A repository without a config goes through the whole path: the claim, the handler over a fake
// Renovate whose report holds a disabled entry for it, the result route and the store. The
// attempt ends no_config, not succeeded or failed, and the repository's config state is missing;
// a later run that finds a config ends succeeded and leaves the state to the installation sync.
func TestAClaimedJobOfARepositoryWithoutAConfigEndsNoConfig(t *testing.T) {
	db := storetest.OpenMigrated(t)
	repos := storetest.SeedRepositories(t, db, 1)
	q := store.NewJobQueue(db, store.JobQueueOptions{})
	srv := httptest.NewServer(internalapi.NewHandler(q, internalapi.Options{Secret: workerSecret, ClaimWait: 100 * time.Millisecond}))
	t.Cleanup(srv.Close)

	// The fake finds a config once $OUT/has-config exists.
	e := newEnv(t, `
if [ -e "$OUT/has-config" ]; then
  echo '{"problems":[],"repositories":{"acme/repo-1":{"problems":[],"branches":[],"packageFiles":{},"status":"activated"}}}' > "$RENOVATE_REPORT_PATH"
else
  echo '{"problems":[],"repositories":{"acme/repo-1":{"problems":[],"branches":[],"packageFiles":{},"status":"disabled"}}}' > "$RENOVATE_REPORT_PATH"
fi`)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		worker.NewRunner(worker.NewClient(srv.URL, workerSecret, nil), worker.Registry{queue.KindRenovateRun: e.h}, worker.Options{
			Slots: 1, HeartbeatInterval: 10 * time.Millisecond, ShutdownGrace: time.Second, RetryDelay: 10 * time.Millisecond,
			Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		}).Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})

	runOnce := func() (jobID string) {
		res, err := q.Enqueue(context.Background(), queue.EnqueueParams{RepositoryID: repos[0], Priority: queue.PriorityHigh, Trigger: queue.TriggerPush})
		if err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(15 * time.Second)
		for {
			var state string
			if err := db.Reader.QueryRow(`SELECT state FROM jobs WHERE id = ?`, res.JobID).Scan(&state); err != nil {
				t.Fatal(err)
			}
			if state == queue.StateSucceeded || state == queue.StateFailed {
				return res.JobID
			}
			if time.Now().After(deadline) {
				t.Fatalf("job still %s", state)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	read := func(jobID string) (outcome, config string) {
		if err := db.Reader.QueryRow(`SELECT r.outcome, p.config_state FROM job_runs r JOIN jobs j ON j.id = r.job_id JOIN repositories p ON p.id = j.repository_id WHERE j.id = ?`, jobID).Scan(&outcome, &config); err != nil {
			t.Fatal(err)
		}
		return outcome, config
	}

	if outcome, config := read(runOnce()); outcome != "no_config" || config != "missing" {
		t.Errorf("a repository without a config: outcome %q, config state %q; want no_config and missing", outcome, config)
	}
	if err := os.WriteFile(filepath.Join(e.out, "has-config"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if outcome, config := read(runOnce()); outcome != "succeeded" || config != "missing" {
		t.Errorf("a repository with a config: outcome %q, config state %q; want succeeded and missing left to the sync", outcome, config)
	}
}
