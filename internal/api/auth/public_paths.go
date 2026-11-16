package auth

// PublicAuthPaths are API paths that do not require an authenticated session.
var PublicAuthPaths = []string{
	"/api/v1/auth/session",
	"/api/v1/auth/github/login",
	"/api/v1/auth/github/callback",
	"/api/v1/auth/login-link",
	"/api/v1/setup/status",
	"/api/v1/setup/session",
	"/api/v1/setup/manifest",
	"/api/v1/setup/callback",
	"/api/v1/setup/installed",
}
