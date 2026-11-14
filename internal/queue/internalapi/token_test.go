package internalapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/crypto"
	"github.com/mdg-labs/release-ops/internal/github"
	"github.com/mdg-labs/release-ops/internal/github/githubtest"
	"github.com/mdg-labs/release-ops/internal/queue"
	"github.com/mdg-labs/release-ops/internal/queue/internalapi"
	"github.com/mdg-labs/release-ops/internal/store"
	"github.com/mdg-labs/release-ops/internal/store/storetest"
)

var tokenT0 = time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)

// syncBuffer is a log sink the server goroutines and the test can share.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type tokenEnv struct {
	srv   *httptest.Server
	q     *store.JobQueue
	fake  *githubtest.Fake
	clock *githubtest.Clock
	repos []string
	logs  *syncBuffer
}

// newTokenEnv wires the real queue, the real App store and AppClient and a fake GitHub behind
// the handler. Installation 1 of the seeded repositories is the one the fake knows.
func newTokenEnv(t *testing.T, presets ...string) *tokenEnv {
	t.Helper()

	logs := &syncBuffer{}
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	db := storetest.OpenMigrated(t)
	clock := githubtest.NewClock(tokenT0)
	fake := githubtest.New(t, clock.Now)
	fake.AddInstallation(1, githubtest.AppID)

	cipher, err := crypto.NewCipherFromHex(strings.Repeat("cd", 32))
	if err != nil {
		t.Fatal(err)
	}
	st := store.New(db, cipher)
	if err := st.GitHubApp().Create(context.Background(), store.NewGitHubApp{
		AppID: githubtest.AppID, Slug: "release-ops-acme", ClientID: "cid", HTMLURL: "https://github.com/apps/release-ops-acme",
		OwnerLogin: "acme", OwnerType: "Organization", PrivateKey: string(fake.PEM), ClientSecret: "client-secret-value", WebhookSecret: "webhook-secret-value",
	}); err != nil {
		t.Fatalf("store app: %v", err)
	}

	q := store.NewJobQueue(db, store.JobQueueOptions{Now: clock.Now})
	app := github.NewAppClient(github.NewClient(fake.URL, 5*time.Second), st.GitHubApp(), github.AppClientOptions{Now: clock.Now})
	srv := httptest.NewServer(internalapi.NewHandler(q, internalapi.Options{Secret: secret, Tokens: app, TokenRepositories: presets}))
	t.Cleanup(srv.Close)
	return &tokenEnv{srv: srv, q: q, fake: fake, clock: clock, repos: storetest.SeedRepositories(t, db, 2), logs: logs}
}

func (e *tokenEnv) claim(t *testing.T, repo string) *queue.ClaimedJob {
	t.Helper()
	if _, err := e.q.Enqueue(context.Background(), queue.EnqueueParams{RepositoryID: repo, Priority: queue.PriorityHigh, Trigger: queue.TriggerPush}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	job, err := e.q.Claim(context.Background())
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	return job
}

func (e *tokenEnv) requestToken(t *testing.T, auth, jobID string, body any) (int, []byte) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, e.srv.URL+strings.Replace(queue.PathToken, "{id}", jobID, 1), bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST token: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var out bytes.Buffer
	_, _ = out.ReadFrom(resp.Body)
	if resp.StatusCode == http.StatusOK && resp.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("token response Cache-Control = %q, want no-store", resp.Header.Get("Cache-Control"))
	}
	return resp.StatusCode, out.Bytes()
}

func (e *tokenEnv) tokenRequests() int { return len(e.fake.TokenRequests()) }

func (e *tokenEnv) assertNoSecretsLeaked(t *testing.T, responses ...[]byte) {
	t.Helper()
	leaks := []string{githubtest.TokenPrefix, "BEGIN RSA PRIVATE KEY", "client-secret-value", "webhook-secret-value", "Bearer "}
	texts := []string{e.logs.String()}
	for _, r := range responses {
		texts = append(texts, string(r))
	}
	for _, text := range texts {
		for _, leak := range leaks {
			if strings.Contains(text, leak) {
				t.Errorf("%q appears in a log or a response: %s", leak, text)
			}
		}
	}
}

