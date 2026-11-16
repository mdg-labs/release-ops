package githubtest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
)

// PullRequest is a pull request of a repository the fake serves.
type PullRequest struct {
	Number   int64
	Title    string
	Body     string
	AuthorID int64
	HeadRef  string
	HeadSHA  string
	// State is open or closed.
	State  string
	Merged bool
	Labels []string
	// CreatedAt and UpdatedAt are required; ClosedAt and MergedAt are zero for none.
	CreatedAt, UpdatedAt, ClosedAt, MergedAt time.Time
}

func (p PullRequest) doc(fullName string) map[string]any {
	stamp := func(t time.Time) any {
		if t.IsZero() {
			return nil
		}
		return t.UTC().Format(time.RFC3339)
	}
	labels := make([]map[string]any, len(p.Labels))
	for i, name := range p.Labels {
		labels[i] = map[string]any{"id": i + 1, "name": name}
	}
	return map[string]any{
		"number":     p.Number,
		"node_id":    fmt.Sprintf("PR_fixtureNodeId%d", p.Number),
		"html_url":   fmt.Sprintf("https://github.com/%s/pull/%d", fullName, p.Number),
		"title":      p.Title,
		"body":       p.Body,
		"state":      p.State,
		"merged":     p.Merged,
		"created_at": stamp(p.CreatedAt),
		"updated_at": stamp(p.UpdatedAt),
		"closed_at":  stamp(p.ClosedAt),
		"merged_at":  stamp(p.MergedAt),
		"user":       map[string]any{"id": p.AuthorID},
		"head":       map[string]any{"ref": p.HeadRef, "sha": p.HeadSHA},
		"labels":     labels,
	}
}

// SetPullRequest adds the pull request to the repository (owner/name) or replaces the one with
// its number.
func (f *Fake) SetPullRequest(fullName string, pr PullRequest) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.pulls[fullName] == nil {
		f.pulls[fullName] = map[int64]PullRequest{}
	}
	f.pulls[fullName][pr.Number] = pr
}

// FailPulls makes every pulls call of the repository answer status; 0 restores normal answers.
func (f *Fake) FailPulls(fullName string, status int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pullStatus[fullName] = status
}

// FailPullRequest makes the fetch of one pull request (GET .../pulls/{number}) answer status;
// 0 restores normal answers. The listing is not affected.
func (f *Fake) FailPullRequest(fullName string, number int64, status int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.pullFetchStatus == nil {
		f.pullFetchStatus = map[string]map[int64]int{}
	}
	if f.pullFetchStatus[fullName] == nil {
		f.pullFetchStatus[fullName] = map[int64]int{}
	}
	f.pullFetchStatus[fullName][number] = status
}

// NotModifiedAnswers returns how many pulls listing pages the fake answered 304 to.
func (f *Fake) NotModifiedAnswers() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.notModifieds
}

// servePulls answers GET /repos/{owner}/{repo}/pulls, which lists the open pull requests newest
// first with an ETag per page, and GET /repos/{owner}/{repo}/pulls/{number}; rest is the path
// after pulls. The caller holds the lock.
func (f *Fake) servePulls(w http.ResponseWriter, r *http.Request, fullName string, rest []string) {
	if status := f.pullStatus[fullName]; status != 0 {
		w.WriteHeader(status)
		return
	}
	all := f.pulls[fullName]
	if r.Method == http.MethodPost && len(rest) == 0 {
		f.createPull(w, r, fullName)
		return
	}
	if len(rest) == 1 {
		number, err := strconv.ParseInt(rest[0], 10, 64)
		if status := f.pullFetchStatus[fullName][number]; status != 0 {
			w.WriteHeader(status)
			return
		}
		pr, ok := all[number]
		if err != nil || !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(pr.doc(fullName))
		return
	}
	if len(rest) != 0 {
		http.NotFound(w, r)
		return
	}

	// A head of owner:branch keeps the pull requests from that branch, as GitHub does.
	_, headBranch, filtered := strings.Cut(r.URL.Query().Get("head"), ":")
	var open []PullRequest
	for _, pr := range all {
		if pr.State == "open" && (!filtered || pr.HeadRef == headBranch) {
			open = append(open, pr)
		}
	}
	slices.SortFunc(open, func(a, b PullRequest) int { return int(b.Number - a.Number) })
	size := f.pageSize
	if size <= 0 {
		size = max(len(open), 1)
	}
	n, _ := strconv.Atoi(r.URL.Query().Get("page"))
	n = max(n, 1)
	from := min((n-1)*size, len(open))
	to := min(from+size, len(open))
	docs := make([]map[string]any, 0, to-from)
	for _, pr := range open[from:to] {
		docs = append(docs, pr.doc(fullName))
	}
	body, _ := json.Marshal(docs)
	sum := sha256.Sum256(body)
	etag := `W/"` + hex.EncodeToString(sum[:8]) + `"`
	if to < len(open) {
		w.Header().Set("Link", fmt.Sprintf(`<%s%s?state=open&per_page=100&page=%d>; rel="next"`, f.srv.URL, r.URL.Path, n+1))
	}
	w.Header().Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		f.notModifieds++
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}
