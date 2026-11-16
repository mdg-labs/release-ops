package githubtest

import (
	"io"
	"net/http"
	"testing"
	"time"
)

// With several open pull requests the issue list has one body, so its ETag is stable and a
// conditional request with it is answered 304.
func TestIssueListWithSeveralOpenPullsIsDeterministic(t *testing.T) {
	f := New(t, time.Now)
	const repo = "acme/api"
	f.mu.Lock()
	f.tokenOwner["tok"] = 1
	f.mu.Unlock()
	f.SetIssue(repo, Issue{Number: 1, Title: "dashboard", State: "open"})
	now := time.Now()
	for _, n := range []int64{7, 3, 11, 5, 9, 2, 13, 4} {
		f.SetPullRequest(repo, PullRequest{Number: n, Title: "update", State: "open", CreatedAt: now, UpdatedAt: now})
	}
	f.SetPullRequest(repo, PullRequest{Number: 20, Title: "closed", State: "closed", CreatedAt: now, UpdatedAt: now})

	get := func(ifNoneMatch string) (*http.Response, string) {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, f.URL+"/repos/"+repo+"/issues?state=open&per_page=100", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer tok")
		if ifNoneMatch != "" {
			req.Header.Set("If-None-Match", ifNoneMatch)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		return resp, string(body)
	}

	first, firstBody := get("")
	if first.StatusCode != http.StatusOK || first.Header.Get("ETag") == "" {
		t.Fatalf("first = %d with ETag %q, want 200 with an ETag", first.StatusCode, first.Header.Get("ETag"))
	}
	for i := range 20 {
		again, body := get("")
		if body != firstBody || again.Header.Get("ETag") != first.Header.Get("ETag") {
			t.Fatalf("request %d: body or ETag differs from the first\nfirst %s %s\nthis  %s %s", i, first.Header.Get("ETag"), firstBody, again.Header.Get("ETag"), body)
		}
	}
	if cond, _ := get(first.Header.Get("ETag")); cond.StatusCode != http.StatusNotModified {
		t.Fatalf("conditional request = %d, want 304", cond.StatusCode)
	}
}
