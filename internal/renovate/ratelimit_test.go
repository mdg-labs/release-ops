package renovate_test

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/queue"
	"github.com/mdg-labs/release-ops/internal/queue/worker"
	"github.com/mdg-labs/release-ops/internal/renovate"
)

// responseLine is a Renovate error record in the JSON log, in the fields of Renovate's error
// serializer for an HTTP error: err.response holds the statusCode and headers but no URL, and
// the request's URL is under err.options.url beside it.
func responseLine(url string, status int, headers string) string {
	return fmt.Sprintf(`{"name":"renovate","level":40,"msg":"GitHub failure: rate limit","err":{"message":"Response code %d","options":{"url":"%s","method":"GET"},"response":{"statusCode":%d,"headers":{%s}}}}`, status, url, status, headers)
}

// responseWithURLLine carries the URL on the response object itself.
func responseWithURLLine(url string, status int, headers string) string {
	return fmt.Sprintf(`{"name":"renovate","level":40,"msg":"GitHub failure: rate limit","err":{"message":"Response code %d","response":{"statusCode":%d,"url":"%s","headers":{%s}}}}`, status, status, url, headers)
}

const githubAPI = "https://api.github.example/v3/repos/acme/repo-1/pulls"

func shellEcho(line string, exit int) string {
	return fmt.Sprintf("cat <<'LOG'\n%s\nLOG\nexit %d", line, exit)
}

func withClock(now time.Time) envOption {
	return func(o *renovate.Options) { o.Now = func() time.Time { return now } }
}

func TestARunStoppedByTheRateLimitIsReportedWithItsHeaders(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	reset := now.Add(10 * time.Minute)

	for name, tc := range map[string]struct {
		line      string
		exit      int
		wantAfter time.Duration
		wantReset int64
	}{
		"primary limit, reset time": {
			responseLine(githubAPI, 403, fmt.Sprintf(`"x-ratelimit-limit":"5000","x-ratelimit-remaining":"0","x-ratelimit-reset":"%d"`, reset.Unix())),
			1, 10 * time.Minute, reset.Unix(),
		},
		"secondary limit, Retry-After seconds": {
			responseLine(githubAPI, 403, `"retry-after":"120","x-ratelimit-remaining":"4000"`),
			1, 2 * time.Minute, 0,
		},
		"Retry-After as an HTTP date": {
			responseLine(githubAPI, 429, fmt.Sprintf(`"retry-after":"%s"`, now.Add(3*time.Minute).Format("Mon, 02 Jan 2006 15:04:05 GMT"))),
			1, 3 * time.Minute, 0,
		},
		"response that carries its own url": {
			responseWithURLLine(githubAPI, 403, fmt.Sprintf(`"x-ratelimit-remaining":"0","x-ratelimit-reset":"%d"`, reset.Unix())),
			1, 10 * time.Minute, reset.Unix(),
		},
		"Renovate gives up on the repository and exits cleanly": {
			responseLine(githubAPI, 403, fmt.Sprintf(`"x-ratelimit-remaining":"0","x-ratelimit-reset":"%d"`, reset.Unix())),
			0, 10 * time.Minute, reset.Unix(),
		},
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, shellEcho(tc.line, tc.exit), withClock(now))
			res := e.h.Run(context.Background(), job(0))

			wantFailure(t, res, renovate.CodeRateLimited)
			if res.RetryAfter != tc.wantAfter {
				t.Errorf("RetryAfter = %v, want %v", res.RetryAfter, tc.wantAfter)
			}
			if res.RateLimit == nil || !res.RateLimit.Limited || res.RateLimit.ResetUnix != tc.wantReset {
				t.Errorf("RateLimit = %+v, want a limited report with reset %d", res.RateLimit, tc.wantReset)
			}
			if res.Run == nil || res.Run.ErrorCode != queue.ErrorCodeRateLimited {
				t.Errorf("run = %+v, want the rate_limited error code stored with the attempt", res.Run)
			}
		})
	}
}

// Only a response from the GitHub API counts: Renovate talks to package registries too, and
// their headers must not hold back the installation.
func TestRateLimitHeadersOfAnotherHostAreIgnored(t *testing.T) {
	e := newEnv(t, shellEcho(responseLine("https://registry.npmjs.org/left-pad", 429, `"retry-after":"3600"`), 1))
	res := e.h.Run(context.Background(), job(0))
	wantFailure(t, res, renovate.CodeRenovateExit)
	if res.RateLimit != nil || res.RetryAfter != 0 {
		t.Fatalf("result = %+v, want the registry's 429 ignored", res)
	}
}

func TestRateLimitHeadersOfAnotherHostAreIgnoredWhateverShapeCarriesTheURL(t *testing.T) {
	e := newEnv(t, shellEcho(responseWithURLLine("https://registry.npmjs.org/left-pad", 429, `"retry-after":"3600"`), 1))
	res := e.h.Run(context.Background(), job(0))
	wantFailure(t, res, renovate.CodeRenovateExit)
	if res.RateLimit != nil || res.RetryAfter != 0 {
		t.Fatalf("result = %+v, want the registry's 429 ignored", res)
	}
}

func TestASuccessfulRunReportsTheBudgetItSaw(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	line := responseLine(githubAPI, 200, fmt.Sprintf(`"x-ratelimit-limit":"5000","x-ratelimit-remaining":"4321","x-ratelimit-reset":"%d"`, now.Add(40*time.Minute).Unix()))
	e := newEnv(t, shellEcho(line, 0), withClock(now))
	res := e.h.Run(context.Background(), job(0))
	if res.Err != nil {
		t.Fatalf("Err = %v", res.Err)
	}
	if rl := res.RateLimit; rl == nil || rl.Limited || rl.Remaining == nil || *rl.Remaining != 4321 || rl.Limit == nil || *rl.Limit != 5000 {
		t.Fatalf("RateLimit = %+v, want the remaining budget of the last response", rl)
	}
}

func TestOnlyTheLastRateLimitStateCounts(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	limited := responseLine(githubAPI, 403, `"retry-after":"60"`)
	recovered := responseLine(githubAPI, 200, fmt.Sprintf(`"x-ratelimit-remaining":"100","x-ratelimit-reset":"%d"`, now.Add(time.Hour).Unix()))
	e := newEnv(t, shellEcho(limited+"\n"+recovered, 1), withClock(now))
	res := e.h.Run(context.Background(), job(0))
	wantFailure(t, res, renovate.CodeRenovateExit)
	if res.RateLimit == nil || res.RateLimit.Limited {
		t.Fatalf("RateLimit = %+v, want the later response that shows requests left", res.RateLimit)
	}
}

// The app refuses to start a run while the installation's budget is exhausted: the attempt
// ends rate limited with the app's Retry-After, and Renovate never starts.
func TestARefusedTokenForTheRateLimitDefersTheRunWithoutStartingRenovate(t *testing.T) {
	e := newEnv(t, `touch "$OUT/started"`)
	e.tokens.err = &worker.RateLimitedError{RetryAfter: 7 * time.Minute}

	res := e.h.Run(context.Background(), job(0))
	wantFailure(t, res, renovate.CodeRateLimited)
	if res.RetryAfter != 7*time.Minute || res.RateLimit != nil {
		t.Errorf("result = retry %v, rate limit %+v, want the app's 7 minutes and no report of a hit that was not Renovate's", res.RetryAfter, res.RateLimit)
	}
	if e.exists("started") {
		t.Error("Renovate was started although the token was refused")
	}
	if got := strconv.Itoa(len(e.tokens.calls)); got != "1" {
		t.Errorf("token requests = %s, want 1", got)
	}
}
