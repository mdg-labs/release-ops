package githubtest

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// BranchFile is a file committed to a branch through the contents API.
type BranchFile struct {
	Content, Message string
}

type writeState struct {
	files map[string]map[string]BranchFile // owner/name -> "branch:path" -> file
	// createStatus makes a pull request creation of the repository answer that status.
	createStatus map[string]int
	// putStatus makes a file commit of the repository answer that status.
	putStatus map[string]int
}

// BranchFile returns the file committed to the branch of the repository (owner/name) through
// the contents API.
func (f *Fake) BranchFile(fullName, branch, path string) (BranchFile, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	file, ok := f.writes.files[fullName][branch+":"+path]
	return file, ok
}

// BranchHeadSHA returns the commit the branch of the repository (owner/name) points at.
func (f *Fake) BranchHeadSHA(fullName, branch string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sha, ok := f.watch.heads[fullName][branch]
	return sha, ok
}

// FailPullCreation makes the creation of a pull request in the repository answer status; 0
// restores normal answers. Listing and fetching pull requests are not affected.
func (f *Fake) FailPullCreation(fullName string, status int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.writes.createStatus == nil {
		f.writes.createStatus = map[string]int{}
	}
	f.writes.createStatus[fullName] = status
}

// FailFileCommit makes committing a file to the repository answer status; 0 restores normal
// answers.
func (f *Fake) FailFileCommit(fullName string, status int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.writes.putStatus == nil {
		f.writes.putStatus = map[string]int{}
	}
	f.writes.putStatus[fullName] = status
}

// createPull answers POST /repos/{owner}/{repo}/pulls. The caller holds the lock.
func (f *Fake) createPull(w http.ResponseWriter, r *http.Request, fullName string) {
	if status := f.writes.createStatus[fullName]; status != 0 {
		w.WriteHeader(status)
		return
	}
	var in struct {
		Title, Body, Head, Base string
	}
	if !decode(r, &in) || in.Title == "" || in.Head == "" || in.Base == "" {
		w.WriteHeader(http.StatusUnprocessableEntity)
		return
	}
	if _, ok := f.watch.heads[fullName][in.Head]; !ok {
		w.WriteHeader(http.StatusUnprocessableEntity)
		return
	}
	var number int64
	for n := range f.pulls[fullName] {
		number = max(number, n)
	}
	number++
	now := f.now()
	pr := PullRequest{
		Number: number, Title: in.Title, Body: in.Body, AuthorID: f.comments.author,
		HeadRef: in.Head, HeadSHA: f.watch.heads[fullName][in.Head], State: "open", CreatedAt: now, UpdatedAt: now,
	}
	if f.pulls[fullName] == nil {
		f.pulls[fullName] = map[int64]PullRequest{}
	}
	f.pulls[fullName][number] = pr
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(pr.doc(fullName))
}

// serveRefs answers POST /repos/{owner}/{repo}/git/refs and PATCH .../git/refs/heads/{branch};
// rest is the path after refs. The caller holds the lock.
func (f *Fake) serveRefs(w http.ResponseWriter, r *http.Request, fullName string, rest []string) {
	if f.watch.heads == nil {
		f.watch.heads = map[string]map[string]string{}
	}
	if f.watch.heads[fullName] == nil {
		f.watch.heads[fullName] = map[string]string{}
	}
	heads := f.watch.heads[fullName]
	switch {
	case r.Method == http.MethodPost && len(rest) == 0:
		var in struct{ Ref, SHA string }
		if !decode(r, &in) {
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		}
		branch, ok := strings.CutPrefix(in.Ref, "refs/heads/")
		if _, exists := heads[branch]; !ok || in.SHA == "" || exists {
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		}
		heads[branch] = in.SHA
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"ref": in.Ref, "object": map[string]any{"sha": in.SHA, "type": "commit"}})
	case r.Method == http.MethodPatch && len(rest) == 1 && strings.HasPrefix(rest[0], "heads/"):
		branch := strings.TrimPrefix(rest[0], "heads/")
		var in struct {
			SHA   string
			Force bool
		}
		if _, exists := heads[branch]; !exists {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if !decode(r, &in) || in.SHA == "" {
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		}
		heads[branch] = in.SHA
		// A reset branch holds none of what it held before.
		for key := range f.writes.files[fullName] {
			if strings.HasPrefix(key, branch+":") {
				delete(f.writes.files[fullName], key)
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ref": "refs/heads/" + branch, "object": map[string]any{"sha": in.SHA, "type": "commit"}})
	default:
		http.NotFound(w, r)
	}
}

// servePutContents answers PUT /repos/{owner}/{repo}/contents/{path}, which commits a new file
// to a branch that exists. The caller holds the lock.
func (f *Fake) servePutContents(w http.ResponseWriter, r *http.Request, fullName, path string) {
	if status := f.writes.putStatus[fullName]; status != 0 {
		w.WriteHeader(status)
		return
	}
	var in struct{ Message, Content, Branch string }
	if !decode(r, &in) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		return
	}
	sha, ok := f.watch.heads[fullName][in.Branch]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	raw, err := base64.StdEncoding.DecodeString(in.Content)
	if err != nil {
		w.WriteHeader(http.StatusUnprocessableEntity)
		return
	}
	key := in.Branch + ":" + path
	if _, exists := f.writes.files[fullName][key]; exists {
		w.WriteHeader(http.StatusUnprocessableEntity)
		return
	}
	if f.writes.files == nil {
		f.writes.files = map[string]map[string]BranchFile{}
	}
	if f.writes.files[fullName] == nil {
		f.writes.files[fullName] = map[string]BranchFile{}
	}
	f.writes.files[fullName][key] = BranchFile{Content: string(raw), Message: in.Message}
	f.watch.heads[fullName][in.Branch] = fmt.Sprintf("%s-%d", sha, len(f.writes.files[fullName]))
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"content": map[string]any{"path": path},
		"commit":  map[string]any{"sha": f.watch.heads[fullName][in.Branch], "message": in.Message, "committer": map[string]any{"date": f.now().UTC().Format(time.RFC3339)}},
	})
}
