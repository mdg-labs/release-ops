package handlers_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alexedwards/scs/v2"
	"github.com/alexedwards/scs/v2/memstore"
	"github.com/go-chi/chi/v5"
	"github.com/mdg-labs/release-ops/internal/api/auth"
	"github.com/mdg-labs/release-ops/internal/api/handlers"
	apimw "github.com/mdg-labs/release-ops/internal/api/middleware"
	"github.com/mdg-labs/release-ops/internal/store"
)

type mockStatusSettingsRepo struct {
	settings *store.AppSettings
}

func (m *mockStatusSettingsRepo) EnsureDefault(context.Context) error {
	return nil
}

func (m *mockStatusSettingsRepo) Get(_ context.Context) (*store.AppSettings, error) {
	return m.settings, nil
}

func (m *mockStatusSettingsRepo) UpdatePollInterval(_ context.Context, pollIntervalMinutes int64) (*store.AppSettings, error) {
	m.settings.PollIntervalMinutes = pollIntervalMinutes
	return m.settings, nil
}

type mockStatusRepoRepo struct {
	items []store.MonitoredRepo
}

func (m *mockStatusRepoRepo) Create(_ context.Context, _ store.CreateMonitoredRepoInput) (*store.MonitoredRepo, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockStatusRepoRepo) Get(_ context.Context, _ string) (*store.MonitoredRepo, error) {
	return nil, sql.ErrNoRows
}

func (m *mockStatusRepoRepo) List(_ context.Context) ([]store.MonitoredRepo, error) {
	return m.items, nil
}

func (m *mockStatusRepoRepo) ListEnabled(_ context.Context) ([]store.MonitoredRepo, error) {
	return nil, nil
}

func (m *mockStatusRepoRepo) Update(_ context.Context, _ string, _ store.UpdateMonitoredRepoInput) (*store.MonitoredRepo, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockStatusRepoRepo) SetEnabled(_ context.Context, _ string, _ bool) (*store.MonitoredRepo, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockStatusRepoRepo) Delete(_ context.Context, _ string) error {
	return fmt.Errorf("not implemented")
}

type mockStatusTicketProjectRepo struct {
	items []store.TicketProject
}

func (m *mockStatusTicketProjectRepo) Create(_ context.Context, _ store.CreateTicketProjectInput) (*store.TicketProject, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockStatusTicketProjectRepo) Get(_ context.Context, _ string) (*store.TicketProject, error) {
	return nil, sql.ErrNoRows
}

func (m *mockStatusTicketProjectRepo) List(_ context.Context) ([]store.TicketProject, error) {
	return m.items, nil
}

func (m *mockStatusTicketProjectRepo) ListByIntegration(_ context.Context, _ string) ([]store.TicketProject, error) {
	return nil, nil
}

func (m *mockStatusTicketProjectRepo) Update(_ context.Context, _ string, _ store.UpdateTicketProjectInput) (*store.TicketProject, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockStatusTicketProjectRepo) Delete(_ context.Context, _ string) error {
	return fmt.Errorf("not implemented")
}

func (m *mockStatusTicketProjectRepo) CountMonitoredRepos(_ context.Context, _ string) (int64, error) {
	return 0, nil
}

type mockStatusPollRepo struct {
	runs []store.PollRun
}

func (m *mockStatusPollRepo) UpdatePollState(_ context.Context, _ string, _ store.PollStateUpdate) (*store.MonitoredRepo, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockStatusPollRepo) InsertRun(_ context.Context) (*store.PollRun, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockStatusPollRepo) FinishRun(_ context.Context, _ string, _ string, _, _, _ int64, _ string) (*store.PollRun, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockStatusPollRepo) GetRun(_ context.Context, _ string) (*store.PollRun, error) {
	return nil, sql.ErrNoRows
}

func (m *mockStatusPollRepo) ListRuns(_ context.Context, limit, offset int64) ([]store.PollRun, error) {
	if offset >= int64(len(m.runs)) {
		return []store.PollRun{}, nil
	}
	end := offset + limit
	if end > int64(len(m.runs)) {
		end = int64(len(m.runs))
	}
	out := make([]store.PollRun, end-offset)
	copy(out, m.runs[offset:end])
	return out, nil
}

func (m *mockStatusPollRepo) InsertEvent(_ context.Context, _ string, _ *string, _ string, _ *string) (*store.PollRunEvent, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockStatusPollRepo) ListEventsByRunID(_ context.Context, _ string) ([]store.PollRunEvent, error) {
	return nil, nil
}

type mockStatusPollRunner struct {
	isPolling bool
}

func (m *mockStatusPollRunner) Trigger(_ context.Context) (string, error) {
	return "", fmt.Errorf("not implemented")
}

func (m *mockStatusPollRunner) IsPolling() bool {
	return m.isPolling
}

func newStatusTestRouter(t *testing.T, h *handlers.StatusHandlers) (http.Handler, *scs.SessionManager) {
	t.Helper()

	sm := scs.New()
	sm.Store = memstore.New()
	sm.Cookie.Name = auth.SessionCookieName

	r := chi.NewRouter()
	r.Use(sm.LoadAndSave)
	r.Group(func(protected chi.Router) {
		protected.Use(apimw.RequireSession(sm))
		protected.Get("/api/v1/status", h.Get)
	})
	return r, sm
}

