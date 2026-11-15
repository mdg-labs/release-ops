package github_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/crypto"
	"github.com/mdg-labs/release-ops/internal/github"
	"github.com/mdg-labs/release-ops/internal/github/githubtest"
	"github.com/mdg-labs/release-ops/internal/store"
	"github.com/mdg-labs/release-ops/internal/store/storetest"
)

func botUserServer(t *testing.T, status int, body string) (*httptest.Server, *atomic.Int32, *sync.Map) {
	t.Helper()
	var calls atomic.Int32
	seen := &sync.Map{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		seen.Store("path", r.URL.Path)
		seen.Store("method", r.Method)
		seen.Store("auth", r.Header.Get("Authorization"))
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &calls, seen
}

func TestBotUserIDReadsTheBotUsersNumericIdWithoutACredential(t *testing.T) {
	t.Parallel()

	srv, _, seen := botUserServer(t, http.StatusOK, `{"id":424242,"login":"Release-Ops-Acme[bot]","type":"Bot"}`)
	id, err := github.NewClient(srv.URL, 0).BotUserID(context.Background(), "release-ops-acme")
	if err != nil || id != 424242 {
		t.Fatalf("BotUserID = %d, %v, want 424242", id, err)
	}
	if got, _ := seen.Load("path"); got != "/users/release-ops-acme[bot]" {
		t.Errorf("path = %v, want /users/release-ops-acme[bot]", got)
	}
	if got, _ := seen.Load("method"); got != http.MethodGet {
		t.Errorf("method = %v, want GET", got)
	}
	if got, _ := seen.Load("auth"); got != "" {
		t.Errorf("Authorization = %v, want none: the endpoint is public", got)
	}
}

func TestBotUserIDRefusesWhatIsNotTheBotUser(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		status int
		body   string
	}{
		"not found":         {http.StatusNotFound, `{"message":"Not Found"}`},
		"server error":      {http.StatusBadGateway, ``},
		"rate limited":      {http.StatusForbidden, `{"message":"rate limit"}`},
		"not json":          {http.StatusOK, `<html>`},
		"no id":             {http.StatusOK, `{"login":"ro[bot]","type":"Bot"}`},
		"zero id":           {http.StatusOK, `{"id":0,"login":"ro[bot]","type":"Bot"}`},
		"negative id":       {http.StatusOK, `{"id":-3,"login":"ro[bot]","type":"Bot"}`},
		"string id":         {http.StatusOK, `{"id":"12","login":"ro[bot]","type":"Bot"}`},
		"a human user":      {http.StatusOK, `{"id":9,"login":"ro[bot]","type":"User"}`},
		"another login":     {http.StatusOK, `{"id":9,"login":"other[bot]","type":"Bot"}`},
		"login without bot": {http.StatusOK, `{"id":9,"login":"ro","type":"Bot"}`},
		"oversized":         {http.StatusOK, `{"id":9,"login":"ro[bot]","type":"Bot","pad":"` + strings.Repeat("x", 2<<20) + `"}`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			srv, _, _ := botUserServer(t, tc.status, tc.body)
			id, err := github.NewClient(srv.URL, 0).BotUserID(context.Background(), "ro")
			if !errors.Is(err, github.ErrBotUserLookupFailed) || id != 0 {
				t.Fatalf("BotUserID = %d, %v, want ErrBotUserLookupFailed and no id", id, err)
			}
			if strings.Contains(err.Error(), "Not Found") || strings.Contains(err.Error(), "rate limit") {
				t.Errorf("error %q carries GitHub's response", err)
			}
		})
	}
}

func TestBotUserIDRefusesASlugThatIsNotOneWithoutCallingGitHub(t *testing.T) {
	t.Parallel()

	srv, calls, _ := botUserServer(t, http.StatusOK, `{}`)
	for _, slug := range []string{"", "a/b", "a b", "../x", "a?b", "a[bot]", strings.Repeat("a", 101)} {
		if _, err := github.NewClient(srv.URL, 0).BotUserID(context.Background(), slug); !errors.Is(err, github.ErrBotUserLookupFailed) {
			t.Errorf("BotUserID(%q) = %v, want ErrBotUserLookupFailed", slug, err)
		}
	}
	if calls.Load() != 0 {
		t.Errorf("GitHub was called %d times for a malformed slug", calls.Load())
	}
}

