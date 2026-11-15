package github_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mdg-labs/release-ops/internal/github"
	"github.com/mdg-labs/release-ops/internal/logging"
)

func assertRedacted(t *testing.T, kind, secret string) {
	t.Helper()
	if got := logging.Redact("a line with " + secret + " in it"); strings.Contains(got, secret) {
		t.Errorf("the %s is not removed from log text: %q", kind, got)
	}
}

// A minted installation token, the App's key and the JWT signed with it are registered where
// they are made.
func TestMintingAnInstallationTokenRegistersItAndTheKey(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	tok, err := r.client.Token(context.Background(), 1000)
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	assertRedacted(t, "installation token", tok.Value())
	assertRedacted(t, "private key", string(r.fake.PEM))
	for _, line := range strings.Split(string(r.fake.PEM), "\n") {
		if len(line) > 20 && !strings.HasPrefix(line, "-----") {
			assertRedacted(t, "private key line", line)
			break
		}
	}
}

func TestTheOAuthExchangeRegistersTheCodeTheVerifierTheSecretAndTheUserToken(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"access_token":"opaque-user-access-token-0001","token_type":"bearer"}`))
	}))
	t.Cleanup(srv.Close)
	o := github.NewOAuthClient(github.NewClient(srv.URL, 0), srv.URL)

	if _, err := o.ExchangeCode(context.Background(), github.ExchangeParams{
		ClientID: "id", ClientSecret: "opaque-oauth-client-secret-0001", Code: "opaquecode0001", RedirectURI: "https://x.example/cb", CodeVerifier: "opaque-code-verifier-0001",
	}); err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	for kind, secret := range map[string]string{
		"user token": "opaque-user-access-token-0001", "client secret": "opaque-oauth-client-secret-0001",
		"code": "opaquecode0001", "verifier": "opaque-code-verifier-0001",
	} {
		assertRedacted(t, kind, secret)
	}
}

func TestTheManifestConversionRegistersTheCodeAndTheNewSecrets(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(fixture(t)))
	}))
	t.Cleanup(srv.Close)
	if _, err := github.NewClient(srv.URL, 0).ConvertManifest(context.Background(), "opaquemanifestcode0001"); err != nil {
		t.Fatalf("ConvertManifest: %v", err)
	}
	for _, s := range append(secrets, "opaquemanifestcode0001") {
		assertRedacted(t, "conversion secret", s)
	}
}
