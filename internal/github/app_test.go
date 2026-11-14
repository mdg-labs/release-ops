package github_test

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/github"
	"github.com/mdg-labs/release-ops/internal/github/githubtest"
)

var t0 = time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)

type staticCreds struct {
	appID int64
	pem   []byte
	err   error
}

func (s staticCreds) SigningKey(context.Context) (int64, []byte, error) {
	if s.err != nil {
		return 0, nil, s.err
	}
	return s.appID, slices.Clone(s.pem), nil
}

type rig struct {
	fake   *githubtest.Fake
	clock  *githubtest.Clock
	client *github.AppClient
}

func newRig(t *testing.T) *rig {
	t.Helper()
	clock := githubtest.NewClock(t0)
	fake := githubtest.New(t, clock.Now)
	fake.AddInstallation(1000, githubtest.AppID)
	api := github.NewClient(fake.URL, 5*time.Second)
	return &rig{
		fake: fake, clock: clock,
		client: github.NewAppClient(api, staticCreds{appID: githubtest.AppID, pem: fake.PEM}, github.AppClientOptions{Now: clock.Now}),
	}
}

func TestSignAppJWTIsRS256WithTheAppIDAndAShortLifetime(t *testing.T) {
	t.Parallel()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pkcs1 := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	pkcs8Der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	pkcs8 := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8Der})

	for name, keyPEM := range map[string][]byte{"pkcs1": pkcs1, "pkcs8": pkcs8} {
		jwt, err := github.SignAppJWT(1234567, keyPEM, t0)
		if err != nil {
			t.Fatalf("%s: SignAppJWT: %v", name, err)
		}
		parts := strings.Split(jwt, ".")
		if len(parts) != 3 {
			t.Fatalf("%s: jwt has %d parts", name, len(parts))
		}
		enc := base64.RawURLEncoding
		sig, err := enc.DecodeString(parts[2])
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
		if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, digest[:], sig); err != nil {
			t.Fatalf("%s: signature does not verify with the matching public key: %v", name, err)
		}

		var header map[string]string
		var claims struct{ Iss, Iat, Exp int64 }
		for i, into := range []any{&header, &claims} {
			raw, err := enc.DecodeString(parts[i])
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(raw, into); err != nil {
				t.Fatal(err)
			}
		}
		if header["alg"] != "RS256" || header["typ"] != "JWT" {
			t.Errorf("%s: header = %v, want RS256 JWT", name, header)
		}
		if claims.Iss != 1234567 {
			t.Errorf("%s: iss = %d, want the app id", name, claims.Iss)
		}
		if claims.Iat > t0.Unix() || claims.Exp <= t0.Unix() || claims.Exp-claims.Iat > 600 {
			t.Errorf("%s: iat %d exp %d, want iat <= now < exp and a lifetime of at most 10 minutes", name, claims.Iat, claims.Exp)
		}
	}
}

func TestSignAppJWTRefusesAnUnusableKeyWithoutQuotingIt(t *testing.T) {
	t.Parallel()

	ecKey := "-----BEGIN PRIVATE KEY-----\nbm90IGEga2V5\n-----END PRIVATE KEY-----\n"
	for name, keyPEM := range map[string]string{
		"not pem":     "super-secret-key-material",
		"bad der":     ecKey,
		"empty":       "",
		"fixture pem": "-----BEGIN RSA PRIVATE KEY-----\nfixture-private-key-body\n-----END RSA PRIVATE KEY-----\n",
	} {
		_, err := github.SignAppJWT(1, []byte(keyPEM), t0)
		if !errors.Is(err, github.ErrInvalidPrivateKey) {
			t.Errorf("%s: err = %v, want ErrInvalidPrivateKey", name, err)
		}
		if err != nil && keyPEM != "" && strings.Contains(err.Error(), "secret") {
			t.Errorf("%s: error quotes the key: %v", name, err)
		}
	}
}

func TestInstallationIsFetchedWithTheAppJWT(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	inst, err := r.client.Installation(context.Background(), 1000)
	if err != nil {
		t.Fatalf("Installation: %v", err)
	}
	if inst.ID != 1000 || inst.Account.Login != "acme" || inst.Account.Type != "Organization" || inst.Account.ID != 4242 ||
		inst.RepositorySelection != "selected" || inst.Permissions["contents"] != "write" || inst.SuspendedAt != nil {
		t.Errorf("installation = %+v", inst)
	}
	reqs := r.fake.Requests()
	if len(reqs) != 1 || reqs[0].Method != http.MethodGet || reqs[0].Path != "/app/installations/1000" || !reqs[0].JWTValid {
		t.Errorf("requests = %+v, want one GET /app/installations/1000 with a valid App JWT", reqs)
	}
}

