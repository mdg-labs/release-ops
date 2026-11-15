package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// ErrBotUserLookupFailed is returned when the numeric id of an App's bot user could not be
// read: GitHub was unreachable, timed out, answered with another status, or sent a document
// that is not that bot user. Nothing of GitHub's response is carried in the error.
var ErrBotUserLookupFailed = errors.New("github bot user lookup failed")

var appSlugPattern = regexp.MustCompile(`^[A-Za-z0-9-]{1,100}$`)

// BotUserID reads the numeric id of the App's bot user <slug>[bot] from GET /users/{login}.
// The endpoint is public, so the call carries no credential. The id is returned only when
// the document is that bot user: type Bot, the login asked for, a positive id.
func (c *Client) BotUserID(ctx context.Context, slug string) (int64, error) {
	if !appSlugPattern.MatchString(slug) {
		return 0, fmt.Errorf("%w: not an App slug", ErrBotUserLookupFailed)
	}
	login := slug + "[bot]"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiBase+"/users/"+url.PathEscape(login), nil)
	if err != nil {
		return 0, ErrBotUserLookupFailed
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", apiVersion)
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("%w: request did not complete", ErrBotUserLookupFailed)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("%w: status %d", ErrBotUserLookupFailed, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return 0, fmt.Errorf("%w: response not readable", ErrBotUserLookupFailed)
	}
	if len(body) > maxResponseBytes {
		return 0, fmt.Errorf("%w: response too large", ErrBotUserLookupFailed)
	}
	var user struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
		Type  string `json:"type"`
	}
	if err := json.Unmarshal(body, &user); err != nil || user.ID <= 0 || user.Type != "Bot" || !strings.EqualFold(user.Login, login) {
		return 0, fmt.Errorf("%w: response is not the bot user", ErrBotUserLookupFailed)
	}
	return user.ID, nil
}
