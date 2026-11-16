package github_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mdg-labs/release-ops/internal/github"
	"github.com/mdg-labs/release-ops/internal/github/githubtest"
)

func throwawayPEM(t *testing.T) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
}

func TestVerifyKeyAcceptsOnlyAKeyGitHubKnowsForThisApp(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	ctx := context.Background()

	second := r.fake.AddKey(t)
	if id, err := r.client.VerifyKey(ctx, second); err != nil || id != githubtest.AppID {
		t.Fatalf("VerifyKey(second key) = %d, %v; want the App id", id, err)
	}
	if id, err := r.client.VerifyKey(ctx, r.fake.PEM); err != nil || id != githubtest.AppID {
		t.Fatalf("VerifyKey(first key) = %d, %v; want the App id", id, err)
	}

	if _, err := r.client.VerifyKey(ctx, []byte("not a pem")); !errors.Is(err, github.ErrInvalidPrivateKey) {
		t.Errorf("VerifyKey(not a pem) = %v, want ErrInvalidPrivateKey", err)
	}
	if _, err := r.client.VerifyKey(ctx, throwawayPEM(t)); !errors.Is(err, github.ErrAppRejected) {
		t.Errorf("VerifyKey(unknown key) = %v, want ErrAppRejected", err)
	}

	r.fake.AnswerAppID(999)
	if _, err := r.client.VerifyKey(ctx, second); !errors.Is(err, github.ErrAppMismatch) {
		t.Errorf("VerifyKey with GitHub answering for another App = %v, want ErrAppMismatch", err)
	}
}

func TestVerifyKeyRefusesAMalformedPEMWithoutCallingGitHub(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	ctx := context.Background()
	before := len(r.fake.Requests())

	validDER := x509.MarshalPKCS1PrivateKey(mustKey(t))
	cases := map[string][]byte{
		"valid markers with invalid base64": []byte("-----BEGIN RSA PRIVATE KEY-----\n!!!not base64!!!\n-----END RSA PRIVATE KEY-----\n"),
		"truncated PKCS#1 DER":              pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: validDER[:len(validDER)/2]}),
	}
	for name, candidate := range cases {
		if _, err := r.client.VerifyKey(ctx, candidate); !errors.Is(err, github.ErrInvalidPrivateKey) {
			t.Errorf("VerifyKey(%s) = %v, want ErrInvalidPrivateKey", name, err)
		}
	}
	if after := len(r.fake.Requests()); after != before {
		t.Errorf("GitHub received %d requests for malformed keys, want none", after-before)
	}
}

func mustKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func TestVerifyKeyDoesNotPrintTheKeyInItsErrors(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.fake.Close()

	candidate := r.fake.PEM
	_, err := r.client.VerifyKey(context.Background(), candidate)
	if !errors.Is(err, github.ErrRequestFailed) {
		t.Fatalf("VerifyKey with GitHub down = %v, want ErrRequestFailed", err)
	}
	if strings.Contains(err.Error(), "PRIVATE KEY") {
		t.Errorf("the error carries the key: %q", err)
	}
}

// versionedCreds is a CredentialsSource whose key can be replaced.
type versionedCreds struct {
	staticCreds
	version atomic.Int64
}

func (v *versionedCreds) KeyVersion() int64 { return v.version.Load() }

func TestACachedTokenIsNotServedAfterTheKeyVersionChanges(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	creds := &versionedCreds{staticCreds: staticCreds{appID: githubtest.AppID, pem: r.fake.PEM}}
	client := github.NewAppClient(github.NewClient(r.fake.URL, 0), creds, github.AppClientOptions{Now: r.clock.Now})
	ctx := context.Background()

	if _, err := client.Token(ctx, 1000); err != nil {
		t.Fatalf("Token: %v", err)
	}
	if _, err := client.Token(ctx, 1000); err != nil || r.fake.Mints() != 1 {
		t.Fatalf("second Token = %v, mints %d; want the cached token", err, r.fake.Mints())
	}

	creds.version.Add(1)
	if _, err := client.Token(ctx, 1000); err != nil {
		t.Fatalf("Token after the key changed: %v", err)
	}
	if r.fake.Mints() != 2 {
		t.Errorf("mints = %d, want 2: a token minted under the old key was served", r.fake.Mints())
	}
	if _, err := client.Token(ctx, 1000); err != nil || r.fake.Mints() != 2 {
		t.Errorf("Token again = %v, mints %d; want the new token cached", err, r.fake.Mints())
	}
}
