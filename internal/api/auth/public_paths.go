package auth

// PublicAuthPaths are API paths that do not require an authenticated session.
var PublicAuthPaths = []string{
	"/api/v1/auth/login",
	"/api/v1/auth/session",
	"/api/v1/auth/accept-invitation",
	"/api/v1/auth/forgot-password",
	"/api/v1/auth/reset-password",
	"/api/v1/auth/confirm-email-change",
}
