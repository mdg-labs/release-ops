package github_test

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/github"
	"github.com/mdg-labs/release-ops/internal/github/githubtest"
	"github.com/mdg-labs/release-ops/internal/ratelimit"
)

type rateRig struct {
	fake     *githubtest.Fake
	clock    *githubtest.Clock
	budget   *ratelimit.Budget
	api      *github.Client
	app      *github.AppClient
	recovery *github.Recovery
}

func newRateRig(t *testing.T) *rateRig {
	t.Helper()
	clock := githubtest.NewClock(t0)
	fake := githubtest.New(t, clock.Now)
	fake.AddInstallation(1000, githubtest.AppID)
	fake.SetRepositories(1000, githubtest.Repo{ID: 1, FullName: "acme/api", DefaultBranch: "main"})
	fake.SetUser(42, "octo")
	fake.SetOrganizationMember("acme", "octo", "active", "member")
	budget := ratelimit.NewBudget(ratelimit.Options{Now: clock.Now})
	api := github.NewClient(fake.URL, 5*time.Second).UseRateLimits(budget)
	app := github.NewAppClient(api, staticCreds{appID: githubtest.AppID, pem: fake.PEM}, github.AppClientOptions{Now: clock.Now, Budget: budget})
	return &rateRig{
		fake: fake, clock: clock, budget: budget, api: api, app: app,
		recovery: github.NewRecovery(app, github.RecoveryOptions{Now: clock.Now}),
	}
}

func rateHeaders(remaining int, reset time.Time, extra ...string) http.Header {
	h := http.Header{}
	h.Set("X-RateLimit-Limit", "5000")
	h.Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
	h.Set("X-RateLimit-Reset", strconv.FormatInt(reset.Unix(), 10))
	for i := 0; i < len(extra); i += 2 {
		h.Set(extra[i], extra[i+1])
	}
	return h
}

// Every consumer of the installation's token draws on one budget: once a response says no
// request remains, each of them defers without calling GitHub, and each resumes after the reset.
func TestAllConsumersDeferWhileTheSharedBudgetIsExhaustedAndResumeAfterTheReset(t *testing.T) {
	t.Parallel()
	r := newRateRig(t)
	ctx := context.Background()
	reset := t0.Add(30 * time.Minute)

	// The reconcile's own listing is the call whose answer reports the budget at zero.
	r.fake.SetRateLimitHeaders(rateHeaders(0, reset))
	if _, err := r.app.Repositories(ctx, 1000); err != nil {
		t.Fatalf("Repositories = %v, want the call that reported 0 remaining to succeed", err)
	}
	if until, exhausted := r.budget.Until(1000); !exhausted || !until.Equal(reset) {
		t.Fatalf("budget = %v, %v, want exhausted until %v", until, exhausted, reset)
	}
	r.fake.SetDeliveries(githubtest.Delivery{ID: 5, GUID: "g", DeliveredAt: t0.Add(-time.Minute), StatusCode: 500, Event: "push"})

	consumers := map[string]func() error{
		"renovate run start": func() error { _, err := r.app.JobToken(ctx, 1000, "acme/api", nil); return err },
		"reconcile (token)":  func() error { _, err := r.app.RepositoryToken(ctx, 1000, "acme/api", nil); return err },
		"reconcile (sync)":   func() error { _, err := r.app.Repositories(ctx, 1000); return err },
		"reconcile (config)": func() error { _, err := r.app.HasRenovateConfig(ctx, 1000, "acme/api"); return err },
		"role check (user)":  func() error { _, _, err := r.app.UserLogin(ctx, 1000, 42); return err },
		"role check (org)": func() error {
			_, _, err := r.app.OrganizationMembership(ctx, 1000, "acme", "octo")
			return err
		},
		"role check (team)": func() error { _, _, err := r.app.TeamMembership(ctx, 1000, "acme", "devs", "octo"); return err },
		"redelivery":        func() error { _, err := r.recovery.Redeliver(ctx); return err },
	}
	before := len(r.fake.Requests())
	for name, call := range consumers {
		err := call()
		if !errors.Is(err, ratelimit.ErrRateLimited) {
			t.Errorf("%s = %v, want it deferred with ErrRateLimited", name, err)
		}
		if d, ok := ratelimit.DelayOf(err); !ok || d != 30*time.Minute {
			t.Errorf("%s delay = %v, %v, want the 30 minutes to the reset", name, d, ok)
		}
	}
	if after := len(r.fake.Requests()); after != before {
		t.Fatalf("GitHub received %d calls while the budget was exhausted, want none", after-before)
	}

	r.clock.Advance(29 * time.Minute)
	if _, err := r.app.JobToken(ctx, 1000, "acme/api", nil); !errors.Is(err, ratelimit.ErrRateLimited) {
		t.Fatalf("JobToken a minute before the reset = %v, want it still deferred", err)
	}

	r.clock.Advance(time.Minute)
	r.fake.SetRateLimitHeaders(rateHeaders(5000, reset.Add(time.Hour)))
	for name, call := range consumers {
		if err := call(); err != nil {
			t.Errorf("%s after the reset = %v, want it to resume", name, err)
		}
	}
}

