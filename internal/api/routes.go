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
//  2. Public auth: GET /auth/session, and the GitHub sign-in: GET /auth/github/login,
//     GET /auth/github/callback and POST /auth/login-link
//  3. Setup: GET /setup/status and POST /setup/session are public; GET /setup/session,
//     POST /setup/manifest and GET /setup/callback require a setup session, which opens
//     only the setup routes; GET /setup/installed is public
//  4. Logout, behind a session alone, so a user whose role cannot be evaluated can still end it
//  5. Protected group (Authenticate: a session with a role, re-evaluated every 15 minutes):
//     status, about and the update PR list for any role (the list limited to the caller's
//     repositories); settings (the concurrency cap, the global Renovate
//     config), github-app (its metadata and role mapping), installations (the default ticket
//     project),
//     integrations, ticket-projects, update-tickets (clearing a pending marker), repositories
//     (the list with ticket projects, run now, pause, enable and the ticket-project override)
//     and users for an admin only
//     (RequireAdmin), a viewer getting 403; the GitHub App's private key rotation
//     (PUT /github-app/private-key, only with a GitHub client) is one of them
func RegisterAll(api chi.Router, deps *ServerDeps) {
	if deps == nil || deps.Session == nil {
		return
	}

	api.Use(deps.Session.LoadAndSave)

	authorizer := &auth.Authorizer{Sessions: deps.Session, Roles: deps.Roles}
	authHandlers := &auth.Handlers{SessionManager: deps.Session, Authorizer: authorizer}

	rateLimiter := apimw.NewRateLimiter()

	api.Get("/auth/session", authHandlers.Session)

	if deps.Store != nil {
		registerGitHubSignIn(api, deps, rateLimiter)
	}

	setupHandlers := &setup.Handlers{
		Sessions:      deps.Session,
		Queries:       deps.Queries,
		AppURL:        deps.AppURL,
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
		registerProtectedAPIRoutes(protected, deps)
	})
}

func registerProtectedAPIRoutes(protected chi.Router, deps *ServerDeps) {
	statusHandlers := &handlers.StatusHandlers{}

	protected.Get("/status", statusHandlers.Get)
	protected.Get("/about", (&handlers.AboutHandlers{}).Get)
	protected.Get("/update-prs", (&handlers.UpdatePRHandlers{PRs: deps.Store.UpdatePRList()}).List)

	protected.Group(func(admin chi.Router) {
		admin.Use(apimw.RequireAdmin)
		registerAdminRoutes(admin, deps)
	})
}

// registerAdminRoutes registers the routes only an admin may call. Every route added to the
// protected API that is not meant for a viewer belongs here, behind RequireAdmin.
func registerAdminRoutes(protected chi.Router, deps *ServerDeps) {
	st := deps.Store
	settingsHandlers := &handlers.SettingsHandlers{
		Settings:    st.InstanceSettings(),
		Config:      st.InstanceConfig,
		PollingMode: deps.WebhookURL == "",
	}
	requiredPermissions := setup.BuildManifest("", "", "").DefaultPermissions
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
	userHandlers := &handlers.UsersHandlers{Queries: deps.Queries}

	protected.Get("/settings", settingsHandlers.Get)
	protected.Patch("/settings", settingsHandlers.Patch)
	protected.Get("/settings/global-config", settingsHandlers.GetGlobalConfig)

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

	updateTicketHandlers := &handlers.UpdateTicketHandlers{Tickets: st.UpdateTickets()}
	protected.Post("/update-tickets/{id}/clear-pending-marker", updateTicketHandlers.ClearPendingMarker)

	installationHandlers := &handlers.InstallationHandlers{Installations: st.InstallationSettings(), RequiredPermissions: requiredPermissions}
	protected.Get("/installations", installationHandlers.List)
	protected.Patch("/installations/{id}", installationHandlers.Patch)

	repositoryHandlers := &handlers.RepositoryHandlers{Repositories: st.Repositories(), Queue: deps.Queue}
	protected.Get("/repositories", repositoryHandlers.List)
	protected.Patch("/repositories/{id}", repositoryHandlers.Patch)
	protected.Post("/repositories/{id}/run", repositoryHandlers.Run)

	githubAppHandlers := &handlers.GitHubAppHandlers{
		Info:                st.GitHubAppInfo(),
		Installations:       st.InstallationSettings(),
		Config:              st.InstanceConfig,
		RequiredPermissions: requiredPermissions,
	}
	protected.Get("/github-app", githubAppHandlers.Get)
	if deps.GitHub != nil {
		githubAppHandlers.Verifier = github.NewAppClient(deps.GitHub, st.GitHubApp(), github.AppClientOptions{})
		githubAppHandlers.Apps = st.GitHubApp()
		protected.Put("/github-app/private-key", githubAppHandlers.RotatePrivateKey)
	}

	protected.Get("/users", userHandlers.List)
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
