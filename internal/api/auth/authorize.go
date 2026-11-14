package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/alexedwards/scs/v2"
)

// RoleRecheckInterval is how long an evaluated role is served before GitHub is asked again.
const RoleRecheckInterval = 15 * time.Minute

var (
	// ErrUnauthenticated means the request has no usable session: none, one that carries no
	// role this code knows, or one whose user lost access, which is destroyed on the way.
	ErrUnauthenticated = errors.New("no valid session")
	// ErrRoleUnavailable means the session's role is older than RoleRecheckInterval and
	// could not be evaluated again. The session is neither extended nor deleted.
	ErrRoleUnavailable = errors.New("role could not be re-evaluated")
)

// Principal is who a request acts as.
type Principal struct {
	UserID string
	Role   string
	// SessionToken keys the viewer's stored repository list.
	SessionToken string
}

// Authorizer turns a request's session into a Principal. For a GitHub session it first
// re-evaluates the role when the last evaluation is older than RoleRecheckInterval:
// a user with no access any more has the session destroyed, and a role that cannot be
// evaluated is not served.
type Authorizer struct {
	Sessions *scs.SessionManager
	// Roles re-evaluates GitHub sessions. Without it, a GitHub session whose role is due for
	// a re-check is refused.
	Roles RoleEvaluator
	// Now is the clock; nil means time.Now.
	Now func() time.Time
}

func (a *Authorizer) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

// Authorize returns the principal of the request's session, or ErrUnauthenticated or
// ErrRoleUnavailable.
func (a *Authorizer) Authorize(ctx context.Context) (Principal, error) {
	userID := a.Sessions.GetString(ctx, SessionUserIDKey)
	if userID == "" {
		return Principal{}, ErrUnauthenticated
	}
	role := a.Sessions.GetString(ctx, SessionRoleKey)
	if role != RoleAdmin && role != RoleViewer {
		return Principal{}, a.refuse(ctx)
	}

	if githubID, ok := a.Sessions.Get(ctx, SessionGitHubIDKey).(int64); ok && githubID > 0 {
		var err error
		if role, err = a.recheck(ctx, githubID, role); err != nil {
			return Principal{}, err
		}
	}
	return Principal{UserID: userID, Role: role, SessionToken: a.Sessions.Token(ctx)}, nil
}

// recheck returns the role to serve: the stored one while it is fresh, otherwise a new
// evaluation.
func (a *Authorizer) recheck(ctx context.Context, githubID int64, role string) (string, error) {
	now := a.now()
	if last := a.Sessions.GetInt64(ctx, SessionRoleCheckedKey); last > 0 {
		if age := now.Sub(time.Unix(last, 0)); age >= 0 && age <= RoleRecheckInterval {
			return role, nil
		}
	}
	if a.Roles == nil {
		return "", ErrRoleUnavailable
	}

	evaluated, err := a.Roles.Evaluate(ctx, githubID)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrRoleUnavailable, err)
	}
	if evaluated != RoleAdmin && evaluated != RoleViewer {
		return "", a.refuse(ctx)
	}
	if evaluated != role {
		// A viewer's list was fixed at sign-in and an admin has none, so whichever way the
		// role moved there is no list to carry over: the viewer sees nothing until the next
		// sign-in rather than a list from another time.
		a.Sessions.Put(ctx, SessionScopeKey, []string{})
		a.Sessions.Put(ctx, SessionRoleKey, evaluated)
	}
	a.Sessions.Put(ctx, SessionRoleCheckedKey, now.Unix())
	return evaluated, nil
}

// refuse destroys the session and reports it as no session. If the session cannot be
// destroyed the request is refused all the same, with the error.
func (a *Authorizer) refuse(ctx context.Context) error {
	if err := a.Sessions.Destroy(ctx); err != nil {
		return fmt.Errorf("destroy session: %w", err)
	}
	return ErrUnauthenticated
}