func TestTokenRouteIssuesAScopedTokenForTheClaimedJob(t *testing.T) {
	e := newTokenEnv(t, "acme/presets", "other/presets")
	job := e.claim(t, e.repos[0])

	status, body := e.requestToken(t, "Bearer "+secret, job.ID, queue.LeaseRequest{LeaseOwner: job.LeaseOwner})
	if status != http.StatusOK {
		t.Fatalf("token = %d %s, want 200", status, body)
	}
	var got queue.TokenResponse
	if err := json.Unmarshal(body, &got); err != nil || got.Token != githubtest.TokenPrefix+"1" || !got.ExpiresAt.Equal(tokenT0.Add(time.Hour)) {
		t.Fatalf("token body = %s (%v)", body, err)
	}

	reqs := e.fake.TokenRequests()
	if len(reqs) != 1 || !slices.Equal(reqs[0], []string{"repo-1", "presets"}) {
		t.Errorf("token requests = %v, want one scoped to the job's repo-1 and the preset acme/presets", reqs)
	}
	for _, r := range e.fake.Requests() {
		if !r.JWTValid {
			t.Errorf("a request to GitHub carried no valid App JWT: %+v", r)
		}
	}
	e.assertNoSecretsLeaked(t)
}

// The repositories come from the job and the configuration: whatever else a request carries is
// not read.
func TestTokenRouteIgnoresRepositoriesInTheRequest(t *testing.T) {
	e := newTokenEnv(t)
	job := e.claim(t, e.repos[0])

	status, body := e.requestToken(t, "Bearer "+secret, job.ID, map[string]any{
		"lease_owner":  job.LeaseOwner,
		"repositories": []string{"acme/repo-2", "acme/secret"},
		"repository":   "acme/secret",
	})
	if status != http.StatusOK {
		t.Fatalf("token = %d %s", status, body)
	}
	if reqs := e.fake.TokenRequests(); len(reqs) != 1 || !slices.Equal(reqs[0], []string{"repo-1"}) {
		t.Errorf("token requests = %v, want the job's repository only", reqs)
	}
}

func TestTokenRouteRefusesAJobTheCallerDoesNotHold(t *testing.T) {
	e := newTokenEnv(t)
	first := e.claim(t, e.repos[0])
	second := e.claim(t, e.repos[1])

	// A released job is queued again and leased to nobody.
	if err := e.q.Release(context.Background(), second.ID, second.LeaseOwner); err != nil {
		t.Fatal(err)
	}

	for name, c := range map[string]struct{ id, owner string }{
		"an unknown job":                 {"no-such-job", first.LeaseOwner},
		"another worker's lease":         {first.ID, "1.0123456789abcdef"},
		"a job that is queued, not held": {second.ID, second.LeaseOwner},
		"no lease owner":                 {first.ID, ""},
	} {
		status, body := e.requestToken(t, "Bearer "+secret, c.id, queue.LeaseRequest{LeaseOwner: c.owner})
		if status != http.StatusConflict {
			t.Errorf("%s: token = %d %s, want 409", name, status, body)
		}
	}
	if e.tokenRequests() != 0 {
		t.Errorf("GitHub saw %d token requests for jobs the caller does not hold, want none", e.tokenRequests())
	}

	// The holder is served.
	if status, body := e.requestToken(t, "Bearer "+secret, first.ID, queue.LeaseRequest{LeaseOwner: first.LeaseOwner}); status != http.StatusOK {
		t.Errorf("holder: token = %d %s, want 200", status, body)
	}
}

func TestTokenRouteRefusesAnExpiredLeaseUntilItIsRenewed(t *testing.T) {
	e := newTokenEnv(t)
	job := e.claim(t, e.repos[0])

	e.clock.Advance(queue.DefaultConfig().LeaseDuration + time.Second)
	if status, body := e.requestToken(t, "Bearer "+secret, job.ID, queue.LeaseRequest{LeaseOwner: job.LeaseOwner}); status != http.StatusConflict {
		t.Fatalf("expired lease: token = %d %s, want 409", status, body)
	}
	if e.tokenRequests() != 0 {
		t.Fatal("a token was requested for an expired lease")
	}

	if _, err := e.q.Heartbeat(context.Background(), job.ID, job.LeaseOwner); err != nil {
		t.Fatal(err)
	}
	if status, body := e.requestToken(t, "Bearer "+secret, job.ID, queue.LeaseRequest{LeaseOwner: job.LeaseOwner}); status != http.StatusOK {
		t.Errorf("renewed lease: token = %d %s, want 200", status, body)
	}
}

