package hooks_test

import (
	"context"
	"testing"
)

// A push job is running when a dashboard tick arrives: the follow-up that the worker's result
// ends the run with names the tick, not the push.
func TestFollowUpOfATickDuringARunningJobNamesTheTick(t *testing.T) {
	t.Parallel()
	r := newTrigRig(t, trigConfig{botID: botUserID})
	ctx := context.Background()

	r.mustDeliver(t, "push", "g-push", fixture(t, "push_default_branch.json"))
	claimed, err := r.jobs.Claim(ctx)
	if err != nil {
		t.Fatal(err)
	}
	r.mustDeliver(t, "issues", "g-tick", fixture(t, "issues_edited_dashboard_tick.json"))

	if err := r.jobs.Complete(ctx, claimed.ID, claimed.LeaseOwner); err != nil {
		t.Fatal(err)
	}
	jobs := r.wantJobs(t, 2)
	if jobs[0].Trigger != "push" || jobs[0].State != "succeeded" {
		t.Errorf("finished job = %+v, want the push job", jobs[0])
	}
	f := jobs[1]
	if f.State != "queued" || f.Priority != "high" || f.Trigger != "dashboard_checkbox" || f.Markers != `["approve-branch=renovate/vite-7.x"]` {
		t.Errorf("follow-up = %+v, want a queued high-priority dashboard_checkbox job with the ticked marker", f)
	}
	if f.WebhookReceivedAt != "2026-11-15T09:30:00.000Z" {
		t.Errorf("follow-up webhook_received_at = %q, want the tick's arrival", f.WebhookReceivedAt)
	}
}

func TestTwoTicksDuringARunningJobGiveOneFollowUpWithBothMarkers(t *testing.T) {
	t.Parallel()
	r := newTrigRig(t, trigConfig{botID: botUserID})
	ctx := context.Background()

	r.mustDeliver(t, "push", "g-push", fixture(t, "push_default_branch.json"))
	claimed, err := r.jobs.Claim(ctx)
	if err != nil {
		t.Fatal(err)
	}
	r.mustDeliver(t, "issues", "g-tick-1", fixture(t, "issues_edited_dashboard_tick.json"))
	r.mustDeliver(t, "pull_request", "g-tick-2", fixture(t, "pull_request_edited_rebase_tick.json"))

	if err := r.jobs.Complete(ctx, claimed.ID, claimed.LeaseOwner); err != nil {
		t.Fatal(err)
	}
	jobs := r.wantJobs(t, 2)
	f := jobs[1]
	if f.State != "queued" || f.Trigger != "pr_checkbox" || f.Markers != `["approve-branch=renovate/vite-7.x","rebase-check"]` {
		t.Errorf("follow-up = %+v, want one queued job from the latest tick with both markers", f)
	}
}
