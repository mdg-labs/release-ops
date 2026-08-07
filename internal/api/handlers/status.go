package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/mdg-labs/release-ops/internal/api/auth"
	"github.com/mdg-labs/release-ops/internal/store"
)

// StatusHandlers serves operational status HTTP endpoints.
type StatusHandlers struct {
	Settings       store.SettingsRepository
	Repos          store.MonitoredRepoRepository
	TicketProjects store.TicketProjectRepository
	Poll           store.PollRepository
	Runner         PollRunner
}

type statusResponse struct {
	PollIntervalMinutes int64                `json:"pollIntervalMinutes"`
	LastRun             *pollRunResponse     `json:"lastRun"`
	Repos               []statusRepoResponse `json:"repos"`
	IsPolling           bool                 `json:"isPolling"`
}

type statusRepoResponse struct {
	ID                   string  `json:"id"`
	SourceKind           string  `json:"sourceKind"`
	ProjectPath          string  `json:"projectPath"`
	TicketProjectID      string  `json:"ticketProjectId"`
	TicketProjectName    string  `json:"ticketProjectName"`
	Enabled              bool    `json:"enabled"`
	OpenTicketExternalID *string `json:"openTicketExternalId"`
	OpenTicketTag        *string `json:"openTicketTag"`
	LastKnownTag         *string `json:"lastKnownTag"`
	LastPolledAt         *string `json:"lastPolledAt"`
	LastError            *string `json:"lastError"`
}

// Get handles GET /api/v1/status.
func (h *StatusHandlers) Get(w http.ResponseWriter, r *http.Request) {
	settings, err := h.Settings.Get(r.Context())
	if err != nil {
		auth.WriteError(w, "INTERNAL_ERROR", "failed to load settings", http.StatusInternalServerError)
		return
	}

	repos, err := h.Repos.List(r.Context())
	if err != nil {
		auth.WriteError(w, "INTERNAL_ERROR", "failed to list repos", http.StatusInternalServerError)
		return
	}

	ticketProjects, err := h.TicketProjects.List(r.Context())
	if err != nil {
		auth.WriteError(w, "INTERNAL_ERROR", "failed to list ticket projects", http.StatusInternalServerError)
		return
	}
	ticketProjectNames := make(map[string]string, len(ticketProjects))
	for i := range ticketProjects {
		ticketProjectNames[ticketProjects[i].ID] = ticketProjects[i].Name
	}

	repoResponses := make([]statusRepoResponse, len(repos))
	for i := range repos {
		repoResponses[i] = statusRepoFromStore(&repos[i], ticketProjectNames[repos[i].TicketProjectID])
	}

	var lastRun *pollRunResponse
	runs, err := h.Poll.ListRuns(r.Context(), 1, 0)
	if err != nil {
		auth.WriteError(w, "INTERNAL_ERROR", "failed to load poll runs", http.StatusInternalServerError)
		return
	}
	if len(runs) > 0 {
		lastRun = pollRunFromStore(&runs[0])
	}

	isPolling := h.isPolling(runs)

	writeStatusJSON(w, http.StatusOK, statusResponse{
		PollIntervalMinutes: settings.PollIntervalMinutes,
		LastRun:             lastRun,
		Repos:               repoResponses,
		IsPolling:           isPolling,
	})
}

func (h *StatusHandlers) isPolling(latestRuns []store.PollRun) bool {
	if h.Runner != nil {
		return h.Runner.IsPolling()
	}
	if len(latestRuns) == 0 {
		return false
	}
	return latestRuns[0].Status == "running" && latestRuns[0].FinishedAt == nil
}

func statusRepoFromStore(repo *store.MonitoredRepo, ticketProjectName string) statusRepoResponse {
	return statusRepoResponse{
		ID:                   repo.ID,
		SourceKind:           repo.SourceKind,
		ProjectPath:          repo.ProjectPath,
		TicketProjectID:      repo.TicketProjectID,
		TicketProjectName:    ticketProjectName,
		Enabled:              repo.Enabled,
		OpenTicketExternalID: repo.OpenTicketExternalID,
		OpenTicketTag:        repo.OpenTicketTag,
		LastKnownTag:         repo.LastKnownTag,
		LastPolledAt:         repo.LastPolledAt,
		LastError:            repo.LastError,
	}
}

func writeStatusJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
