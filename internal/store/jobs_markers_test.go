package store_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/mdg-labs/release-ops/internal/queue"
)

// distinctMarkers is n markers named prefix-0 … prefix-(n-1).
func distinctMarkers(prefix string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("%s-%d", prefix, i)
	}
	return out
}

// storedMarkers decodes a markers column and fails when it holds more than the queue allows.
func storedMarkers(t *testing.T, what string, raw sql.NullString) []string {
	t.Helper()

	if !raw.Valid {
		return nil
	}
	var markers []string
	if err := json.Unmarshal([]byte(raw.String), &markers); err != nil {
		t.Fatalf("%s = %q: %v", what, raw.String, err)
	}
	size := 0
	for _, m := range markers {
		size += len(m)
	}
	if len(markers) > queue.MaxMarkers || size > queue.MaxMergedMarkerBytes {
		t.Errorf("%s holds %d markers of %d bytes, want at most %d of %d", what, len(markers), size, queue.MaxMarkers, queue.MaxMergedMarkerBytes)
	}
	return markers
}

func wantMarkers(t *testing.T, what string, got, want []string) {
	t.Helper()

	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}

// Triggers that keep coalescing into a queued job with new markers cannot grow the stored list
// past the cap, and the earliest markers are the ones kept.
func TestCoalescedMarkersOfAQueuedJobStayCapped(t *testing.T) {
	e := newJobEnv(t, 1)

	first := distinctMarkers("first", queue.MaxMarkers/2)
	e.tick(t, e.repos[0], queue.PriorityHigh, queue.TriggerDashboardCheckbox, first, arrival(1))
	for i := range 100 {
		res := e.tick(t, e.repos[0], queue.PriorityHigh, queue.TriggerDashboardCheckbox, distinctMarkers(fmt.Sprintf("edit%d", i), queue.MaxMarkers), arrival(2))
		if res.Outcome != queue.EnqueueCoalesced {
			t.Fatalf("tick %d = %s, want coalesced", i, res.Outcome)
		}
	}

	_, job := e.queuedFor(t, e.repos[0])
	want := append(append([]string{}, first...), distinctMarkers("edit0", queue.MaxMarkers/2)...)
	wantMarkers(t, "matched_markers", storedMarkers(t, "matched_markers", job.Markers), want)
}

// The byte cap holds on its own when the markers are long enough to reach it before the count.
func TestCoalescedMarkersStayWithinTheByteCap(t *testing.T) {
	e := newJobEnv(t, 1)

	long := func(i int) string { return fmt.Sprintf("%04d", i) + strings.Repeat("x", 996) }
	for i := range 20 {
		e.tick(t, e.repos[0], queue.PriorityHigh, queue.TriggerDashboardCheckbox, []string{long(i)}, nil)
	}

	_, job := e.queuedFor(t, e.repos[0])
	got := storedMarkers(t, "matched_markers", job.Markers)
	if want := queue.MaxMergedMarkerBytes / 1000; len(got) != want {
		t.Fatalf("%d markers kept, want the %d earliest that fit", len(got), want)
	}
	for i, m := range got {
		if m != long(i) {
			t.Fatalf("marker %d = %.8q…, want the earliest ones in order", i, m)
		}
	}
}

// A list stored before the cap existed shrinks to it on the next merge.
func TestCoalesceShrinksAnOversizedStoredList(t *testing.T) {
	e := newJobEnv(t, 1)

	e.tick(t, e.repos[0], queue.PriorityHigh, queue.TriggerPush, nil, nil)
	id, _ := e.queuedFor(t, e.repos[0])
	stored, err := json.Marshal(distinctMarkers("old", 3*queue.MaxMarkers))
	if err != nil {
		t.Fatal(err)
	}
	e.exec(t, `UPDATE jobs SET matched_markers = ? WHERE id = ?`, string(stored), id)

	e.tick(t, e.repos[0], queue.PriorityHigh, queue.TriggerDashboardCheckbox, []string{"new"}, nil)

	_, job := e.queuedFor(t, e.repos[0])
	wantMarkers(t, "matched_markers", storedMarkers(t, "matched_markers", job.Markers), distinctMarkers("old", queue.MaxMarkers))
}

