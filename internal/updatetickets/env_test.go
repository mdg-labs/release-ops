package updatetickets_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/crypto"
	"github.com/mdg-labs/release-ops/internal/providers/ticket"
	"github.com/mdg-labs/release-ops/internal/store"
	"github.com/mdg-labs/release-ops/internal/store/storetest"
	"github.com/mdg-labs/release-ops/internal/updatetickets"
)

const testKeyHex = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

var epoch = time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)

// tracker is a fake Kaneo: it keeps the tasks it was given, records every request and answers as
// status says. A task is created by POST /task/{project}, read by GET /task/{id} and changed by
// the PUT and POST routes of the Kaneo provider.
type tracker struct {
	srv *httptest.Server

	mu       sync.Mutex
	requests []trackerRequest
	// status answers the nth request (1-based); 0 or 200 is a stored task.
	status func(n int) int
	tasks  map[string]*task
	// failures are consumed in order by the requests they match.
	failures []*trackerFailure
}

// task is a ticket in the fake.
type task struct {
	ID, Title, Description, Status string
	Comments                       []string
}

type trackerRequest struct {
	Method      string
	Path        string
	Title       string
	Description string
	Status      string
	Content     string
}

// trackerFailure makes the next Times requests whose method and path prefix match answer Code. With
// Applied the request is carried out first, as a ticket system that stored the change and then
// failed to answer.
type trackerFailure struct {
	Method, PathPrefix string
	Code, Times        int
	Applied            bool
	// Header is sent with the failed answer, such as Retry-After.
	Header http.Header
}

func newTracker(t *testing.T) *tracker {
	t.Helper()
	tr := &tracker{status: func(int) int { return http.StatusOK }, tasks: map[string]*task{}}
	tr.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body struct{ Title, Description, Status, Content string }
		_ = json.Unmarshal(raw, &body)
		tr.mu.Lock()
		defer tr.mu.Unlock()
		tr.requests = append(tr.requests, trackerRequest{Method: r.Method, Path: r.URL.Path, Title: body.Title, Description: body.Description, Status: body.Status, Content: body.Content})
		n := len(tr.requests)
		code := tr.status(n)
		applied := false
		var header http.Header
		for _, f := range tr.failures {
			if f.Times > 0 && f.Method == r.Method && strings.HasPrefix(r.URL.Path, f.PathPrefix) {
				f.Times--
				code, applied, header = f.Code, f.Applied, f.Header
				break
			}
		}
		for name, values := range header {
			w.Header()[name] = values
		}
		if code != http.StatusOK && !applied {
			http.Error(w, "tracker says no", code)
			return
		}
		resp := tr.handle(r.Method, strings.TrimPrefix(r.URL.Path, "/api"), body.Title, body.Description, body.Status, body.Content)
		if code != http.StatusOK {
			http.Error(w, "tracker says no", code)
			return
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(tr.srv.Close)
	return tr
}

// handle carries out a request on the tasks and returns the answer; tr.mu is held.
func (tr *tracker) handle(method, path, title, description, status, content string) map[string]string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	switch {
	case method == http.MethodPost && len(parts) == 2 && parts[0] == "task":
		id := fmt.Sprintf("task-%d", len(tr.tasks)+1)
		tr.tasks[id] = &task{ID: id, Title: title, Description: description, Status: status}
		return map[string]string{"id": id, "status": status}
	case method == http.MethodGet && len(parts) == 2 && parts[0] == "task":
		if t := tr.tasks[parts[1]]; t != nil {
			return map[string]string{"id": t.ID, "status": t.Status}
		}
	case method == http.MethodPut && len(parts) == 3 && parts[0] == "task":
		if t := tr.tasks[parts[2]]; t != nil {
			switch parts[1] {
			case "status":
				t.Status = status
			case "title":
				t.Title = title
			case "description":
				t.Description = description
			}
		}
	case method == http.MethodPost && len(parts) == 2 && parts[0] == "comment":
		if t := tr.tasks[parts[1]]; t != nil {
			t.Comments = append(t.Comments, content)
		}
	}
	return map[string]string{}
}

