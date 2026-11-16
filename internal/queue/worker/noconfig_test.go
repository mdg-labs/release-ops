package worker_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/metrics"
	"github.com/mdg-labs/release-ops/internal/queue"
	"github.com/mdg-labs/release-ops/internal/queue/worker"
)

// An attempt whose handler reports a skip for a missing config goes through the worker, the
// internal listener and the store: the job ends succeeded, its job_runs row is no_config and the
// attempt is counted under outcome="no_config", not as a failure.
func TestWorkerReportsASkippedRepositoryAsNoConfig(t *testing.T) {
	e := newEnv(t)
	reg := metrics.NewRegistry()
	jobs, err := metrics.NewJobs(reg)
	if err != nil {
		t.Fatal(err)
	}
	o := opts()
	o.Jobs = jobs

	id := e.enqueue(t, e.repos[0])
	start(t, e, worker.Registry{
		queue.KindRenovateRun: worker.HandlerFunc(func(context.Context, queue.ClaimedJob) worker.Result {
			return worker.Result{Run: &queue.RunRecord{ReportStatus: queue.ReportOK, NoConfig: true}}
		}),
	}, o)
	e.waitState(t, id, "succeeded")

	var outcome string
	if err := e.db.Reader.QueryRow(`SELECT outcome FROM job_runs WHERE job_id = ?`, id).Scan(&outcome); err != nil {
		t.Fatal(err)
	}
	if outcome != "no_config" {
		t.Errorf("job_runs outcome = %q, want no_config", outcome)
	}
	var config string
	if err := e.db.Reader.QueryRow(`SELECT config_state FROM repositories WHERE id = ?`, e.repos[0]).Scan(&config); err != nil {
		t.Fatal(err)
	}
	if config != "missing" {
		t.Errorf("config state = %q, want missing", config)
	}

	deadline := time.Now().Add(10 * time.Second)
	for {
		body := scrapeMetrics(t, reg)
		if strings.Contains(body, `outcome="no_config",repository="acme/repo-1"} 1`) {
			if strings.Contains(body, `release_ops_job_failures_total{repository="acme/repo-1"}`) || strings.Contains(body, `outcome="succeeded"`) {
				t.Fatalf("a skipped attempt was also counted as a success or a failure:\n%s", body)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("metrics lack outcome=\"no_config\":\n%s", body)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
