package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// ErrBranchNotFound is returned for a branch GitHub does not know, such as the default branch
// of a repository that has no commit yet.
var ErrBranchNotFound = errors.New("github branch not found")

// Issue is an issue as GET /repos/{owner}/{repo}/issues lists it. The API lists pull requests
// among the issues; PullRequest is set for them.
type Issue struct {
	Number int64   `json:"number"`
	Body   *string `json:"body"`
	State  string  `json:"state"`
	User   struct {
		ID int64 `json:"id"`
	} `json:"user"`
	PullRequest *struct{} `json:"pull_request"`
}

// IssuePage is one page of the open issues a user created in a repository.
type IssuePage struct {
	Issues []Issue
	// Next is the URL of the next page, empty on the last one.
	Next string
	// ETag is the entity tag of this page, for a conditional request of it.
	ETag string
	// NotModified is true when the request carried the page's ETag and GitHub answered 304;
	// the page has no issues then.
	NotModified bool
}

// OpenIssuesPage reads one page of GET /repos/{owner}/{repo}/issues?state=open&creator={creator}.
// An empty pageURL asks for the first page; a later one is the Next of the page before it. A
// non-empty etag makes the request conditional. A page URL on another host than the API's is
// refused, so the bearer credential never leaves it.
func (c *Client) OpenIssuesPage(ctx context.Context, token, fullName, creator, pageURL, etag string) (IssuePage, error) {
	base, err := url.Parse(c.apiBase)
	if err != nil {
		return IssuePage{}, ErrRequestFailed
	}
	if pageURL == "" {
		owner, name, ok := ownerAndName(fullName)
		if !ok {
			return IssuePage{}, fmt.Errorf("repository %q is not owner/name", fullName)
		}
		pageURL = c.apiBase + "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(name) +
			"/issues?state=open&creator=" + url.QueryEscape(creator) + "&per_page=" + strconv.Itoa(perPage)
	} else if u, err := url.Parse(pageURL); err != nil || u.Scheme != base.Scheme || u.Host != base.Host {
		return IssuePage{}, fmt.Errorf("%w: page is on another host", ErrRequestFailed)
	}
	status, header, body, err := c.sendWith(ctx, http.MethodGet, pageURL, token, nil, maxListBytes, conditional(etag))
	if err != nil {
		return IssuePage{}, err
	}
	switch status {
	case http.StatusOK:
	case http.StatusNotModified:
		next, err := nextLink(header.Values("Link"), base)
		if err != nil {
			return IssuePage{}, err
		}
		return IssuePage{NotModified: true, ETag: etag, Next: next}, nil
	default:
		return IssuePage{}, statusError(status)
	}
	var issues []Issue
	if err := json.Unmarshal(body, &issues); err != nil {
		return IssuePage{}, fmt.Errorf("%w: response is not a list of issues", ErrRequestFailed)
	}
	next, err := nextLink(header.Values("Link"), base)
	if err != nil {
		return IssuePage{}, err
	}
	return IssuePage{Issues: issues, Next: next, ETag: header.Get("ETag")}, nil
}

// BranchHead is the commit a branch points at.
type BranchHead struct {
	SHA string
	// ETag is the entity tag of the answer, for a conditional request of it.
	ETag string
	// NotModified is true when the request carried the ETag and GitHub answered 304; SHA is
	// empty then.
	NotModified bool
}

// BranchHead reads GET /repos/{owner}/{repo}/git/ref/heads/{branch}. A non-empty etag makes the
// request conditional. It returns ErrBranchNotFound for a branch that does not exist.
func (c *Client) BranchHead(ctx context.Context, token, fullName, branch, etag string) (BranchHead, error) {
	owner, name, ok := ownerAndName(fullName)
	if !ok {
		return BranchHead{}, fmt.Errorf("repository %q is not owner/name", fullName)
	}
	segments := strings.Split(branch, "/")
	for i, s := range segments {
		segments[i] = url.PathEscape(s)
	}
	target := c.apiBase + "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(name) + "/git/ref/heads/" + strings.Join(segments, "/")
	status, header, body, err := c.sendWith(ctx, http.MethodGet, target, token, nil, maxResponseBytes, conditional(etag))
	if err != nil {
		return BranchHead{}, err
	}
	switch status {
	case http.StatusOK:
	case http.StatusNotModified:
		return BranchHead{NotModified: true, ETag: etag}, nil
	case http.StatusNotFound:
		return BranchHead{}, ErrBranchNotFound
	default:
		return BranchHead{}, statusError(status)
	}
	var ref struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := json.Unmarshal(body, &ref); err != nil || ref.Object.SHA == "" {
		return BranchHead{}, fmt.Errorf("%w: response is not a branch reference", ErrRequestFailed)
	}
	return BranchHead{SHA: ref.Object.SHA, ETag: header.Get("ETag")}, nil
}

func conditional(etag string) http.Header {
	if etag == "" {
		return nil
	}
	return http.Header{"If-None-Match": {etag}}
}
