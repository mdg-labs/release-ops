package github_test

import (
	"context"
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/github"
	"github.com/mdg-labs/release-ops/internal/github/githubtest"
)

const enableRepo = "acme/api"

type enableRig struct {
	fake    *githubtest.Fake
	app     *github.AppClient
	enabler *github.RenovateEnabler
}

func newEnableRig(t *testing.T) *enableRig {
	t.Helper()
	clock := githubtest.NewClock(t0)
	fake := githubtest.New(t, clock.Now)
	fake.AddInstallation(1000, githubtest.AppID)
	fake.SetFiles(enableRepo, "README.md", "src/main.go")
	fake.SetBranchHead(enableRepo, "main", "base-sha")
	api := github.NewClient(fake.URL, 5*time.Second)
	app := github.NewAppClient(api, staticCreds{appID: githubtest.AppID, pem: fake.PEM}, github.AppClientOptions{Now: clock.Now})
	return &enableRig{fake: fake, app: app, enabler: github.NewRenovateEnabler(api, app)}
}

func (r *enableRig) enable() (github.EnabledPullRequest, error) {
	return r.enabler.Enable(context.Background(), 1000, enableRepo, "main", "local>acme/presets")
}

func (r *enableRig) writes() []string {
	var out []string
	for _, req := range r.fake.Requests() {
		if strings.HasPrefix(req.Path, "/repos/") && req.Method != http.MethodGet {
			out = append(out, req.Method+" "+req.Path)
		}
	}
	return out
}

func TestEnableOpensOnePullRequestAddingTheConfigAndLeavesTheDefaultBranchAlone(t *testing.T) {
	r := newEnableRig(t)

	pr, err := r.enable()
	if err != nil || pr.Number != 1 || pr.Existing || !strings.HasSuffix(pr.URL, "/pull/1") {
		t.Fatalf("Enable = %+v, %v, want a new pull request 1", pr, err)
	}
	file, ok := r.fake.BranchFile(enableRepo, github.EnableRenovateBranch, "renovate.json")
	if !ok {
		t.Fatal("renovate.json was not committed to the enable branch")
	}
	if want := "{\n  \"extends\": [\n    \"local>acme/presets\"\n  ]\n}\n"; file.Content != want {
		t.Errorf("renovate.json = %q, want %q", file.Content, want)
	}
	if sha, _ := r.fake.BranchHeadSHA(enableRepo, "main"); sha != "base-sha" {
		t.Errorf("default branch head = %q, want it untouched", sha)
	}
	if _, ok := r.fake.BranchFile(enableRepo, "main", "renovate.json"); ok {
		t.Error("renovate.json was committed to the default branch")
	}
	for _, w := range r.writes() {
		if strings.Contains(w, "/merge") || strings.HasPrefix(w, "PUT /repos/acme/api/pulls") {
			t.Errorf("unexpected write %q: the pull request is never merged", w)
		}
	}
	grants := r.fake.TokenGrants()
	if len(grants) != 1 {
		t.Fatalf("%d tokens were requested, want 1", len(grants))
	}
	if g := grants[0]; len(g.Repositories) != 1 || g.Repositories[0] != "api" {
		t.Errorf("token requested for %v, want the repository alone", g.Repositories)
	}
	if want := map[string]string{"contents": "write", "pull_requests": "write"}; !maps.Equal(grants[0].Permissions, want) {
		t.Errorf("token permissions = %v, want %v", grants[0].Permissions, want)
	}
}

