package github_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mdg-labs/release-ops/internal/github"
)

const issueListing = `[{"number":3,"body":"- [ ] <!-- a -->x","state":"open","user":{"id":42}},
{"number":7,"body":null,"state":"open","user":{"id":42},"pull_request":{"url":"https://api.github.com/repos/acme/api/pulls/7"}}]`

func TestOpenIssuesPageAsksForTheCreatorsOpenIssuesAndTellsPullRequestsApart(t *testing.T) {
	var gotAuth, gotMatch, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotMatch, gotQuery = r.Header.Get("Authorization"), r.Header.Get("If-None-Match"), r.URL.RawQuery
		if r.URL.Path != "/repos/acme/api/issues" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Link", `<http://`+r.Host+`/repos/acme/api/issues?page=2>; rel="next"`)
		if gotMatch == `W/"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `W/"v1"`)
		_, _ = w.Write([]byte(issueListing))
	}))
	t.Cleanup(srv.Close)
	c := github.NewClient(srv.URL, 0)

	page, err := c.OpenIssuesPage(context.Background(), "tok", "acme/api", "renovate-acme[bot]", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer tok" || gotMatch != "" || gotQuery != "state=open&creator=renovate-acme%5Bbot%5D&per_page=100" {
		t.Fatalf("request: auth %q, If-None-Match %q, query %q", gotAuth, gotMatch, gotQuery)
	}
	if len(page.Issues) != 2 || page.Issues[0].Number != 3 || page.Issues[0].User.ID != 42 || page.Issues[0].PullRequest != nil ||
		page.Issues[1].PullRequest == nil || page.ETag != `W/"v1"` || !strings.HasSuffix(page.Next, "page=2") {
		t.Fatalf("page = %+v", page)
	}

	again, err := c.OpenIssuesPage(context.Background(), "tok", "acme/api", "renovate-acme[bot]", "", page.ETag)
	if err != nil || !again.NotModified || again.ETag != page.ETag || !strings.HasSuffix(again.Next, "page=2") || len(again.Issues) != 0 {
		t.Fatalf("conditional page = %+v, %v", again, err)
	}
	if gotMatch != `W/"v1"` {
		t.Fatalf("If-None-Match = %q", gotMatch)
	}
}

func TestOpenIssuesPageKeepsTheTokenOnTheAPIHostAndFailsOnAnErrorStatus(t *testing.T) {
	var other int
	foreign := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { other++ }))
	t.Cleanup(foreign.Close)
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Link", `<`+foreign.URL+`/issues?page=2>; rel="next"`)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(srv.Close)
	c := github.NewClient(srv.URL, 0)
	ctx := context.Background()

	if _, err := c.OpenIssuesPage(ctx, "tok", "acme/api", "bot[bot]", "", ""); !errors.Is(err, github.ErrRequestFailed) {
		t.Fatalf("a next link on another host = %v, want ErrRequestFailed", err)
	}
	if _, err := c.OpenIssuesPage(ctx, "tok", "acme/api", "bot[bot]", foreign.URL+"/issues", ""); !errors.Is(err, github.ErrRequestFailed) {
		t.Fatalf("a page URL on another host = %v, want ErrRequestFailed", err)
	}
	status = http.StatusInternalServerError
	if _, err := c.OpenIssuesPage(ctx, "tok", "acme/api", "bot[bot]", "", ""); !errors.Is(err, github.ErrRequestFailed) {
		t.Fatalf("a 500 = %v, want ErrRequestFailed", err)
	}
	if other != 0 {
		t.Fatalf("the other host got %d requests", other)
	}
}

func TestBranchHeadReadsTheCommitConditionallyAndEscapesTheBranch(t *testing.T) {
	var gotPath, gotMatch string
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMatch = r.URL.EscapedPath(), r.Header.Get("If-None-Match")
		if gotMatch == `W/"h1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		w.Header().Set("ETag", `W/"h1"`)
		_, _ = w.Write([]byte(`{"ref":"refs/heads/release/1.x","object":{"sha":"abc123","type":"commit"}}`))
	}))
	t.Cleanup(srv.Close)
	c := github.NewClient(srv.URL, 0)
	ctx := context.Background()

	head, err := c.BranchHead(ctx, "tok", "acme/api", "release/1.x", "")
	if err != nil || head.SHA != "abc123" || head.ETag != `W/"h1"` || head.NotModified {
		t.Fatalf("head = %+v, %v", head, err)
	}
	if gotPath != "/repos/acme/api/git/ref/heads/release/1.x" {
		t.Fatalf("path = %q", gotPath)
	}
	again, err := c.BranchHead(ctx, "tok", "acme/api", "release/1.x", head.ETag)
	if err != nil || !again.NotModified || again.SHA != "" || again.ETag != head.ETag {
		t.Fatalf("conditional head = %+v, %v", again, err)
	}

	status = http.StatusNotFound
	if _, err := c.BranchHead(ctx, "tok", "acme/api", "main", ""); !errors.Is(err, github.ErrBranchNotFound) {
		t.Fatalf("404 = %v, want ErrBranchNotFound", err)
	}
	status = http.StatusBadGateway
	if _, err := c.BranchHead(ctx, "tok", "acme/api", "main", ""); !errors.Is(err, github.ErrRequestFailed) {
		t.Fatalf("502 = %v, want ErrRequestFailed", err)
	}
}

func TestBranchHeadRefusesAnswerWithoutACommit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ref":"refs/heads/main","object":{}}`))
	}))
	t.Cleanup(srv.Close)
	if _, err := github.NewClient(srv.URL, 0).BranchHead(context.Background(), "tok", "acme/api", "main", ""); !errors.Is(err, github.ErrRequestFailed) {
		t.Fatalf("an answer without a sha = %v, want ErrRequestFailed", err)
	}
}
