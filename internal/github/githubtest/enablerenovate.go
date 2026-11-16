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

// BranchCommit is a commit a branch holds on top of the default branch.
type BranchCommit struct {
	Message string
	Files   []string
	// RenamedFrom maps a file the commit renamed to the path it had before.
	RenamedFrom map[string]string
}

type writeState struct {
	// ahead lists, per repository (owner/name) and branch, the commits the branch has that the
	// default branch has not, oldest first.
	ahead map[string]map[string][]BranchCommit
	// compareStatus makes a branch comparison of the repository answer that status.
	compareStatus map[string]int
	files         map[string]map[string]BranchFile // owner/name -> "branch:path" -> file
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

// AddBranchCommit adds a commit with the message that changes the files to the branch of the
// repository (owner/name), which must exist, as one the default branch does not have.
func (f *Fake) AddBranchCommit(fullName, branch, message string, files ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.addAhead(fullName, branch, BranchCommit{Message: message, Files: files})
	f.watch.heads[fullName][branch] = fmt.Sprintf("%s-ahead-%d", f.watch.heads[fullName][branch], len(f.writes.ahead[fullName][branch]))
}

// BranchCommits returns the commits the branch of the repository (owner/name) has that the
// default branch has not.
func (f *Fake) BranchCommits(fullName, branch string) []BranchCommit {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]BranchCommit(nil), f.writes.ahead[fullName][branch]...)
}

// FailBranchComparison makes comparing two branches of the repository answer status; 0
// restores normal answers.
func (f *Fake) FailBranchComparison(fullName string, status int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.writes.compareStatus == nil {
		f.writes.compareStatus = map[string]int{}
	}
	f.writes.compareStatus[fullName] = status
}

// AddRenamedBranchCommit adds a commit with the message that renames the file from to the
// branch of the repository (owner/name), which must exist.
func (f *Fake) AddRenamedBranchCommit(fullName, branch, message, from, to string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.addAhead(fullName, branch, BranchCommit{Message: message, Files: []string{to}, RenamedFrom: map[string]string{to: from}})
	f.watch.heads[fullName][branch] = fmt.Sprintf("%s-ahead-%d", f.watch.heads[fullName][branch], len(f.writes.ahead[fullName][branch]))
}

func (f *Fake) addAhead(fullName, branch string, c BranchCommit) {
	if f.writes.ahead == nil {
		f.writes.ahead = map[string]map[string][]BranchCommit{}
	}
	if f.writes.ahead[fullName] == nil {
		f.writes.ahead[fullName] = map[string][]BranchCommit{}
	}
	f.writes.ahead[fullName][branch] = append(f.writes.ahead[fullName][branch], c)
}

// serveCompare answers GET /repos/{owner}/{repo}/compare/{base}...{head}: the commits the head
// has that the base has not, and the files they changed. The caller holds the lock.
func (f *Fake) serveCompare(w http.ResponseWriter, r *http.Request, fullName, spec string) {
	if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	if status := f.writes.compareStatus[fullName]; status != 0 {
		w.WriteHeader(status)
		return
	}
	base, head, ok := strings.Cut(spec, "...")
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if _, ok := f.watch.heads[fullName][base]; !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if _, ok := f.watch.heads[fullName][head]; !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	commits := []map[string]any{}
	files := []map[string]any{}
	seen := map[string]bool{}
	for i, c := range f.writes.ahead[fullName][head] {
		commits = append(commits, map[string]any{"sha": fmt.Sprintf("commit-%d", i), "commit": map[string]any{"message": c.Message}})
		for _, name := range c.Files {
			if !seen[name] {
				seen[name] = true
				file := map[string]any{"filename": name, "status": "added"}
				if from := c.RenamedFrom[name]; from != "" {
					file["status"] = "renamed"
					file["previous_filename"] = from
				}
				files = append(files, file)
			}
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": "ahead", "ahead_by": len(commits), "behind_by": 0, "total_commits": len(commits),
		"commits": commits, "files": files,
	})
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
		delete(f.writes.ahead[fullName], branch)
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
	f.addAhead(fullName, in.Branch, BranchCommit{Message: in.Message, Files: []string{path}})
	f.watch.heads[fullName][in.Branch] = fmt.Sprintf("%s-%d", sha, len(f.writes.files[fullName]))
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"content": map[string]any{"path": path},
		"commit":  map[string]any{"sha": f.watch.heads[fullName][in.Branch], "message": in.Message, "committer": map[string]any{"date": f.now().UTC().Format(time.RFC3339)}},
	})
}