func TestTokenRouteNeedsTheWorkerSecret(t *testing.T) {
	e := newTokenEnv(t)
	job := e.claim(t, e.repos[0])

	for _, auth := range []string{"", "Bearer wrong", "Bearer " + secret + "x", secret} {
		status, body := e.requestToken(t, auth, job.ID, queue.LeaseRequest{LeaseOwner: job.LeaseOwner})
		if status != http.StatusUnauthorized {
			t.Errorf("Authorization %q: token = %d, want 401", auth, status)
		}
		if strings.Contains(string(body), githubtest.TokenPrefix) {
			t.Errorf("a 401 carries a token: %s", body)
		}
	}
	if e.tokenRequests() != 0 {
		t.Error("an unauthenticated request reached GitHub")
	}
}

func TestTokenRouteRefreshesAnExpiredCachedToken(t *testing.T) {
	e := newTokenEnv(t)
	job := e.claim(t, e.repos[0])
	lease := queue.LeaseRequest{LeaseOwner: job.LeaseOwner}

	var first, again, fresh queue.TokenResponse
	for i, into := range []*queue.TokenResponse{&first, &again} {
		status, body := e.requestToken(t, "Bearer "+secret, job.ID, lease)
		if status != http.StatusOK || json.Unmarshal(body, into) != nil {
			t.Fatalf("request %d = %d %s", i+1, status, body)
		}
	}
	if first.Token != again.Token || e.fake.Mints() != 1 {
		t.Fatalf("second request: token %q vs %q, mints %d, want the cached token", again.Token, first.Token, e.fake.Mints())
	}

	e.clock.Advance(56 * time.Minute)
	if _, err := e.q.Heartbeat(context.Background(), job.ID, job.LeaseOwner); err != nil {
		t.Fatal(err)
	}
	status, body := e.requestToken(t, "Bearer "+secret, job.ID, lease)
	if status != http.StatusOK || json.Unmarshal(body, &fresh) != nil || fresh.Token == first.Token || e.fake.Mints() != 2 {
		t.Errorf("after 56 minutes: %d %s, mints %d, want a refreshed token", status, body, e.fake.Mints())
	}
}

// A job cannot renew the token it is given, so a later job on the same repository must not be
// handed an earlier job's token that is close to expiry.
func TestTokenRouteGivesANewJobATokenWithAboutAnHourLeft(t *testing.T) {
	e := newTokenEnv(t)
	first := e.claim(t, e.repos[0])
	var firstToken queue.TokenResponse
	status, body := e.requestToken(t, "Bearer "+secret, first.ID, queue.LeaseRequest{LeaseOwner: first.LeaseOwner})
	if status != http.StatusOK || json.Unmarshal(body, &firstToken) != nil {
		t.Fatalf("first token = %d %s", status, body)
	}
	if err := e.q.Complete(context.Background(), first.ID, first.LeaseOwner); err != nil {
		t.Fatal(err)
	}

	e.clock.Advance(54 * time.Minute)
	second := e.claim(t, e.repos[0])
	var secondToken queue.TokenResponse
	status, body = e.requestToken(t, "Bearer "+secret, second.ID, queue.LeaseRequest{LeaseOwner: second.LeaseOwner})
	if status != http.StatusOK || json.Unmarshal(body, &secondToken) != nil {
		t.Fatalf("second token = %d %s", status, body)
	}
	if secondToken.Token == firstToken.Token || e.fake.Mints() != 2 {
		t.Errorf("the second job got the first job's token (mints %d)", e.fake.Mints())
	}
	if left := secondToken.ExpiresAt.Sub(e.clock.Now()); left < 55*time.Minute {
		t.Errorf("the second job's token has %s left, want about an hour", left)
	}
}

func TestTokenRouteAnswersAGitHubFailureCleanly(t *testing.T) {
	for _, githubStatus := range []int{401, 403, 500, 503} {
		e := newTokenEnv(t)
		job := e.claim(t, e.repos[0])
		e.fake.FailTokens(githubStatus)

		status, body := e.requestToken(t, "Bearer "+secret, job.ID, queue.LeaseRequest{LeaseOwner: job.LeaseOwner})
		if status != http.StatusBadGateway {
			t.Errorf("GitHub %d: token = %d %s, want 502", githubStatus, status, body)
		}
		if strings.Contains(string(body), "refused") {
			t.Errorf("GitHub %d: the response carries GitHub's body: %s", githubStatus, body)
		}
		e.assertNoSecretsLeaked(t, body)
	}
}
