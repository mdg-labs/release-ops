package api

import (
	"github.com/go-chi/chi/v5"
	"github.com/mdg-labs/release-ops/internal/api/auth"
	"github.com/mdg-labs/release-ops/internal/api/handlers"
	apimw "github.com/mdg-labs/release-ops/internal/api/middleware"
	"github.com/mdg-labs/release-ops/internal/api/setup"
)

// RegisterAll wires §7 REST endpoints on the /api/v1 subrouter.
//
// Route order:
//  1. Session LoadAndSave middleware (all /api/v1/*)
//  2. Public auth: POST /auth/login, GET /auth/session
//  3. Setup: GET /setup/status and POST /setup/session are public; GET /setup/session
//     requires a setup session, which opens only the setup routes
//  4. Protected group (RequireSession): logout, status, settings, integrations,
//     ticket-projects, users
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

	appURL := deps.AppURL

	api.With(rateLimiter.Login).Post("/auth/login", authHandlers.Login)
	api.Get("/auth/session", authHandlers.Session)
	api.With(rateLimiter.AcceptInvitation).Post("/auth/accept-invitation", authHandlers.AcceptInvitation)
	api.With(rateLimiter.ConfirmEmailChange).Post("/auth/confirm-email-change", authHandlers.ConfirmEmailChange)

	passwordResetHandlers := &auth.PasswordResetHandlers{
		SessionManager: deps.Session,
		Queries:        deps.Queries,
		TokenService:   auth.NewTokenService(deps.Queries),
		Mailer:         deps.Mailer,
		AppPublicURL:   appURL,
	}
	api.With(rateLimiter.ForgotPassword).Post("/auth/forgot-password", passwordResetHandlers.ForgotPassword)
	api.With(rateLimiter.ResetPassword).Post("/auth/reset-password", passwordResetHandlers.ResetPassword)

	setupHandlers := &setup.Handlers{Sessions: deps.Session, Queries: deps.Queries}
	api.With(rateLimiter.SetupStatus).Get("/setup/status", setupHandlers.Status)
	api.With(rateLimiter.SetupSession).Post("/setup/session", setupHandlers.CreateSession)
	api.With(setupHandlers.RequireSession).Get("/setup/session", setupHandlers.GetSession)

	api.Group(func(protected chi.Router) {
		protected.Use(apimw.RequireSession(deps.Session))
		protected.Post("/auth/logout", authHandlers.Logout)

		if deps.Store == nil {
			return
		}

		registerProtectedAPIRoutes(protected, deps, appURL, rateLimiter)
	})
}

func registerProtectedAPIRoutes(protected chi.Router, deps *ServerDeps, appURL string, rateLimiter *apimw.RateLimiter) {
	st := deps.Store

	settingsHandlers := &handlers.SettingsHandlers{Settings: st.Settings()}
	statusHandlers := &handlers.StatusHandlers{}
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
	tokenService := auth.NewTokenService(deps.Queries)
	userHandlers := &handlers.UsersHandlers{
		Queries:        deps.Queries,
		SessionManager: deps.Session,
		TokenService:   tokenService,
		Mailer:         deps.Mailer,
		AppPublicURL:   appURL,
	}
	invitationHandlers := &handlers.InvitationHandlers{
		Queries:      deps.Queries,
		TokenService: tokenService,
		Mailer:       deps.Mailer,
		AppPublicURL: appURL,
	}

	protected.Get("/status", statusHandlers.Get)

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

	protected.Get("/users", userHandlers.List)
	protected.Delete("/users/{id}", userHandlers.Delete)
	protected.Post("/users/me/email-change-request", userHandlers.EmailChangeRequest)
	protected.With(rateLimiter.ResetPassword).Post("/users/me/password-change", userHandlers.PasswordChange)
	protected.Get("/users/invitations", invitationHandlers.List)
	protected.Post("/users/invitations", invitationHandlers.Create)
	protected.Delete("/users/invitations/{id}", invitationHandlers.Delete)
	protected.Post("/users/invitations/{id}/send-email", invitationHandlers.SendEmail)
}
