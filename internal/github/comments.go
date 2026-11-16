package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/mdg-labs/release-ops/internal/store"
)

// ConsumerPRComment is the consumer label of the deferrals the PR comment upsert has while an
// installation's rate-limit budget is used up.
const ConsumerPRComment = "pr_comment"

// maxCommentBytes bounds the body of a comment Release Ops writes; GitHub's own limit is 65536
// characters.
const maxCommentBytes = 60000

var (
	// ErrCommentNotFound is returned by UpdateIssueComment for a comment GitHub does not know.
	ErrCommentNotFound = errors.New("github comment not found")
	// ErrBotUnknown is returned by PRCommenter.Upsert while the bot user's id is not stored, so
	// the comment of the bot cannot be told from another one.
	ErrBotUnknown = errors.New("the app's bot user id is not known yet")
)

// IssueComment is a comment on an issue or a pull request, from the issues API.
type IssueComment struct {
	ID   int64  `json:"id"`
	Body string `json:"body"`
	User struct {
		ID int64 `json:"id"`
	} `json:"user"`
}

func issueCommentsPath(fullName string, number int64) (string, error) {
	owner, name, ok := ownerAndName(fullName)
	if !ok {
		return "", fmt.Errorf("repository %q is not owner/name", fullName)
	}
	return "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(name) + "/issues/" + strconv.FormatInt(number, 10) + "/comments", nil
}

