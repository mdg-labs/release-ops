package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/config"
	"github.com/mdg-labs/release-ops/internal/queue"
	"github.com/mdg-labs/release-ops/internal/queue/worker"
	"github.com/mdg-labs/release-ops/internal/renovate"
)

type constantToken struct{}

func (constantToken) Token(context.Context, string, string) (queue.TokenResponse, error) {
	return queue.TokenResponse{Token: "ghs_" + strings.Repeat("J", 36), ExpiresAt: time.Now().Add(time.Hour)}, nil
}

// A worker that completes a Renovate job returns the global config it ran with; the app stores
// it for the installation, and the settings page's route shows it, with token-shaped strings
// replaced and every other byte as the file has it.
func TestACompletedRenovateJobMakesTheGlobalConfigAvailableOnTheSettingsRoute(t *testing.T) {
	s := startPRServer(t)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	admin := &apiClient{t: t, base: s.public, http: &http.Client{Jar: jar}}
	signInAsAdmin(t, s.cfg, admin)

	read := func() map[string]any {
		t.Helper()
		code, body := admin.do(http.MethodGet, "/api/v1/settings/global-config", nil)
		if code != http.StatusOK {
			t.Fatalf("GET global-config = %d %s", code, body)
		}
		var out map[string]any
		if err := json.Unmarshal(body, &out); err != nil {
			t.Fatalf("global-config: %v in %s", err, body)
		}
		return out
	}
	if got := read(); got["content"] != nil {
		t.Fatalf("global config before any job = %v, want null content", got)
	}

	dir := t.TempDir()
	mount := filepath.Join(dir, "config")
	if err := os.Mkdir(mount, 0o755); err != nil {
		t.Fatal(err)
	}
	ghp := "ghp_" + strings.Repeat("A", 36)
	raw := "{\r\n  \"onboarding\": false,\r\n\t\"note\": \"" + ghp + "\"\r\n}"
	if err := os.WriteFile(filepath.Join(mount, "config.json"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "renovate")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	h, err := renovate.NewHandler(renovate.Options{
		Tokens: constantToken{}, GitHubAPIURL: "https://api.github.example/v3", SameUser: true, WorkerSecret: testWorkerSecret,
		Binary: bin, CacheRoot: filepath.Join(dir, "cache"), ConfigMount: mount, ToolsMount: filepath.Join(dir, "no-tools"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "cache"), 0o755); err != nil {
		t.Fatal(err)
	}

	ctx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = runWorker(ctx, &config.WorkerConfig{InternalURL: s.internal, WorkerSecret: testWorkerSecret},
			worker.Registry{queue.KindRenovateRun: h}, worker.Options{Slots: 1, HeartbeatInterval: 50 * time.Millisecond})
	}()
	t.Cleanup(func() {
		stop()
		<-done
	})
	s.enqueueRun(t)

	want := strings.ReplaceAll(raw, ghp, "[REDACTED]")
	deadline := time.Now().Add(30 * time.Second)
	var got map[string]any
	for {
		if got = read(); got["content"] != nil || time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if got["content"] != want || got["source"] != "mounted" || got["capturedAt"] == nil {
		t.Fatalf("global config after a completed job = %v, want %q from the mount", got, want)
	}
}
