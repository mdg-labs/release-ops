package store_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/store"
	storedb "github.com/mdg-labs/release-ops/internal/store/db"
)

func TestSignInKeysUsersByNumericIDAndKeepsTheBootstrapAdmin(t *testing.T) {
	t.Parallel()
	st, db, _ := newGitHubAppStore(t)
	ctx := context.Background()
	repo := st.SignIn()

	first, err := repo.SignIn(ctx, store.GitHubUser{ID: 583231, Login: "octocat"}, true)
	if err != nil {
		t.Fatalf("first sign-in: %v", err)
	}
	renamed, err := repo.SignIn(ctx, store.GitHubUser{ID: 583231, Login: "octocat-renamed"}, false)
	if err != nil {
		t.Fatalf("sign-in after a rename: %v", err)
	}
	if renamed != first {
		t.Fatalf("a changed login produced user %q, want the same user %q", renamed, first)
	}
	if n := countRows(t, db, "users"); n != 1 {
		t.Errorf("users = %d, want 1", n)
	}
	if n := countRows(t, db, "auth_identities"); n != 1 {
		t.Errorf("identities = %d, want 1", n)
	}
	admin, ok, err := repo.BootstrapAdmin(ctx)
	if err != nil || !ok {
		t.Fatalf("BootstrapAdmin = %v, %v; want the pinned admin", ok, err)
	}
	if admin.UserID != first || admin.ID != 583231 || admin.Login != "octocat-renamed" {
		t.Errorf("bootstrap admin = %+v, want user %s, id 583231, login octocat-renamed", admin, first)
	}

	// Another user takes over the old login name: a different numeric id is a different user
	// and cannot pin itself over the stored admin.
	other, err := repo.SignIn(ctx, store.GitHubUser{ID: 999, Login: "octocat"}, true)
	if !errors.Is(err, store.ErrBootstrapAdminTaken) {
		t.Fatalf("pin by another user = %q, %v; want ErrBootstrapAdminTaken", other, err)
	}
	if n := countRows(t, db, "users"); n != 1 {
		t.Errorf("users after a refused pin = %d, want 1: the refused sign-in must write nothing", n)
	}
	admin, _, _ = repo.BootstrapAdmin(ctx)
	if admin.UserID != first {
		t.Errorf("bootstrap admin = %s after a refused pin, want %s", admin.UserID, first)
	}
}

func TestBootstrapAdminIsAbsentUntilPinned(t *testing.T) {
	t.Parallel()
	st, _, _ := newGitHubAppStore(t)
	ctx := context.Background()
	if _, ok, err := st.SignIn().BootstrapAdmin(ctx); err != nil || ok {
		t.Fatalf("BootstrapAdmin = %v, %v; want none", ok, err)
	}
	if _, err := st.SignIn().SignIn(ctx, store.GitHubUser{ID: 7, Login: "someone"}, false); err != nil {
		t.Fatalf("sign-in: %v", err)
	}
	if _, ok, _ := st.SignIn().BootstrapAdmin(ctx); ok {
		t.Fatal("a sign-in without a pin made a bootstrap admin")
	}
}

func TestLoginLinkRedeemsOnceAndOnlyBeforeItExpires(t *testing.T) {
	t.Parallel()
	st, db, _ := newGitHubAppStore(t)
	ctx := context.Background()
	repo := st.SignIn()

	if err := repo.CreateLoginLink(ctx, "hash-early", time.Now().Add(time.Minute)); !errors.Is(err, store.ErrNoBootstrapAdmin) {
		t.Fatalf("CreateLoginLink with no admin = %v, want ErrNoBootstrapAdmin", err)
	}
	if n := countRows(t, db, "login_link_tokens"); n != 0 {
		t.Fatalf("login_link_tokens = %d after a refused mint, want 0", n)
	}

	userID, err := repo.SignIn(ctx, store.GitHubUser{ID: 5, Login: "admin"}, true)
	if err != nil {
		t.Fatalf("pin admin: %v", err)
	}
	if err := repo.CreateLoginLink(ctx, "hash-ok", time.Now().Add(15*time.Minute)); err != nil {
		t.Fatalf("CreateLoginLink: %v", err)
	}
	if err := repo.CreateLoginLink(ctx, "hash-expired", time.Now().Add(-time.Second)); err != nil {
		t.Fatalf("CreateLoginLink expired: %v", err)
	}

	if _, err := repo.RedeemLoginLink(ctx, "hash-expired"); !errors.Is(err, store.ErrLoginLinkInvalid) {
		t.Errorf("expired link = %v, want ErrLoginLinkInvalid", err)
	}
	if _, err := repo.RedeemLoginLink(ctx, "hash-unknown"); !errors.Is(err, store.ErrLoginLinkInvalid) {
		t.Errorf("unknown link = %v, want ErrLoginLinkInvalid", err)
	}
	admin, err := repo.RedeemLoginLink(ctx, "hash-ok")
	if err != nil {
		t.Fatalf("RedeemLoginLink: %v", err)
	}
	if admin.UserID != userID || admin.ID != 5 {
		t.Errorf("redeemed admin = %+v, want user %s id 5", admin, userID)
	}
	if _, err := repo.RedeemLoginLink(ctx, "hash-ok"); !errors.Is(err, store.ErrLoginLinkInvalid) {
		t.Errorf("reused link = %v, want ErrLoginLinkInvalid", err)
	}
}

