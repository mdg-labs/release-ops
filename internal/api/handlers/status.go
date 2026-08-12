package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/mdg-labs/release-ops/internal/api/auth"
	"github.com/mdg-labs/release-ops/internal/poll"
	"github.com/mdg-labs/release-ops/internal/providers/source"
	"github.com/mdg-labs/release-ops/internal/providers/ticket"
	"github.com/mdg-labs/release-ops/internal/store"
)

// StatusHandlers serves operational status HTTP endpoints.
type StatusHandlers struct {
	Settings       store.SettingsRepository
	Repos          store.MonitoredRepoRepository
	TicketProjects store.TicketProjectRepository
	Integrations   store.IntegrationRepository
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
	LastKnownTag           *string `json:"lastKnownTag"`
	LastReleasePublishedAt *string `json:"lastReleasePublishedAt"`
	LastPolledAt           *string `json:"lastPolledAt"`
	LastError            *string `json:"lastError"`
	RepoURL              *string `json:"repoUrl"`
	ReleaseURL           *string `json:"releaseUrl"`
	OpenTicketURL        *string `json:"openTicketUrl"`
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
	ticketProjectsByID := make(map[string]store.TicketProject, len(ticketProjects))
	for i := range ticketProjects {
		ticketProjectNames[ticketProjects[i].ID] = ticketProjects[i].Name
		ticketProjectsByID[ticketProjects[i].ID] = ticketProjects[i]
	}

	integrationsByID, err := h.loadIntegrationsByID(r.Context())
	if err != nil {
		auth.WriteError(w, "INTERNAL_ERROR", "failed to list integrations", http.StatusInternalServerError)
		return
	}

	urlResolver := newStatusURLResolver(h.Integrations, integrationsByID)

	repoResponses := make([]statusRepoResponse, len(repos))
	for i := range repos {
		repoResponses[i] = statusRepoFromStore(
			&repos[i],
			ticketProjectNames[repos[i].TicketProjectID],
			urlResolver.resolve(r.Context(), &repos[i], ticketProjectsByID[repos[i].TicketProjectID]),
		)
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

func (h *StatusHandlers) loadIntegrationsByID(ctx context.Context) (map[string]store.Integration, error) {
	if h.Integrations == nil {
		return map[string]store.Integration{}, nil
	}
	integrations, err := h.Integrations.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]store.Integration, len(integrations))
	for i := range integrations {
		out[integrations[i].ID] = integrations[i]
	}
	return out, nil
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

type statusRepoURLs struct {
	RepoURL       *string
	ReleaseURL    *string
	OpenTicketURL *string
}

type statusURLResolver struct {
	integrations   store.IntegrationRepository
	integrationsBy map[string]store.Integration
	httpClient     *http.Client
	ticketProviders map[string]ticket.TicketProvider
}

func newStatusURLResolver(integrations store.IntegrationRepository, integrationsBy map[string]store.Integration) *statusURLResolver {
	return &statusURLResolver{
		integrations:    integrations,
		integrationsBy:  integrationsBy,
		httpClient:      &http.Client{Timeout: 30 * time.Second},
		ticketProviders: make(map[string]ticket.TicketProvider),
	}
}

func (r *statusURLResolver) resolve(ctx context.Context, repo *store.MonitoredRepo, ticketProject store.TicketProject) statusRepoURLs {
	var urls statusRepoURLs

	sourceBaseURL := sourceIntegrationBaseURL(repo, r.integrationsBy)
	repoWebURL, err := poll.ResolveRepoWebURL(*repo, sourceBaseURL)
	if err == nil {
		urls.RepoURL = &repoWebURL

		if repo.LastKnownTag != nil && strings.TrimSpace(*repo.LastKnownTag) != "" {
			releaseURL, releaseErr := source.BuildReleaseWebURL(repo.SourceKind, repoWebURL, *repo.LastKnownTag)
			if releaseErr == nil {
				urls.ReleaseURL = &releaseURL
			}
		}
	}

	if repo.OpenTicketExternalID != nil && strings.TrimSpace(*repo.OpenTicketExternalID) != "" && ticketProject.ID != "" {
		provider, providerErr := r.ticketProvider(ctx, ticketProject.IntegrationID)
		if providerErr == nil {
			ticketURL, ticketErr := provider.TicketWebURL(*repo.OpenTicketExternalID)
			if ticketErr == nil {
				urls.OpenTicketURL = &ticketURL
			}
		}
	}

	return urls
}

func sourceIntegrationBaseURL(repo *store.MonitoredRepo, integrationsBy map[string]store.Integration) *string {
	if repo.SourceIntegrationID == nil || strings.TrimSpace(*repo.SourceIntegrationID) == "" {
		return nil
	}
	integration, ok := integrationsBy[*repo.SourceIntegrationID]
	if !ok || integration.Kind != repo.SourceKind {
		return nil
	}
	return integration.BaseURL
}

func (r *statusURLResolver) ticketProvider(ctx context.Context, integrationID string) (ticket.TicketProvider, error) {
	if provider, ok := r.ticketProviders[integrationID]; ok {
		return provider, nil
	}
	if r.integrations == nil {
		return nil, errStatusIntegrationUnavailable
	}

	integration, ok := r.integrationsBy[integrationID]
	if !ok {
		return nil, errStatusIntegrationUnavailable
	}

	payload, err := r.integrations.DecryptPayload(ctx, integration.ID)
	if err != nil {
		return nil, err
	}

	provider, err := newTicketProvider(integration, payload, r.httpClient)
	if err != nil {
		return nil, err
	}
	r.ticketProviders[integrationID] = provider
	return provider, nil
}

func statusRepoFromStore(repo *store.MonitoredRepo, ticketProjectName string, urls statusRepoURLs) statusRepoResponse {
	return statusRepoResponse{
		ID:                   repo.ID,
		SourceKind:           repo.SourceKind,
		ProjectPath:          repo.ProjectPath,
		TicketProjectID:      repo.TicketProjectID,
		TicketProjectName:    ticketProjectName,
		Enabled:              repo.Enabled,
		OpenTicketExternalID: repo.OpenTicketExternalID,
		OpenTicketTag:        repo.OpenTicketTag,
		LastKnownTag:           repo.LastKnownTag,
		LastReleasePublishedAt: repo.LastReleasePublishedAt,
		LastPolledAt:           repo.LastPolledAt,
		LastError:            repo.LastError,
		RepoURL:              urls.RepoURL,
		ReleaseURL:           urls.ReleaseURL,
		OpenTicketURL:        urls.OpenTicketURL,
	}
}

func writeStatusJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
