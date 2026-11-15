package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
)

// slugPattern is a team slug as GitHub names one; it keeps a configured value from adding
// path segments to a request.
var slugPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)

// Membership is a user's standing in an organization or a team. State is "active" or
// "pending"; Role is "admin" or "member" in an organization and "member" or "maintainer" in
// a team. Only an active membership counts for anything.
type Membership struct {
	State string `json:"state"`
	Role  string `json:"role"`
}

// Active reports whether the membership is accepted.
func (m Membership) Active() bool { return m.State == "active" }

// UserLogin resolves the current login of the account with the numeric id. found is false
// for an account GitHub does not know.
func (c *Client) UserLogin(ctx context.Context, bearer string, userID int64) (login string, found bool, err error) {
	status, body, err := c.do(ctx, http.MethodGet, "/user/"+strconv.FormatInt(userID, 10), bearer, nil)
	if err != nil {
		return "", false, err
	}
	switch status {
	case http.StatusOK:
	case http.StatusNotFound:
		return "", false, nil
	default:
		return "", false, statusError(status)
	}
	var out User
	if err := json.Unmarshal(body, &out); err != nil || !out.valid() || out.ID != userID {
		return "", false, fmt.Errorf("%w: response is not the requested user", ErrRequestFailed)
	}
	return out.Login, true, nil
}

// OrganizationMembership is GET /orgs/{org}/memberships/{login}. found is false when the
// user has no membership, which is an answer and not an error.
func (c *Client) OrganizationMembership(ctx context.Context, bearer, org, login string) (Membership, bool, error) {
	if !loginPattern.MatchString(org) || !validUserLogin(login) {
		return Membership{}, false, nil
	}
	return c.membership(ctx, bearer, "/orgs/"+url.PathEscape(org)+"/memberships/"+url.PathEscape(login))
}

// TeamMembership is GET /orgs/{org}/teams/{team}/memberships/{login}, which includes the
// members of child teams. found is false when the user is not in the team or GitHub does
// not know the team.
func (c *Client) TeamMembership(ctx context.Context, bearer, org, team, login string) (Membership, bool, error) {
	if !loginPattern.MatchString(org) || !validUserLogin(login) || !slugPattern.MatchString(team) {
		return Membership{}, false, nil
	}
	return c.membership(ctx, bearer, "/orgs/"+url.PathEscape(org)+"/teams/"+url.PathEscape(team)+"/memberships/"+url.PathEscape(login))
}

func (c *Client) membership(ctx context.Context, bearer, path string) (Membership, bool, error) {
	status, body, err := c.do(ctx, http.MethodGet, path, bearer, nil)
	if err != nil {
		return Membership{}, false, err
	}
	switch status {
	case http.StatusOK:
	case http.StatusNotFound:
		return Membership{}, false, nil
	default:
		return Membership{}, false, statusError(status)
	}
	var out Membership
	if err := json.Unmarshal(body, &out); err != nil || out.State == "" {
		return Membership{}, false, fmt.Errorf("%w: response is not a membership", ErrRequestFailed)
	}
	return out, true, nil
}

// ListUserInstallationRepositoryIDs lists the ids of the repositories of the installation
// that the user token's user can access, every page. A listing that fails part-way is an
// error, never a shorter list.
func (c *Client) ListUserInstallationRepositoryIDs(ctx context.Context, t UserToken, installationID int64) ([]int64, error) {
	var ids []int64
	path := "/user/installations/" + strconv.FormatInt(installationID, 10) + "/repositories"
	err := c.pages(ctx, path, t.value, func(body []byte) error {
		var page struct {
			Repositories []struct {
				ID int64 `json:"id"`
			} `json:"repositories"`
		}
		if err := json.Unmarshal(body, &page); err != nil {
			return fmt.Errorf("%w: response is not a list of repositories", ErrRequestFailed)
		}
		for _, repo := range page.Repositories {
			if repo.ID <= 0 {
				return fmt.Errorf("%w: response holds something that is not a repository", ErrRequestFailed)
			}
			ids = append(ids, repo.ID)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// InstallationRepositoryIDs lists the ids of the installation's repositories the user can
// access, with the user's own token.
func (o *OAuthClient) InstallationRepositoryIDs(ctx context.Context, t UserToken, installationID int64) ([]int64, error) {
	ids, err := o.api.ListUserInstallationRepositoryIDs(ctx, t, installationID)
	if err != nil {
		return nil, userTokenError(err)
	}
	return ids, nil
}

// userTokenError reports a refusal of a user token as ErrUserTokenRejected, not as a
// refusal of the App's credentials.
func userTokenError(err error) error {
	if errors.Is(err, ErrAppRejected) {
		return ErrUserTokenRejected
	}
	return err
}

// UserLogin resolves the current login of the account with the numeric id, with an
// installation token.
func (c *AppClient) UserLogin(ctx context.Context, installationID, userID int64) (string, bool, error) {
	tok, err := c.Token(ctx, installationID)
	if err != nil {
		return "", false, err
	}
	return c.api.UserLogin(ctx, tok.Value(), userID)
}

// OrganizationMembership reads a user's membership of an organization with an installation
// token.
func (c *AppClient) OrganizationMembership(ctx context.Context, installationID int64, org, login string) (Membership, bool, error) {
	tok, err := c.Token(ctx, installationID)
	if err != nil {
		return Membership{}, false, err
	}
	return c.api.OrganizationMembership(ctx, tok.Value(), org, login)
}

// TeamMembership reads a user's membership of a team with an installation token.
func (c *AppClient) TeamMembership(ctx context.Context, installationID int64, org, team, login string) (Membership, bool, error) {
	tok, err := c.Token(ctx, installationID)
	if err != nil {
		return Membership{}, false, err
	}
	return c.api.TeamMembership(ctx, tok.Value(), org, team, login)
}
