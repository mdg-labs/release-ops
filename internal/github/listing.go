package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const (
	// maxListBytes bounds one page of a listing; a page of 100 repositories is a few hundred
	// kilobytes.
	maxListBytes = 8 << 20
	// maxPages bounds how many pages a listing follows, so a server that keeps answering
	// with a next link cannot hold the sync forever.
	maxPages = 200
	perPage  = 100
)

// Repository is a repository of an installation, from GET /installation/repositories.
type Repository struct {
	ID            int64  `json:"id"`
	FullName      string `json:"full_name"`
	DefaultBranch string `json:"default_branch"`
}

func (r Repository) valid() bool {
	_, _, ok := ownerAndName(r.FullName)
	return r.ID > 0 && ok && r.DefaultBranch != ""
}

// ContentEntry is one entry of a repository directory.
type ContentEntry struct {
	Name string `json:"name"`
	// Type is file or dir.
	Type string `json:"type"`
}

// ListInstallations lists the installations of the App that appJWT authenticates, every page.
func (c *Client) ListInstallations(ctx context.Context, appJWT string) ([]Installation, error) {
	var out []Installation
	err := c.pages(ctx, "/app/installations", appJWT, func(body []byte) error {
		var page []Installation
		if err := json.Unmarshal(body, &page); err != nil {
			return fmt.Errorf("%w: response is not a list of installations", ErrRequestFailed)
		}
		for _, inst := range page {
			if !inst.valid() {
				return fmt.Errorf("%w: response holds something that is not an installation", ErrRequestFailed)
			}
		}
		out = append(out, page...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ListInstallationRepositories lists the repositories the installation token can access,
// every page.
func (c *Client) ListInstallationRepositories(ctx context.Context, token string) ([]Repository, error) {
	var out []Repository
	err := c.pages(ctx, "/installation/repositories", token, func(body []byte) error {
		var page struct {
			Repositories []Repository `json:"repositories"`
		}
		if err := json.Unmarshal(body, &page); err != nil {
			return fmt.Errorf("%w: response is not a list of repositories", ErrRequestFailed)
		}
		for _, repo := range page.Repositories {
			if !repo.valid() {
				return fmt.Errorf("%w: response holds something that is not a repository", ErrRequestFailed)
			}
		}
		out = append(out, page.Repositories...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ListDirectory lists a directory of the repository (owner/name) at its default branch; dir
// is empty for the root. A directory that does not exist, a path that is a file and an empty
// repository all give no entries and no error.
func (c *Client) ListDirectory(ctx context.Context, token, fullName, dir string) ([]ContentEntry, error) {
	owner, name, ok := ownerAndName(fullName)
	if !ok {
		return nil, fmt.Errorf("repository %q is not owner/name", fullName)
	}
	path := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(name) + "/contents"
	if dir != "" {
		path += "/" + url.PathEscape(dir)
	}
	status, _, body, err := c.sendTo(ctx, http.MethodGet, c.apiBase+path, token, nil, maxListBytes)
	if err != nil {
		return nil, err
	}
	switch status {
	case http.StatusOK:
	case http.StatusNotFound:
		return nil, nil
	default:
		return nil, statusError(status)
	}
	var entries []ContentEntry
	if err := json.Unmarshal(body, &entries); err != nil {
		var single map[string]any
		if json.Unmarshal(body, &single) == nil {
			return nil, nil
		}
		return nil, fmt.Errorf("%w: response is not a directory listing", ErrRequestFailed)
	}
	return entries, nil
}

// pages GETs path and every page its Link headers point to, handing each body to handle. A
// next link to another host ends the listing with an error, so the bearer credential never
// leaves the configured API host; so does a page that fails, because a partial listing is
// not one the caller may act on.
func (c *Client) pages(ctx context.Context, path, bearer string, handle func(body []byte) error) error {
	base, err := url.Parse(c.apiBase)
	if err != nil {
		return ErrRequestFailed
	}
	next := c.apiBase + path + "?per_page=" + fmt.Sprint(perPage)
	for page := 0; next != ""; page++ {
		if page >= maxPages {
			return fmt.Errorf("%w: listing has too many pages", ErrRequestFailed)
		}
		status, header, body, err := c.sendTo(ctx, http.MethodGet, next, bearer, nil, maxListBytes)
		if err != nil {
			return err
		}
		if status != http.StatusOK {
			return statusError(status)
		}
		if err := handle(body); err != nil {
			return err
		}
		next, err = nextLink(header.Values("Link"), base)
		if err != nil {
			return err
		}
	}
	return nil
}

func statusError(status int) error {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return ErrAppRejected
	case http.StatusNotFound:
		return ErrInstallationNotFound
	default:
		return fmt.Errorf("%w: status %d", ErrRequestFailed, status)
	}
}

// nextLink returns the rel="next" target of the Link headers, or "" when there is none. A
// target on another scheme or host than base is an error.
func nextLink(values []string, base *url.URL) (string, error) {
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			target, params, ok := strings.Cut(strings.TrimSpace(part), ";")
			if !ok || !strings.Contains(params, `rel="next"`) {
				continue
			}
			target = strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(target), "<"), ">")
			u, err := url.Parse(target)
			if err != nil || u.Scheme != base.Scheme || u.Host != base.Host {
				return "", fmt.Errorf("%w: next page is on another host", ErrRequestFailed)
			}
			return u.String(), nil
		}
	}
	return "", nil
}
