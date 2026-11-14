package github

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// tokenRefreshMargin is how long before expiry a cached installation token stops being
	// served, so a caller never receives one about to lapse.
	tokenRefreshMargin = 5 * time.Minute
	// JobTokenMinLife is the least lifetime a token handed to a job may have left. The job
	// runs on that one token and cannot renew it, and the job timeout is below the roughly one
	// hour GitHub grants, so a cached token older than this is replaced by a fresh one.
	JobTokenMinLife = 55 * time.Minute
	// sweepThreshold is the cache size from which expired entries are dropped on insert.
	sweepThreshold = 64
)

// CredentialsSource yields the App id and the decrypted private key (PEM). The key lives only
// as long as the caller signs with it.
type CredentialsSource interface {
	SigningKey(ctx context.Context) (appID int64, privateKeyPEM []byte, err error)
}

// AppClientOptions configures an AppClient.
type AppClientOptions struct {
	// Now is the clock; nil means time.Now.
	Now func() time.Time
}

// AppClient authenticates as the GitHub App: it signs the App JWT, confirms installations and
// mints installation access tokens. It belongs in the app process, the only one that may read
// the private key. Installation tokens are cached in memory until shortly before they expire
// and are never written anywhere.
type AppClient struct {
	api   *Client
	creds CredentialsSource
	now   func() time.Time

	mu      sync.Mutex
	entries map[tokenKey]*tokenEntry
	warned  sync.Map
}

type tokenKey struct {
	installationID int64
	// repositories is the sorted, lower-cased list of repository names the token is
	// scoped to, joined by commas; empty for an unscoped token.
	repositories string
	// target is the lower-cased target repository name of a scoped token. A token minted
	// after a fallback covers the target alone, so two targets never share an entry even
	// when their requested sets are equal.
	target string
}

type tokenEntry struct {
	mu    sync.Mutex
	token InstallationToken
}

// NewAppClient returns an AppClient that calls GitHub through api.
func NewAppClient(api *Client, creds CredentialsSource, opts AppClientOptions) *AppClient {
	c := &AppClient{api: api, creds: creds, now: opts.Now, entries: map[tokenKey]*tokenEntry{}}
	if c.now == nil {
		c.now = time.Now
	}
	return c
}

// appJWT signs a fresh App JWT. The key is cleared as soon as the signature is made.
func (c *AppClient) appJWT(ctx context.Context) (jwt string, appID int64, err error) {
	appID, pem, err := c.creds.SigningKey(ctx)
	if err != nil {
		return "", 0, fmt.Errorf("github app credentials: %w", err)
	}
	defer clear(pem)
	jwt, err = SignAppJWT(appID, pem, c.now())
	if err != nil {
		return "", 0, err
	}
	return jwt, appID, nil
}

// Installation fetches the installation with the App JWT, so what the caller learns about it
// comes from GitHub and not from the request that named it. An installation of another App is
// reported as not found.
func (c *AppClient) Installation(ctx context.Context, installationID int64) (*Installation, error) {
	jwt, appID, err := c.appJWT(ctx)
	if err != nil {
		return nil, err
	}
	inst, err := c.api.GetInstallation(ctx, jwt, installationID)
	if err != nil {
		return nil, err
	}
	if inst.ID != installationID || inst.AppID != appID {
		return nil, ErrInstallationNotFound
	}
	return inst, nil
}

// Token returns an access token for every repository of the installation.
func (c *AppClient) Token(ctx context.Context, installationID int64) (InstallationToken, error) {
	return c.cached(tokenKey{installationID: installationID}, tokenRefreshMargin, func() (InstallationToken, error) {
		return c.mint(ctx, installationID, nil)
	})
}

