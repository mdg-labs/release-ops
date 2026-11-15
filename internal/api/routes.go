package api

import (
	"github.com/go-chi/chi/v5"
	"github.com/mdg-labs/release-ops/internal/api/auth"
	"github.com/mdg-labs/release-ops/internal/api/handlers"
	apimw "github.com/mdg-labs/release-ops/internal/api/middleware"
	"github.com/mdg-labs/release-ops/internal/api/setup"
	"github.com/mdg-labs/release-ops/internal/github"
)

// RegisterAll wires §7 REST endpoints on the /api/v1 subrouter.
//
// Route order:
//  1. Session LoadAndSave middleware (all /api/v1/*)
//  2. Public auth: POST /auth/login, GET /auth/session, and the GitHub sign-in: GET
//     /auth/github/login, GET /auth/github/callback and POST /auth/login-link
//  3. Setup: GET /setup/status and POST /setup/session are public; GET /setup/session,
//     POST /setup/manifest and GET /setup/callback require a setup session, which opens
//     only the setup routes; GET /setup/installed is public
//  4. Logout, behind a session alone, so a user whose role cannot be evaluated can still end it
//  5. Protected group (Authenticate: a session with a role, re-evaluated every 15 minutes):
//     status for any role; settings, integrations, ticket-projects, repositories (run now,
//     pause and enable) and users for an admin only (RequireAdmin), a viewer getting 403
func RegisterAll(api chi.Router, deps *ServerDeps) {
	if deps == nil || deps.Session == nil {
		return
	}

	api.Use(deps.Session.LoadAndSave)

	authorizer := &auth.Authorizer{Sessions: deps.Session, Roles: deps.Roles}
	authHandlers := &auth.Handlers{
		SessionManager: deps.Session,
		Queries:        deps.Queries,
		TokenService:   auth.NewTokenService(deps.Queries),
		Authorizer:     authorizer,
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

	if deps.Store != nil {
		registerGitHubSignIn(api, deps, rateLimiter)
	}

	setupHandlers := &setup.Handlers{
		Sessions:      deps.Session,
		Queries:       deps.Queries,
		AppURL:        appURL,
		WebhookURL:    deps.WebhookURL,
		GitHubBaseURL: deps.GitHubBaseURL,
	}
	api.With(rateLimiter.SetupStatus).Get("/setup/status", setupHandlers.Status)
	api.With(rateLimiter.SetupSession).Post("/setup/session", setupHandlers.CreateSession)
	api.With(setupHandlers.RequireSession).Get("/setup/session", setupHandlers.GetSession)
	if deps.Store != nil && deps.GitHub != nil {
		setupHandlers.Apps = deps.Store.GitHubApp()
		setupHandlers.Converter = deps.GitHub
		// Each manifest-flow route is limited on its own, 10 a minute per client address; the
		// callback, a browser navigation, redirects to /setup instead of answering 429 JSON.
		api.With(apimw.NewRateLimiter().SetupSession, setupHandlers.RequireSession).Post("/setup/manifest", setupHandlers.Manifest)
		api.With(apimw.NewRateLimiter().SetupCallback, setupHandlers.RequireCallbackSession).Get("/setup/callback", setupHandlers.Callback)
		setupHandlers.Installations = deps.Store.Installations()
		setupHandlers.Installer = github.NewAppClient(deps.GitHub, deps.Store.GitHubApp(), github.AppClientOptions{})
		api.With(apimw.NewRateLimiter().SetupSession).Get("/setup/installed", setupHandlers.Installed)
	}

	api.With(apimw.RequireSession(deps.Session)).Post("/auth/logout", authHandlers.Logout)

	if deps.Store == nil {
		return
	}
	api.Group(func(protected chi.Router) {
		protected.Use(apimw.Authenticate(authorizer, deps.Store.Access()))
		registerProtectedAPIRoutes(protected, deps, appURL, rateLimiter)
	})
}

func registerProtectedAPIRoutes(protected chi.Router, deps *ServerDeps, appURL string, rateLimiter *apimw.RateLimiter) {
	statusHandlers := &handlers.StatusHandlers{}

	protected.Get("/status", statusHandlers.Get)

	protected.Group(func(admin chi.Router) {
		admin.Use(apimw.RequireAdmin)
		registerAdminRoutes(admin, deps, appURL, rateLimiter)
	})
}

// registerAdminRoutes registers the routes only an admin may call. Every route added to the
// protected API that is not meant for a viewer belongs here, behind RequireAdmin.
func registerAdminRoutes(protected chi.Router, deps *ServerDeps, appURL string, rateLimiter *apimw.RateLimiter) {
	st := deps.Store
	settingsHandlers := &handlers.SettingsHandlers{Settings: st.Settings()}
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

	repositoryHandlers := &handlers.RepositoryHandlers{Repositories: st.Repositories(), Queue: deps.Queue}
	protected.Patch("/repositories/{id}", repositoryHandlers.Patch)
	protected.Post("/repositories/{id}/run", repositoryHandlers.Run)

	protected.Get("/users", userHandlers.List)
	protected.Delete("/users/{id}", userHandlers.Delete)
	protected.Post("/users/me/email-change-request", userHandlers.EmailChangeRequest)
	protected.With(rateLimiter.ResetPassword).Post("/users/me/password-change", userHandlers.PasswordChange)
	protected.Get("/users/invitations", invitationHandlers.List)
	protected.Post("/users/invitations", invitationHandlers.Create)
	protected.Delete("/users/invitations/{id}", invitationHandlers.Delete)
	protected.Post("/users/invitations/{id}/send-email", invitationHandlers.SendEmail)
}

// registerGitHubSignIn mounts the GitHub OAuth sign-in and the break-glass redemption. The
// OAuth routes need a GitHub client; without one they are not mounted.
func registerGitHubSignIn(api chi.Router, deps *ServerDeps, rateLimiter *apimw.RateLimiter) {
	users := deps.Store.SignIn()

	api.With(rateLimiter.LoginLink).Post("/auth/login-link", (&auth.LoginLinkHandlers{
		Sessions: deps.Session,
		Users:    users,
	}).Redeem)

	provider := deps.GitHubOAuth
	if provider == nil && deps.GitHub != nil {
		provider = github.NewOAuthClient(deps.GitHub, deps.GitHubBaseURL)
	}
	if provider == nil {
		return
	}
	signIn := &auth.GitHubSignInHandlers{
		Sessions:            deps.Session,
		Apps:                deps.Store.GitHubApp(),
		Users:               users,
		GitHub:              provider,
		Roles:               deps.Roles,
		Access:              deps.Store.Access(),
		AppURL:              deps.AppURL,
		BootstrapAdminLogin: deps.BootstrapAdminGitHubLogin,
	}
	api.With(rateLimiter.GitHubLogin).Get("/auth/github/login", signIn.Login)
	api.With(rateLimiter.GitHubCallback).Get("/auth/github/callback", signIn.Callback)
}
