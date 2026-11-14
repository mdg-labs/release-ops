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
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/config"
	"github.com/mdg-labs/release-ops/internal/crypto"
	"github.com/mdg-labs/release-ops/internal/github/githubtest"
	"github.com/mdg-labs/release-ops/internal/store"
)

// recoveryRig is a database with the App stored and a fake GitHub that the server reaches
// under /api/v3, as it reaches a GitHub Enterprise base URL.
type recoveryRig struct {
	fake      *githubtest.Fake
	db        *store.DB
	dbPath    string
	githubURL string
}

func newRecoveryRig(t *testing.T) *recoveryRig {
	t.Helper()
	fake := githubtest.New(t, time.Now)
	fake.AddInstallation(1000, githubtest.AppID)
	fake.SetRepositories(1000, githubtest.Repo{ID: 7001, FullName: "acme/widgets", DefaultBranch: "main"})
	fake.SetFiles("acme/widgets", "renovate.json")
	fake.SetFiles("acme/gadgets", ".github/renovate.json")

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
	return &recoveryRig{fake: fake, db: db, dbPath: dbPath, githubURL: github.URL}
}

func (r *recoveryRig) count(method, pathPrefix string) int {
	n := 0
	for _, req := range r.fake.Requests() {
		if req.Method == method && strings.HasPrefix(req.Path, pathPrefix) {
			n++
		}
	}
	return n
}

func shortenIntervals(t *testing.T) {
	t.Helper()
	sync, polling, redelivery := installationSyncInterval, pollingInterval, redeliveryInterval
	t.Cleanup(func() { installationSyncInterval, pollingInterval, redeliveryInterval = sync, polling, redelivery })
	pollingInterval, redeliveryInterval = 50*time.Millisecond, 50*time.Millisecond
}

// With a webhook URL the server asks GitHub, at start-up and on every tick, to redeliver a
// failed delivery; the redelivery reaches the signed webhook route and is applied. Once the
// redelivery succeeded, later passes leave the GUID alone.
func TestServerRedeliversAFailedWebhookDelivery(t *testing.T) {
	shortenIntervals(t)
	r := newRecoveryRig(t)

	body, err := os.ReadFile(filepath.Join("..", "..", "internal", "api", "hooks", "testdata", "installation_repositories_added.json"))
	if err != nil {
		t.Fatal(err)
	}
	ready := make(chan struct{})
	var base string
	r.fake.OnRedelivery(func(d githubtest.Delivery) int {
		<-ready
		// GitHub repeats the original delivery once the installation is recorded.
		deadline := time.Now().Add(20 * time.Second)
		for repositoryRow(t, r.db, 7001) == "" {
			if time.Now().After(deadline) {
				return 0
			}
			time.Sleep(25 * time.Millisecond)
		}
		mac := hmac.New(sha256.New, []byte(syncTestWebhookSecret))
		mac.Write(body)
		req, err := http.NewRequest(http.MethodPost, base+"/hooks/github", bytes.NewReader(body))
		if err != nil {
			return 0
		}
		req.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
		req.Header.Set("X-GitHub-Event", "installation_repositories")
		req.Header.Set("X-GitHub-Delivery", d.GUID)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return 0
		}
		_ = resp.Body.Close()
		return resp.StatusCode
	})
	r.fake.SetDeliveries(githubtest.Delivery{
		ID: 100, GUID: "guid-missed-1", DeliveredAt: time.Now().Add(-time.Hour), StatusCode: 502, Event: "installation_repositories",
	})

	c := startServer(t, &config.Config{
		AppDBPath:     r.dbPath,
		AppURL:        "https://release-ops.example.com",
		WebhookURL:    "https://release-ops.example.com/hooks/github",
		GitHubBaseURL: r.githubURL,
	})
	base = c.base
	close(ready)
	r.fake.SetRepositories(1000,
		githubtest.Repo{ID: 7001, FullName: "acme/widgets", DefaultBranch: "main"},
		githubtest.Repo{ID: 7002, FullName: "acme/gadgets", DefaultBranch: "trunk"},
	)

	waitFor(t, "the redelivered event to add the repository", func() bool {
		return repositoryRow(t, r.db, 7002) == "acme/gadgets trunk present"
	})
	waitFor(t, "later passes to read the deliveries again", func() bool {
		return r.count(http.MethodGet, "/app/hook/deliveries") >= 4
	})
	if got := r.fake.Redeliveries(); !slices.Equal(got, []int64{100}) {
		t.Errorf("redelivery requested for %v, want the failed delivery once", got)
	}
	for _, req := range r.fake.Requests() {
		if strings.HasPrefix(req.Path, "/app/hook/deliveries") && !req.JWTValid {
			t.Errorf("%s %s was not made with a valid App JWT", req.Method, req.Path)
		}
	}
	var guids int
	if err := r.db.Reader.QueryRow(`SELECT count(*) FROM webhook_deliveries WHERE guid = 'guid-missed-1'`).Scan(&guids); err != nil || guids != 1 {
		t.Errorf("delivery records for the redelivered GUID = %d, %v, want 1", guids, err)
	}
}

// A GitHub error on the deliveries list does not stop the loop: the next pass redelivers.
func TestServerKeepsRedeliveringAfterAFailedDeliveryList(t *testing.T) {
	shortenIntervals(t)
	r := newRecoveryRig(t)
	r.fake.FailDeliveryList(http.StatusBadGateway)
	r.fake.SetDeliveries(githubtest.Delivery{ID: 100, GUID: "guid-missed-1", DeliveredAt: time.Now().Add(-time.Hour), StatusCode: 502})

	startServer(t, &config.Config{
		AppDBPath:     r.dbPath,
		AppURL:        "https://release-ops.example.com",
		WebhookURL:    "https://release-ops.example.com/hooks/github",
		GitHubBaseURL: r.githubURL,
	})

	waitFor(t, "a failed list pass", func() bool { return r.count(http.MethodGet, "/app/hook/deliveries") >= 1 })
	if len(r.fake.Redeliveries()) != 0 {
		t.Fatalf("redelivered %v while the list failed", r.fake.Redeliveries())
	}
	r.fake.FailDeliveryList(0)
	waitFor(t, "the next pass to redeliver", func() bool { return len(r.fake.Redeliveries()) > 0 })
}

// Without a webhook URL no delivery is listed or redelivered, and the installation and
// repository sync runs at the polling interval instead of the hourly one.
func TestServerPollsInsteadOfRedeliveringWithoutAWebhookURL(t *testing.T) {
	shortenIntervals(t)
	r := newRecoveryRig(t)
	r.fake.SetDeliveries(githubtest.Delivery{ID: 100, GUID: "guid-missed-1", DeliveredAt: time.Now().Add(-time.Hour), StatusCode: 502})

	startServer(t, &config.Config{
		AppDBPath:     r.dbPath,
		AppURL:        "https://release-ops.example.com",
		GitHubBaseURL: r.githubURL,
	})

	waitFor(t, "the start-up sync and two polls", func() bool { return r.count(http.MethodGet, "/app/installations") >= 3 })
	if n := r.count(http.MethodGet, "/app/hook/deliveries") + r.count(http.MethodPost, "/app/hook/deliveries"); n != 0 {
		t.Errorf("%d delivery calls without a webhook URL, want none", n)
	}
	if got := repositoryRow(t, r.db, 7001); got != "acme/widgets main present" {
		t.Errorf("polled repository = %q, want it synced", got)
	}
}
