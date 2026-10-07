package poll_test

import (
	"context"
	"errors"
	"testing"

	"github.com/mdg-labs/release-ops/internal/poll"
	"github.com/mdg-labs/release-ops/internal/providers/ticket"
	"github.com/mdg-labs/release-ops/internal/store"
)

type failingWebURLProvider struct {
	mockTicketProvider
}

func (p *failingWebURLProvider) TicketWebURL(ticket.TicketProject, string) (string, error) {
	return "", errors.New("lookup failed")
}

func strVal(p *string) string {
	if p == nil {
		return "<nil>"
	}
	return *p
}

func requireRef(t *testing.T, got *poll.RepoEvaluation, action, wantID, wantURL, wantTag string) {
	t.Helper()
	ref, ok := got.Refs[action]
	if !ok {
		t.Fatalf("Refs[%s] missing; refs = %v", action, got.Refs)
	}
	if v := strVal(ref.TicketExternalID); v != wantID {
		t.Errorf("Refs[%s].TicketExternalID = %s, want %s", action, v, wantID)
	}
	if v := strVal(ref.TicketURL); v != wantURL {
		t.Errorf("Refs[%s].TicketURL = %s, want %s", action, v, wantURL)
	}
	if v := strVal(ref.ReleaseTag); v != wantTag {
		t.Errorf("Refs[%s].ReleaseTag = %s, want %s", action, v, wantTag)
	}
}

func openTicketRepo() store.MonitoredRepo {
	repo := baseRepo()
	last := "v1.0.0"
	openID := "old-ticket"
	repo.LastKnownTag = &last
	repo.OpenTicketExternalID = &openID
	repo.OpenTicketTag = &last
	return repo
}

func evaluate(t *testing.T, repo store.MonitoredRepo, tag string, policy string, provider ticket.TicketProvider) *poll.RepoEvaluation {
	t.Helper()
	engine := poll.NewEngine(newMockPollRepo(repo))
	got, err := engine.EvaluateRepo(context.Background(), repo, testRelease(tag), nil, testTicketProject(policy), provider, testRepoWebURL())
	if err != nil {
		t.Fatalf("EvaluateRepo: %v", err)
	}
	return got
}

func TestEvaluateRepoRefsBaselineCarriesOnlyTag(t *testing.T) {
	t.Parallel()

	got := evaluate(t, baseRepo(), "v1.0.0", ticket.PolicySupersede, &mockTicketProvider{})
	ref := got.Refs[poll.ActionBaseline]
	if ref.TicketExternalID != nil || ref.TicketURL != nil {
		t.Fatalf("baseline ticket ref = %v/%v, want none", ref.TicketExternalID, ref.TicketURL)
	}
	if strVal(ref.ReleaseTag) != "v1.0.0" {
		t.Fatalf("baseline tag = %s, want v1.0.0", strVal(ref.ReleaseTag))
	}
}

func TestEvaluateRepoRefsSkipAndErrorCarryNothing(t *testing.T) {
	t.Parallel()

	repo := baseRepo()
	last := "v1.0.0"
	repo.LastKnownTag = &last
	skip := evaluate(t, repo, "v1.0.0", ticket.PolicySupersede, &mockTicketProvider{})
	if len(skip.Refs) != 0 {
		t.Fatalf("skip refs = %v, want none", skip.Refs)
	}

	engine := poll.NewEngine(newMockPollRepo(repo))
	failed, err := engine.EvaluateRepo(context.Background(), repo, nil, errors.New("boom"), testTicketProject(ticket.PolicySupersede), &mockTicketProvider{}, testRepoWebURL())
	if err != nil {
		t.Fatalf("EvaluateRepo: %v", err)
	}
	if len(failed.Refs) != 0 {
		t.Fatalf("error refs = %v, want none", failed.Refs)
	}
}

func TestEvaluateRepoRefsCreateCarriesNewTicket(t *testing.T) {
	t.Parallel()

	repo := baseRepo()
	last := "v1.0.0"
	repo.LastKnownTag = &last
	got := evaluate(t, repo, "v2.0.0", ticket.PolicySupersede, &mockTicketProvider{createID: "new-ticket"})
	requireRef(t, got, poll.ActionCreate, "new-ticket", "https://tickets.example/new-ticket", "v2.0.0")
}

func TestEvaluateRepoRefsSupersedePairHasOneRefPerAction(t *testing.T) {
	t.Parallel()

	provider := &mockTicketProvider{statuses: map[string]string{"old-ticket": "in-progress"}, createID: "new-ticket"}
	got := evaluate(t, openTicketRepo(), "v2.0.0", ticket.PolicySupersede, provider)
	requireRef(t, got, poll.ActionSupersede, "old-ticket", "https://tickets.example/old-ticket", "v2.0.0")
	requireRef(t, got, poll.ActionCreate, "new-ticket", "https://tickets.example/new-ticket", "v2.0.0")
}

