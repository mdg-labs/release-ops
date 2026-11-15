package middleware

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/alexedwards/scs/v2"
	"github.com/mdg-labs/release-ops/internal/api/auth"
	"github.com/mdg-labs/release-ops/internal/store"
)

type contextKey string

const (
	userIDContextKey    contextKey = "userID"
	principalContextKey contextKey = "principal"
	scopeContextKey     contextKey = "scope"
)

// RequireSession rejects requests without a valid authenticated session. It checks that a
// session names a user and nothing more; the protected API uses Authenticate.
func RequireSession(sm *scs.SessionManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID := sm.GetString(r.Context(), auth.SessionUserIDKey)
			if userID == "" {
				auth.WriteError(w, "unauthorized", "authentication required", http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), userIDContextKey, userID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// Authenticate is RequireSession for the protected API: it requires a session that carries
// a role, re-evaluates the role when it is due (specs.html § 3.8), and puts the principal
// and the repository scope in the request context. A session without a known role, or
// whose user lost access, is answered 401 and destroyed; a role that cannot be evaluated
// is answered 503, and a viewer's list that cannot be read 500 — never served as if it
// were fine.
func Authenticate(a *auth.Authorizer, access store.AccessRepository) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			p, err := a.Authorize(ctx)
			switch {
			case err == nil:
			case errors.Is(err, auth.ErrUnauthenticated):
				auth.WriteError(w, "unauthorized", "authentication required", http.StatusUnauthorized)
				return
			case errors.Is(err, auth.ErrRoleUnavailable):
				slog.Warn("auth: the role could not be re-evaluated", "err", err)
				auth.WriteError(w, "SERVICE_UNAVAILABLE", "the role could not be checked with GitHub", http.StatusServiceUnavailable)
				return
			default:
				slog.Error("auth: authorizing the request failed", "err", err)
				auth.WriteError(w, "INTERNAL_ERROR", "failed to authorize the request", http.StatusInternalServerError)
				return
			}

			scope := AllRepositories()
			if p.Role != auth.RoleAdmin {
				ids, err := access.SessionRepositoryIDs(ctx, p.SessionToken)
				if err != nil {
					slog.Error("auth: reading the repository scope failed", "err", err)
					auth.WriteError(w, "INTERNAL_ERROR", "failed to authorize the request", http.StatusInternalServerError)
					return
				}
				scope = RepositoryScope(ids)
			}

			ctx = context.WithValue(ctx, userIDContextKey, p.UserID)
			ctx = context.WithValue(ctx, principalContextKey, p)
			ctx = context.WithValue(ctx, scopeContextKey, scope)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireAdmin answers 403 FORBIDDEN to every role but admin. It belongs behind
// Authenticate; without a principal in the context the request is unauthenticated.
func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := PrincipalFromContext(r.Context())
		if !ok {
			auth.WriteError(w, "unauthorized", "authentication required", http.StatusUnauthorized)
			return
		}
		if p.Role != auth.RoleAdmin {
			auth.WriteError(w, "FORBIDDEN", "this action needs the admin role", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// UserIDFromContext returns the authenticated user ID stored by RequireSession or
// Authenticate.
func UserIDFromContext(ctx context.Context) (string, bool) {
	userID, ok := ctx.Value(userIDContextKey).(string)
	return userID, ok && userID != ""
}

// PrincipalFromContext returns the principal Authenticate stored.
func PrincipalFromContext(ctx context.Context) (auth.Principal, bool) {
	p, ok := ctx.Value(principalContextKey).(auth.Principal)
	return p, ok && p.UserID != ""
}

// ScopeFromContext returns the repository scope Authenticate stored. Without one it is the
// zero Scope, which allows nothing.
func ScopeFromContext(ctx context.Context) Scope {
	s, _ := ctx.Value(scopeContextKey).(Scope)
	return s
}
