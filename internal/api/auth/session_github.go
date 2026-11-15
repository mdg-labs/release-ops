package auth

import (
	"context"
	"time"

	"github.com/alexedwards/scs/v2"
)

const (
	// SessionGitHubIDKey and SessionGitHubLoginKey hold the numeric GitHub user id and the
	// login the user signed in with; SessionRoleKey holds the role and SessionRoleCheckedKey
	// the Unix time the role was last evaluated. A session that carries the GitHub id is a
	// GitHub sign-in.
	SessionGitHubIDKey    = "githubID"
	SessionGitHubLoginKey = "githubLogin"
	SessionRoleKey        = "role"
	SessionRoleCheckedKey = "roleCheckedAt"

	// SessionScopeKey carries the repository ids of a viewer's session to the session
	// store, which keeps them in session_repositories and removes the key from the stored
	// data. It is never part of a stored session.
	SessionScopeKey = "repositoryScope"

	// RoleAdmin sees every repository and may use every endpoint; RoleViewer is read-only
	// and sees the repositories on its stored list. A session with any other role, or none,
	// is refused.
	RoleAdmin  = "admin"
	RoleViewer = "viewer"
)

// GitHubSession is what a GitHub sign-in stores in the session. The user token is not part
// of it and is never stored.
type GitHubSession struct {
	UserID   string
	GitHubID int64
	Login    string
	// Role is RoleAdmin or RoleViewer, as evaluated at EvaluatedAt.
	Role        string
	EvaluatedAt time.Time
	// RepositoryIDs are the row ids of the repositories a viewer may see; admins have none.
	RepositoryIDs []string
}

// StartGitHubSession discards everything in the session the request arrived with (a pre-login
// or setup session), issues a fresh token so a token known before sign-in is worthless, and
// stores the user in it.
func StartGitHubSession(ctx context.Context, sm *scs.SessionManager, s GitHubSession) error {
	if err := sm.Clear(ctx); err != nil {
		return err
	}
	if err := sm.RenewToken(ctx); err != nil {
		return err
	}
	sm.Put(ctx, SessionUserIDKey, s.UserID)
	sm.Put(ctx, SessionGitHubIDKey, s.GitHubID)
	sm.Put(ctx, SessionGitHubLoginKey, s.Login)
	sm.Put(ctx, SessionRoleKey, s.Role)
	sm.Put(ctx, SessionRoleCheckedKey, s.EvaluatedAt.Unix())
	sm.Put(ctx, SessionScopeKey, append([]string{}, s.RepositoryIDs...))
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