// Triggers that arrive while a job runs record at most the cap in rerun_markers, and the
// follow-up and the retry that fold them in stay within it too.
func TestRerunMarkersOfARunningJobStayCapped(t *testing.T) {
	ctx := context.Background()

	t.Run("recorded", func(t *testing.T) {
		e := newJobEnv(t, 1)
		e.tick(t, e.repos[0], queue.PriorityHigh, queue.TriggerPush, nil, nil)
		job := e.claim(t)

		for i := range 100 {
			res := e.tick(t, e.repos[0], queue.PriorityHigh, queue.TriggerDashboardCheckbox, distinctMarkers(fmt.Sprintf("edit%d", i), queue.MaxMarkers), arrival(2))
			if res.Outcome != queue.EnqueueRerunRequested {
				t.Fatalf("tick %d = %s, want rerun_requested", i, res.Outcome)
			}
		}
		running := e.rerunRow(t, job.ID)
		wantMarkers(t, "rerun_markers", storedMarkers(t, "rerun_markers", running.RerunMarkers), distinctMarkers("edit0", queue.MaxMarkers))
	})

	t.Run("follow-up", func(t *testing.T) {
		e := newJobEnv(t, 1)
		e.tick(t, e.repos[0], queue.PriorityHigh, queue.TriggerPush, nil, nil)
		job := e.claim(t)
		for i := range 10 {
			e.tick(t, e.repos[0], queue.PriorityHigh, queue.TriggerDashboardCheckbox, distinctMarkers(fmt.Sprintf("edit%d", i), queue.MaxMarkers), nil)
		}
		if err := e.q.Complete(ctx, job.ID, job.LeaseOwner); err != nil {
			t.Fatal(err)
		}
		_, follow := e.queuedFor(t, e.repos[0])
		wantMarkers(t, "follow-up markers", storedMarkers(t, "follow-up markers", follow.Markers), distinctMarkers("edit0", queue.MaxMarkers))
	})

	t.Run("follow-up merged into a queued job", func(t *testing.T) {
		e := newJobEnv(t, 1)
		e.tick(t, e.repos[0], queue.PriorityHigh, queue.TriggerPush, nil, nil)
		job := e.claim(t)
		e.tick(t, e.repos[0], queue.PriorityHigh, queue.TriggerDashboardCheckbox, distinctMarkers("rerun", queue.MaxMarkers), nil)
		// A queued job beside a running one is a row a restore or an older version left.
		own := distinctMarkers("own", queue.MaxMarkers-2)
		stored, err := json.Marshal(own)
		if err != nil {
			t.Fatal(err)
		}
		e.exec(t,
			`INSERT INTO jobs (id, kind, repository_id, priority, state, run_after, "trigger", matched_markers, created_at)
			 VALUES ('queued', 'renovate_run', ?, 'normal', 'queued', 't', 'schedule', ?, 't')`, e.repos[0], string(stored))
		if err := e.q.Complete(ctx, job.ID, job.LeaseOwner); err != nil {
			t.Fatal(err)
		}
		_, merged := e.queuedFor(t, e.repos[0])
		wantMarkers(t, "merged markers", storedMarkers(t, "merged markers", merged.Markers), append(own, "rerun-0", "rerun-1"))
	})

	t.Run("retry", func(t *testing.T) {
		e := newJobEnv(t, 1)
		own := distinctMarkers("own", queue.MaxMarkers-1)
		e.tick(t, e.repos[0], queue.PriorityHigh, queue.TriggerPush, own, nil)
		job := e.claim(t)
		for i := range 10 {
			e.tick(t, e.repos[0], queue.PriorityHigh, queue.TriggerDashboardCheckbox, distinctMarkers(fmt.Sprintf("edit%d", i), queue.MaxMarkers), nil)
		}
		if out, err := e.q.Fail(ctx, job.ID, job.LeaseOwner, queue.FailParams{}); err != nil || out != queue.FailRequeued {
			t.Fatalf("Fail = %s, %v; want requeued", out, err)
		}
		retry := e.rerunRow(t, job.ID)
		wantMarkers(t, "retry markers", storedMarkers(t, "retry markers", retry.Markers), append(own, "edit0-0"))
	})
}
