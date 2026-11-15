package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/config"
	"github.com/mdg-labs/release-ops/internal/crypto"
	"github.com/mdg-labs/release-ops/internal/github/githubtest"
	"github.com/mdg-labs/release-ops/internal/store"
)

const (
	syncTestEncryptionKey = "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"
	syncTestWebhookSecret = "fixture-webhook-secret-value"
)

// The server syncs the installation and its repositories from GitHub when it starts, and a
// signed installation_repositories delivery on its webhook route adds a repository and makes
// the sync fill in the default branch the payload does not carry.
func TestServerSyncsInstalledRepositoriesAtStartAndFromWebhooks(t *testing.T) {
	fake := githubtest.New(t, time.Now)
	fake.AddInstallation(1000, githubtest.AppID)
	fake.SetRepositories(1000,
		githubtest.Repo{ID: 7001, FullName: "acme/widgets", DefaultBranch: "main"},
		githubtest.Repo{ID: 7003, FullName: "acme/gizmos", DefaultBranch: "main"},
	)
	fake.SetFiles("acme/widgets", "renovate.json")
	fake.SetFiles("acme/gizmos", "README.md")
	fake.SetFiles("acme/gadgets", ".github/renovate.json")

	// The server reaches the REST API of a GitHub base URL under /api/v3.
	target, err := url.Parse(fake.URL)
	if err != nil {
		t.Fatal(err)
	}
	github := httptest.NewServer(http.StripPrefix("/api/v3", httputil.NewSingleHostReverseProxy(target)))
	t.Cleanup(github.Close)

	dbPath := filepath.Join(t.TempDir(), "app.db")
	if _, err := store.Migrate(context.Background(), dbPath); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db, err := store.OpenPath(dbPath)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	cipher, err := crypto.NewCipherFromHex(syncTestEncryptionKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.New(db, cipher).GitHubApp().Create(context.Background(), store.NewGitHubApp{
		AppID: githubtest.AppID, Slug: "release-ops-acme", ClientID: "cid", HTMLURL: "https://github.com/apps/release-ops-acme",
		OwnerLogin: "acme", OwnerType: "Organization", PrivateKey: string(fake.PEM), ClientSecret: "client-secret-value", WebhookSecret: syncTestWebhookSecret,
	}); err != nil {
		t.Fatalf("store app: %v", err)
	}

	c := startServer(t, &config.Config{
		AppDBPath:     dbPath,
		AppURL:        "https://release-ops.example.com",
		WebhookURL:    "https://release-ops.example.com/hooks/github",
		GitHubBaseURL: github.URL,
	})

	waitFor(t, "the start-up sync to record the repositories", func() bool {
		return repositoryRow(t, db, 7001) == "acme/widgets main present" && repositoryRow(t, db, 7003) == "acme/gizmos main missing"
	})
	var status string
	if err := db.Reader.QueryRow(`SELECT status FROM installations WHERE github_installation_id = 1000`).Scan(&status); err != nil || status != "active" {
		t.Fatalf("installation = %q, %v, want the synced active installation", status, err)
	}

	fake.SetRepositories(1000,
		githubtest.Repo{ID: 7001, FullName: "acme/widgets", DefaultBranch: "main"},
		githubtest.Repo{ID: 7003, FullName: "acme/gizmos", DefaultBranch: "main"},
		githubtest.Repo{ID: 7002, FullName: "acme/gadgets", DefaultBranch: "trunk"},
	)
	body, err := os.ReadFile(filepath.Join("..", "..", "internal", "api", "hooks", "testdata", "installation_repositories_added.json"))
	if err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, []byte(syncTestWebhookSecret))
	mac.Write(body)
	req, err := http.NewRequest(http.MethodPost, c.base+"/hooks/github", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	req.Header.Set("X-GitHub-Event", "installation_repositories")
	req.Header.Set("X-GitHub-Delivery", "guid-added-1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("deliver: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("webhook = %d, want 202", resp.StatusCode)
	}

	waitFor(t, "the webhook-added repository to get its default branch and config from the sync", func() bool {
		return repositoryRow(t, db, 7002) == "acme/gadgets trunk present"
	})
}

func repositoryRow(t *testing.T, db *store.DB, githubID int64) string {
	t.Helper()
	var name, branch, state string
	err := db.Reader.QueryRow(`SELECT full_name, default_branch, config_state FROM repositories WHERE github_repository_id = ?`, githubID).Scan(&name, &branch, &state)
	if err != nil {
		return ""
	}
	return name + " " + branch + " " + state
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(25 * time.Millisecond)
	}
}
