package ratelimit_test

import (
	"errors"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/ratelimit"
)

var now = time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)

func header(kv ...string) http.Header {
	h := http.Header{}
	for i := 0; i < len(kv); i += 2 {
		h.Set(kv[i], kv[i+1])
	}
	return h
}

func TestParseRetryAfterAsSecondsAndAsHTTPDate(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		status int
		h      http.Header
		want   time.Duration
	}{
		"seconds":     {http.StatusTooManyRequests, header("Retry-After", "120"), 2 * time.Minute},
		"http date":   {http.StatusForbidden, header("Retry-After", now.Add(90*time.Second).Format(http.TimeFormat)), 90 * time.Second},
		"past date":   {http.StatusTooManyRequests, header("Retry-After", now.Add(-time.Hour).Format(http.TimeFormat)), 0},
		"garbage":     {http.StatusTooManyRequests, header("Retry-After", "soon"), 0},
		"negative":    {http.StatusTooManyRequests, header("Retry-After", "-5"), 0},
		"huge":        {http.StatusTooManyRequests, header("Retry-After", "99999999"), ratelimit.MaxUpstreamDelay},
		"overflowing": {http.StatusTooManyRequests, header("Retry-After", "100000000000000000"), ratelimit.MaxUpstreamDelay},
		"int64 max":   {http.StatusTooManyRequests, header("Retry-After", "9223372036854775807"), ratelimit.MaxUpstreamDelay},
		"at the cap":  {http.StatusTooManyRequests, header("Retry-After", "86400"), ratelimit.MaxUpstreamDelay},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := ratelimit.Parse(tc.status, tc.h, now)
			if s.RetryAfter != tc.want {
				t.Errorf("RetryAfter = %v, want %v", s.RetryAfter, tc.want)
			}
			if !s.Limited {
				t.Errorf("a %d is limited", tc.status)
			}
		})
	}
}

func TestParseClassifiesPrimaryAndSecondaryLimits(t *testing.T) {
	t.Parallel()

	reset := now.Add(25 * time.Minute)
	primary := ratelimit.Parse(http.StatusForbidden, header(
		"X-RateLimit-Remaining", "0", "X-RateLimit-Limit", "5000", "X-RateLimit-Reset", strconv.FormatInt(reset.Unix(), 10)), now)
	if !primary.Limited || !primary.HasRemaining || primary.Remaining != 0 || primary.Limit != 5000 || !primary.ResetAt.Equal(reset) {
		t.Fatalf("primary = %+v", primary)
	}
	if got := primary.Until(now); !got.Equal(reset) {
		t.Errorf("primary Until = %v, want the reset %v", got, reset)
	}

	secondary := ratelimit.Parse(http.StatusForbidden, header("Retry-After", "60", "X-RateLimit-Remaining", "4000"), now)
	if !secondary.Limited || !secondary.Until(now).Equal(now.Add(time.Minute)) {
		t.Errorf("secondary = %+v until %v, want limited for the Retry-After", secondary, secondary.Until(now))
	}

	if s := ratelimit.Parse(http.StatusForbidden, header("X-RateLimit-Remaining", "12"), now); s.Limited {
		t.Errorf("a 403 with requests left is a permission error, not a rate limit: %+v", s)
	}
	if s := ratelimit.Parse(http.StatusOK, header("X-RateLimit-Remaining", "0"), now); s.Limited {
		t.Errorf("a 200 is never limited: %+v", s)
	}
	bare := ratelimit.Parse(http.StatusTooManyRequests, nil, now)
	if !bare.Limited || !bare.Until(now).Equal(now.Add(ratelimit.DefaultBlock)) {
		t.Errorf("a bare 429 = %+v until %v, want a hold of DefaultBlock", bare, bare.Until(now))
	}
	if !ratelimit.Parse(http.StatusOK, nil, now).Empty() {
		t.Error("a response without rate-limit headers is empty")
	}
}

func TestRetryAfterWinsOverTheResetTime(t *testing.T) {
	t.Parallel()

	s := ratelimit.Parse(http.StatusForbidden, header(
		"Retry-After", "30", "X-RateLimit-Remaining", "0", "X-RateLimit-Reset", strconv.FormatInt(now.Add(time.Hour).Unix(), 10)), now)
	if got := s.Until(now); !got.Equal(now.Add(30 * time.Second)) {
		t.Errorf("Until = %v, want Retry-After first", got)
	}
}

func TestLimitedErrorMatchesErrRateLimitedAndCarriesTheDelay(t *testing.T) {
	t.Parallel()

	err := error(ratelimit.NewLimitedError(now.Add(3*time.Minute), now))
	wrapped := errors.Join(errors.New("context"), err)
	if !errors.Is(wrapped, ratelimit.ErrRateLimited) {
		t.Error("errors.Is(ErrRateLimited) = false")
	}
	if d, ok := ratelimit.DelayOf(wrapped); !ok || d != 3*time.Minute {
		t.Errorf("DelayOf = %v, %v, want 3m", d, ok)
	}
	if _, ok := ratelimit.DelayOf(errors.New("other")); ok {
		t.Error("DelayOf(other) = true")
	}
}

func TestJitteredNeverUndercuts(t *testing.T) {
	t.Parallel()

	for _, rnd := range []float64{0, 0.25, 0.999} {
		if got := ratelimit.Jittered(time.Minute, 0.2, rnd); got < time.Minute || got > 72*time.Second {
			t.Errorf("Jittered(1m, 0.2, %v) = %v, want within [1m, 1m12s]", rnd, got)
		}
	}
}

// A reset time far in the future is cut to the cap when it is read, even where the value would
// overflow a time.
func TestParseCapsTheResetTime(t *testing.T) {
	t.Parallel()

	for name, reset := range map[string]string{
		"far future": "99999999999",
		"five digit": "999999999999",
		"1<<62":      "4611686018427387904",
		"max int64":  "9223372036854775807",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := ratelimit.Parse(http.StatusForbidden, header("X-RateLimit-Remaining", "0", "X-RateLimit-Reset", reset), now)
			if want := now.Add(ratelimit.MaxUpstreamDelay); !s.ResetAt.Equal(want) {
				t.Errorf("ResetAt = %v, want it cut to %v", s.ResetAt, want)
			}
		})
	}
}
