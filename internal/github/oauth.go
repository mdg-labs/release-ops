package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

var (
	// ErrOAuthRejected is returned when GitHub refuses the authorization code (it is wrong,
	// expired, used, or does not match the PKCE verifier or the client credentials).
	ErrOAuthRejected = errors.New("github rejected the authorization code")
	// ErrUserTokenRejected is returned when GitHub refuses a user token (401, 403).
	ErrUserTokenRejected = errors.New("github rejected the user token")
)

// loginPattern is an account name that is also an organization name: up to 39 letters,
// digits and hyphens, starting with a letter or a digit.
var loginPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})$`)

// userLoginPattern is a user's login: letters, digits and single hyphens, neither starting
// nor ending with a hyphen, followed for an Enterprise Managed User by an underscore and the
// short code of the enterprise (mona-cat_octo).
var userLoginPattern = regexp.MustCompile(`^[A-Za-z0-9]+(?:-[A-Za-z0-9]+)*(?:_[A-Za-z0-9]{1,39})?$`)

func validUserLogin(s string) bool {
	name, _, _ := strings.Cut(s, "_")
	return len(name) <= 39 && userLoginPattern.MatchString(s)
}

// ValidUserLogin reports whether s is a GitHub user login, a managed user's included.
func ValidUserLogin(s string) bool { return validUserLogin(s) }

// UserToken is a GitHub user access token. It is held in memory for the length of one
// sign-in; the type renders without the value when printed or logged.
type UserToken struct{ value string }

// String omits the token.
func (UserToken) String() string { return "github user token" }

// GoString omits the token.
func (t UserToken) GoString() string { return t.String() }

// LogValue omits the token.
func (UserToken) LogValue() slog.Value { return slog.StringValue("github user token") }

// User is GET /user: the numeric id is the identity, the login is display data.
type User struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`
}

func (u User) valid() bool {
	return u.ID > 0 && validUserLogin(u.Login)
}

// ExchangeParams is what the token endpoint takes for an authorization code.
type ExchangeParams struct {
	ClientID     string
	ClientSecret string
	Code         string
	RedirectURI  string
	CodeVerifier string
}

// OAuthClient runs the OAuth web flow of a GitHub App against the web host and reads the
// signed-in user from the API host.
type OAuthClient struct {
	api     *Client
	webBase string
}

// NewOAuthClient returns an OAuthClient that sends the authorize redirect and the token
// exchange to webBaseURL (GITHUB_BASE_URL) and the user calls through api.
func NewOAuthClient(api *Client, webBaseURL string) *OAuthClient {
	return &OAuthClient{api: api, webBase: strings.TrimRight(webBaseURL, "/")}
}

// AuthorizeURL is where the browser is sent to authorize the App: only the App's client id,
// the registered callback, the single-use state and the S256 PKCE challenge.
func (o *OAuthClient) AuthorizeURL(clientID, redirectURI, state, codeChallenge string) string {
	q := url.Values{}
	q.Set("client_id", clientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("state", state)
	q.Set("code_challenge", codeChallenge)
	q.Set("code_challenge_method", "S256")
	return o.webBase + "/login/oauth/authorize?" + q.Encode()
}

// ExchangeCode exchanges the authorization code for the user's access token. GitHub answers
// 200 with an error field for a refused code; that is ErrOAuthRejected. Nothing of GitHub's
// response is carried in an error.
func (o *OAuthClient) ExchangeCode(ctx context.Context, p ExchangeParams) (UserToken, error) {
	if !ValidCode(p.Code) {
		return UserToken{}, ErrOAuthRejected
	}
	payload, err := json.Marshal(map[string]string{
		"client_id":     p.ClientID,
		"client_secret": p.ClientSecret,
		"code":          p.Code,
		"redirect_uri":  p.RedirectURI,
		"code_verifier": p.CodeVerifier,
	})
	if err != nil {
		return UserToken{}, ErrRequestFailed
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.webBase+"/login/oauth/access_token", bytes.NewReader(payload))
	if err != nil {
		return UserToken{}, ErrRequestFailed
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)

	status, body, err := o.api.send(req)
	if err != nil {
		return UserToken{}, err
	}
	if status != http.StatusOK {
		return UserToken{}, fmt.Errorf("%w: status %d", ErrRequestFailed, status)
	}
	var out struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return UserToken{}, fmt.Errorf("%w: response is not a token response", ErrRequestFailed)
	}
	if out.Error != "" {
		return UserToken{}, ErrOAuthRejected
	}
	if out.AccessToken == "" {
		return UserToken{}, fmt.Errorf("%w: response carries no token", ErrRequestFailed)
	}
	return UserToken{value: out.AccessToken}, nil
}

// User fetches the user the token belongs to.
func (o *OAuthClient) User(ctx context.Context, t UserToken) (User, error) {
	status, body, err := o.api.do(ctx, http.MethodGet, "/user", t.value, nil)
	if err != nil {
		return User{}, err
	}
	switch status {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden:
		return User{}, ErrUserTokenRejected
	default:
		return User{}, fmt.Errorf("%w: status %d", ErrRequestFailed, status)
	}
	var out User
	if err := json.Unmarshal(body, &out); err != nil || !out.valid() {
		return User{}, fmt.Errorf("%w: response is not a user", ErrRequestFailed)
	}
	return out, nil
}

// IsOrganizationOwner reports whether the token's user is an active owner of org: an active
// membership with the admin role. Not a member is false, not an error.
func (o *OAuthClient) IsOrganizationOwner(ctx context.Context, t UserToken, org string) (bool, error) {
	if !loginPattern.MatchString(org) {
		return false, nil
	}
	status, body, err := o.api.do(ctx, http.MethodGet, "/user/memberships/orgs/"+url.PathEscape(org), t.value, nil)
	if err != nil {
		return false, err
	}
	switch status {
	case http.StatusOK:
	case http.StatusNotFound:
		return false, nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return false, ErrUserTokenRejected
	default:
		return false, fmt.Errorf("%w: status %d", ErrRequestFailed, status)
	}
	var out struct {
		State string `json:"state"`
		Role  string `json:"role"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return false, fmt.Errorf("%w: response is not a membership", ErrRequestFailed)
	}
	return out.State == "active" && out.Role == "admin", nil
}

// send performs a prepared request with the client's bounds: no redirects, a timeout and a
// capped body.
func (c *Client) send(req *http.Request) (int, []byte, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("%w: request did not complete", ErrRequestFailed)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return 0, nil, fmt.Errorf("%w: response not readable", ErrRequestFailed)
	}
	if len(body) > maxResponseBytes {
		return 0, nil, fmt.Errorf("%w: response too large", ErrRequestFailed)
	}
	return resp.StatusCode, body, nil
}