func TestInstallationFailuresAreClassified(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.fake.AddInstallation(2000, githubtest.AppID+1)
	if _, err := r.client.Installation(context.Background(), 999); !errors.Is(err, github.ErrInstallationNotFound) {
		t.Errorf("unknown installation: err = %v, want ErrInstallationNotFound", err)
	}
	// An installation GitHub returns for another App is not this instance's.
	if _, err := r.client.Installation(context.Background(), 2000); !errors.Is(err, github.ErrInstallationNotFound) {
		t.Errorf("foreign installation: err = %v, want ErrInstallationNotFound", err)
	}

	for status, want := range map[int]error{401: github.ErrAppRejected, 403: github.ErrAppRejected, 500: github.ErrRequestFailed, 502: github.ErrRequestFailed} {
		r.fake.FailInstallations(status)
		_, err := r.client.Installation(context.Background(), 1000)
		if !errors.Is(err, want) {
			t.Errorf("status %d: err = %v, want %v", status, err, want)
		}
	}

	before := len(r.fake.Requests())
	noKey := github.NewAppClient(github.NewClient(r.fake.URL, time.Second), staticCreds{err: errors.New("no app")}, github.AppClientOptions{})
	if _, err := noKey.Installation(context.Background(), 1000); err == nil || len(r.fake.Requests()) != before {
		t.Errorf("without credentials: err = %v, requests = %d, want an error and no request to GitHub", err, len(r.fake.Requests()))
	}
}

func TestTokenIsCachedUntilFiveMinutesBeforeExpiryAndThenRefreshed(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	ctx := context.Background()
	first, err := r.client.Token(ctx, 1000)
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if first.Value() != githubtest.TokenPrefix+"1" || !first.ExpiresAt.Equal(t0.Add(time.Hour)) {
		t.Fatalf("token = %v, expires %v", first, first.ExpiresAt)
	}

	r.clock.Advance(54 * time.Minute)
	again, err := r.client.Token(ctx, 1000)
	if err != nil || again.Value() != first.Value() || r.fake.Mints() != 1 {
		t.Fatalf("54 minutes in: token %q, mints %d, err %v, want the cached token", again.Value(), r.fake.Mints(), err)
	}

	r.clock.Advance(2 * time.Minute) // 56 minutes: inside the five-minute margin
	fresh, err := r.client.Token(ctx, 1000)
	if err != nil || fresh.Value() != githubtest.TokenPrefix+"2" || r.fake.Mints() != 2 {
		t.Fatalf("56 minutes in: token %q, mints %d, err %v, want a refreshed token", fresh.Value(), r.fake.Mints(), err)
	}
	if !fresh.ExpiresAt.After(first.ExpiresAt) {
		t.Errorf("refreshed token expires %v, not after %v", fresh.ExpiresAt, first.ExpiresAt)
	}
}

func TestConcurrentCallersShareOneMint(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	var wg sync.WaitGroup
	var bad atomic.Int32
	for range 20 {
		wg.Go(func() {
			tok, err := r.client.RepositoryToken(context.Background(), 1000, "acme/api", []string{"acme/presets"})
			if err != nil || tok.Value() != githubtest.TokenPrefix+"1" {
				bad.Add(1)
			}
		})
	}
	wg.Wait()
	if bad.Load() != 0 || r.fake.Mints() != 1 {
		t.Errorf("failed callers = %d, mints = %d, want every caller served by one mint", bad.Load(), r.fake.Mints())
	}
}

func TestRepositoryTokenIsScopedToTheTargetAndTheSameOwnersPresets(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	ctx := context.Background()
	tok, err := r.client.RepositoryToken(ctx, 1000, "acme/api", []string{"acme/presets", "ACME/Presets", "other/presets", "not-a-repo", "acme/api"})
	if err != nil {
		t.Fatalf("RepositoryToken: %v", err)
	}
	if got := r.fake.TokenRequests(); len(got) != 1 || !slices.Equal(got[0], []string{"api", "presets"}) {
		t.Errorf("token requests = %v, want one for [api presets]: the target, the preset once, nothing of another owner", got)
	}

	// The same set in another order is served from the cache; another target is a new token.
	if again, err := r.client.RepositoryToken(ctx, 1000, "acme/api", []string{"acme/presets"}); err != nil || again.Value() != tok.Value() {
		t.Errorf("same scope: token %q, err %v, want the cached one", again.Value(), err)
	}
	other, err := r.client.RepositoryToken(ctx, 1000, "acme/web", []string{"acme/presets"})
	if err != nil || other.Value() == tok.Value() {
		t.Errorf("another target: token %q, err %v, want its own token", other.Value(), err)
	}
	if _, err := r.client.RepositoryToken(ctx, 1000, "no-slash", nil); err == nil {
		t.Error("a target that is not owner/name was accepted")
	}
}

