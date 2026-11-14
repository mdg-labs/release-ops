package auth_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/api/auth"
	"github.com/mdg-labs/release-ops/internal/crypto"
	"github.com/mdg-labs/release-ops/internal/github"
	"github.com/mdg-labs/release-ops/internal/github/githubtest"
	"github.com/mdg-labs/release-ops/internal/store"
	"github.com/mdg-labs/release-ops/internal/store/storetest"
)

const (
	rolesInstallationID int64 = 1000
	rolesOrgOwnerID     int64 = 101
	rolesMemberID       int64 = 102
	rolesAdminTeamID    int64 = 103
	rolesViewerTeamID   int64 = 104
	rolesOutsiderID     int64 = 105
	rolesPendingID      int64 = 106
	rolesBootstrapID    int64 = 107
	rolesPersonalID     int64 = 108
)

type rolesStaticCreds struct {
	appID int64
	pem   []byte
}

func (s rolesStaticCreds) SigningKey(context.Context) (int64, []byte, error) {
	return s.appID, slices.Clone(s.pem), nil
}

// rolesRig is the role evaluator over the fake GitHub API and a migrated database holding one
// installation.
type rolesRig struct {
	fake  *githubtest.Fake
	store *store.Store
	roles *auth.GitHubRoles
}

func newRolesRig(t *testing.T, accountType, accountLogin string, accountID int64) *rolesRig {
	t.Helper()
	db := storetest.OpenMigrated(t)
	cipher, err := crypto.NewCipherFromHex(signInKeyHex)
	if err != nil {
		t.Fatalf("NewCipherFromHex: %v", err)
	}
	st := store.New(db, cipher)
	if outcome, err := st.Installations().Record(context.Background(), store.NewInstallation{
		GitHubInstallationID: rolesInstallationID, AccountID: accountID, AccountLogin: accountLogin,
		AccountType: accountType, Status: "active", RepositorySelection: "all",
	}); err != nil || outcome != store.InstallationRecorded {
		t.Fatalf("record installation = %v, %v", outcome, err)
	}

	fake := githubtest.New(t, time.Now)
	fake.AddInstallation(rolesInstallationID, githubtest.AppID)
	app := github.NewAppClient(github.NewClient(fake.URL, 5*time.Second), rolesStaticCreds{appID: githubtest.AppID, pem: fake.PEM}, github.AppClientOptions{})
	for id, login := range map[int64]string{
		rolesOrgOwnerID: "org-owner", rolesMemberID: "plain-member", rolesAdminTeamID: "admin-teamer", rolesViewerTeamID: "viewer-teamer",
		rolesOutsiderID: "outsider", rolesPendingID: "pending-invitee", rolesBootstrapID: "bootstrap-admin", rolesPersonalID: "personal-owner",
	} {
		fake.SetUser(id, login)
	}
	return &rolesRig{
		fake: fake, store: st,
		roles: &auth.GitHubRoles{Access: st.Access(), Users: st.SignIn(), GitHub: app},
	}
}

func (r *rolesRig) evaluate(t *testing.T, id int64) string {
	t.Helper()
	role, err := r.roles.Evaluate(context.Background(), id)
	if err != nil {
		t.Fatalf("Evaluate(%d): %v", id, err)
	}
	return role
}

func (r *rolesRig) populateOrg() {
	r.fake.SetOrganizationMember("acme", "org-owner", "active", "admin")
	r.fake.SetOrganizationMember("acme", "plain-member", "active", "member")
	r.fake.SetOrganizationMember("acme", "admin-teamer", "active", "member")
	r.fake.SetOrganizationMember("acme", "viewer-teamer", "active", "member")
	r.fake.SetOrganizationMember("acme", "pending-invitee", "pending", "member")
	r.fake.SetTeamMember("acme", "release-admins", "admin-teamer", "active", "member")
	r.fake.SetTeamMember("acme", "release-viewers", "viewer-teamer", "active", "member")
	r.fake.SetTeamMember("acme", "release-admins", "pending-invitee", "pending", "member")
}