// RepositoryToken returns an access token scoped to the target repository (owner/name) plus
// the preset repositories. A preset owned by another account than the target cannot be covered
// by the installation and is left out with a warning; if GitHub refuses the token because the
// App is not installed on a preset, the token is requested for the target alone.
func (c *AppClient) RepositoryToken(ctx context.Context, installationID int64, target string, presets []string) (InstallationToken, error) {
	return c.repositoryToken(ctx, installationID, target, presets, tokenRefreshMargin)
}

// JobToken is RepositoryToken for a token that a job will use unrenewed: a cached token with
// less than JobTokenMinLife left is not served, so every job gets about the full lifetime.
func (c *AppClient) JobToken(ctx context.Context, installationID int64, target string, presets []string) (InstallationToken, error) {
	return c.repositoryToken(ctx, installationID, target, presets, JobTokenMinLife)
}

func (c *AppClient) repositoryToken(ctx context.Context, installationID int64, target string, presets []string, minLife time.Duration) (InstallationToken, error) {
	owner, name, ok := ownerAndName(target)
	if !ok {
		return InstallationToken{}, fmt.Errorf("repository %q is not owner/name", target)
	}
	names := []string{name}
	for _, preset := range presets {
		presetOwner, presetName, ok := ownerAndName(preset)
		switch {
		case !ok:
			continue
		case !strings.EqualFold(presetOwner, owner):
			c.warnOnce("preset repository is owned by another account than the installation and is left out of the token", preset)
			continue
		case slices.ContainsFunc(names, func(n string) bool { return strings.EqualFold(n, presetName) }):
			continue
		}
		names = append(names, presetName)
	}

	lowered := make([]string, len(names))
	for i, n := range names {
		lowered[i] = strings.ToLower(n)
	}
	slices.Sort(lowered)
	key := tokenKey{installationID: installationID, repositories: strings.Join(lowered, ","), target: strings.ToLower(name)}

	return c.cached(key, minLife, func() (InstallationToken, error) {
		tok, err := c.mint(ctx, installationID, names)
		if errors.Is(err, ErrRepositoriesRejected) && len(names) > 1 {
			c.warnOnce("a preset repository is not covered by the installation; the token covers the target repository only", strconv.FormatInt(installationID, 10))
			return c.mint(ctx, installationID, names[:1])
		}
		return tok, err
	})
}

func (c *AppClient) mint(ctx context.Context, installationID int64, names []string) (InstallationToken, error) {
	jwt, _, err := c.appJWT(ctx)
	if err != nil {
		return InstallationToken{}, err
	}
	return c.api.CreateInstallationToken(ctx, jwt, installationID, names)
}

// cached returns the entry's token while it is more than minLife from expiry and
// otherwise mints a new one. Callers of one key wait on the same lock, so a burst mints once.
func (c *AppClient) cached(key tokenKey, minLife time.Duration, mint func() (InstallationToken, error)) (InstallationToken, error) {
	entry := c.entry(key)
	entry.mu.Lock()
	defer entry.mu.Unlock()

	if entry.token.value != "" && c.fresh(entry.token, minLife) {
		return entry.token, nil
	}
	tok, err := mint()
	if err != nil {
		return InstallationToken{}, err
	}
	entry.token = tok
	return tok, nil
}

func (c *AppClient) fresh(t InstallationToken, minLife time.Duration) bool {
	return c.now().Before(t.ExpiresAt.Add(-minLife))
}

func (c *AppClient) entry(key tokenKey) *tokenEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.entries[key]; ok {
		return e
	}
	if len(c.entries) >= sweepThreshold {
		for k, e := range c.entries {
			if !e.mu.TryLock() {
				continue
			}
			if e.token.value == "" || !c.fresh(e.token, tokenRefreshMargin) {
				delete(c.entries, k)
			}
			e.mu.Unlock()
		}
	}
	e := &tokenEntry{}
	c.entries[key] = e
	return e
}

func (c *AppClient) warnOnce(msg, subject string) {
	if _, seen := c.warned.LoadOrStore(msg+"\x00"+subject, struct{}{}); !seen {
		slog.Warn(msg, "subject", subject)
	}
}
