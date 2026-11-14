package github_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/github"
)

const conversionPath = "/app-manifests/abc123/conversions"

var secrets = []string{"fixture-client-secret-value", "fixture-webhook-secret-value", "fixture-private-key-body"}

func fixture(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("testdata/manifest_conversion.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return string(b)
}

func TestConvertManifestDecodesTheDocumentedResponse(t *testing.T) {
	t.Parallel()

	var gotAuth, gotMethod, gotPath atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth.Store(r.Header.Get("Authorization"))
		gotMethod.Store(r.Method)
		gotPath.Store(r.URL.Path)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(fixture(t)))
	}))
	defer srv.Close()

	got, err := github.NewClient(srv.URL, 0).ConvertManifest(context.Background(), "abc123")
	if err != nil {
		t.Fatalf("ConvertManifest: %v", err)
	}
	if got.ID != 1234567 || got.Slug != "release-ops-acme" || got.ClientID != "fixture-client-id" ||
		got.Owner.Login != "acme" || got.Owner.Type != "Organization" ||
		got.HTMLURL != "https://github.com/apps/release-ops-acme" ||
		got.ClientSecret != "fixture-client-secret-value" || got.WebhookSecret != "fixture-webhook-secret-value" ||
		!strings.Contains(got.PEM, "fixture-private-key-body") {
		t.Fatalf("decoded conversion = %+v", got)
	}
	if gotMethod.Load() != http.MethodPost || gotPath.Load() != conversionPath {
		t.Errorf("request = %v %v, want POST %s", gotMethod.Load(), gotPath.Load(), conversionPath)
	}
	if gotAuth.Load() != "" {
		t.Errorf("the conversion sent an Authorization header: no credential belongs on this call")
	}
}

func TestConvertManifestStatuses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		status  int
		wantErr error
	}{
		{"unknown or expired code", http.StatusNotFound, github.ErrCodeRejected},
		{"validation failed", http.StatusUnprocessableEntity, github.ErrCodeRejected},
		{"server error", http.StatusInternalServerError, github.ErrConversionFailed},
		{"forbidden", http.StatusForbidden, github.ErrConversionFailed},
		{"redirect is not followed", http.StatusFound, github.ErrConversionFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var followed atomic.Bool
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/elsewhere" {
					followed.Store(true)
				}
				w.Header().Set("Location", "/elsewhere")
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(fixture(t)))
			}))
			defer srv.Close()

			got, err := github.NewClient(srv.URL, 0).ConvertManifest(context.Background(), "abc123")
			if got != nil || !errors.Is(err, tt.wantErr) {
				t.Fatalf("ConvertManifest = %v, %v; want nil and %v", got, err, tt.wantErr)
			}
			for _, s := range secrets {
				if strings.Contains(err.Error(), s) {
					t.Errorf("error %q contains a secret from the response", err)
				}
			}
			if followed.Load() {
				t.Error("the client followed a redirect")
			}
		})
	}
}

func TestConvertManifestTimesOut(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		<-release
	}))
	defer srv.Close()
	defer close(release)

	start := time.Now()
	got, err := github.NewClient(srv.URL, 50*time.Millisecond).ConvertManifest(context.Background(), "abc123")
	if got != nil || !errors.Is(err, github.ErrConversionFailed) {
		t.Fatalf("ConvertManifest = %v, %v; want nil and ErrConversionFailed", got, err)
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("the call outlived its timeout: %v", time.Since(start))
	}
}

func TestConvertManifestRejectsAnUnusableResponse(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"not json":           "<html>",
		"missing secret":     `{"id":1,"slug":"a","html_url":"https://x","owner":{"login":"o","type":"User"},"client_id":"c","client_secret":"","webhook_secret":"w","pem":"p"}`,
		"unknown owner type": `{"id":1,"slug":"a","html_url":"https://x","owner":{"login":"o","type":"Bot"},"client_id":"c","client_secret":"s","webhook_secret":"w","pem":"p"}`,
		"too large":          `{"pad":"` + strings.Repeat("a", 2<<20) + `"}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(body))
			}))
			defer srv.Close()

			got, err := github.NewClient(srv.URL, 0).ConvertManifest(context.Background(), "abc123")
			if got != nil || !errors.Is(err, github.ErrConversionFailed) {
				t.Fatalf("ConvertManifest = %v, %v; want nil and ErrConversionFailed", got, err)
			}
		})
	}
}

func TestConvertManifestRefusesACodeThatIsNotPathSafe(t *testing.T) {
	t.Parallel()

	var called atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called.Store(true) }))
	defer srv.Close()

	for _, code := range []string{"", "../x", "a/b", "a b", "a?b", strings.Repeat("a", 129)} {
		if _, err := github.NewClient(srv.URL, 0).ConvertManifest(context.Background(), code); !errors.Is(err, github.ErrCodeRejected) {
			t.Errorf("code %q: err = %v, want ErrCodeRejected", code, err)
		}
	}
	if called.Load() {
		t.Error("a malformed code reached GitHub")
	}
}

func TestConversionDoesNotPrintItsSecrets(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(fixture(t)))
	}))
	defer srv.Close()
	got, err := github.NewClient(srv.URL, 0).ConvertManifest(context.Background(), "abc123")
	if err != nil {
		t.Fatalf("ConvertManifest: %v", err)
	}

	var logs bytes.Buffer
	slog.New(slog.NewJSONHandler(&logs, nil)).Info("converted", "app", got)
	rendered := logs.String() + fmt.Sprintf("%v|%+v|%#v|%s", got, got, got, got) + fmt.Sprintf("%v|%+v", *got, *got)
	for _, s := range append(secrets, "fixture-client-id") {
		if strings.Contains(rendered, s) {
			t.Errorf("formatted conversion contains %q: %s", s, rendered)
		}
	}
	if !strings.Contains(rendered, "release-ops-acme") {
		t.Errorf("formatted conversion lost the slug: %s", rendered)
	}
}

func TestAPIBaseURL(t *testing.T) {
	t.Parallel()

	for in, want := range map[string]string{
		"https://github.com":      "https://api.github.com",
		"https://github.com/":     "https://api.github.com",
		"https://ghe.example.com": "https://ghe.example.com/api/v3",
		"http://127.0.0.1:8081/":  "http://127.0.0.1:8081/api/v3",
	} {
		if got := github.APIBaseURL(in); got != want {
			t.Errorf("APIBaseURL(%q) = %q, want %q", in, got, want)
		}
	}
}