func TestRoleDerivationForAnOrganizationInstallation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		user       int64
		adminTeam  string
		viewerTeam string
		want       string
	}{
		{"organization owner is admin", rolesOrgOwnerID, "", "", auth.RoleAdmin},
		{"organization member is viewer", rolesMemberID, "", "", auth.RoleViewer},
		{"non-member has no access", rolesOutsiderID, "", "", ""},
		{"pending invitation has no access", rolesPendingID, "", "", ""},
		{"ADMIN_TEAM member is admin", rolesAdminTeamID, "acme/release-admins", "", auth.RoleAdmin},
		{"organization member outside ADMIN_TEAM stays viewer", rolesMemberID, "acme/release-admins", "", auth.RoleViewer},
		{"pending ADMIN_TEAM membership is not admin", rolesPendingID, "acme/release-admins", "", ""},
		{"VIEWER_TEAM member is viewer", rolesViewerTeamID, "", "acme/release-viewers", auth.RoleViewer},
		{"member outside VIEWER_TEAM has no access", rolesMemberID, "", "acme/release-viewers", ""},
		{"owner is not excluded by VIEWER_TEAM", rolesOrgOwnerID, "", "acme/release-viewers", auth.RoleAdmin},
		{"ADMIN_TEAM member is not excluded by VIEWER_TEAM", rolesAdminTeamID, "acme/release-admins", "acme/release-viewers", auth.RoleAdmin},
		{"VIEWER_TEAM is checked for a non-member of the organization", rolesOutsiderID, "", "acme/release-viewers", ""},
		{"an ADMIN_TEAM of another organization grants nothing", rolesAdminTeamID, "other/release-admins", "", auth.RoleViewer},
		{"a VIEWER_TEAM of another organization admits nobody", rolesViewerTeamID, "", "other/release-viewers", ""},
		{"team organization is compared without regard to case", rolesAdminTeamID, "ACME/release-admins", "", auth.RoleAdmin},
		{"an unknown ADMIN_TEAM grants nothing", rolesMemberID, "acme/no-such-team", "", auth.RoleViewer},
		{"an unknown VIEWER_TEAM admits nobody", rolesMemberID, "", "acme/no-such-team", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := newRolesRig(t, "Organization", "acme", 5000)
			r.populateOrg()
			r.roles.AdminTeam, r.roles.ViewerTeam = tc.adminTeam, tc.viewerTeam
			if got := r.evaluate(t, tc.user); got != tc.want {
				t.Fatalf("role = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBootstrapAdminIsAdminWithoutAnyMembershipCall(t *testing.T) {
	t.Parallel()
	r := newRolesRig(t, "Organization", "acme", 5000)
	r.populateOrg()
	if _, err := r.store.SignIn().SignIn(context.Background(), store.GitHubUser{ID: rolesBootstrapID, Login: "bootstrap-admin"}, true); err != nil {
		t.Fatalf("pin bootstrap admin: %v", err)
	}
	// GitHub being down changes nothing for the bootstrap admin: the stored numeric id decides.
	r.fake.FailMemberships(502)
	before := len(r.fake.Requests())

	if got := r.evaluate(t, rolesBootstrapID); got != auth.RoleAdmin {
		t.Fatalf("role = %q, want admin", got)
	}
	if n := len(r.fake.Requests()) - before; n != 0 {
		t.Errorf("%d GitHub calls for the bootstrap admin, want none", n)
	}
}

func TestBootstrapAdminIsMatchedByNumericIDNotLogin(t *testing.T) {
	t.Parallel()
	r := newRolesRig(t, "Organization", "acme", 5000)
	r.populateOrg()
	if _, err := r.store.SignIn().SignIn(context.Background(), store.GitHubUser{ID: rolesBootstrapID, Login: "bootstrap-admin"}, true); err != nil {
		t.Fatalf("pin bootstrap admin: %v", err)
	}
	// Another account that now carries the login the admin once had is not the admin.
	r.fake.SetUser(rolesOutsiderID, "bootstrap-admin")
	r.fake.SetOrganizationMember("acme", "bootstrap-admin", "active", "member")
	if got := r.evaluate(t, rolesOutsiderID); got != auth.RoleViewer {
		t.Fatalf("role of an account with the admin's old login = %q, want only what its membership gives (viewer)", got)
	}
}

func TestRoleDerivationFindsARenamedUserByTheirNumericID(t *testing.T) {
	t.Parallel()
	r := newRolesRig(t, "Organization", "acme", 5000)
	r.fake.SetUser(rolesMemberID, "renamed-member")
	r.fake.SetOrganizationMember("acme", "renamed-member", "active", "member")
	if got := r.evaluate(t, rolesMemberID); got != auth.RoleViewer {
		t.Fatalf("role = %q, want viewer for the account whose current login is renamed-member", got)
	}
}

func TestRoleDerivationForAPersonalAccountInstallation(t *testing.T) {
	t.Parallel()
	r := newRolesRig(t, "User", "personal-owner", rolesPersonalID)
	r.populateOrg()
	r.roles.AdminTeam, r.roles.ViewerTeam = "acme/release-admins", "acme/release-viewers"
	before := len(r.fake.Requests())

	if got := r.evaluate(t, rolesPersonalID); got != auth.RoleAdmin {
		t.Errorf("owner role = %q, want admin", got)
	}
	for _, id := range []int64{rolesMemberID, rolesOrgOwnerID, rolesAdminTeamID, rolesViewerTeamID, rolesOutsiderID} {
		if got := r.evaluate(t, id); got != "" {
			t.Errorf("role of %d on a personal account = %q, want no access: there are no viewers", id, got)
		}
	}
	if n := len(r.fake.Requests()) - before; n != 0 {
		t.Errorf("%d GitHub calls on a personal account, want none: the numeric id of the owner decides", n)
	}
}

func TestRoleDerivationWithoutAnInstallationAdmitsOnlyTheBootstrapAdmin(t *testing.T) {
	t.Parallel()
	db := storetest.OpenMigrated(t)
	cipher, _ := crypto.NewCipherFromHex(signInKeyHex)
	st := store.New(db, cipher)
	roles := &auth.GitHubRoles{Access: st.Access(), Users: st.SignIn(), GitHub: nil}
	ctx := context.Background()

	if role, err := roles.Evaluate(ctx, rolesMemberID); err != nil || role != "" {
		t.Fatalf("role with no installation = %q, %v; want no access", role, err)
	}
	if _, err := st.SignIn().SignIn(ctx, store.GitHubUser{ID: rolesBootstrapID, Login: "bootstrap-admin"}, true); err != nil {
		t.Fatalf("pin: %v", err)
	}
	if role, err := roles.Evaluate(ctx, rolesBootstrapID); err != nil || role != auth.RoleAdmin {
		t.Fatalf("bootstrap admin role = %q, %v; want admin", role, err)
	}
}

func TestRoleDerivationFailsClosedWhenGitHubCannotAnswer(t *testing.T) {
	t.Parallel()
	for _, status := range []int{401, 403, 500, 502} {
		r := newRolesRig(t, "Organization", "acme", 5000)
		r.populateOrg()
		r.roles.ViewerTeam = "acme/release-viewers"
		r.fake.FailMemberships(status)
		for _, id := range []int64{rolesOrgOwnerID, rolesMemberID, rolesViewerTeamID, rolesOutsiderID} {
			role, err := r.roles.Evaluate(context.Background(), id)
			if err == nil || role != "" {
				t.Errorf("status %d, user %d: role = %q, err = %v; want an error and no role, never a decision", status, id, role, err)
			}
		}
	}
}

func TestRoleDerivationFailsClosedOnATeamCallFailing(t *testing.T) {
	t.Parallel()
	r := newRolesRig(t, "Organization", "acme", 5000)
	r.populateOrg()
	r.roles.AdminTeam = "acme/release-admins"
	// The organization lookup works but the team lookup does not: a member must not be
	// demoted to viewer or kept as one on a guess.
	r.fake.FailMemberships(0)
	r.fake.SetOrganizationMember("acme", "plain-member", "active", "member")
	failing := &failOnTeam{MembershipSource: r.roles.GitHub}
	r.roles.GitHub = failing
	role, err := r.roles.Evaluate(context.Background(), rolesMemberID)
	if err == nil || role != "" {
		t.Fatalf("role = %q, err = %v; want an error", role, err)
	}
}

type failOnTeam struct{ auth.MembershipSource }

func (failOnTeam) TeamMembership(context.Context, int64, string, string, string) (github.Membership, bool, error) {
	return github.Membership{}, false, github.ErrRequestFailed
}
