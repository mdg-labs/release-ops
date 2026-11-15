package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// ErrPullRequestNotFound is returned for a pull request GitHub does not know.
var ErrPullRequestNotFound = errors.New("github pull request not found")

// PullRequest is a pull request as GitHub reports it, in a webhook payload and in the pulls
// API. Merged is absent from a listing, where MergedAt tells the same.
type PullRequest struct {
	Number    int64      `json:"number"`
	NodeID    string     `json:"node_id"`
	HTMLURL   string     `json:"html_url"`
	Title     string     `json:"title"`
	Body      *string    `json:"body"`
	State     string     `json:"state"`
	Merged    bool       `json:"merged"`
	CreatedAt *time.Time `json:"created_at"`
	UpdatedAt *time.Time `json:"updated_at"`
	ClosedAt  *time.Time `json:"closed_at"`
	MergedAt  *time.Time `json:"merged_at"`
	User      struct {
		ID int64 `json:"id"`
	} `json:"user"`
	Head struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	} `json:"head"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
}

// Valid reports whether the pull request carries what tracking it needs: its number, node id,
// web URL, head branch, a state of open or closed and the creation and update times.
func (p PullRequest) Valid() bool {
	return p.Number > 0 && p.NodeID != "" && p.HTMLURL != "" && p.Head.Ref != "" &&
		(p.State == "open" || p.State == "closed") && p.CreatedAt != nil && p.UpdatedAt != nil
}

// PullRequestPage is one page of the open pull requests of a repository.
type PullRequestPage struct {
	PullRequests []PullRequest
	// Next is the URL of the next page, empty on the last one.
	Next string
	// ETag is the entity tag of this page, for a conditional request of it.
	ETag string
	// NotModified is true when the request carried the page's ETag and GitHub answered 304;
	// the page has no pull requests then.
	NotModified bool
}

// OpenPullRequestsPage reads one page of GET /repos/{owner}/{repo}/pulls?state=open. An empty
// pageURL asks for the first page; a later one is the Next of the page before it. A non-empty
// etag makes the request conditional. A page URL on another host than the API's is refused, so
// the bearer credential never leaves it.
func (c *Client) OpenPullRequestsPage(ctx context.Context, token, fullName, pageURL, etag string) (PullRequestPage, error) {
	base, err := url.Parse(c.apiBase)
	if err != nil {
		return PullRequestPage{}, ErrRequestFailed
	}
	if pageURL == "" {
		owner, name, ok := ownerAndName(fullName)
		if !ok {
			return PullRequestPage{}, fmt.Errorf("repository %q is not owner/name", fullName)
		}
		pageURL = c.apiBase + "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(name) + "/pulls?state=open&per_page=" + strconv.Itoa(perPage)
	} else if u, err := url.Parse(pageURL); err != nil || u.Scheme != base.Scheme || u.Host != base.Host {
		return PullRequestPage{}, fmt.Errorf("%w: page is on another host", ErrRequestFailed)
	}
	var extra http.Header
	if etag != "" {
		extra = http.Header{"If-None-Match": {etag}}
	}
	status, header, body, err := c.sendWith(ctx, http.MethodGet, pageURL, token, nil, maxListBytes, extra)
	if err != nil {
		return PullRequestPage{}, err
	}
	switch status {
	case http.StatusOK:
	case http.StatusNotModified:
		next, err := nextLink(header.Values("Link"), base)
		if err != nil {
			return PullRequestPage{}, err
		}
		return PullRequestPage{NotModified: true, ETag: etag, Next: next}, nil
	default:
		return PullRequestPage{}, statusError(status)
	}
	var prs []PullRequest
	if err := json.Unmarshal(body, &prs); err != nil {
		return PullRequestPage{}, fmt.Errorf("%w: response is not a list of pull requests", ErrRequestFailed)
	}
	next, err := nextLink(header.Values("Link"), base)
	if err != nil {
		return PullRequestPage{}, err
	}
	return PullRequestPage{PullRequests: prs, Next: next, ETag: header.Get("ETag")}, nil
}

// PullRequest reads GET /repos/{owner}/{repo}/pulls/{number}, or ErrPullRequestNotFound.
func (c *Client) PullRequest(ctx context.Context, token, fullName string, number int64) (PullRequest, error) {
	owner, name, ok := ownerAndName(fullName)
	if !ok {
		return PullRequest{}, fmt.Errorf("repository %q is not owner/name", fullName)
	}
	path := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(name) + "/pulls/" + strconv.FormatInt(number, 10)
	status, _, body, err := c.sendTo(ctx, http.MethodGet, c.apiBase+path, token, nil, maxListBytes)
	if err != nil {
		return PullRequest{}, err
	}
	switch status {
	case http.StatusOK:
	case http.StatusNotFound:
		return PullRequest{}, ErrPullRequestNotFound
	default:
		return PullRequest{}, statusError(status)
	}
	var pr PullRequest
	if err := json.Unmarshal(body, &pr); err != nil || pr.Number != number {
		return PullRequest{}, fmt.Errorf("%w: response is not the requested pull request", ErrRequestFailed)
	}
	return pr, nil
}
