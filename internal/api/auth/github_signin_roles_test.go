package auth_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/mdg-labs/release-ops/internal/api/auth"
	"github.com/mdg-labs/release-ops/internal/github"
)

// rolesByUser is a RoleEvaluator that answers per numeric user id.
type rolesByUser struct {
	mu    sync.Mutex
	roles map[int64]string
	err   error
	asked []int64
}

func (r *rolesByUser) Evaluate(_ context.Context, id int64) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.asked = append(r.asked, id)
	if r.err != nil {
		return "", r.err
	}
	return r.roles[id], nil
}

var (
	viewerUser = github.User{ID: 700, Login: "viewer-vera"}
	adminUser  = github.User{ID: 583231, Login: "octocat"}
)

func (h *signInHarness) sessionRepositories() []string {
	h.t.Helper()
	rows, err := h.db.Writer.Query(`SELECT repository_id FROM session_repositories ORDER BY repository_id`)
	if err != nil {
		h.t.Fatalf("read session_repositories: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			h.t.Fatalf("scan: %v", err)
		}
		ids = append(ids, id)
	}
	return ids
}

func TestViewerSignInStoresTheScopeFromTheUsersOwnToken(t *testing.T) {
	var logs bytes.Buffer
	var logsMu sync.Mutex
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(writerFunc(func(p []byte) (int, error) {
		logsMu.Lock()
		defer logsMu.Unlock()
		return logs.Write(p)
	}), &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	roles := &rolesByUser{roles: map[int64]string{viewerUser.ID: auth.RoleViewer}}
	h := newSignInHarness(t, harnessOptions{bootstrapLogin: "octocat", roles: roles, repositories: 3})
	// The user can access repository 2, repository 3 and one the instance does not have.
	h.gh.userRepos[viewerUser.ID] = []int64{2, 3, 99}
	c := h.newClient()

	if resp := h.signIn(c, viewerUser, "", false); resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/" {
		t.Fatalf("viewer sign-in = %d %q, want 302 to /", resp.StatusCode, resp.Header.Get("Location"))
	}

	if got := h.sessionRepositories(); !slices.Equal(got, []string{"repo-2", "repo-3"}) {
		t.Fatalf("stored scope = %v, want the accessible repositories that are installed: repo-2, repo-3", got)
	}
	if user := h.sessionUser(c); user["role"] != "viewer" || user["id"] != float64(viewerUser.ID) {
		t.Fatalf("session user = %v, want the viewer", user)
	}
	if len(h.gh.repoBearers) != 1 || h.gh.repoBearers[0] != "fixture-user-token-1" {
		t.Fatalf("repository listing tokens = %v, want one call with the user's own token", h.gh.repoBearers)
	}

	secrets := []string{"fixture-user-token-1", "fixture-refresh-token-1", "code-1", signInClientSecret}
	rows, err := h.db.Writer.Query(`SELECT data FROM sessions`)
	if err != nil {
		t.Fatalf("read sessions: %v", err)
	}
	for rows.Next() {
		var blob []byte
		if err := rows.Scan(&blob); err != nil {
			t.Fatalf("scan: %v", err)
		}
		for _, s := range secrets {
			if bytes.Contains(blob, []byte(s)) {
				t.Errorf("a session blob contains %q", s)
			}
		}
		for _, id := range []string{"repo-2", "repo-3", auth.SessionScopeKey} {
			if bytes.Contains(blob, []byte(id)) {
				t.Errorf("a session blob contains %q: the list lives in session_repositories only", id)
			}
		}
	}
	_ = rows.Close()

	logsMu.Lock()
	defer logsMu.Unlock()
	for _, s := range secrets {
		if strings.Contains(logs.String(), s) {
			t.Errorf("logs contain %q: %s", s, logs.String())
		}
	}
}