func TestConcurrentLoginLinkRedemptionsYieldOneWinner(t *testing.T) {
	t.Parallel()
	st, _, _ := newGitHubAppStore(t)
	ctx := context.Background()
	repo := st.SignIn()
	if _, err := repo.SignIn(ctx, store.GitHubUser{ID: 5, Login: "admin"}, true); err != nil {
		t.Fatalf("pin admin: %v", err)
	}
	if err := repo.CreateLoginLink(ctx, "hash-race", time.Now().Add(time.Minute)); err != nil {
		t.Fatalf("CreateLoginLink: %v", err)
	}

	var wins, refused atomic.Int32
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			_, err := repo.RedeemLoginLink(ctx, "hash-race")
			switch {
			case err == nil:
				wins.Add(1)
			case errors.Is(err, store.ErrLoginLinkInvalid):
				refused.Add(1)
			default:
				t.Errorf("RedeemLoginLink: %v", err)
			}
		})
	}
	wg.Wait()
	if wins.Load() != 1 || refused.Load() != 7 {
		t.Fatalf("wins = %d, refused = %d; want 1 and 7", wins.Load(), refused.Load())
	}
}

func TestGitHubAppSignInReturnsTheDecryptedClientSecret(t *testing.T) {
	t.Parallel()
	st, _, _ := newGitHubAppStore(t)
	ctx := context.Background()
	if _, err := st.GitHubApp().SignIn(ctx); !errors.Is(err, store.ErrGitHubAppNotFound) {
		t.Fatalf("SignIn before an App = %v, want ErrGitHubAppNotFound", err)
	}
	if err := st.GitHubApp().Create(ctx, sampleApp()); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := st.GitHubApp().SignIn(ctx)
	if err != nil {
		t.Fatalf("SignIn: %v", err)
	}
	if got.ClientID != "fixture-client-id" || string(got.ClientSecret) != "fixture-client-secret-value" ||
		got.OwnerLogin != "acme" || got.OwnerType != "Organization" {
		t.Errorf("SignIn = %+v, want the stored client id, decrypted secret and owner", got)
	}
}

// ListUsers lists the users that have a GitHub identity, by their current login, and leaves out
// a user row with no identity (one a local account left behind).
func TestListUsersListsGitHubIdentitiesOnly(t *testing.T) {
	t.Parallel()
	st, db, _ := newGitHubAppStore(t)
	ctx := context.Background()
	userID, err := st.SignIn().SignIn(ctx, store.GitHubUser{ID: 5, Login: "octocat"}, false)
	if err != nil {
		t.Fatalf("sign-in: %v", err)
	}
	if _, err := db.Writer.Exec(`INSERT INTO users (id, created_at, updated_at) VALUES ('orphan', 't', 't')`); err != nil {
		t.Fatalf("seed user without identity: %v", err)
	}

	listed, err := storedb.New(db.Reader).ListUsers(ctx)
	if err != nil || len(listed) != 1 || listed[0].ID != userID || listed[0].Login != "octocat" {
		t.Fatalf("ListUsers = %+v, %v; want only the GitHub user octocat", listed, err)
	}
}