// A token narrowed to contents and pull request write is never served to a consumer that did
// not ask for it, and the reverse: the cache keys on the permissions as well as the repository.
func TestTheEnableTokenAndTheTokensOfOtherConsumersAreNeverSharedForOneRepository(t *testing.T) {
	ctx := context.Background()
	consumers := map[string]func(app *github.AppClient) (github.InstallationToken, error){
		"CommentToken": func(app *github.AppClient) (github.InstallationToken, error) {
			return app.CommentToken(ctx, 1000, enableRepo)
		},
		"RepositoryToken": func(app *github.AppClient) (github.InstallationToken, error) {
			return app.RepositoryToken(ctx, 1000, enableRepo, nil)
		},
		"JobToken": func(app *github.AppClient) (github.InstallationToken, error) {
			return app.JobToken(ctx, 1000, enableRepo, nil)
		},
	}
	for name, mint := range consumers {
		// The enable token first, then the consumer's: the consumer gets a token of its own.
		r := newEnableRig(t)
		if _, err := r.app.EnableToken(ctx, 1000, enableRepo); err != nil {
			t.Fatalf("EnableToken: %v", err)
		}
		if _, err := mint(r.app); err != nil {
			t.Fatalf("%s after EnableToken: %v", name, err)
		}
		grants := r.fake.TokenGrants()
		if len(grants) != 2 || grants[1].Permissions != nil {
			t.Errorf("%s after EnableToken: token requests = %+v, want a second one with the installation's own permissions", name, grants)
		}

		// The consumer's token first, then the enable token: the narrowed one is minted anew.
		r = newEnableRig(t)
		if _, err := mint(r.app); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if _, err := r.app.EnableToken(ctx, 1000, enableRepo); err != nil {
			t.Fatalf("EnableToken after %s: %v", name, err)
		}
		grants = r.fake.TokenGrants()
		if want := map[string]string{"contents": "write", "pull_requests": "write"}; len(grants) != 2 || !maps.Equal(grants[1].Permissions, want) {
			t.Errorf("EnableToken after %s: token requests = %+v, want a second one narrowed to %v", name, grants, want)
		}
	}

	r := newEnableRig(t)
	if _, err := r.app.EnableToken(ctx, 1000, enableRepo); err != nil {
		t.Fatalf("EnableToken: %v", err)
	}
	if _, err := r.app.EnableToken(ctx, 1000, enableRepo); err != nil {
		t.Fatalf("EnableToken again: %v", err)
	}
	if n := len(r.fake.TokenGrants()); n != 1 {
		t.Errorf("two EnableToken calls minted %d tokens, want 1 (cached)", n)
	}
}

func TestEnableTwiceOpensNoSecondPullRequest(t *testing.T) {
	r := newEnableRig(t)
	first, err := r.enable()
	if err != nil {
		t.Fatalf("first Enable: %v", err)
	}
	before := len(r.writes())

	second, err := r.enable()
	if err != nil || !second.Existing || second.Number != first.Number || second.URL != first.URL {
		t.Fatalf("second Enable = %+v, %v, want the open pull request %+v", second, err, first)
	}
	if after := len(r.writes()); after != before {
		t.Errorf("the second call wrote %d more times, want none", after-before)
	}
}

func TestEnableRefusesARepositoryThatHasAConfigFile(t *testing.T) {
	for _, files := range [][]string{
		{"renovate.json"},
		{".github/renovate.json5"},
		{".renovaterc"},
	} {
		r := newEnableRig(t)
		r.fake.SetFiles(enableRepo, append([]string{"README.md"}, files...)...)

		if _, err := r.enable(); !errors.Is(err, github.ErrConfigExists) {
			t.Errorf("Enable with %v = %v, want ErrConfigExists", files, err)
		}
		if w := r.writes(); len(w) != 0 {
			t.Errorf("Enable with %v wrote %v, want nothing", files, w)
		}
	}
}

func TestEnableFailureOpensNoPullRequestAndTheNextAttemptReusesTheBranch(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusUnprocessableEntity} {
		r := newEnableRig(t)
		r.fake.FailPullCreation(enableRepo, status)

		if pr, err := r.enable(); err == nil {
			t.Fatalf("Enable with %d from GitHub = %+v, want an error", status, pr)
		}
		if _, ok := r.fake.BranchFile(enableRepo, "main", "renovate.json"); ok {
			t.Errorf("status %d: the default branch was written", status)
		}

		r.fake.FailPullCreation(enableRepo, 0)
		pr, err := r.enable()
		if err != nil || pr.Existing || pr.Number != 1 {
			t.Fatalf("retry after %d = %+v, %v, want one new pull request", status, pr, err)
		}
		if _, ok := r.fake.BranchFile(enableRepo, github.EnableRenovateBranch, "renovate.json"); !ok {
			t.Errorf("status %d: the retry left no renovate.json on the branch", status)
		}
	}
}

