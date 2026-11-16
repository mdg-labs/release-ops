package githubtest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
)

// Issue is an issue of a repository the fake serves.
type Issue struct {
	Number   int64
	Title    string
	Body     string
	AuthorID int64
	// State is open or closed.
	State string
}

type watchState struct {
	issues       map[string]map[int64]Issue // owner/name -> number -> issue
	heads        map[string]map[string]string
	notModifieds int
}

// SetIssue adds the issue to the repository (owner/name) or replaces the one with its number.
func (f *Fake) SetIssue(fullName string, issue Issue) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.watch.issues == nil {
		f.watch.issues = map[string]map[int64]Issue{}
	}
	if f.watch.issues[fullName] == nil {
		f.watch.issues[fullName] = map[int64]Issue{}
	}
	f.watch.issues[fullName][issue.Number] = issue
}

// SetBranchHead makes the branch of the repository (owner/name) point at the commit sha.
func (f *Fake) SetBranchHead(fullName, branch, sha string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.watch.heads == nil {
		f.watch.heads = map[string]map[string]string{}
	}
	if f.watch.heads[fullName] == nil {
		f.watch.heads[fullName] = map[string]string{}
	}
	f.watch.heads[fullName][branch] = sha
}

// WatchNotModifiedAnswers returns how many issues listing pages and branch references the fake
// answered 304 to.
func (f *Fake) WatchNotModifiedAnswers() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.watch.notModifieds
}

// answerConditional answers 304 when the request's If-None-Match is the ETag of body, and sets the
// ETag header otherwise. The caller holds the lock.
func (f *Fake) answerConditional(w http.ResponseWriter, r *http.Request, body []byte) bool {
	sum := sha256.Sum256(body)
	etag := `W/"` + hex.EncodeToString(sum[:8]) + `"`
	w.Header().Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		f.watch.notModifieds++
		w.WriteHeader(http.StatusNotModified)
		return true
	}
	return false
}

// serveIssues answers GET /repos/{owner}/{repo}/issues, which lists the open issues newest
// first with an ETag per page. The caller holds the lock.
func (f *Fake) serveIssues(w http.ResponseWriter, r *http.Request, fullName string) {
	var open []Issue
	for _, issue := range f.watch.issues[fullName] {
		if issue.State == "open" {
			open = append(open, issue)
		}
	}
	slices.SortFunc(open, func(a, b Issue) int { return int(b.Number - a.Number) })
	size := f.pageSize
	if size <= 0 {
		size = max(len(open), 1)
	}
	n, _ := strconv.Atoi(r.URL.Query().Get("page"))
	n = max(n, 1)
	from := min((n-1)*size, len(open))
	to := min(from+size, len(open))
	docs := make([]map[string]any, 0, to-from)
	for _, issue := range open[from:to] {
		docs = append(docs, map[string]any{
			"number": issue.Number, "title": issue.Title, "body": issue.Body, "state": issue.State,
			"user": map[string]any{"id": issue.AuthorID},
		})
	}
	// The issues API lists pull requests among the issues.
	for _, pr := range f.pulls[fullName] {
		if pr.State == "open" && n == 1 {
			docs = append(docs, map[string]any{
				"number": pr.Number, "title": pr.Title, "body": pr.Body, "state": pr.State,
				"user": map[string]any{"id": pr.AuthorID}, "pull_request": map[string]any{},
			})
		}
	}
	body, _ := json.Marshal(docs)
	if to < len(open) {
		w.Header().Set("Link", fmt.Sprintf(`<%s%s?state=open&per_page=100&page=%d>; rel="next"`, f.srv.URL, r.URL.Path, n+1))
	}
	if f.answerConditional(w, r, body) {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}

// serveBranchRef answers GET /repos/{owner}/{repo}/git/ref/heads/{branch}. The caller holds the
// lock.
func (f *Fake) serveBranchRef(w http.ResponseWriter, r *http.Request, fullName, branch string) {
	sha, ok := f.watch.heads[fullName][branch]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	body, _ := json.Marshal(map[string]any{"ref": "refs/heads/" + branch, "object": map[string]any{"sha": sha, "type": "commit"}})
	if f.answerConditional(w, r, body) {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}
