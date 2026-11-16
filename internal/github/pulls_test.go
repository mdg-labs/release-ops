package github_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mdg-labs/release-ops/internal/github"
)

const pullListing = `[{"number":7,"node_id":"PR_7","html_url":"https://github.com/acme/api/pull/7","title":"t","body":null,"state":"open",
"created_at":"2026-11-15T10:00:00Z","updated_at":"2026-11-15T10:01:00Z","closed_at":null,"merged_at":null,
"user":{"id":42},"head":{"ref":"renovate/x","sha":"abc"},"labels":[{"name":"dependencies"}]}]`

func TestOpenPullRequestsPageSendsTheETagAndReadsTheNextLink(t *testing.T) {
	var gotAuth, gotMatch, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotMatch, gotQuery = r.Header.Get("Authorization"), r.Header.Get("If-None-Match"), r.URL.RawQuery
		if r.URL.Path != "/repos/acme/api/pulls" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("If-None-Match") == `W/"v1"` {
			w.Header().Set("Link", `<http://`+r.Host+`/repos/acme/api/pulls?page=2>; rel="next"`)
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `W/"v1"`)
		w.Header().Set("Link", `<http://`+r.Host+`/repos/acme/api/pulls?page=2>; rel="next"`)
		_, _ = w.Write([]byte(pullListing))
	}))
	t.Cleanup(srv.Close)
	c := github.NewClient(srv.URL, 0)

	page, err := c.OpenPullRequestsPage(context.Background(), "tok", "acme/api", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer tok" || gotMatch != "" || gotQuery != "state=open&per_page=100" {
		t.Fatalf("first request: auth %q, If-None-Match %q, query %q", gotAuth, gotMatch, gotQuery)
	}
	if len(page.PullRequests) != 1 || !page.PullRequests[0].Valid() || page.PullRequests[0].User.ID != 42 || page.ETag != `W/"v1"` || !strings.HasSuffix(page.Next, "page=2") {
		t.Fatalf("page = %+v", page)
	}

	again, err := c.OpenPullRequestsPage(context.Background(), "tok", "acme/api", "", page.ETag)
	if err != nil || !again.NotModified || again.ETag != page.ETag || !strings.HasSuffix(again.Next, "page=2") || len(again.PullRequests) != 0 {
		t.Fatalf("conditional page = %+v, %v", again, err)
	}
	if gotMatch != `W/"v1"` {
		t.Fatalf("If-None-Match = %q", gotMatch)
	}
}

// The bearer token goes to the configured API host only: a next link or a page URL on another
// host is refused, and a failing page is an error, never a partial list.
func TestOpenPullRequestsPageKeepsTheTokenOnTheAPIHost(t *testing.T) {
	var other int
	foreign := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { other++ }))
	t.Cleanup(foreign.Close)
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Link", `<`+foreign.URL+`/pulls?page=2>; rel="next"`)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(srv.Close)
	c := github.NewClient(srv.URL, 0)
	ctx := context.Background()

	if _, err := c.OpenPullRequestsPage(ctx, "tok", "acme/api", "", ""); !errors.Is(err, github.ErrRequestFailed) {
		t.Fatalf("a next link on another host = %v, want ErrRequestFailed", err)
	}
	if _, err := c.OpenPullRequestsPage(ctx, "tok", "acme/api", foreign.URL+"/pulls", ""); !errors.Is(err, github.ErrRequestFailed) {
		t.Fatalf("a page URL on another host = %v, want ErrRequestFailed", err)
	}
	status = http.StatusBadGateway
	if _, err := c.OpenPullRequestsPage(ctx, "tok", "acme/api", "", ""); !errors.Is(err, github.ErrRequestFailed) {
		t.Fatalf("a 502 = %v, want ErrRequestFailed", err)
	}
	if _, err := c.OpenPullRequestsPage(ctx, "tok", "not-a-repository", "", ""); err == nil {
		t.Fatal("a repository that is not owner/name was accepted")
	}
	if other != 0 {
		t.Fatalf("the other host received %d requests", other)
	}
}

func TestPullRequestReadsOneAndReportsAnUnknownNumber(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/api/pulls/7":
			_, _ = w.Write([]byte(strings.Trim(pullListing, "[]")))
		case "/repos/acme/api/pulls/8":
			_, _ = w.Write([]byte(`{"number":99}`)) // not the PR that was asked for
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	c := github.NewClient(srv.URL, 0)
	ctx := context.Background()

	pr, err := c.PullRequest(ctx, "tok", "acme/api", 7)
	if err != nil || pr.Number != 7 || pr.Head.Ref != "renovate/x" || len(pr.Labels) != 1 {
		t.Fatalf("PullRequest = %+v, %v", pr, err)
	}
	if _, err := c.PullRequest(ctx, "tok", "acme/api", 6); !errors.Is(err, github.ErrPullRequestNotFound) {
		t.Fatalf("unknown number = %v, want ErrPullRequestNotFound", err)
	}
	if _, err := c.PullRequest(ctx, "tok", "acme/api", 8); !errors.Is(err, github.ErrRequestFailed) {
		t.Fatalf("a response for another number = %v, want ErrRequestFailed", err)
	}
}

func TestPullRequestValidNeedsWhatTrackingUses(t *testing.T) {
	const full = `{"number":7,"node_id":"PR_7","html_url":"https://github.com/acme/api/pull/7","state":"open",
"created_at":"2026-11-15T10:00:00Z","updated_at":"2026-11-15T10:01:00Z","user":{"id":42},"head":{"ref":"renovate/x","sha":"abc"}}`
	for _, tc := range []struct {
		name, from, to string
		want           bool
	}{
		{"complete", "", "", true},
		{"no node id", `"node_id":"PR_7",`, ``, false},
		{"no html url", `"html_url":"https://github.com/acme/api/pull/7",`, ``, false},
		{"no head ref", `"ref":"renovate/x"`, `"ref":""`, false},
		{"no update time", `"updated_at":"2026-11-15T10:01:00Z",`, ``, false},
		{"state of a draft", `"state":"open"`, `"state":"draft"`, false},
	} {
		doc := full
		if tc.from != "" {
			if !strings.Contains(doc, tc.from) {
				t.Fatalf("%s: fixture lacks %q", tc.name, tc.from)
			}
			doc = strings.Replace(doc, tc.from, tc.to, 1)
		}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(doc)) }))
		pr, err := github.NewClient(srv.URL, 0).PullRequest(context.Background(), "tok", "acme/api", 7)
		srv.Close()
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if pr.Valid() != tc.want {
			t.Errorf("%s: Valid = %v, want %v", tc.name, pr.Valid(), tc.want)
		}
	}
}

func TestPullRequestAutoMerge(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		body    string
		present bool
		enabled bool
	}{
		"absent":  {`{"number":1}`, false, false},
		"null":    {`{"number":1,"auto_merge":null}`, true, false},
		"enabled": {`{"number":1,"auto_merge":{"merge_method":"squash"}}`, true, true},
	} {
		var pr github.PullRequest
		if err := json.Unmarshal([]byte(tc.body), &pr); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if pr.AutoMerge.Present != tc.present || pr.AutoMerge.Enabled != tc.enabled {
			t.Errorf("%s: auto_merge = %+v, want present %v enabled %v", name, pr.AutoMerge, tc.present, tc.enabled)
		}
	}
}
