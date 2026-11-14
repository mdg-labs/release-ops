package auth

import (
	"context"

	"github.com/alexedwards/scs/v2"
)

const (
	// SessionGitHubIDKey and SessionGitHubLoginKey hold the numeric GitHub user id and the
	// login the user signed in with; SessionRoleKey holds the role. A session that carries
	// the GitHub id is a GitHub sign-in.
	SessionGitHubIDKey    = "githubID"
	SessionGitHubLoginKey = "githubLogin"
	SessionRoleKey        = "role"

	// RoleAdmin is the only role a sign-in grants until roles are derived from GitHub.
	RoleAdmin = "admin"
)

// StartGitHubSession discards everything in the session the request arrived with (a pre-login
// or setup session), issues a fresh token so a token known before sign-in is worthless, and
// stores the user in it.
func StartGitHubSession(ctx context.Context, sm *scs.SessionManager, userID string, githubID int64, login, role string) error {
	if err := sm.Clear(ctx); err != nil {
		return err
	}
	if err := sm.RenewToken(ctx); err != nil {
		return err
	}
	sm.Put(ctx, SessionUserIDKey, userID)
	sm.Put(ctx, SessionGitHubIDKey, githubID)
	sm.Put(ctx, SessionGitHubLoginKey, login)
	sm.Put(ctx, SessionRoleKey, role)
	return nil
}

type githubUserResponse struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`
	Role  string `json:"role"`
}

type githubSessionResponse struct {
	User *githubUserResponse `json:"user"`
}

func githubSessionUser(ctx context.Context, sm *scs.SessionManager) (*githubUserResponse, bool) {
	id, ok := sm.Get(ctx, SessionGitHubIDKey).(int64)
	if !ok || id <= 0 || sm.GetString(ctx, SessionUserIDKey) == "" {
		return nil, false
	}
	role := sm.GetString(ctx, SessionRoleKey)
	if role == "" {
		return nil, false
	}
	return &githubUserResponse{ID: id, Login: sm.GetString(ctx, SessionGitHubLoginKey), Role: role}, true
}