// A call a consumer makes with a token it already holds is refused too, not only the request
// for a token: the reconcile pages through a repository's PRs with one token.
func TestACallWithAnInstallationTokenIsRefusedWhileTheBudgetIsExhausted(t *testing.T) {
	t.Parallel()
	r := newRateRig(t)
	ctx := context.Background()

	tok, err := r.app.RepositoryToken(ctx, 1000, "acme/api", nil)
	if err != nil {
		t.Fatal(err)
	}
	r.fake.SetRateLimitHeaders(rateHeaders(0, t0.Add(10*time.Minute)))
	if _, err := r.api.OpenPullRequestsPage(ctx, tok.Value(), "acme/api", "", ""); err != nil {
		t.Fatalf("the call that reports 0 remaining = %v", err)
	}
	before := len(r.fake.Requests())
	_, err = r.api.OpenPullRequestsPage(ctx, tok.Value(), "acme/api", "", "")
	if !errors.Is(err, ratelimit.ErrRateLimited) || len(r.fake.Requests()) != before {
		t.Fatalf("second page = %v, requests +%d, want a refusal without a call", err, len(r.fake.Requests())-before)
	}
}

func TestRateLimitedAnswersGiveTheRetryTimeFromRetryAfterAndTheResetHeaders(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		status int
		h      http.Header
		want   time.Duration
	}{
		"secondary limit, Retry-After seconds": {http.StatusForbidden, http.Header{"Retry-After": {"90"}}, 90 * time.Second},
		"secondary limit, Retry-After date":    {http.StatusTooManyRequests, http.Header{"Retry-After": {t0.Add(5 * time.Minute).Format(http.TimeFormat)}}, 5 * time.Minute},
		"primary limit, reset time":            {http.StatusForbidden, rateHeaders(0, t0.Add(12*time.Minute)), 12 * time.Minute},
		"Retry-After beats the reset":          {http.StatusForbidden, rateHeaders(0, t0.Add(40*time.Minute), "Retry-After", "20"), 20 * time.Second},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := newRateRig(t)
			ctx := context.Background()
			// A token is minted first, then every call with it is refused.
			if _, err := r.app.Repositories(ctx, 1000); err != nil {
				t.Fatal(err)
			}
			r.fake.SetRateLimitHeaders(tc.h)
			r.fake.RefuseAll(tc.status)

			_, err := r.app.Repositories(ctx, 1000)
			if !errors.Is(err, ratelimit.ErrRateLimited) {
				t.Fatalf("Repositories = %v, want ErrRateLimited", err)
			}
			var limited *ratelimit.LimitedError
			if !errors.As(err, &limited) || limited.After != tc.want || !limited.Until.Equal(t0.Add(tc.want)) {
				t.Fatalf("error = %+v, want a retry at %v (in %v)", limited, t0.Add(tc.want), tc.want)
			}
			if until, ok := r.budget.Until(1000); !ok || !until.Equal(t0.Add(tc.want)) {
				t.Errorf("budget until = %v, %v, want the installation held back until %v", until, ok, t0.Add(tc.want))
			}
		})
	}
}

// A 403 without a rate-limit marker is a refused request, not a rate limit.
func TestAPlainForbiddenIsNotARateLimit(t *testing.T) {
	t.Parallel()
	r := newRateRig(t)
	ctx := context.Background()
	if _, err := r.app.Repositories(ctx, 1000); err != nil {
		t.Fatal(err)
	}
	r.fake.SetRateLimitHeaders(rateHeaders(4000, t0.Add(time.Hour)))
	r.fake.RefuseAll(http.StatusForbidden)

	_, err := r.app.Repositories(ctx, 1000)
	if errors.Is(err, ratelimit.ErrRateLimited) || !errors.Is(err, github.ErrAppRejected) {
		t.Fatalf("Repositories = %v, want the usual refusal and no deferral", err)
	}
	if _, ok := r.budget.Until(1000); ok {
		t.Error("a plain 403 held the installation back")
	}
}

const secondaryLimitBody = `{"message":"You have exceeded a secondary rate limit. Please wait a few minutes before you try again.","documentation_url":"https://docs.github.com/rest/overview/rate-limits-for-the-rest-api#about-secondary-rate-limits"}`

