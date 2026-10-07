package handlers_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alexedwards/scs/v2"
	"github.com/alexedwards/scs/v2/memstore"
	"github.com/go-chi/chi/v5"
	"github.com/mdg-labs/release-ops/internal/api/auth"
	"github.com/mdg-labs/release-ops/internal/api/handlers"
	apimw "github.com/mdg-labs/release-ops/internal/api/middleware"
	"github.com/mdg-labs/release-ops/internal/store"
)

var validPollRunActions = map[string]struct{}{
	"baseline":  {},
	"skip":      {},
	"create":    {},
	"supersede": {},
	"merge":     {},
	"skip_open": {},
	"error":     {},
}

type mockPollRepo struct {
	runs   map[string]*store.PollRun
	events map[string][]store.PollRunEvent
}

func (m *mockPollRepo) UpdatePollState(_ context.Context, _ string, _ store.PollStateUpdate) (*store.MonitoredRepo, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockPollRepo) InsertRun(_ context.Context, _ string) (*store.PollRun, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockPollRepo) FinishRun(_ context.Context, _ string, _ string, _, _, _ int64, _ string) (*store.PollRun, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockPollRepo) GetRun(_ context.Context, id string) (*store.PollRun, error) {
	run, ok := m.runs[id]
	if !ok {
		return nil, sql.ErrNoRows
	}
	return run, nil
}

func (m *mockPollRepo) ListRuns(_ context.Context, limit, offset int64) ([]store.PollRun, error) {
	all := make([]store.PollRun, 0, len(m.runs))
	for _, run := range m.runs {
		all = append(all, *run)
	}
	if offset >= int64(len(all)) {
		return []store.PollRun{}, nil
	}
	end := offset + limit
	if end > int64(len(all)) {
		end = int64(len(all))
	}
	return all[offset:end], nil
}

func (m *mockPollRepo) InsertEvent(_ context.Context, _ string, _ *string, _ string, _ *string) (*store.PollRunEvent, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockPollRepo) ListEventsByRunID(_ context.Context, pollRunID string) ([]store.PollRunEvent, error) {
	return m.events[pollRunID], nil
}

type mockPollRunner struct {
	triggerFn func(ctx context.Context) (string, error)
	isPolling bool
}

func (m *mockPollRunner) Trigger(ctx context.Context) (string, error) {
	if m.triggerFn != nil {
		return m.triggerFn(ctx)
	}
	return "run-new", nil
}

func (m *mockPollRunner) IsPolling() bool {
	return m.isPolling
}

func newPollTestRouter(t *testing.T, h *handlers.PollHandlers) (http.Handler, *scs.SessionManager) {
	t.Helper()

	sm := scs.New()
	sm.Store = memstore.New()
	sm.Cookie.Name = auth.SessionCookieName

	r := chi.NewRouter()
	r.Use(sm.LoadAndSave)
	r.Group(func(protected chi.Router) {
		protected.Use(apimw.RequireSession(sm))
		protected.Post("/api/v1/poll/trigger", h.Trigger)
		protected.Get("/api/v1/poll/runs", h.ListRuns)
		protected.Get("/api/v1/poll/runs/{id}", h.GetRun)
	})
	return r, sm
}

func TestTriggerPollReturnsAccepted(t *testing.T) {
	t.Parallel()

	h := &handlers.PollHandlers{
		Poll:   &mockPollRepo{runs: map[string]*store.PollRun{}},
		Runner: &mockPollRunner{},
	}
	router, sm := newPollTestRouter(t, h)
	cookie := seedSession(t, sm)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/poll/trigger", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusAccepted, rec.Body.String())
	}

	var resp struct {
		RunID string `json:"runId"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.RunID != "run-new" {
		t.Fatalf("runId = %q, want run-new", resp.RunID)
	}
}

func TestTriggerPollReturnsConflictWhenAlreadyRunning(t *testing.T) {
	t.Parallel()

	h := &handlers.PollHandlers{
		Poll: &mockPollRepo{runs: map[string]*store.PollRun{}},
		Runner: &mockPollRunner{
			triggerFn: func(_ context.Context) (string, error) {
				return "", handlers.ErrPollAlreadyRunning
			},
		},
	}
	router, sm := newPollTestRouter(t, h)
	cookie := seedSession(t, sm)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/poll/trigger", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusConflict, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"code":"CONFLICT"`) {
		t.Fatalf("body = %s, want CONFLICT error", rec.Body.String())
	}
}

func TestListPollRunsDefaultsToLimit20(t *testing.T) {
	t.Parallel()

	runs := make(map[string]*store.PollRun, 25)
	for i := 0; i < 25; i++ {
		id := fmt.Sprintf("run-%02d", i)
		runs[id] = &store.PollRun{
			ID:         id,
			StartedAt:  fmt.Sprintf("2026-08-07T%02d:00:00.000Z", i),
			Status:     "success",
			TriggerSource: store.PollTriggerSourceScheduled,
			ErrorsJSON: "[]",
		}
	}

	h := &handlers.PollHandlers{
		Poll:   &mockPollRepo{runs: runs},
		Runner: &mockPollRunner{},
	}
	router, sm := newPollTestRouter(t, h)
	cookie := seedSession(t, sm)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/poll/runs", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp []struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Events []any  `json:"events"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp) != 20 {
		t.Fatalf("runs len = %d, want default limit 20", len(resp))
	}
	for _, item := range resp {
		if item.Events != nil {
			t.Fatalf("list item %s includes events, want omitted", item.ID)
		}
	}
}

