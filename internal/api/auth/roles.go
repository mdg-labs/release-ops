package auth

import (
	"context"
	"fmt"
	"strings"

	"github.com/mdg-labs/release-ops/internal/github"
	"github.com/mdg-labs/release-ops/internal/store"
)

// MembershipSource reads GitHub memberships with the installation's token.
type MembershipSource interface {
	UserLogin(ctx context.Context, installationID, userID int64) (login string, found bool, err error)
	OrganizationMembership(ctx context.Context, installationID int64, org, login string) (github.Membership, bool, error)
	TeamMembership(ctx context.Context, installationID int64, org, team, login string) (github.Membership, bool, error)
}

// RoleEvaluator derives the role of a GitHub user (specs.html § 3.7, § 3.8). The role is
// never stored on the user: it is evaluated at sign-in and again every RoleRecheckInterval.
type RoleEvaluator interface {
	// Evaluate returns RoleAdmin, RoleViewer, or "" for a user with no access. An error
	// means the role could not be decided and must never be read as a role.
	Evaluate(ctx context.Context, githubID int64) (string, error)
}

// GitHubRoles is the RoleEvaluator over GitHub's organization and team memberships.
type GitHubRoles struct {
	Access store.AccessRepository
	Users  store.SignInRepository
	GitHub MembershipSource
	// AdminTeam and ViewerTeam are ADMIN_TEAM and VIEWER_TEAM as org/team-slug; empty when
	// unset. A team of another organization than the installation's grants nothing
	// (AdminTeam) or admits nobody (ViewerTeam). Both are ignored on a personal account.
	AdminTeam  string
	ViewerTeam string
}

// Evaluate returns the role of the user with the numeric GitHub id. The bootstrap admin is
// admin without a GitHub call. A personal-account installation has its owner as admin and
// nobody else. For an organization the user's current login is resolved from the id first,
// so a renamed user is still found; only an active membership counts.
func (g *GitHubRoles) Evaluate(ctx context.Context, githubID int64) (string, error) {
	if githubID <= 0 {
		return "", nil
	}
	admin, pinned, err := g.Users.BootstrapAdmin(ctx)
	if err != nil {
		return "", fmt.Errorf("read bootstrap admin: %w", err)
	}
	if pinned && admin.ID == githubID {
		return RoleAdmin, nil
	}

	inst, ok, err := g.Access.LiveInstallation(ctx)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", nil
	}
	if inst.AccountType != "Organization" {
		if githubID == inst.AccountID {
			return RoleAdmin, nil
		}
		return "", nil
	}

	login, found, err := g.GitHub.UserLogin(ctx, inst.GitHubInstallationID, githubID)
	if err != nil || !found {
		return "", err
	}
	org, found, err := g.GitHub.OrganizationMembership(ctx, inst.GitHubInstallationID, inst.AccountLogin, login)
	if err != nil || !found || !org.Active() {
		return "", err
	}
	if org.Role == "admin" {
		return RoleAdmin, nil
	}

	inAdminTeam, err := g.inTeam(ctx, inst, g.AdminTeam, login)
	if err != nil {
		return "", err
	}
	if inAdminTeam {
		return RoleAdmin, nil
	}
	if g.ViewerTeam == "" {
		return RoleViewer, nil
	}
	inViewerTeam, err := g.inTeam(ctx, inst, g.ViewerTeam, login)
	if err != nil || !inViewerTeam {
		return "", err
	}
	return RoleViewer, nil
}

// inTeam reports whether login is an active member of the team (org/slug) of the
// installation's organization. An unset team and a team of another organization have no
// members.
func (g *GitHubRoles) inTeam(ctx context.Context, inst store.InstallationAccount, team, login string) (bool, error) {
	if team == "" {
		return false, nil
	}
	org, slug, ok := strings.Cut(team, "/")
	if !ok || !strings.EqualFold(org, inst.AccountLogin) {
		return false, nil
	}
	m, found, err := g.GitHub.TeamMembership(ctx, inst.GitHubInstallationID, inst.AccountLogin, slug, login)
	if err != nil || !found {
		return false, err
	}
	return m.Active(), nil
}
