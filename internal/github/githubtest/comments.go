package githubtest

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// Comment is a comment on a pull request of a repository the fake serves.
type Comment struct {
	ID     int64
	Body   string
	UserID int64
}

type commentKey struct {
	fullName string
	number   int64
}

type commentFailure struct {
	method string
	status int
	times  int
	// applied carries the call out before it answers, as GitHub that stored the change and then
	// failed to answer.
	applied bool
}

type commentState struct {
	next     int64
	byPR     map[commentKey][]*Comment
	author   int64
	failures []*commentFailure
}

func (c *commentState) init() {
	if c.byPR == nil {
		c.byPR = map[commentKey][]*Comment{}
		c.next = 1000
	}
}

// SetCommentAuthor sets the user id the comments posted through the API are by: the bot's.
func (f *Fake) SetCommentAuthor(id int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.comments.author = id
}

// AddComment adds a comment by userID to the pull request and returns its id.
func (f *Fake) AddComment(fullName string, number, userID int64, body string) int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.comments.init()
	f.comments.next++
	key := commentKey{fullName, number}
	f.comments.byPR[key] = append(f.comments.byPR[key], &Comment{ID: f.comments.next, Body: body, UserID: userID})
	return f.comments.next
}

// DeleteComment removes the comment as a person would; it reports whether there was one.
func (f *Fake) DeleteComment(fullName string, number, id int64) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := commentKey{fullName, number}
	for i, c := range f.comments.byPR[key] {
		if c.ID == id {
			f.comments.byPR[key] = append(f.comments.byPR[key][:i], f.comments.byPR[key][i+1:]...)
			return true
		}
	}
	return false
}

// Comments returns the comments of the pull request, oldest first.
func (f *Fake) Comments(fullName string, number int64) []Comment {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []Comment
	for _, c := range f.comments.byPR[commentKey{fullName, number}] {
		out = append(out, *c)
	}
	return out
}

// FailComments makes the next times comment calls with the method (GET, POST or PATCH) answer
// status. With applied a POST or PATCH is carried out first.
func (f *Fake) FailComments(method string, status, times int, applied bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.comments.failures = append(f.comments.failures, &commentFailure{method: method, status: status, times: times, applied: applied})
}

func (f *Fake) commentDoc(fullName string, number int64, c *Comment) map[string]any {
	var doc map[string]any
	_ = json.Unmarshal(issueCommentFixture, &doc)
	doc["id"] = c.ID
	doc["body"] = c.Body
	doc["user"].(map[string]any)["id"] = c.UserID
	doc["html_url"] = fmt.Sprintf("https://github.com/%s/pull/%d#issuecomment-%d", fullName, number, c.ID)
	doc["url"] = fmt.Sprintf("https://api.github.com/repos/%s/issues/comments/%d", fullName, c.ID)
	doc["updated_at"] = f.now().UTC().Format(time.RFC3339)
	return doc
}

// serveComments answers the comment calls of the issues API: GET and POST
// /repos/{owner}/{repo}/issues/{number}/comments and PATCH .../issues/comments/{id}; rest is the
// path after issues. The caller holds the lock.
func (f *Fake) serveComments(w http.ResponseWriter, r *http.Request, fullName string, rest []string) {
	f.comments.init()
	var raw struct {
		Body string `json:"body"`
	}
	var failure *commentFailure
	for _, fl := range f.comments.failures {
		if fl.times > 0 && fl.method == r.Method {
			fl.times--
			failure = fl
			break
		}
	}
	if failure != nil && !failure.applied {
		w.WriteHeader(failure.status)
		return
	}
	finish := func(status int, doc any) {
		if failure != nil {
			w.WriteHeader(failure.status)
			return
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(doc)
	}

	switch {
	case len(rest) == 2 && rest[1] == "comments":
		number, err := strconv.ParseInt(rest[0], 10, 64)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		key := commentKey{fullName, number}
		switch r.Method {
		case http.MethodGet:
			list := f.comments.byPR[key]
			entries := make([]any, len(list))
			for i, c := range list {
				entries[i] = f.commentDoc(fullName, number, c)
			}
			f.page(w, r, entries, func(e []any) any { return e })
		case http.MethodPost:
			if _, ok := f.pulls[fullName][number]; !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			if !decode(r, &raw) {
				w.WriteHeader(http.StatusUnprocessableEntity)
				return
			}
			f.comments.next++
			c := &Comment{ID: f.comments.next, Body: raw.Body, UserID: f.comments.author}
			f.comments.byPR[key] = append(f.comments.byPR[key], c)
			finish(http.StatusCreated, f.commentDoc(fullName, number, c))
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	case len(rest) == 2 && rest[0] == "comments" && r.Method == http.MethodPatch:
		id, err := strconv.ParseInt(rest[1], 10, 64)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if !decode(r, &raw) {
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		}
		for key, list := range f.comments.byPR {
			if key.fullName != fullName {
				continue
			}
			for _, c := range list {
				if c.ID == id {
					c.Body = raw.Body
					finish(http.StatusOK, f.commentDoc(fullName, key.number, c))
					return
				}
			}
		}
		w.WriteHeader(http.StatusNotFound)
	default:
		http.NotFound(w, r)
	}
}

// decode reads the JSON body of the request, which serve has put back after recording it.
func decode(r *http.Request, v any) bool {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	return err == nil && json.Unmarshal(raw, v) == nil
}