func TestListPollRunsSupportsLimitAndOffset(t *testing.T) {
	t.Parallel()

	runs := map[string]*store.PollRun{
		"run-1": {ID: "run-1", StartedAt: "2026-08-07T10:00:00.000Z", Status: "success", TriggerSource: store.PollTriggerSourceManual, ErrorsJSON: "[]"},
		"run-2": {ID: "run-2", StartedAt: "2026-08-07T11:00:00.000Z", Status: "failed", TriggerSource: store.PollTriggerSourceScheduled, ErrorsJSON: "[]"},
	}

	h := &handlers.PollHandlers{
		Poll:   &mockPollRepo{runs: runs},
		Runner: &mockPollRunner{},
	}
	router, sm := newPollTestRouter(t, h)
	cookie := seedSession(t, sm)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/poll/runs?limit=1&offset=1", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp []struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp) != 1 {
		t.Fatalf("runs len = %d, want 1", len(resp))
	}
}

func TestListPollRunsRejectsInvalidLimit(t *testing.T) {
	t.Parallel()

	h := &handlers.PollHandlers{
		Poll:   &mockPollRepo{runs: map[string]*store.PollRun{}},
		Runner: &mockPollRunner{},
	}
	router, sm := newPollTestRouter(t, h)
	cookie := seedSession(t, sm)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/poll/runs?limit=0", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestGetPollRunIncludesEventsWithActionEnum(t *testing.T) {
	t.Parallel()

	repoID := "repo-1"
	detail := "created ticket"
	h := &handlers.PollHandlers{
		Poll: &mockPollRepo{
			runs: map[string]*store.PollRun{
				"run-1": {
					ID:                "run-1",
					StartedAt:         "2026-08-07T10:00:00.000Z",
					Status:            "success",
					TriggerSource:     store.PollTriggerSourceManual,
					ReposChecked:      1,
					TicketsCreated:    1,
					TicketsSuperseded: 0,
					ErrorsJSON:        "[]",
				},
			},
			events: map[string][]store.PollRunEvent{
				"run-1": {
					{
						ID:              "evt-1",
						PollRunID:       "run-1",
						MonitoredRepoID: &repoID,
						Action:          "baseline",
						CreatedAt:       "2026-08-07T10:00:01.000Z",
					},
					{
						ID:              "evt-2",
						PollRunID:       "run-1",
						MonitoredRepoID: &repoID,
						Action:          "create",
						Detail:          &detail,
						CreatedAt:       "2026-08-07T10:00:02.000Z",
					},
				},
			},
		},
		Runner: &mockPollRunner{},
	}
	router, sm := newPollTestRouter(t, h)
	cookie := seedSession(t, sm)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/poll/runs/run-1", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp struct {
		ID     string `json:"id"`
		Events []struct {
			Action string `json:"action"`
		} `json:"events"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Events) != 2 {
		t.Fatalf("events len = %d, want 2", len(resp.Events))
	}
	for _, event := range resp.Events {
		if _, ok := validPollRunActions[event.Action]; !ok {
			t.Fatalf("action = %q, want valid poll_run_events.action enum value", event.Action)
		}
	}
}

func TestGetPollRunIncludesTriggerSource(t *testing.T) {
	t.Parallel()

	h := &handlers.PollHandlers{
		Poll: &mockPollRepo{
			runs: map[string]*store.PollRun{
				"run-1": {
					ID:            "run-1",
					StartedAt:     "2026-08-07T10:00:00.000Z",
					Status:        "success",
					TriggerSource: store.PollTriggerSourceManual,
					ErrorsJSON:    "[]",
				},
			},
		},
		Runner: &mockPollRunner{},
	}
	router, sm := newPollTestRouter(t, h)
	cookie := seedSession(t, sm)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/poll/runs/run-1", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp struct {
		TriggerSource string `json:"triggerSource"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.TriggerSource != store.PollTriggerSourceManual {
		t.Fatalf("triggerSource = %q, want %q", resp.TriggerSource, store.PollTriggerSourceManual)
	}
}

func TestGetPollRunReturnsNotFound(t *testing.T) {
	t.Parallel()

	h := &handlers.PollHandlers{
		Poll:   &mockPollRepo{runs: map[string]*store.PollRun{}},
		Runner: &mockPollRunner{},
	}
	router, sm := newPollTestRouter(t, h)
	cookie := seedSession(t, sm)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/poll/runs/missing", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestPollEndpointsRequireSession(t *testing.T) {
	t.Parallel()

	h := &handlers.PollHandlers{
		Poll:   &mockPollRepo{runs: map[string]*store.PollRun{}},
		Runner: &mockPollRunner{},
	}
	router, _ := newPollTestRouter(t, h)

	tests := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/v1/poll/trigger"},
		{http.MethodGet, "/api/v1/poll/runs"},
		{http.MethodGet, "/api/v1/poll/runs/run-1"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
			}
		})
	}
}