func TestGetStatusReturnsReposWithTicketProjectName(t *testing.T) {
	t.Parallel()

	openTicketID := "task-uuid"
	openTicketTag := "1.26.0"
	lastKnownTag := "1.26.0"
	lastPolledAt := "2026-08-06T12:00:00.000Z"

	h := &handlers.StatusHandlers{
		Settings: &mockStatusSettingsRepo{
			settings: &store.AppSettings{ID: 1, PollIntervalMinutes: 360},
		},
		Repos: &mockStatusRepoRepo{
			items: []store.MonitoredRepo{
				{
					ID:                   "repo-1",
					SourceKind:           "github",
					ProjectPath:          "FreshRSS/FreshRSS",
					Enabled:              true,
					TicketProjectID:      "tp-1",
					OpenTicketExternalID: &openTicketID,
					OpenTicketTag:        &openTicketTag,
					LastKnownTag:         &lastKnownTag,
					LastPolledAt:         &lastPolledAt,
				},
			},
		},
		TicketProjects: &mockStatusTicketProjectRepo{
			items: []store.TicketProject{
				{ID: "tp-1", Name: "Phasical — Release Ops"},
			},
		},
		Poll: &mockStatusPollRepo{},
	}

	router, sm := newStatusTestRouter(t, h)
	cookie := seedSession(t, sm)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp struct {
		PollIntervalMinutes int64 `json:"pollIntervalMinutes"`
		LastRun             *struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"lastRun"`
		Repos []struct {
			ID                string  `json:"id"`
			TicketProjectID   string  `json:"ticketProjectId"`
			TicketProjectName string  `json:"ticketProjectName"`
			OpenTicketTag     *string `json:"openTicketTag"`
		} `json:"repos"`
		IsPolling bool `json:"isPolling"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.PollIntervalMinutes != 360 {
		t.Fatalf("pollIntervalMinutes = %d, want 360", resp.PollIntervalMinutes)
	}
	if resp.LastRun != nil {
		t.Fatalf("lastRun = %+v, want null", resp.LastRun)
	}
	if len(resp.Repos) != 1 {
		t.Fatalf("repos len = %d, want 1", len(resp.Repos))
	}
	if resp.Repos[0].TicketProjectName != "Phasical — Release Ops" {
		t.Fatalf("ticketProjectName = %q, want %q", resp.Repos[0].TicketProjectName, "Phasical — Release Ops")
	}
	if resp.IsPolling {
		t.Fatal("isPolling = true, want false")
	}
}

func TestGetStatusIncludesLastRunAndIsPolling(t *testing.T) {
	t.Parallel()

	finishedAt := "2026-08-07T11:00:00.000Z"
	h := &handlers.StatusHandlers{
		Settings: &mockStatusSettingsRepo{
			settings: &store.AppSettings{ID: 1, PollIntervalMinutes: 120},
		},
		Repos:          &mockStatusRepoRepo{},
		TicketProjects: &mockStatusTicketProjectRepo{},
		Poll: &mockStatusPollRepo{
			runs: []store.PollRun{
				{
					ID:                "run-1",
					StartedAt:         "2026-08-07T10:00:00.000Z",
					FinishedAt:        &finishedAt,
					Status:            "success",
					ReposChecked:      5,
					TicketsCreated:    1,
					TicketsSuperseded: 0,
					ErrorsJSON:        "[]",
				},
			},
		},
		Runner: &mockStatusPollRunner{isPolling: true},
	}

	router, sm := newStatusTestRouter(t, h)
	cookie := seedSession(t, sm)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp struct {
		LastRun *struct {
			ID                string `json:"id"`
			Status            string `json:"status"`
			ReposChecked      int64  `json:"reposChecked"`
			TicketsCreated    int64  `json:"ticketsCreated"`
			TicketsSuperseded int64  `json:"ticketsSuperseded"`
			Errors            []any  `json:"errors"`
		} `json:"lastRun"`
		IsPolling bool `json:"isPolling"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.LastRun == nil {
		t.Fatal("lastRun is null, want object")
	}
	if resp.LastRun.ID != "run-1" || resp.LastRun.Status != "success" {
		t.Fatalf("lastRun = %+v, want id run-1 status success", resp.LastRun)
	}
	if resp.LastRun.ReposChecked != 5 || resp.LastRun.TicketsCreated != 1 {
		t.Fatalf("lastRun counters = %+v, want reposChecked 5 ticketsCreated 1", resp.LastRun)
	}
	if !resp.IsPolling {
		t.Fatal("isPolling = false, want true")
	}
}

func TestGetStatusDerivesIsPollingFromLatestRun(t *testing.T) {
	t.Parallel()

	h := &handlers.StatusHandlers{
		Settings: &mockStatusSettingsRepo{
			settings: &store.AppSettings{ID: 1, PollIntervalMinutes: 360},
		},
		Repos:          &mockStatusRepoRepo{},
		TicketProjects: &mockStatusTicketProjectRepo{},
		Poll: &mockStatusPollRepo{
			runs: []store.PollRun{
				{
					ID:         "run-running",
					StartedAt:  "2026-08-07T10:00:00.000Z",
					Status:     "running",
					ErrorsJSON: "[]",
				},
			},
		},
	}

	router, sm := newStatusTestRouter(t, h)
	cookie := seedSession(t, sm)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp struct {
		IsPolling bool `json:"isPolling"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.IsPolling {
		t.Fatal("isPolling = false, want true")
	}
}

func TestGetStatusRequiresSession(t *testing.T) {
	t.Parallel()

	h := &handlers.StatusHandlers{
		Settings:       &mockStatusSettingsRepo{settings: &store.AppSettings{ID: 1, PollIntervalMinutes: 360}},
		Repos:          &mockStatusRepoRepo{},
		TicketProjects: &mockStatusTicketProjectRepo{},
		Poll:           &mockStatusPollRepo{},
	}
	router, _ := newStatusTestRouter(t, h)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}