func TestEnableRefusedFileCommitIsAnErrorWithoutAPullRequest(t *testing.T) {
	r := newEnableRig(t)
	r.fake.FailFileCommit(enableRepo, http.StatusForbidden)

	if _, err := r.enable(); !errors.Is(err, github.ErrAppRejected) {
		t.Fatalf("Enable = %v, want ErrAppRejected", err)
	}
	for _, w := range r.writes() {
		if w == "POST /repos/acme/api/pulls" {
			t.Error("a pull request was opened for a commit GitHub refused")
		}
	}
}

func TestRenovatePresetIsTheFirstPresetRepositoryOrTheDefault(t *testing.T) {
	if got := github.RenovatePreset([]string{"acme/presets", "acme/other"}); got != "local>acme/presets" {
		t.Errorf("RenovatePreset = %q, want local>acme/presets", got)
	}
	if got := github.RenovatePreset(nil); got != "config:recommended" {
		t.Errorf("RenovatePreset(nil) = %q, want config:recommended", got)
	}
}

func (r *enableRig) branchWrites() []string {
	var out []string
	for _, w := range r.writes() {
		if strings.Contains(w, "/git/refs") {
			out = append(out, w)
		}
	}
	return out
}

func TestEnableCreatesTheBranchWhenItIsAbsent(t *testing.T) {
	r := newEnableRig(t)

	if pr, err := r.enable(); err != nil || pr.Number != 1 {
		t.Fatalf("Enable = %+v, %v, want a new pull request", pr, err)
	}
	if got := r.branchWrites(); len(got) != 1 || got[0] != "POST /repos/acme/api/git/refs" {
		t.Errorf("ref writes = %v, want one creation", got)
	}
}

func TestEnableResetsABranchHoldingNothingBeyondTheDefaultBranch(t *testing.T) {
	r := newEnableRig(t)
	r.fake.SetBranchHead(enableRepo, github.EnableRenovateBranch, "old-sha")

	if pr, err := r.enable(); err != nil || pr.Existing || pr.Number != 1 {
		t.Fatalf("Enable = %+v, %v, want a new pull request", pr, err)
	}
	if got := r.branchWrites(); len(got) != 1 || got[0] != "PATCH /repos/acme/api/git/refs/heads/release-ops/enable-renovate" {
		t.Errorf("ref writes = %v, want one reset", got)
	}
	if _, ok := r.fake.BranchFile(enableRepo, github.EnableRenovateBranch, "renovate.json"); !ok {
		t.Error("the reset branch has no renovate.json")
	}
}

func TestEnableResetsABranchHoldingOnlyTheActionsOwnCommit(t *testing.T) {
	r := newEnableRig(t)
	r.fake.SetBranchHead(enableRepo, github.EnableRenovateBranch, "base-sha")
	r.fake.AddBranchCommit(enableRepo, github.EnableRenovateBranch, "chore: add renovate.json", "renovate.json")

	if pr, err := r.enable(); err != nil || pr.Number != 1 {
		t.Fatalf("Enable = %+v, %v, want a new pull request", pr, err)
	}
	if got := r.branchWrites(); len(got) != 1 || !strings.HasPrefix(got[0], "PATCH ") {
		t.Errorf("ref writes = %v, want one reset", got)
	}
	commits := r.fake.BranchCommits(enableRepo, github.EnableRenovateBranch)
	if len(commits) != 1 || commits[0].Message != "chore: add renovate.json" {
		t.Errorf("the branch holds %+v after the reset, want the action's one new commit", commits)
	}
}

