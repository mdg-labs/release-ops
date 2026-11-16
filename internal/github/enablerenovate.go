package github

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

const (
	// EnableRenovateBranch is the branch the "Enable repo" pull request is opened from. It is
	// how an earlier, still open pull request of the action is recognised.
	EnableRenovateBranch = "release-ops/enable-renovate"

	// DefaultRenovatePreset is what the config extends when no preset repository is configured.
	DefaultRenovatePreset = "config:recommended"

	renovateConfigPath = "renovate.json"
)

var (
	// ErrConfigExists is returned by Enable when the default branch already has a Renovate
	// config file.
	ErrConfigExists = errors.New("the repository already has a Renovate config file")
	// ErrBranchExists is returned by CreateBranch for a branch that already exists.
	ErrBranchExists = errors.New("github branch already exists")
)

// RenovatePreset is the preset the config of "Enable repo" extends: the first preset
// repository, written as local>owner/repo, or the default preset when none is configured.
func RenovatePreset(presetRepositories []string) string {
	if len(presetRepositories) == 0 || presetRepositories[0] == "" {
		return DefaultRenovatePreset
	}
	return "local>" + presetRepositories[0]
}

// EnableTokens mints the installation token "Enable repo" writes with.
type EnableTokens interface {
	// EnableToken returns a token for the repository (owner/name) of the installation, or a
	// rate-limit error while the installation's budget is used up.
	EnableToken(ctx context.Context, installationID int64, repository string) (InstallationToken, error)
}

// enableTokenPermissions is all "Enable repo" needs: it writes a file and a branch, and opens a
// pull request.
var enableTokenPermissions = map[string]string{"contents": "write", "pull_requests": "write"}

// EnableToken returns an installation token scoped to the one repository (owner/name) and to
// contents and pull request write, for the pull request of "Enable repo". While the
// installation's rate-limit budget is used up no token is served; the refusal is not counted
// as the deferral of a consumer, because the action is answered to the admin at once and is
// not retried later.
func (c *AppClient) EnableToken(ctx context.Context, installationID int64, repository string) (InstallationToken, error) {
	if err := c.budget.Gate(installationID, ""); err != nil {
		return InstallationToken{}, err
	}
	return c.repositoryToken(ctx, installationID, repository, nil, enableTokenPermissions, tokenRefreshMargin)
}

// EnabledPullRequest is the pull request that adds the Renovate config.
type EnabledPullRequest struct {
	URL    string
	Number int64
	// Existing is true when the pull request was already open and nothing was opened.
	Existing bool
}

// RenovateEnabler opens the pull request that adds a renovate.json to a repository.
type RenovateEnabler struct {
	api    *Client
	tokens EnableTokens

	// mu serialises the action, so two clicks at once open one pull request.
	mu sync.Mutex
}

// NewRenovateEnabler returns an enabler that calls GitHub through api with tokens from tokens.
func NewRenovateEnabler(api *Client, tokens EnableTokens) *RenovateEnabler {
	return &RenovateEnabler{api: api, tokens: tokens}
}

// Enable opens a pull request in the repository (owner/name) that adds a renovate.json
// extending preset, from EnableRenovateBranch to the default branch. A pull request of the
// action that is still open is returned as it is. It returns ErrConfigExists when the default
// branch already has a Renovate config file. Nothing is written to the default branch, and
// the pull request is never merged.
func (e *RenovateEnabler) Enable(ctx context.Context, installationID int64, fullName, defaultBranch, preset string) (EnabledPullRequest, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	tok, err := e.tokens.EnableToken(ctx, installationID, fullName)
	if err != nil {
		return EnabledPullRequest{}, err
	}
	token := tok.Value()

	exists, err := e.api.hasRenovateConfig(ctx, token, fullName)
	if err != nil {
		return EnabledPullRequest{}, err
	}
	if exists {
		return EnabledPullRequest{}, ErrConfigExists
	}

	open, found, err := e.api.OpenPullRequestFromBranch(ctx, token, fullName, EnableRenovateBranch)
	if err != nil {
		return EnabledPullRequest{}, err
	}
	if found {
		return EnabledPullRequest{URL: open.HTMLURL, Number: open.Number, Existing: true}, nil
	}

	base, err := e.api.BranchHead(ctx, token, fullName, defaultBranch, "")
	if err != nil {
		return EnabledPullRequest{}, err
	}
	// A branch left by an earlier attempt that failed, or by a pull request that was closed
	// unmerged, only ever holds this one commit: it is moved to the default branch's head.
	_, err = e.api.BranchHead(ctx, token, fullName, EnableRenovateBranch, "")
	switch {
	case err == nil:
		err = e.api.ResetBranch(ctx, token, fullName, EnableRenovateBranch, base.SHA)
	case errors.Is(err, ErrBranchNotFound):
		err = e.api.CreateBranch(ctx, token, fullName, EnableRenovateBranch, base.SHA)
	}
	if err != nil {
		return EnabledPullRequest{}, err
	}

	var config bytes.Buffer
	enc := json.NewEncoder(&config)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(map[string][]string{"extends": {preset}}); err != nil {
		return EnabledPullRequest{}, ErrRequestFailed
	}
	if err := e.api.CreateFile(ctx, token, fullName, renovateConfigPath, EnableRenovateBranch, "chore: add renovate.json", config.Bytes()); err != nil {
		return EnabledPullRequest{}, err
	}
	pr, err := e.api.CreatePullRequest(ctx, token, fullName, "Configure Renovate", enableRenovateBody(preset), EnableRenovateBranch, defaultBranch)
	if err != nil {
		return EnabledPullRequest{}, err
	}
	return EnabledPullRequest{URL: pr.HTMLURL, Number: pr.Number}, nil
}