func TestAdminSignInStoresNoScopeAndSkipsTheRepositoryListing(t *testing.T) {
	t.Parallel()
	roles := &rolesByUser{roles: map[int64]string{}}
	h := newSignInHarness(t, harnessOptions{bootstrapLogin: "octocat", roles: roles, repositories: 2})
	c := h.newClient()

	if resp := h.signIn(c, adminUser, "", false); resp.StatusCode != http.StatusFound {
		t.Fatalf("sign-in = %d", resp.StatusCode)
	}
	if got := h.sessionRepositories(); len(got) != 0 {
		t.Errorf("an admin session stores %v, want no list", got)
	}
	if len(h.gh.repoBearers) != 0 {
		t.Errorf("the repository listing was called %d times for an admin", len(h.gh.repoBearers))
	}
	if len(roles.asked) != 0 {
		t.Errorf("roles were evaluated for the bootstrap admin: %v", roles.asked)
	}
}

func TestAnOrganizationMemberAdminRoleFromTheEvaluatorSignsIn(t *testing.T) {
	t.Parallel()
	roles := &rolesByUser{roles: map[int64]string{viewerUser.ID: auth.RoleAdmin}}
	h := newSignInHarness(t, harnessOptions{bootstrapLogin: "octocat", roles: roles, repositories: 1})
	c := h.newClient()

	if resp := h.signIn(c, viewerUser, "", false); resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/" {
		t.Fatalf("sign-in = %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	if user := h.sessionUser(c); user["role"] != "admin" {
		t.Fatalf("session user = %v, want an admin", user)
	}
	if len(h.sessionRepositories()) != 0 || len(h.gh.repoBearers) != 0 {
		t.Error("an admin got a repository scope")
	}
	// Being admin by membership does not pin a bootstrap admin.
	if _, ok, _ := h.store.SignIn().BootstrapAdmin(context.Background()); ok {
		t.Error("a sign-in by membership pinned a bootstrap admin")
	}
}

func TestSignInRefusalsLeaveNoSessionAndNoUser(t *testing.T) {
	t.Parallel()

	t.Run("a user with no role is refused", func(t *testing.T) {
		t.Parallel()
		h := newSignInHarness(t, harnessOptions{bootstrapLogin: "octocat", roles: &rolesByUser{roles: map[int64]string{}}, repositories: 1})
		c := h.newClient()
		wantLoginError(t, h.signIn(c, viewerUser, "", false), auth.LoginErrorNoRole)
		if h.sessionUser(c) != nil || h.count("users") != 0 {
			t.Error("a user without a role got a session or a user row")
		}
	})
	t.Run("a role that cannot be evaluated is refused", func(t *testing.T) {
		t.Parallel()
		h := newSignInHarness(t, harnessOptions{bootstrapLogin: "octocat", roles: &rolesByUser{err: errors.New("github down")}, repositories: 1})
		c := h.newClient()
		wantLoginError(t, h.signIn(c, viewerUser, "", false), auth.LoginErrorGitHubUnavailable)
		if h.sessionUser(c) != nil || h.count("users") != 0 {
			t.Error("a failed evaluation got a session or a user row")
		}
	})
	t.Run("a viewer whose repository listing fails is refused, not given an empty or full list", func(t *testing.T) {
		t.Parallel()
		roles := &rolesByUser{roles: map[int64]string{viewerUser.ID: auth.RoleViewer}}
		h := newSignInHarness(t, harnessOptions{bootstrapLogin: "octocat", roles: roles, repositories: 2})
		h.gh.userRepos[viewerUser.ID] = []int64{1}
		h.gh.repoStatus.Store(http.StatusBadGateway)
		c := h.newClient()
		wantLoginError(t, h.signIn(c, viewerUser, "", false), auth.LoginErrorGitHubUnavailable)
		if h.sessionUser(c) != nil || h.count("users") != 0 || h.count("session_repositories") != 0 {
			t.Error("a failed repository listing left a session, a user or a list")
		}
	})
	t.Run("without a role evaluator only the bootstrap admin signs in", func(t *testing.T) {
		t.Parallel()
		h := newSignInHarness(t, harnessOptions{bootstrapLogin: "octocat", repositories: 1})
		c := h.newClient()
		wantLoginError(t, h.signIn(c, viewerUser, "", false), auth.LoginErrorNoRole)
		if h.sessionUser(c) != nil || h.count("users") != 0 {
			t.Error("a user got in without a role evaluator")
		}
	})
}
