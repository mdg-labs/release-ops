package github_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/github"
)

func TestMembershipCallsUseTheInstallationToken(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.fake.SetUser(42, "Octo-Renamed")
	r.fake.SetOrganizationMember("acme", "Octo-Renamed", "active", "admin")
	r.fake.SetTeamMember("acme", "release-admins", "Octo-Renamed", "pending", "member")
	ctx := context.Background()

	login, found, err := r.client.UserLogin(ctx, 1000, 42)
	if err != nil || !found || login != "Octo-Renamed" {
		t.Fatalf("UserLogin = %q, %v, %v; want the current login", login, found, err)
	}
	m, found, err := r.client.OrganizationMembership(ctx, 1000, "acme", login)
	if err != nil || !found || !m.Active() || m.Role != "admin" {
		t.Fatalf("OrganizationMembership = %+v, %v, %v; want an active owner", m, found, err)
	}
	m, found, err = r.client.TeamMembership(ctx, 1000, "acme", "release-admins", login)
	if err != nil || !found || m.Active() || m.State != "pending" {
		t.Fatalf("TeamMembership = %+v, %v, %v; want a pending membership, which is not active", m, found, err)
	}

	for _, req := range r.fake.Requests() {
		if strings.HasPrefix(req.Path, "/orgs/") || strings.HasPrefix(req.Path, "/user/") {
			if req.JWTValid {
				t.Errorf("%s was sent with the App JWT, want an installation token", req.Path)
			}
		}
	}
	if r.fake.Mints() != 1 {
		t.Errorf("mints = %d, want the one installation token reused by the three calls", r.fake.Mints())
	}
}

func TestMembershipCallsAnswerNotFoundWithoutAnError(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	ctx := context.Background()

	if _, found, err := r.client.UserLogin(ctx, 1000, 7); err != nil || found {
		t.Errorf("UserLogin of an unknown account = found %v, err %v; want not found", found, err)
	}
	if _, found, err := r.client.OrganizationMembership(ctx, 1000, "acme", "nobody"); err != nil || found {
		t.Errorf("OrganizationMembership of a non-member = found %v, err %v; want not found", found, err)
	}
	if _, found, err := r.client.TeamMembership(ctx, 1000, "acme", "team", "nobody"); err != nil || found {
		t.Errorf("TeamMembership of a non-member = found %v, err %v; want not found", found, err)
	}
}

func TestMembershipCallsReportGitHubFailuresAsErrors(t *testing.T) {
	t.Parallel()
	for _, status := range []int{http.StatusForbidden, http.StatusUnauthorized, http.StatusInternalServerError, http.StatusBadGateway} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			t.Parallel()
			r := newRig(t)
			r.fake.SetOrganizationMember("acme", "octocat", "active", "member")
			r.fake.FailMemberships(status)
			ctx := context.Background()

			if _, found, err := r.client.OrganizationMembership(ctx, 1000, "acme", "octocat"); err == nil || found {
				t.Errorf("OrganizationMembership on %d = found %v, err %v; want an error, never an answer", status, found, err)
			}
			if _, _, err := r.client.TeamMembership(ctx, 1000, "acme", "team", "octocat"); err == nil {
				t.Errorf("TeamMembership on %d returned no error", status)
			}
			if _, _, err := r.client.UserLogin(ctx, 1000, 1); err == nil {
				t.Errorf("UserLogin on %d returned no error", status)
			}
		})
	}
}

func TestMembershipCallsRefuseNamesThatAddPathSegments(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	ctx := context.Background()
	before := len(r.fake.Requests())

	if _, found, err := r.client.OrganizationMembership(ctx, 1000, "acme/../other", "octocat"); err != nil || found {
		t.Errorf("a bad org = found %v, err %v; want not found", found, err)
	}
	if _, found, err := r.client.TeamMembership(ctx, 1000, "acme", "team/../x", "octocat"); err != nil || found {
		t.Errorf("a bad team slug = found %v, err %v; want not found", found, err)
	}
	if _, found, err := r.client.OrganizationMembership(ctx, 1000, "acme", "../octocat"); err != nil || found {
		t.Errorf("a bad login = found %v, err %v; want not found", found, err)
	}
	for _, req := range r.fake.Requests()[before:] {
		if strings.HasPrefix(req.Path, "/orgs/") {
			t.Errorf("a request reached GitHub for a malformed name: %s", req.Path)
		}
	}
}

func TestUserInstallationRepositoryIDsFollowEveryPageWithTheUserToken(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.fake.SetPageSize(2)
	r.fake.SetUserRepositories("fixture-user-token", 1000, 11, 12, 13, 14, 15)

	ids, err := listUserRepositories(t, r.fake.URL, "fixture-user-token", 1000)
	if err != nil || !slices.Equal(ids, []int64{11, 12, 13, 14, 15}) {
		t.Fatalf("ids = %v, %v; want all five across three pages", ids, err)
	}
	for _, req := range r.fake.Requests() {
		if req.JWTValid {
			t.Errorf("%s was sent with an App JWT", req.Path)
		}
	}
}

func TestUserInstallationRepositoryIDsFailClosed(t *testing.T) {
	t.Parallel()

	t.Run("a refused token is a rejected user token", func(t *testing.T) {
		t.Parallel()
		r := newRig(t)
		_, err := listUserRepositories(t, r.fake.URL, "unknown-user-token", 1000)
		if !errors.Is(err, github.ErrUserTokenRejected) {
			t.Fatalf("err = %v, want ErrUserTokenRejected", err)
		}
	})
	t.Run("a failing page gives no list", func(t *testing.T) {
		t.Parallel()
		r := newRig(t)
		r.fake.SetUserRepositories("tok", 1000, 1, 2)
		r.fake.FailUserRepositories(http.StatusBadGateway)
		ids, err := listUserRepositories(t, r.fake.URL, "tok", 1000)
		if err == nil || ids != nil {
			t.Fatalf("ids = %v, err = %v; want an error and no ids", ids, err)
		}
	})
	t.Run("a next page on another host ends the listing", func(t *testing.T) {
		t.Parallel()
		r := newRig(t)
		var stolen atomic.Bool
		other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { stolen.Store(true) }))
		t.Cleanup(other.Close)
		r.fake.SetPageSize(1)
		r.fake.LinkTo(other.URL)
		r.fake.SetUserRepositories("tok", 1000, 1, 2)
		ids, err := listUserRepositories(t, r.fake.URL, "tok", 1000)
		if err == nil || ids != nil {
			t.Fatalf("ids = %v, err = %v; want an error", ids, err)
		}
		if stolen.Load() {
			t.Error("the user token was sent to another host")
		}
	})
}

func listUserRepositories(t *testing.T, apiURL, token string, installationID int64) ([]int64, error) {
	t.Helper()
	// The token is a UserToken only through the OAuth exchange; stand one up on a token endpoint.
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, `{"access_token":%q}`, token)
	}))
	t.Cleanup(web.Close)
	api := github.NewClient(apiURL, 5*time.Second)
	oauth := github.NewOAuthClient(api, web.URL)
	tok, err := oauth.ExchangeCode(context.Background(), github.ExchangeParams{ClientID: "id", ClientSecret: "s", Code: "abc", RedirectURI: "https://x.example/cb", CodeVerifier: "v"})
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	return oauth.InstallationRepositoryIDs(context.Background(), tok, installationID)
}