func enableRenovateBody(preset string) string {
	return "This pull request adds a `renovate.json` that extends `" + preset + "`, so Renovate starts " +
		"opening update pull requests for this repository.\n\nRelease Ops opened it and never merges it."
}

func repoPath(fullName string) (string, error) {
	owner, name, ok := ownerAndName(fullName)
	if !ok {
		return "", fmt.Errorf("repository %q is not owner/name", fullName)
	}
	return "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(name), nil
}

func pathSegments(path string) string {
	segments := strings.Split(path, "/")
	for i, s := range segments {
		segments[i] = url.PathEscape(s)
	}
	return strings.Join(segments, "/")
}

// OpenPullRequestFromBranch finds the open pull request of the repository (owner/name) whose
// head is the branch of that repository itself; found is false when there is none.
func (c *Client) OpenPullRequestFromBranch(ctx context.Context, token, fullName, branch string) (pr PullRequest, found bool, err error) {
	base, err := repoPath(fullName)
	if err != nil {
		return PullRequest{}, false, err
	}
	owner, _, _ := ownerAndName(fullName)
	query := url.Values{"state": {"open"}, "head": {owner + ":" + branch}, "per_page": {"100"}}
	status, _, body, err := c.sendTo(ctx, http.MethodGet, c.apiBase+base+"/pulls?"+query.Encode(), token, nil, maxListBytes)
	if err != nil {
		return PullRequest{}, false, err
	}
	if status != http.StatusOK {
		return PullRequest{}, false, statusError(status)
	}
	var prs []PullRequest
	if err := json.Unmarshal(body, &prs); err != nil {
		return PullRequest{}, false, fmt.Errorf("%w: response is not a list of pull requests", ErrRequestFailed)
	}
	for _, p := range prs {
		if p.Head.Ref == branch && p.HTMLURL != "" && p.Number > 0 {
			return p, true, nil
		}
	}
	return PullRequest{}, false, nil
}

// CreateBranch creates the branch at the commit sha, or answers ErrBranchExists.
func (c *Client) CreateBranch(ctx context.Context, token, fullName, branch, sha string) error {
	base, err := repoPath(fullName)
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]string{"ref": "refs/heads/" + branch, "sha": sha})
	status, _, err := c.sendJSON(ctx, http.MethodPost, base+"/git/refs", token, payload)
	if err != nil {
		return err
	}
	switch status {
	case http.StatusCreated:
		return nil
	case http.StatusUnprocessableEntity:
		return ErrBranchExists
	default:
		return statusError(status)
	}
}

// ResetBranch moves the branch to the commit sha, discarding what it held.
func (c *Client) ResetBranch(ctx context.Context, token, fullName, branch, sha string) error {
	base, err := repoPath(fullName)
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]any{"sha": sha, "force": true})
	status, _, err := c.sendJSON(ctx, http.MethodPatch, base+"/git/refs/heads/"+pathSegments(branch), token, payload)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return statusError(status)
	}
	return nil
}

// CreateFile commits a new file to the branch.
func (c *Client) CreateFile(ctx context.Context, token, fullName, path, branch, message string, content []byte) error {
	base, err := repoPath(fullName)
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]string{
		"message": message, "content": base64.StdEncoding.EncodeToString(content), "branch": branch,
	})
	status, _, err := c.sendJSON(ctx, http.MethodPut, base+"/contents/"+pathSegments(path), token, payload)
	if err != nil {
		return err
	}
	if status != http.StatusCreated && status != http.StatusOK {
		return statusError(status)
	}
	return nil
}

// CreatePullRequest opens a pull request from head to base and returns it.
func (c *Client) CreatePullRequest(ctx context.Context, token, fullName, title, body, head, base string) (PullRequest, error) {
	path, err := repoPath(fullName)
	if err != nil {
		return PullRequest{}, err
	}
	payload, _ := json.Marshal(map[string]string{"title": title, "body": body, "head": head, "base": base})
	status, resp, err := c.sendJSON(ctx, http.MethodPost, path+"/pulls", token, payload)
	if err != nil {
		return PullRequest{}, err
	}
	if status != http.StatusCreated {
		return PullRequest{}, statusError(status)
	}
	var pr PullRequest
	if err := json.Unmarshal(resp, &pr); err != nil || pr.Number <= 0 || pr.HTMLURL == "" {
		return PullRequest{}, fmt.Errorf("%w: response is not a pull request", ErrRequestFailed)
	}
	return pr, nil
}

func (c *Client) sendJSON(ctx context.Context, method, path, token string, payload []byte) (int, []byte, error) {
	return c.do(ctx, method, path, token, bytes.NewReader(payload))
}
