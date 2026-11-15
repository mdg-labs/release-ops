package auth_test

import (
	"net/url"
	"strings"
	"testing"

	"github.com/mdg-labs/release-ops/internal/logging"
)

func assertRemoved(t *testing.T, kind, secret string) {
	t.Helper()
	if got := logging.Redact("log line with " + secret + " in it"); strings.Contains(got, secret) {
		t.Errorf("the %s is not removed from log text: %q", kind, got)
	}
}

func TestASessionTokenIsRegisteredAtCommit(t *testing.T) {
	t.Parallel()
	r := newSessionStoreRig(t)
	assertRemoved(t, "session token", r.signedIn())
}

// A session that outlives a restart is registered when its token is first presented.
func TestASessionTokenFoundInTheStoreIsRegistered(t *testing.T) {
	t.Parallel()
	r := newSessionStoreRig(t)
	token := r.signedIn()
	const copied = "opaque-session-token-copied-from-before-a-restart-0001"
	if _, err := r.db.Exec(`INSERT INTO sessions (token, data, expiry, user_id) SELECT ?, data, expiry, user_id FROM sessions WHERE token = ?`, copied, token); err != nil {
		t.Fatalf("copy the session row: %v", err)
	}
	r.request(copied)
	assertRemoved(t, "found session token", copied)
}

func TestTheGitHubSignInStateAndVerifierAreRegistered(t *testing.T) {
	t.Parallel()
	h := newSignInHarness(t, harnessOptions{})
	resp, _ := h.get(h.newClient(), "/auth/github/login")
	loc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	state := loc.Query().Get("state")
	if state == "" {
		t.Fatal("the redirect carries no state")
	}
	assertRemoved(t, "oauth state", state)
}