// task returns a copy of the task.
func (tr *tracker) task(id string) task {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	t := tr.tasks[id]
	if t == nil {
		return task{}
	}
	cp := *t
	cp.Comments = append([]string(nil), t.Comments...)
	return cp
}

// setTaskStatus moves a task as a person would, outside Release Ops.
func (tr *tracker) setTaskStatus(id, status string) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.tasks[id].Status = status
}

// fail adds a failure for the requests that match.
func (tr *tracker) fail(f trackerFailure) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.failures = append(tr.failures, &f)
}

// writes are the recorded requests that change something: everything but the reads.
func (tr *tracker) writes() []trackerRequest {
	var out []trackerRequest
	for _, r := range tr.calls() {
		if r.Method != http.MethodGet {
			out = append(out, r)
		}
	}
	return out
}

func (tr *tracker) calls() []trackerRequest {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	return append([]trackerRequest(nil), tr.requests...)
}

func (tr *tracker) setStatus(f func(n int) int) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.status = f
}

type targets struct{ tr *tracker }

func (g targets) Target(_ context.Context, _ string) (updatetickets.Target, error) {
	p, err := ticket.NewKaneoProvider(g.tr.srv.URL, "test-key", g.tr.srv.Client())
	return updatetickets.Target{Provider: p, Kind: ticket.IntegrationKindKaneo}, err
}

type env struct {
	db      *store.DB
	st      *store.Store
	tracker *tracker
	engine  *updatetickets.Engine
	now     time.Time
	nowMu   sync.Mutex
	// repo is the repository PRs are added to; project the default ticket project.
	repo, project string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	db := storetest.OpenMigrated(t)
	cipher, err := crypto.NewCipherFromHex(testKeyHex)
	if err != nil {
		t.Fatal(err)
	}
	st := store.New(db, cipher)
	repos := storetest.SeedRepositories(t, db, 2)
	e := &env{db: db, st: st, tracker: newTracker(t), repo: repos[0], now: epoch}
	e.project = e.addProject(t, "DEV", store.CreateTicketProjectInput{})
	if err := st.InstallationSettings().SetDefaultTicketProject(context.Background(), "inst-1", e.project); err != nil {
		t.Fatal(err)
	}
	e.engine = &updatetickets.Engine{Tickets: st.UpdateTickets(), Projects: st.TicketProjects(), Targets: targets{e.tracker}, Now: e.clock}
	return e
}

func (e *env) clock() time.Time {
	e.nowMu.Lock()
	defer e.nowMu.Unlock()
	return e.now
}

func (e *env) advance(d time.Duration) {
	e.nowMu.Lock()
	defer e.nowMu.Unlock()
	e.now = e.now.Add(d)
}