func TestEvaluateRepoRefsPartialSupersedeKeepsNewTicketRefOnCreateOnly(t *testing.T) {
	t.Parallel()

	provider := &mockTicketProvider{
		statuses:        map[string]string{"old-ticket": "in-progress"},
		createID:        "new-ticket",
		updateStatusErr: errors.New("no transition"),
	}
	got := evaluate(t, openTicketRepo(), "v2.0.0", ticket.PolicySupersede, provider)
	requireRef(t, got, poll.ActionCreate, "new-ticket", "https://tickets.example/new-ticket", "v2.0.0")
	if _, ok := got.Refs[poll.ActionError]; ok {
		t.Fatalf("error action carries a ref: %v", got.Refs[poll.ActionError])
	}
}

func TestEvaluateRepoRefsMergeCarriesExistingTicket(t *testing.T) {
	t.Parallel()

	provider := &mockTicketProvider{statuses: map[string]string{"old-ticket": "ready"}}
	got := evaluate(t, openTicketRepo(), "v2.0.0", ticket.PolicyMerge, provider)
	requireRef(t, got, poll.ActionMerge, "old-ticket", "https://tickets.example/old-ticket", "v2.0.0")
}

func TestEvaluateRepoRefsSkipIfOpenCarriesOnlyTag(t *testing.T) {
	t.Parallel()

	provider := &mockTicketProvider{statuses: map[string]string{"old-ticket": "ready"}}
	got := evaluate(t, openTicketRepo(), "v2.0.0", ticket.PolicySkipIfOpen, provider)
	ref := got.Refs[poll.ActionSkipOpen]
	if ref.TicketExternalID != nil || ref.TicketURL != nil {
		t.Fatalf("skip_open ticket ref = %v/%v, want none", ref.TicketExternalID, ref.TicketURL)
	}
	if strVal(ref.ReleaseTag) != "v2.0.0" {
		t.Fatalf("skip_open tag = %s, want v2.0.0", strVal(ref.ReleaseTag))
	}
}

func TestEvaluateRepoRefsWebURLFailureKeepsIDAndPollOutcome(t *testing.T) {
	t.Parallel()

	repo := baseRepo()
	last := "v1.0.0"
	repo.LastKnownTag = &last
	provider := &failingWebURLProvider{mockTicketProvider{createID: "new-ticket"}}
	got := evaluate(t, repo, "v2.0.0", ticket.PolicySupersede, provider)
	if len(got.Actions) != 1 || got.Actions[0] != poll.ActionCreate {
		t.Fatalf("actions = %v, want [create]", got.Actions)
	}
	if got.Repo.OpenTicketExternalID == nil || *got.Repo.OpenTicketExternalID != "new-ticket" {
		t.Fatalf("OpenTicketExternalID = %v, want new-ticket", got.Repo.OpenTicketExternalID)
	}
	requireRef(t, got, poll.ActionCreate, "new-ticket", "<nil>", "v2.0.0")
}

func TestRunRecorderPersistsRefPerAction(t *testing.T) {
	t.Parallel()

	repo := store.MonitoredRepo{ID: "repo-1"}
	pollRepo := &runTestPollRepo{}
	recorder := poll.NewRunRecorder(pollRepo, nil)

	oldID, newID, oldURL, tag := "old", "new", "https://t.example/old", "v2"
	var created, superseded int64
	recorder.RecordEvaluation(context.Background(), "run-1", "repo-1", &poll.RepoEvaluation{
		Actions: []string{poll.ActionSupersede, poll.ActionCreate},
		Repo:    &repo,
		Refs: map[string]store.PollEventRef{
			poll.ActionSupersede: {TicketExternalID: &oldID, TicketURL: &oldURL, ReleaseTag: &tag},
			poll.ActionCreate:    {TicketExternalID: &newID, ReleaseTag: &tag},
		},
	}, &created, &superseded)

	if len(pollRepo.events) != 2 {
		t.Fatalf("events len = %d, want 2", len(pollRepo.events))
	}
	if strVal(pollRepo.events[0].TicketExternalID) != "old" || strVal(pollRepo.events[0].TicketURL) != oldURL {
		t.Fatalf("supersede event ref = %v/%v", pollRepo.events[0].TicketExternalID, pollRepo.events[0].TicketURL)
	}
	if strVal(pollRepo.events[1].TicketExternalID) != "new" || pollRepo.events[1].TicketURL != nil {
		t.Fatalf("create event ref = %v/%v", pollRepo.events[1].TicketExternalID, pollRepo.events[1].TicketURL)
	}
	if strVal(pollRepo.events[1].ReleaseTag) != "v2" {
		t.Fatalf("create event tag = %v", pollRepo.events[1].ReleaseTag)
	}
}
