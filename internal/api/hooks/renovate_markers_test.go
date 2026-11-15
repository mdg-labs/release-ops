package hooks_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/mdg-labs/release-ops/internal/queue"
)

// tickWith delivers a dashboard edit that ticks the given markers.
func (r *trigRig) tickWith(t *testing.T, guid string, markers []string) {
	t.Helper()
	var from, to strings.Builder
	for _, m := range markers {
		fmt.Fprintf(&from, " - [ ] <!-- %s -->x\n", m)
		fmt.Fprintf(&to, " - [x] <!-- %s -->x\n", m)
	}
	r.mustDeliver(t, "issues", guid, mutate(t, "issues_edited_dashboard_tick.json", func(m map[string]any) {
		obj(m, "issue")["body"] = to.String()
		obj(m, "changes", "body")["from"] = from.String()
	}))
}

func (r *trigRig) wantCapped(t *testing.T, what, raw string, first string) {
	t.Helper()
	var got []string
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("%s = %q: %v", what, raw, err)
	}
	if len(got) != queue.MaxMarkers || got[0] != first {
		t.Errorf("%s holds %d markers starting %q, want %d starting %q", what, len(got), got[0], queue.MaxMarkers, first)
	}
}

// A user who keeps editing the dashboard with new checkbox markers cannot grow the stored
// markers of the repository's job, whether the job is queued or running.
func TestRepeatedTicksCannotGrowAJobsMarkersWithoutBound(t *testing.T) {
	t.Parallel()
	r := newTrigRig(t, trigConfig{botID: botUserID})
	ctx := context.Background()

	batch := func(i int) []string {
		out := make([]string, 8)
		for j := range out {
			out[j] = fmt.Sprintf("approve-branch=edit%d-%d", i, j)
		}
		return out
	}

	for i := range 40 {
		r.tickWith(t, fmt.Sprintf("g-queued-%d", i), batch(i))
	}
	j := r.wantJobs(t, 1)[0]
	if j.State != "queued" {
		t.Fatalf("job = %+v, want the one queued job", j)
	}
	r.wantCapped(t, "matched_markers", j.Markers, "approve-branch=edit0-0")

	claimed, err := r.jobs.Claim(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 40 {
		r.tickWith(t, fmt.Sprintf("g-running-%d", i), batch(100+i))
	}
	var rerun string
	if err := r.db.Reader.QueryRow(`SELECT rerun_markers FROM jobs WHERE id = ?`, claimed.ID).Scan(&rerun); err != nil {
		t.Fatal(err)
	}
	r.wantCapped(t, "rerun_markers", rerun, "approve-branch=edit100-0")

	if err := r.jobs.Complete(ctx, claimed.ID, claimed.LeaseOwner); err != nil {
		t.Fatal(err)
	}
	jobs := r.wantJobs(t, 2)
	r.wantCapped(t, "follow-up markers", jobs[1].Markers, "approve-branch=edit100-0")
}
