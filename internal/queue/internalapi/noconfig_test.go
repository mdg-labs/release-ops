package internalapi_test

import (
	"database/sql"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/queue"
)

// A result whose outcome is no_config ends the job succeeded with a no_config job_runs row, with
// or without a run record, and records the repository's config as missing (specs.html section 2.7).
// The same result with the outcome succeeded leaves the config state it found (missing here, from
// the first case), and a run record cannot make a succeeded outcome a skipped one.
func TestResultNoConfigEndsTheAttemptNoConfig(t *testing.T) {
	e := newEnv(t, 50*time.Millisecond)
	auth := "Bearer " + secret
	exit := 0
	cases := []struct {
		repo      int
		outcome   string
		run       *queue.RunRecord
		wantRun   string
		wantState string
	}{
		{0, queue.OutcomeNoConfig, &queue.RunRecord{ExitCode: &exit, ReportStatus: queue.ReportOK}, "no_config", "missing"},
		{1, queue.OutcomeNoConfig, nil, "no_config", "missing"},
		{0, queue.OutcomeSucceeded, &queue.RunRecord{ExitCode: &exit, ReportStatus: queue.ReportOK}, "succeeded", "missing"},
	}
	for _, c := range cases {
		e.enqueue(t, e.repos[c.repo])
		job := e.claimJob(t)
		path := strings.Replace(queue.PathResult, "{id}", job.ID, 1)
		if status, body := e.post(t, path, auth, queue.ResultRequest{LeaseOwner: job.LeaseOwner, Outcome: c.outcome, Run: c.run}); status != http.StatusNoContent {
			t.Fatalf("%s result = %d %s", c.outcome, status, body)
		}
		var state, outcome string
		var code sql.NullString
		if err := e.db.Reader.QueryRow(`SELECT j.state, r.outcome, r.error_code FROM jobs j JOIN job_runs r ON r.job_id = j.id WHERE j.id = ?`, job.ID).Scan(&state, &outcome, &code); err != nil {
			t.Fatalf("%s result left no job_runs row: %v", c.outcome, err)
		}
		if state != queue.StateSucceeded || outcome != c.wantRun || code.Valid {
			t.Errorf("%s: job %q, run %q, error %v; want succeeded and %q", c.outcome, state, outcome, code, c.wantRun)
		}
		var config string
		if err := e.db.Reader.QueryRow(`SELECT config_state FROM repositories WHERE id = ?`, e.repos[c.repo]).Scan(&config); err != nil {
			t.Fatal(err)
		}
		if config != c.wantState {
			t.Errorf("%s: config state %q, want %q", c.outcome, config, c.wantState)
		}
	}
}