func TestEnableLeavesABranchWithCommitsThatAreNotItsOwnUntouched(t *testing.T) {
	type commit struct {
		message string
		files   []string
	}
	for name, commits := range map[string][]commit{
		"a foreign commit":                    {{"fix: tune the preset", []string{"renovate.json"}}},
		"the message on another file":         {{"chore: add renovate.json", []string{"README.md"}}},
		"the message on more than the config": {{"chore: add renovate.json", []string{"renovate.json", "src/main.go"}}},
		"the config commit plus a second one": {{"chore: add renovate.json", []string{"renovate.json"}}, {"docs: note", []string{"README.md"}}},
		"two commits of the action":           {{"chore: add renovate.json", []string{"renovate.json"}}, {"chore: add renovate.json", []string{"renovate.json"}}},
		"a commit with no message":            {{"", []string{"renovate.json"}}},
	} {
		r := newEnableRig(t)
		r.fake.SetBranchHead(enableRepo, github.EnableRenovateBranch, "base-sha")
		for _, c := range commits {
			r.fake.AddBranchCommit(enableRepo, github.EnableRenovateBranch, c.message, c.files...)
		}
		headBefore, _ := r.fake.BranchHeadSHA(enableRepo, github.EnableRenovateBranch)

		pr, err := r.enable()
		if !errors.Is(err, github.ErrEnableBranchExists) {
			t.Errorf("%s: Enable = %+v, %v, want ErrEnableBranchExists", name, pr, err)
		}
		if w := r.writes(); len(w) != 0 {
			t.Errorf("%s: Enable wrote %v, want nothing", name, w)
		}
		if head, _ := r.fake.BranchHeadSHA(enableRepo, github.EnableRenovateBranch); head != headBefore {
			t.Errorf("%s: the branch moved from %q to %q", name, headBefore, head)
		}
		if got := r.fake.BranchCommits(enableRepo, github.EnableRenovateBranch); len(got) != len(commits) {
			t.Errorf("%s: the branch holds %d commits, want its %d kept", name, len(got), len(commits))
		}
	}
}

func TestEnableDoesNotResetTheBranchWhenTheComparisonFails(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusForbidden, http.StatusInternalServerError} {
		r := newEnableRig(t)
		r.fake.SetBranchHead(enableRepo, github.EnableRenovateBranch, "base-sha")
		r.fake.AddBranchCommit(enableRepo, github.EnableRenovateBranch, "chore: add renovate.json", "renovate.json")
		r.fake.FailBranchComparison(enableRepo, status)

		pr, err := r.enable()
		if err == nil || errors.Is(err, github.ErrEnableBranchExists) {
			t.Errorf("status %d: Enable = %+v, %v, want the comparison's error", status, pr, err)
		}
		if w := r.writes(); len(w) != 0 {
			t.Errorf("status %d: Enable wrote %v, want nothing", status, w)
		}
		if got := r.fake.BranchCommits(enableRepo, github.EnableRenovateBranch); len(got) != 1 {
			t.Errorf("status %d: the branch holds %d commits, want it kept", status, len(got))
		}
	}
}

func TestCompareBranchesFailsClosedOnWhatItCannotTrust(t *testing.T) {
	for name, tc := range map[string]struct {
		status int
		body   string
	}{
		"no ahead count":  {http.StatusOK, `{"commits":[],"files":[]}`},
		"negative count":  {http.StatusOK, `{"ahead_by":-1}`},
		"not json":        {http.StatusOK, `<html>`},
		"too large":       {http.StatusOK, `{"ahead_by":0,"files":[` + strings.Repeat(`{"filename":"x"},`, 1<<20) + `{"filename":"x"}]}`},
		"a server error":  {http.StatusBadGateway, `{"ahead_by":0}`},
		"not found":       {http.StatusNotFound, `{"message":"Not Found"}`},
		"a redirect page": {http.StatusFound, ``},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(tc.body))
		}))
		c := github.NewClient(srv.URL, 5*time.Second)
		if cmp, err := c.CompareBranches(context.Background(), "tok", enableRepo, "main", github.EnableRenovateBranch); err == nil {
			t.Errorf("%s: CompareBranches = %+v, want an error", name, cmp)
		}
		srv.Close()
	}
}

func TestEnableLeavesABranchWhoseOneCommitRenamedAFileToTheConfigUntouched(t *testing.T) {
	r := newEnableRig(t)
	r.fake.SetBranchHead(enableRepo, github.EnableRenovateBranch, "base-sha")
	r.fake.AddRenamedBranchCommit(enableRepo, github.EnableRenovateBranch, "chore: add renovate.json", "config/old.json", "renovate.json")

	if _, err := r.enable(); !errors.Is(err, github.ErrEnableBranchExists) {
		t.Fatalf("Enable = %v, want ErrEnableBranchExists", err)
	}
	if w := r.writes(); len(w) != 0 {
		t.Errorf("Enable wrote %v, want nothing", w)
	}
}
