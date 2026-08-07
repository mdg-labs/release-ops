package api

import (
	"context"
	"errors"
	"os"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/mdg-labs/release-ops/internal/api/auth"
	"github.com/mdg-labs/release-ops/internal/api/handlers"
	apimw "github.com/mdg-labs/release-ops/internal/api/middleware"
)

// RegisterAll wires §7 REST endpoints on the /api/v1 subrouter.
//
// Route order:
//  1. Session LoadAndSave middleware (all /api/v1/*)
//  2. Public auth: POST /auth/login, GET /auth/session
//  3. Protected group (RequireSession): logout, status, poll, settings,
//     integrations, ticket-projects, repos, notification-targets
func RegisterAll(api chi.Router, deps *ServerDeps) {
	if deps == nil || deps.Session == nil {
		return
	}

	api.Use(deps.Session.LoadAndSave)

	authHandlers := &auth.Handlers{
		SessionManager: deps.Session,
		Queries:        deps.Queries,
		TokenService:   auth.NewTokenService(deps.Queries),
	}

	rateLimiter := apimw.NewRateLimiter()

	api.With(rateLimiter.Login).Post("/auth/login", authHandlers.Login)
	api.Get("/auth/session", authHandlers.Session)
	api.With(rateLimiter.AcceptInvitation).Post("/auth/accept-invitation", authHandlers.AcceptInvitation)
	api.With(rateLimiter.ConfirmEmailChange).Post("/auth/confirm-email-change", authHandlers.ConfirmEmailChange)

	api.Group(func(protected chi.Router) {
		protected.Use(apimw.RequireSession(deps.Session))
		protected.Post("/auth/logout", authHandlers.Logout)

		if deps.Store == nil {
			return
		}

		registerProtectedAPIRoutes(protected, deps)
	})
}

func registerProtectedAPIRoutes(protected chi.Router, deps *ServerDeps) {
	st := deps.Store
	pollRunner := deps.PollRunner
	if pollRunner == nil {
		pollRunner = noopPollRunner{}
	}

	settingsHandlers := &handlers.SettingsHandlers{Settings: st.Settings()}
	statusHandlers := &handlers.StatusHandlers{
		Settings:       st.Settings(),
		Repos:          st.Repos(),
		TicketProjects: st.TicketProjects(),
		Poll:           st.Poll(),
		Runner:         pollRunner,
	}
	pollHandlers := &handlers.PollHandlers{
		Poll:   st.Poll(),
		Runner: pollRunner,
	}
	integrationHandlers := &handlers.IntegrationHandlers{
		Integrations: st.Integrations(),
		Tester:       deps.IntegrationTester,
	}
	integrationMetadataHandlers := &handlers.IntegrationMetadataHandlers{
		Integrations: st.Integrations(),
	}
	ticketProjectHandlers := &handlers.TicketProjectHandlers{
		TicketProjects: st.TicketProjects(),
	}
	repoHandlers := &handlers.RepoHandlers{Repos: st.Repos()}
	notificationHandlers := &handlers.NotificationHandlers{
		Notifications: st.Notifications(),
		Tester:        deps.NotificationTester,
	}
	appPublicURL := strings.TrimSpace(os.Getenv("APP_PUBLIC_URL"))
	tokenService := auth.NewTokenService(deps.Queries)
	userHandlers := &handlers.UsersHandlers{
		Queries:        deps.Queries,
		SessionManager: deps.Session,
		TokenService:   tokenService,
		Mailer:         deps.Mailer,
		AppPublicURL:   appPublicURL,
	}
	invitationHandlers := &handlers.InvitationHandlers{
		Queries:      deps.Queries,
		TokenService: tokenService,
		Mailer:       deps.Mailer,
		AppPublicURL: appPublicURL,
	}

	protected.Get("/status", statusHandlers.Get)

	protected.Post("/poll/trigger", pollHandlers.Trigger)
	protected.Get("/poll/runs", pollHandlers.ListRuns)
	protected.Get("/poll/runs/{id}", pollHandlers.GetRun)

	protected.Get("/settings", settingsHandlers.Get)
	protected.Patch("/settings", settingsHandlers.Patch)

	protected.Get("/integrations", integrationHandlers.List)
	protected.Post("/integrations", integrationHandlers.Create)
	protected.Patch("/integrations/{id}", integrationHandlers.Patch)
	protected.Delete("/integrations/{id}", integrationHandlers.Delete)
	protected.Post("/integrations/{id}/test", integrationHandlers.TestConnection)
	protected.Get("/integrations/{id}/ticket-metadata/workspaces", integrationMetadataHandlers.ListWorkspaces)
	protected.Get("/integrations/{id}/ticket-metadata/projects", integrationMetadataHandlers.ListProjects)
	protected.Get("/integrations/{id}/ticket-metadata/statuses", integrationMetadataHandlers.ListStatuses)
	protected.Get("/integrations/{id}/ticket-metadata/priorities", integrationMetadataHandlers.ListPriorities)
	protected.Get("/integrations/{id}/ticket-metadata/issue-types", integrationMetadataHandlers.ListIssueTypes)

	protected.Get("/ticket-projects", ticketProjectHandlers.List)
	protected.Post("/ticket-projects", ticketProjectHandlers.Create)
	protected.Patch("/ticket-projects/{id}", ticketProjectHandlers.Patch)
	protected.Delete("/ticket-projects/{id}", ticketProjectHandlers.Delete)

	protected.Get("/repos", repoHandlers.List)
	protected.Post("/repos", repoHandlers.Create)
	protected.Patch("/repos/{id}", repoHandlers.Patch)
	protected.Delete("/repos/{id}", repoHandlers.Delete)

	protected.Get("/notification-targets", notificationHandlers.List)
	protected.Post("/notification-targets", notificationHandlers.Create)
	protected.Patch("/notification-targets/{id}", notificationHandlers.Patch)
	protected.Delete("/notification-targets/{id}", notificationHandlers.Delete)
	protected.Post("/notification-targets/{id}/test", notificationHandlers.Test)

	protected.Get("/users", userHandlers.List)
	protected.Delete("/users/{id}", userHandlers.Delete)
	protected.Post("/users/me/email-change-request", userHandlers.EmailChangeRequest)
	protected.Get("/users/invitations", invitationHandlers.List)
	protected.Post("/users/invitations", invitationHandlers.Create)
	protected.Delete("/users/invitations/{id}", invitationHandlers.Delete)
	protected.Post("/users/invitations/{id}/send-email", invitationHandlers.SendEmail)
}

type noopPollRunner struct{}

func (noopPollRunner) Trigger(_ context.Context) (string, error) {
	return "", errors.New("poll runner not configured")
}

func (noopPollRunner) IsPolling() bool {
	return false
}