func TestBotUserIDTimesOut(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	t.Cleanup(func() { close(release); srv.Close() })
	start := time.Now()
	if _, err := github.NewClient(srv.URL, 50*time.Millisecond).BotUserID(context.Background(), "ro"); !errors.Is(err, github.ErrBotUserLookupFailed) {
		t.Fatalf("BotUserID = %v, want ErrBotUserLookupFailed", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("the lookup took %s, want it bounded by the client timeout", time.Since(start))
	}
}

type botRig struct {
	fake     *githubtest.Fake
	apps     store.GitHubAppRepository
	resolver *github.BotResolver
}

func newBotRig(t *testing.T) *botRig {
	t.Helper()
	fake := githubtest.New(t, time.Now)
	db := storetest.OpenMigrated(t)
	cipher, err := crypto.NewCipherFromHex(strings.Repeat("ab", 32))
	if err != nil {
		t.Fatal(err)
	}
	apps := store.New(db, cipher).GitHubApp()
	return &botRig{fake: fake, apps: apps, resolver: github.NewBotResolver(github.NewClient(fake.URL, 5*time.Second), apps)}
}

func (r *botRig) createApp(t *testing.T) {
	t.Helper()
	if err := r.apps.Create(context.Background(), store.NewGitHubApp{
		AppID: githubtest.AppID, Slug: "release-ops-acme", ClientID: "cid", HTMLURL: "https://github.com/apps/release-ops-acme",
		OwnerLogin: "acme", OwnerType: "Organization", PrivateKey: string(r.fake.PEM), ClientSecret: "cs", WebhookSecret: "ws",
	}); err != nil {
		t.Fatalf("create app: %v", err)
	}
}

func (r *botRig) lookups() int {
	n := 0
	for _, req := range r.fake.Requests() {
		if strings.HasPrefix(req.Path, "/users/") {
			n++
		}
	}
	return n
}

func (r *botRig) botUserID(t *testing.T) (int64, bool) {
	t.Helper()
	bot, err := r.apps.BotIdentity(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return bot.UserID, bot.Known
}

func TestEnsureFillsInTheBotUserIdOnceAndNeverReadsItAgain(t *testing.T) {
	t.Parallel()

	r := newBotRig(t)
	r.createApp(t)
	r.fake.SetBotUser("release-ops-acme", 424242)

	if err := r.resolver.Ensure(context.Background()); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if id, known := r.botUserID(t); !known || id != 424242 {
		t.Fatalf("bot user id = %d, %v, want 424242", id, known)
	}
	r.fake.SetBotUser("release-ops-acme", 999)
	if err := r.resolver.Ensure(context.Background()); err != nil {
		t.Fatalf("second Ensure: %v", err)
	}
	if id, _ := r.botUserID(t); id != 424242 {
		t.Errorf("bot user id = %d after a second Ensure, want 424242 kept", id)
	}
	if n := r.lookups(); n != 1 {
		t.Errorf("GitHub was asked %d times, want once", n)
	}
}

func TestEnsureBeforeTheAppIsStoredDoesNothing(t *testing.T) {
	t.Parallel()

	r := newBotRig(t)
	if err := r.resolver.Ensure(context.Background()); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if n := r.lookups(); n != 0 {
		t.Errorf("GitHub was asked %d times with no App stored", n)
	}
}

func TestEnsureLeavesTheIdUnsetWhenTheLookupFailsAndRetriesLater(t *testing.T) {
	t.Parallel()

	r := newBotRig(t)
	r.createApp(t)
	r.fake.SetBotUser("release-ops-acme", 424242)
	r.fake.FailBotUser(http.StatusBadGateway)

	if err := r.resolver.Ensure(context.Background()); !errors.Is(err, github.ErrBotUserLookupFailed) {
		t.Fatalf("Ensure = %v, want ErrBotUserLookupFailed", err)
	}
	if _, known := r.botUserID(t); known {
		t.Fatal("a failed lookup stored an id")
	}

	r.fake.FailBotUser(0)
	if err := r.resolver.Ensure(context.Background()); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if id, known := r.botUserID(t); !known || id != 424242 {
		t.Errorf("bot user id after the retry = %d, %v, want 424242", id, known)
	}
}

func TestEnsureStoresNothingForAnUnknownBotUser(t *testing.T) {
	t.Parallel()

	r := newBotRig(t)
	r.createApp(t)
	if err := r.resolver.Ensure(context.Background()); !errors.Is(err, github.ErrBotUserLookupFailed) {
		t.Fatalf("Ensure = %v, want ErrBotUserLookupFailed", err)
	}
	if _, known := r.botUserID(t); known {
		t.Fatal("an id was stored for a bot user GitHub does not know")
	}
}

func TestRunTriesAtOnceAndAgainOnEveryTick(t *testing.T) {
	t.Parallel()

	r := newBotRig(t)
	r.createApp(t)
	r.fake.FailBotUser(http.StatusBadGateway)

	ctx, cancel := context.WithCancel(context.Background())
	ticks := make(chan time.Time)
	reports := make(chan error, 4)
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.resolver.Run(ctx, ticks, func(err error) { reports <- err })
	}()

	if err := <-reports; !errors.Is(err, github.ErrBotUserLookupFailed) {
		t.Fatalf("first report = %v, want a failed lookup", err)
	}
	r.fake.SetBotUser("release-ops-acme", 424242)
	r.fake.FailBotUser(0)
	ticks <- time.Now()
	if err := <-reports; err != nil {
		t.Fatalf("report after the tick = %v, want success", err)
	}
	if id, known := r.botUserID(t); !known || id != 424242 {
		t.Errorf("bot user id = %d, %v, want 424242", id, known)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop when its context ended")
	}
}
