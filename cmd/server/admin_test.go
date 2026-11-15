package main

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/mdg-labs/release-ops/internal/config"
	"github.com/mdg-labs/release-ops/internal/crypto"
	"github.com/mdg-labs/release-ops/internal/store"
)

func TestAdminCommandTakesOnlyLoginLink(t *testing.T) {
	for _, args := range [][]string{nil, {"login-link", "extra"}, {"other"}} {
		if err := runAdminCommand(context.Background(), args, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "usage: release-ops admin login-link") {
			t.Errorf("runAdminCommand(%v) = %v, want the usage", args, err)
		}
	}
}

// `release-ops admin login-link` against the database of the running server: it refuses before a
// bootstrap admin exists, and afterwards prints a link whose token signs the admin in through
// POST /api/v1/auth/login-link once, and only once.
func TestAdminLoginLinkSignsInTheBootstrapAdminThroughTheRunningServer(t *testing.T) {
	cfg := &config.Config{AppURL: "https://release-ops.example.com"}
	c := startServer(t, cfg)

	var out bytes.Buffer
	if err := printLoginLink(context.Background(), cfg, &out); err == nil || !strings.Contains(err.Error(), "no bootstrap admin") {
		t.Fatalf("printLoginLink before an admin = %v, want the refusal", err)
	}
	if out.Len() != 0 {
		t.Fatalf("a refused command printed %q", out.String())
	}

	db, err := store.OpenPath(cfg.AppDBPath)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	cipher, err := crypto.NewCipherFromHex(cfg.AppEncryptionKey)
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	if _, err := store.New(db, cipher).SignIn().SignIn(context.Background(), store.GitHubUser{ID: 583231, Login: "octocat"}, true); err != nil {
		t.Fatalf("pin the bootstrap admin: %v", err)
	}

	if err := printLoginLink(context.Background(), cfg, &out); err != nil {
		t.Fatalf("printLoginLink: %v", err)
	}
	const prefix = "https://release-ops.example.com/login/link?token="
	_, link, ok := strings.Cut(out.String(), "\n")
	if !ok || !strings.HasPrefix(link, prefix) {
		t.Fatalf("output = %q, want a link under %s", out.String(), prefix)
	}
	token := strings.TrimSpace(strings.TrimPrefix(link, prefix))
	if len(token) != 64 {
		t.Fatalf("token = %q, want 32 random bytes as hex", token)
	}

	if code, _ := c.do(http.MethodGet, "/api/v1/status", nil); code != http.StatusUnauthorized {
		t.Fatalf("status before the link = %d, want 401", code)
	}
	if code, body := c.do(http.MethodPost, "/api/v1/auth/login-link", map[string]string{"token": token}); code != http.StatusOK {
		t.Fatalf("redeem = %d %s, want 200", code, body)
	}
	if code, body := c.do(http.MethodGet, "/api/v1/auth/session", nil); code != http.StatusOK || !strings.Contains(string(body), `"id":583231`) || !strings.Contains(string(body), `"role":"admin"`) {
		t.Fatalf("session = %d %s, want the admin", code, body)
	}
	if code, _ := c.do(http.MethodGet, "/api/v1/status", nil); code != http.StatusOK {
		t.Fatalf("status with the link's session = %d, want 200", code)
	}
	if code, _ := c.do(http.MethodPost, "/api/v1/auth/login-link", map[string]string{"token": token}); code != http.StatusBadRequest {
		t.Fatalf("second redemption = %d, want 400", code)
	}
}