// addProject creates a Kaneo ticket project whose external project is key.
func (e *env) addProject(t *testing.T, key string, in store.CreateTicketProjectInput) string {
	t.Helper()
	ctx := context.Background()
	url := e.tracker.srv.URL
	integ, err := e.st.Integrations().Create(ctx, store.CreateIntegrationInput{Kind: "kaneo", Name: "Kaneo " + key, BaseURL: &url, Secret: []byte(`{"api_key":"k"}`)})
	if err != nil {
		t.Fatal(err)
	}
	in.IntegrationID, in.ExternalProjectID, in.Name = integ.ID, key, "Kaneo "+key
	in.CreateConfig = `{"workspaceId":"ws-1","status":"ready","priority":"medium"}`
	if in.StatusMapping == "" {
		in.StatusMapping = `{"open":["ready"],"done":["done"],"cancelled":["cancelled"],"superseded":"cancelled"}`
	}
	if in.ContentTemplates == "" {
		in.ContentTemplates = `{"title":"","description":"","updateComment":"","supersedeComment":""}`
	}
	tp, err := e.st.TicketProjects().Create(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	return tp.ID
}

// pr is an update PR to insert.
type pr struct {
	ID        string
	Number    int64
	Repo      string
	State     string
	Labels    []string
	Automerge *bool
	Notes     string
	Deps      []dep
}

type dep struct{ Name, From, To, Type string }

// addPR inserts the PR and its dependencies and returns its id.
func (e *env) addPR(t *testing.T, p pr) string {
	t.Helper()
	if p.Number == 0 {
		p.Number = 21
	}
	if p.ID == "" {
		p.ID = fmt.Sprintf("pr-%d", p.Number)
	}
	if p.Repo == "" {
		p.Repo = e.repo
	}
	if p.State == "" {
		p.State = "open"
	}
	if p.Labels == nil {
		p.Labels = []string{}
	}
	labels, _ := json.Marshal(p.Labels)
	var automerge any
	if p.Automerge != nil {
		automerge = 0
		if *p.Automerge {
			automerge = 1
		}
	}
	at := "2026-11-15T10:00:00.000Z"
	_, err := e.db.Writer.Exec(`INSERT INTO update_prs (id, repository_id, number, node_id, url, title, head_ref, state, labels, automerge, release_notes, last_seen_at, created_at, updated_at)
		VALUES (?, ?, ?, 'PR_n', ?, 'Update dependency foo to v2', 'renovate/foo', ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Repo, p.Number, fmt.Sprintf("https://github.com/acme/repo-1/pull/%d", p.Number), p.State, string(labels), automerge, nullable(p.Notes), at, at, at)
	if err != nil {
		t.Fatalf("insert update PR: %v", err)
	}
	for i, d := range p.Deps {
		_, err := e.db.Writer.Exec(`INSERT INTO update_pr_dependencies (id, update_pr_id, name, from_version, to_version, update_type, source)
			VALUES (?, ?, ?, ?, ?, ?, 'report')`, fmt.Sprintf("%s-d%d", p.ID, i), p.ID, d.Name, nullable(d.From), nullable(d.To), nullable(d.Type))
		if err != nil {
			t.Fatalf("insert dependency: %v", err)
		}
	}
	return p.ID
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func boolp(b bool) *bool { return &b }

var foo = []dep{{Name: "foo", From: "1.4.0", To: "2.0.0", Type: "major"}}

// ticketRow is the update_tickets row of a PR.
type ticketRow struct {
	ID, ProjectID, State                  string
	ExternalID, ExternalURL, PendingSince sql.NullString
	SyncedHash, LastError, NextAttemptAt  sql.NullString
	TargetState, HandsOffAt               sql.NullString
	Attempts                              int
}

func (e *env) row(t *testing.T, prID string) (ticketRow, bool) {
	t.Helper()
	var r ticketRow
	err := e.db.Reader.QueryRow(`SELECT id, ticket_project_id, state, external_id, external_url, pending_since, synced_hash, last_error, next_attempt_at, attempts, target_state, hands_off_at
		FROM update_tickets WHERE update_pr_id = ?`, prID).Scan(&r.ID, &r.ProjectID, &r.State, &r.ExternalID, &r.ExternalURL, &r.PendingSince, &r.SyncedHash, &r.LastError, &r.NextAttemptAt, &r.Attempts, &r.TargetState, &r.HandsOffAt)
	if err == sql.ErrNoRows {
		return ticketRow{}, false
	}
	if err != nil {
		t.Fatal(err)
	}
	return r, true
}

func (e *env) rowCount(t *testing.T) int {
	t.Helper()
	var n int
	if err := e.db.Reader.QueryRow(`SELECT COUNT(*) FROM update_tickets`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (e *env) exec(t *testing.T, query string, args ...any) {
	t.Helper()
	if _, err := e.db.Writer.Exec(query, args...); err != nil {
		t.Fatalf("%s: %v", strings.TrimSpace(query), err)
	}
}