// ListIssueComments lists every comment on the pull request, oldest first, every page. A failing
// page is an error, never a partial list: the caller decides from the whole of it that no comment
// of its own exists.
func (c *Client) ListIssueComments(ctx context.Context, token, fullName string, number int64) ([]IssueComment, error) {
	path, err := issueCommentsPath(fullName, number)
	if err != nil {
		return nil, err
	}
	var out []IssueComment
	err = c.pages(ctx, path, token, func(body []byte) error {
		var page []IssueComment
		if err := json.Unmarshal(body, &page); err != nil {
			return fmt.Errorf("%w: response is not a list of comments", ErrRequestFailed)
		}
		out = append(out, page...)
		return nil
	})
	if errors.Is(err, ErrInstallationNotFound) {
		return nil, ErrPullRequestNotFound
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

// CreateIssueComment posts a comment on the pull request and returns it.
func (c *Client) CreateIssueComment(ctx context.Context, token, fullName string, number int64, body string) (IssueComment, error) {
	path, err := issueCommentsPath(fullName, number)
	if err != nil {
		return IssueComment{}, err
	}
	status, resp, err := c.writeComment(ctx, http.MethodPost, path, token, body)
	if err != nil {
		return IssueComment{}, err
	}
	switch status {
	case http.StatusCreated:
	case http.StatusNotFound:
		return IssueComment{}, ErrPullRequestNotFound
	default:
		return IssueComment{}, statusError(status)
	}
	var out IssueComment
	if err := json.Unmarshal(resp, &out); err != nil || out.ID <= 0 {
		return IssueComment{}, fmt.Errorf("%w: response is not a comment", ErrRequestFailed)
	}
	return out, nil
}

// UpdateIssueComment replaces the body of a comment of the repository, or answers
// ErrCommentNotFound for one that is gone.
func (c *Client) UpdateIssueComment(ctx context.Context, token, fullName string, id int64, body string) (IssueComment, error) {
	owner, name, ok := ownerAndName(fullName)
	if !ok {
		return IssueComment{}, fmt.Errorf("repository %q is not owner/name", fullName)
	}
	path := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(name) + "/issues/comments/" + strconv.FormatInt(id, 10)
	status, resp, err := c.writeComment(ctx, http.MethodPatch, path, token, body)
	if err != nil {
		return IssueComment{}, err
	}
	switch status {
	case http.StatusOK:
	case http.StatusNotFound:
		return IssueComment{}, ErrCommentNotFound
	default:
		return IssueComment{}, statusError(status)
	}
	var out IssueComment
	if err := json.Unmarshal(resp, &out); err != nil || out.ID != id {
		return IssueComment{}, fmt.Errorf("%w: response is not the updated comment", ErrRequestFailed)
	}
	return out, nil
}

func (c *Client) writeComment(ctx context.Context, method, path, token, body string) (int, []byte, error) {
	if len(body) > maxCommentBytes {
		return 0, nil, fmt.Errorf("%w: comment is too long", ErrRequestFailed)
	}
	raw, err := json.Marshal(map[string]string{"body": body})
	if err != nil {
		return 0, nil, ErrRequestFailed
	}
	return c.do(ctx, method, path, token, bytes.NewReader(raw))
}

// CommentTokens mints the installation token a PR comment is written with.
type CommentTokens interface {
	// CommentToken returns a token for the repository (owner/name) of the installation, or a
	// rate-limit error while the installation's budget is used up.
	CommentToken(ctx context.Context, installationID int64, repository string) (InstallationToken, error)
}

// CommentToken returns an installation token scoped to the repository (owner/name) for the PR
// comment upsert: while the installation's rate-limit budget is exhausted no token is served and
// the upsert is deferred.
func (c *AppClient) CommentToken(ctx context.Context, installationID int64, repository string) (InstallationToken, error) {
	if err := c.budget.Gate(installationID, ConsumerPRComment); err != nil {
		return InstallationToken{}, err
	}
	return c.repositoryToken(ctx, installationID, repository, nil, tokenRefreshMargin)
}

// PRCommenter keeps one comment of the App's bot user on a pull request.
type PRCommenter struct {
	api    *Client
	tokens CommentTokens
	bot    BotStore
}

// NewPRCommenter returns a commenter that calls GitHub through api with tokens from tokens and
// tells the bot's comments by the bot user id kept in bot.
func NewPRCommenter(api *Client, tokens CommentTokens, bot BotStore) *PRCommenter {
	return &PRCommenter{api: api, tokens: tokens, bot: bot}
}

// CommentUpsert is one comment to bring to a body.
type CommentUpsert struct {
	InstallationID int64
	// Repository is owner/name.
	Repository string
	Number     int64
	// Marker is the hidden text every comment of Release Ops on the pull request carries.
	Marker string
	// CommentID is the stored id of the comment; 0 when none is stored.
	CommentID int64
	Body      string
}

// Upsert makes the pull request carry exactly one comment of the bot with the marker and the
// body, and returns its id. A stored id is edited in place; one that GitHub no longer knows, or
// none, makes it look through the comments of the pull request for a comment of the bot with the
// marker and adopt it, so a comment posted before its id was stored is never posted twice, and
// only then post a new one. It never touches the pull request itself.
func (p *PRCommenter) Upsert(ctx context.Context, in CommentUpsert) (int64, error) {
	if in.Marker == "" || !strings.Contains(in.Body, in.Marker) {
		return 0, errors.New("the comment body does not carry its marker")
	}
	bot, err := p.bot.BotIdentity(ctx)
	switch {
	case errors.Is(err, store.ErrGitHubAppNotFound):
		return 0, ErrBotUnknown
	case err != nil:
		return 0, fmt.Errorf("read the bot user: %w", err)
	case !bot.Known:
		return 0, ErrBotUnknown
	}
	tok, err := p.tokens.CommentToken(ctx, in.InstallationID, in.Repository)
	if err != nil {
		return 0, err
	}

	if in.CommentID != 0 {
		c, err := p.api.UpdateIssueComment(ctx, tok.Value(), in.Repository, in.CommentID, in.Body)
		if err == nil {
			return c.ID, nil
		}
		if !errors.Is(err, ErrCommentNotFound) {
			return 0, err
		}
	}

	comments, err := p.api.ListIssueComments(ctx, tok.Value(), in.Repository, in.Number)
	if err != nil {
		return 0, err
	}
	for _, c := range comments {
		if c.User.ID != bot.UserID || !strings.Contains(c.Body, in.Marker) {
			continue
		}
		if c.Body == in.Body {
			return c.ID, nil
		}
		updated, err := p.api.UpdateIssueComment(ctx, tok.Value(), in.Repository, c.ID, in.Body)
		if err == nil {
			return updated.ID, nil
		}
		if !errors.Is(err, ErrCommentNotFound) {
			return 0, err
		}
		// Deleted since the listing: the next marker comment, if any, is tried, then a new one.
	}

	c, err := p.api.CreateIssueComment(ctx, tok.Value(), in.Repository, in.Number, in.Body)
	if err != nil {
		return 0, err
	}
	return c.ID, nil
}
