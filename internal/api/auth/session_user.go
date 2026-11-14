package auth

import (
	"context"
	"strings"

	"github.com/alexedwards/scs/v2"
)

// NormalizeEmail trims and lowercases an email address for storage and lookup.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// StartUserSession issues a fresh session token (preventing session fixation) and stores
// the authenticated user in it. A local account has no GitHub identity to derive a role
// from, so its session is stored as an admin session, as every local session was before
// roles; it goes with the local accounts.
func StartUserSession(ctx context.Context, sm *scs.SessionManager, userID, email string) error {
	if err := sm.RenewToken(ctx); err != nil {
		return err
	}
	sm.Put(ctx, SessionUserIDKey, userID)
	sm.Put(ctx, SessionUserEmailKey, email)
	sm.Put(ctx, SessionRoleKey, RoleAdmin)
	return nil
}