func TestRepositoryTokenFallsBackToTheTargetWhenAPresetIsNotInstalled(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.fake.RejectRepository("presets")
	tok, err := r.client.RepositoryToken(context.Background(), 1000, "acme/api", []string{"acme/presets"})
	if err != nil {
		t.Fatalf("RepositoryToken: %v", err)
	}
	got := r.fake.TokenRequests()
	if len(got) != 2 || !slices.Equal(got[0], []string{"api", "presets"}) || !slices.Equal(got[1], []string{"api"}) || tok.Value() == "" {
		t.Errorf("token requests = %v, want [api presets] refused and then [api]", got)
	}

	// A refusal that does not involve a preset is an error.
	r.fake.RejectRepository("api")
	if _, err := r.client.RepositoryToken(context.Background(), 1000, "acme/other", []string{"acme/api"}); err != nil {
		t.Fatalf("target alone after fallback: %v", err)
	}
	if _, err := r.client.RepositoryToken(context.Background(), 1000, "acme/api", nil); !errors.Is(err, github.ErrRepositoriesRejected) {
		t.Errorf("target refused: err = %v, want ErrRepositoriesRejected", err)
	}
}

func TestTokenFailuresAreCleanErrorsAndNothingIsCached(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	ctx := context.Background()
	for status, want := range map[int]error{401: github.ErrAppRejected, 403: github.ErrAppRejected, 404: github.ErrInstallationNotFound, 500: github.ErrRequestFailed, 503: github.ErrRequestFailed} {
		r.fake.FailTokens(status)
		tok, err := r.client.Token(ctx, 1000)
		if !errors.Is(err, want) || tok.Value() != "" {
			t.Errorf("status %d: token %q, err %v, want %v and no token", status, tok.Value(), err, want)
		}
		if err != nil && (strings.Contains(err.Error(), githubtest.TokenPrefix) || strings.Contains(err.Error(), "refused")) {
			t.Errorf("status %d: error carries GitHub's response: %v", status, err)
		}
	}

	r.fake.FailTokens(0)
	if tok, err := r.client.Token(ctx, 1000); err != nil || tok.Value() == "" {
		t.Errorf("after the failures: token %q, err %v, want a token: a failure is not cached", tok.Value(), err)
	}
}

func TestATokenIsNotPrinted(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	tok, err := r.client.Token(context.Background(), 1000)
	if err != nil {
		t.Fatal(err)
	}
	for _, rendered := range []string{fmt.Sprint(tok), fmt.Sprintf("%v", tok), fmt.Sprintf("%+v", tok), fmt.Sprintf("%#v", tok)} {
		if strings.Contains(rendered, githubtest.TokenPrefix) {
			t.Errorf("token printed as %q", rendered)
		}
	}
}

func TestACredentialIsNeverSentToAnotherHost(t *testing.T) {
	t.Parallel()

	var elsewhere atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { elsewhere.Add(1) }))
	t.Cleanup(other.Close)
	redirecting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(redirecting.Close)

	fake := githubtest.New(t, func() time.Time { return t0 })
	c := github.NewAppClient(github.NewClient(redirecting.URL, time.Second), staticCreds{appID: githubtest.AppID, pem: fake.PEM}, github.AppClientOptions{Now: func() time.Time { return t0 }})
	if _, err := c.Token(context.Background(), 1000); !errors.Is(err, github.ErrRequestFailed) {
		t.Errorf("token through a redirect: err = %v, want ErrRequestFailed", err)
	}
	if _, err := c.Installation(context.Background(), 1000); !errors.Is(err, github.ErrRequestFailed) {
		t.Errorf("installation through a redirect: err = %v, want ErrRequestFailed", err)
	}
	if elsewhere.Load() != 0 {
		t.Errorf("the redirect target received %d requests, want none", elsewhere.Load())
	}
}

func TestAResponseOverTheLimitIsRefused(t *testing.T) {
	t.Parallel()

	big := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"token":"` + strings.Repeat("x", 2<<20) + `"}`))
	}))
	t.Cleanup(big.Close)
	fake := githubtest.New(t, func() time.Time { return t0 })
	c := github.NewAppClient(github.NewClient(big.URL, time.Second), staticCreds{appID: githubtest.AppID, pem: fake.PEM}, github.AppClientOptions{Now: func() time.Time { return t0 }})
	if tok, err := c.Token(context.Background(), 1000); !errors.Is(err, github.ErrRequestFailed) || tok.Value() != "" {
		t.Errorf("oversized response: token %q, err %v, want ErrRequestFailed", tok.Value(), err)
	}
}