// A secondary limit that answers 403 with no Retry-After and a count that is not exhausted is
// recognised by the message of its body: it is deferred for a minute and the installation's
// shared budget refuses every consumer until then.
func TestASecondaryLimitForbiddenWithoutHeadersIsBackedOffForAMinute(t *testing.T) {
	t.Parallel()

	for name, h := range map[string]http.Header{
		"no rate-limit headers":       nil,
		"requests remain":             rateHeaders(4000, t0.Add(time.Hour)),
		"uppercase message in a body": nil,
	} {
		body := secondaryLimitBody
		if name == "uppercase message in a body" {
			body = `{"message":"YOU HAVE EXCEEDED A SECONDARY RATE LIMIT."}`
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := newRateRig(t)
			ctx := context.Background()
			if _, err := r.app.Repositories(ctx, 1000); err != nil {
				t.Fatal(err)
			}
			r.fake.SetRateLimitHeaders(h)
			r.fake.RefuseAllWithBody(http.StatusForbidden, body)

			_, err := r.app.Repositories(ctx, 1000)
			var limited *ratelimit.LimitedError
			if !errors.Is(err, ratelimit.ErrRateLimited) || !errors.As(err, &limited) {
				t.Fatalf("Repositories = %v, want ErrRateLimited", err)
			}
			if limited.After != time.Minute || !limited.Until.Equal(t0.Add(time.Minute)) {
				t.Fatalf("error = %+v, want a retry in one minute", limited)
			}
			if strings.Contains(err.Error(), "secondary") || strings.Contains(err.Error(), "exceeded") {
				t.Errorf("error %q carries the response body", err)
			}
			if until, ok := r.budget.Until(1000); !ok || !until.Equal(t0.Add(time.Minute)) {
				t.Fatalf("budget until = %v, %v, want the installation held back until %v", until, ok, t0.Add(time.Minute))
			}

			r.fake.RefuseAll(0)
			before := len(r.fake.Requests())
			if _, err := r.app.JobToken(ctx, 1000, "acme/api", nil); !errors.Is(err, ratelimit.ErrRateLimited) {
				t.Fatalf("JobToken while held back = %v, want ErrRateLimited", err)
			}
			if after := len(r.fake.Requests()); after != before {
				t.Fatalf("GitHub received %d calls while the budget was held back, want none", after-before)
			}
			r.clock.Advance(time.Minute)
			if _, err := r.app.JobToken(ctx, 1000, "acme/api", nil); err != nil {
				t.Fatalf("JobToken after the minute = %v, want it to resume", err)
			}
		})
	}
}

// A 403 whose message does not name a secondary rate limit stays an ordinary refusal, whatever
// else the body says.
func TestAForbiddenWithoutTheSecondaryLimitMessageIsNotARateLimit(t *testing.T) {
	t.Parallel()

	for name, body := range map[string]string{
		"permission denied":          `{"message":"Resource not accessible by integration","documentation_url":"https://docs.github.com/rest/overview/rate-limits-for-the-rest-api#about-secondary-rate-limits"}`,
		"primary limit wording":      `{"message":"API rate limit exceeded for installation."}`,
		"not json":                   "You have exceeded a secondary rate limit.",
		"empty":                      "",
		"message is not a string":    `{"message":{"text":"secondary rate limit"}}`,
		"secondary limit in a field": `{"message":"Forbidden","errors":["secondary rate limit"]}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := newRateRig(t)
			ctx := context.Background()
			if _, err := r.app.Repositories(ctx, 1000); err != nil {
				t.Fatal(err)
			}
			r.fake.RefuseAllWithBody(http.StatusForbidden, body)

			_, err := r.app.Repositories(ctx, 1000)
			if errors.Is(err, ratelimit.ErrRateLimited) || !errors.Is(err, github.ErrAppRejected) {
				t.Fatalf("Repositories = %v, want the usual refusal and no deferral", err)
			}
			if _, ok := r.budget.Until(1000); ok {
				t.Error("a 403 that is not a secondary limit held the installation back")
			}
		})
	}
}

// The message identifies a secondary limit on a 403 only: another status with the same body is
// answered as before.
func TestTheSecondaryLimitMessageOnAnotherStatusIsNotARateLimit(t *testing.T) {
	t.Parallel()
	r := newRateRig(t)
	ctx := context.Background()
	if _, err := r.app.Repositories(ctx, 1000); err != nil {
		t.Fatal(err)
	}
	r.fake.RefuseAllWithBody(http.StatusUnauthorized, secondaryLimitBody)

	_, err := r.app.Repositories(ctx, 1000)
	if errors.Is(err, ratelimit.ErrRateLimited) {
		t.Fatalf("Repositories = %v, want no deferral for a 401", err)
	}
	if _, ok := r.budget.Until(1000); ok {
		t.Error("a 401 held the installation back")
	}
}

func TestRedeliveryStopsAtTheFirstRateLimitedRequest(t *testing.T) {
	t.Parallel()
	r := newRateRig(t)
	r.fake.SetDeliveries(
		delivery(60, "guid-c", time.Minute, 500),
		delivery(40, "guid-a", 3*time.Minute, 500),
	)
	r.fake.FailRedelivery(40, http.StatusTooManyRequests)

	got, err := r.recovery.Redeliver(context.Background())
	if !errors.Is(err, ratelimit.ErrRateLimited) || got != 0 {
		t.Fatalf("Redeliver = %d, %v, want it to stop with the rate limit", got, err)
	}
	if ids := r.fake.Redeliveries(); len(ids) != 1 || ids[0] != 40 {
		t.Errorf("redelivery attempted %v, want it to stop after the refused one", ids)
	}
}
