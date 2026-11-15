package auth_test

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/mdg-labs/release-ops/internal/api/auth"
	"github.com/mdg-labs/release-ops/internal/logging"
)

type sessionStoreRig struct {
	t  *testing.T
	db *sql.DB
	sm *scs.SessionManager
}

func newSessionStoreRig(t *testing.T) *sessionStoreRig {
	t.Helper()
	db := openMigratedDB(t)
	t.Cleanup(func() { _ = db.Close() })
	seedSessionUser(t, db, "user-1")
	return &sessionStoreRig{t: t, db: db, sm: auth.NewSessionManager(db, strings.Repeat("s", 32), false)}
}

// signedIn stores a session for user-1 and returns its token.
func (r *sessionStoreRig) signedIn() string {
	r.t.Helper()
	ctx, err := r.sm.Load(context.Background(), "")
	if err != nil {
		r.t.Fatalf("load: %v", err)
	}
	r.sm.Put(ctx, auth.SessionUserIDKey, "user-1")
	token, _, err := r.sm.Commit(ctx)
	if err != nil {
		r.t.Fatalf("commit: %v", err)
	}
	return token
}

// request loads the session of token the way a request does.
func (r *sessionStoreRig) request(token string) context.Context {
	r.t.Helper()
	ctx, err := r.sm.Load(context.Background(), token)
	if err != nil {
		r.t.Fatalf("load: %v", err)
	}
	return ctx
}

func (r *sessionStoreRig) rows(token string) int {
	r.t.Helper()
	var n int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE token = ?`, token).Scan(&n); err != nil {
		r.t.Fatalf("count sessions: %v", err)
	}
	return n
}

func TestADestroyedSessionIsNotWrittenBackByAnInFlightRequest(t *testing.T) {
	t.Parallel()
	r := newSessionStoreRig(t)
	token := r.signedIn()

	inFlight := r.request(token)
	destroying := r.request(token)
	if err := r.sm.Destroy(destroying); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	if n := r.rows(token); n != 0 {
		t.Fatalf("session rows after Destroy = %d, want 0", n)
	}

	r.sm.Put(inFlight, auth.SessionRoleCheckedKey, int64(1))
	if _, _, err := r.sm.Commit(inFlight); err != nil {
		t.Fatalf("commit of the in-flight request: %v", err)
	}

	if n := r.rows(token); n != 0 {
		t.Fatalf("session rows after the in-flight commit = %d, want the destroyed session to stay gone", n)
	}
	if got := r.sm.GetString(r.request(token), auth.SessionUserIDKey); got != "" {
		t.Fatalf("the destroyed token still loads user %q", got)
	}
}

func TestADestroyedSessionsRepositoryListIsNotWrittenBack(t *testing.T) {
	t.Parallel()
	r := newSessionStoreRig(t)
	token := r.signedIn()

	inFlight := r.request(token)
	if err := r.sm.Destroy(r.request(token)); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	r.sm.Put(inFlight, auth.SessionScopeKey, []string{})
	if _, _, err := r.sm.Commit(inFlight); err != nil {
		t.Fatalf("commit of the in-flight request: %v", err)
	}
	if n := r.rows(token); n != 0 {
		t.Fatalf("session rows = %d, want 0", n)
	}
}

func TestARenewedSessionsOldTokenIsNotWrittenBack(t *testing.T) {
	t.Parallel()
	r := newSessionStoreRig(t)
	oldToken := r.signedIn()

	inFlight := r.request(oldToken)
	renewing := r.request(oldToken)
	if err := r.sm.RenewToken(renewing); err != nil {
		t.Fatalf("renew: %v", err)
	}
	newToken, _, err := r.sm.Commit(renewing)
	if err != nil {
		t.Fatalf("commit of the renewed session: %v", err)
	}
	if newToken == oldToken {
		t.Fatal("RenewToken kept the token")
	}
	if n := r.rows(newToken); n != 1 {
		t.Fatalf("rows for the renewed token = %d, want 1", n)
	}

	r.sm.Put(inFlight, auth.SessionRoleCheckedKey, int64(1))
	if _, _, err := r.sm.Commit(inFlight); err != nil {
		t.Fatalf("commit of the in-flight request: %v", err)
	}
	if n := r.rows(oldToken); n != 0 {
		t.Fatalf("rows for the replaced token = %d, want 0", n)
	}
}

func TestALiveSessionIsStillUpdatedAndANewTokenStillInserted(t *testing.T) {
	t.Parallel()
	r := newSessionStoreRig(t)
	token := r.signedIn()

	update := r.request(token)
	r.sm.Put(update, auth.SessionRoleCheckedKey, int64(42))
	if got, _, err := r.sm.Commit(update); err != nil || got != token {
		t.Fatalf("commit = %q, %v; want the same token", got, err)
	}
	if got := r.sm.GetInt64(r.request(token), auth.SessionRoleCheckedKey); got != 42 {
		t.Fatalf("stored role check time = %d, want 42: an update of a live session is persisted", got)
	}

	// Sign-in: Clear and RenewToken, then a commit under the new token.
	signIn := r.request(token)
	if err := r.sm.Clear(signIn); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if err := r.sm.RenewToken(signIn); err != nil {
		t.Fatalf("renew: %v", err)
	}
	r.sm.Put(signIn, auth.SessionUserIDKey, "user-1")
	renewed, _, err := r.sm.Commit(signIn)
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if r.rows(renewed) != 1 || r.rows(token) != 0 {
		t.Fatalf("rows: new token %d, old token %d; want 1 and 0", r.rows(renewed), r.rows(token))
	}
	if got := r.sm.GetString(r.request(renewed), auth.SessionUserIDKey); got != "user-1" {
		t.Fatalf("renewed session user = %q, want user-1", got)
	}
}

func TestRevocationMarkersAreDroppedOnceTheirSessionCouldNotBeLive(t *testing.T) {
	t.Parallel()
	r := newSessionStoreRig(t)
	if _, err := r.db.Exec(`INSERT INTO revoked_sessions (token_hash, expiry) VALUES ('old', julianday('now', '-1 hour'))`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	token := r.signedIn()
	if err := r.sm.Destroy(r.request(token)); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	var old, fresh int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM revoked_sessions WHERE token_hash = 'old'`).Scan(&old); err != nil {
		t.Fatal(err)
	}
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM revoked_sessions WHERE expiry > julianday('now')`).Scan(&fresh); err != nil {
		t.Fatal(err)
	}
	if old != 0 || fresh != 1 {
		t.Fatalf("expired markers = %d, live markers = %d; want 0 and 1", old, fresh)
	}
	var plain int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM revoked_sessions WHERE token_hash = ?`, token).Scan(&plain); err != nil || plain != 0 {
		t.Fatalf("a marker holds the plain token (%d rows, %v), want only its hash", plain, err)
	}
}

// Sign-in starts are public: each one stores a session that has no user. However many of them
// arrive, a credential that is registered for the logs is still removed from them afterwards.
func TestAFloodOfSessionsWithoutAUserDoesNotPushALiveSecretOutOfTheLogRegistry(t *testing.T) {
	rig := newSessionStoreRig(t)
	// The flood is many commits; the database need not wait for the disk on each of them.
	rig.db.SetMaxOpenConns(1)
	if _, err := rig.db.Exec(`PRAGMA synchronous = OFF`); err != nil {
		t.Fatalf("pragma: %v", err)
	}
	const credential = "opaque-flood-credential-0001"
	logging.RegisterFor(time.Hour, credential)
	signedIn := rig.signedIn()

	for i := range 1100 {
		ctx, err := rig.sm.Load(context.Background(), "")
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		rig.sm.Put(ctx, "oauth_state", fmt.Sprintf("flood-state-%06d-padding", i))
		if _, _, err := rig.sm.Commit(ctx); err != nil {
			t.Fatalf("commit: %v", err)
		}
	}

	if got := logging.Redact("provider said " + credential + " and " + signedIn); strings.Contains(got, credential) || strings.Contains(got, signedIn) {
		t.Fatalf("a live secret was pushed out of the registry by sign-in starts: %q", got)
	}
}
