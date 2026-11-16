package ticket_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/providers/ticket"
	"github.com/mdg-labs/release-ops/internal/ratelimit"
)

// Every provider reports the delay a rate-limit answer asked for, and a rate-limited create is
// still one the engine may retry without risking a duplicate.
func TestRateLimitAnswersCarryTheDelayTheTicketSystemAskedFor(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		status  int
		headers map[string]string
		body    string
		// low and high bound the delay; a date or a reset time is read a moment after it was made.
		low, high time.Duration
		limited   bool
	}{
		"429 with Retry-After seconds": {http.StatusTooManyRequests, map[string]string{"Retry-After": "90"}, "", 90 * time.Second, 90 * time.Second, true},
		"429 with a Retry-After date": {http.StatusTooManyRequests, map[string]string{"Retry-After": time.Now().Add(10 * time.Minute).UTC().Format(http.TimeFormat)}, "",
			10*time.Minute - 10*time.Second, 10 * time.Minute, true},
		"403 with Retry-After": {http.StatusForbidden, map[string]string{"Retry-After": "30"}, "", 30 * time.Second, 30 * time.Second, true},
		"403 with an exhausted limit": {http.StatusForbidden, map[string]string{
			"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": strconv.FormatInt(time.Now().Add(5*time.Minute).Unix(), 10)}, "",
			5*time.Minute - 10*time.Second, 5 * time.Minute, true},
		"429 without headers":                {http.StatusTooManyRequests, nil, "", time.Minute, time.Minute, true},
		"a 403 that is a permission error":   {http.StatusForbidden, nil, `{"error":"forbidden"}`, 0, 0, false},
		"a 400 that is an ordinary rejected": {http.StatusBadRequest, nil, `{"error":"bad"}`, 0, 0, false},
		"a 500 is not a rate limit":          {http.StatusInternalServerError, map[string]string{"Retry-After": "30"}, "", 0, 0, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				for k, v := range tc.headers {
					w.Header().Set(k, v)
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			t.Cleanup(server.Close)
			for kind, provider := range createProviders(t, server.URL, server.Client()) {
				_, err := provider.CreateTicket(context.Background(), createInput())
				d, limited := ticket.RateLimitDelay(err)
				if limited != tc.limited || d < tc.low || d > tc.high {
					t.Errorf("%s: RateLimitDelay = %v, %v, want %v within [%v, %v]", kind, d, limited, tc.limited, tc.low, tc.high)
				}
				if tc.limited && !ticket.CreateDefinitelyNotStored(err) {
					t.Errorf("%s: a rate-limited create must be one that stored nothing", kind)
				}
			}
		})
	}
}

func TestLinearRateLimitedDelayIsTheLongerOfRetryAfterAndTheReset(t *testing.T) {
	t.Parallel()

	resetIn := func(d time.Duration) string {
		return strconv.FormatInt(time.Now().Add(d).UnixMilli(), 10)
	}
	for name, tc := range map[string]struct {
		header    map[string]string
		low, high time.Duration
	}{
		"retry-after only":           {map[string]string{"Retry-After": "600"}, 10 * time.Minute, 10 * time.Minute},
		"reset only":                 {map[string]string{"X-RateLimit-Requests-Reset": resetIn(20 * time.Minute)}, 20*time.Minute - 10*time.Second, 20 * time.Minute},
		"retry-after beyond reset":   {map[string]string{"Retry-After": "1800", "X-RateLimit-Requests-Reset": resetIn(20 * time.Minute)}, 30 * time.Minute, 30 * time.Minute},
		"reset beyond retry-after":   {map[string]string{"Retry-After": "600", "X-RateLimit-Requests-Reset": resetIn(20 * time.Minute)}, 20*time.Minute - 10*time.Second, 20 * time.Minute},
		"neither":                    {nil, ratelimit.DefaultBlock, ratelimit.DefaultBlock},
		"retry-after beyond the cap": {map[string]string{"Retry-After": "99999999"}, ratelimit.MaxUpstreamDelay, ratelimit.MaxUpstreamDelay},
		"reset beyond the cap":       {map[string]string{"X-RateLimit-Requests-Reset": resetIn(72 * time.Hour)}, ratelimit.MaxUpstreamDelay, ratelimit.MaxUpstreamDelay},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				for k, v := range tc.header {
					w.Header().Set(k, v)
				}
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"errors":[{"message":"Rate limit exceeded","extensions":{"code":"RATELIMITED"}}]}`))
			}))
			t.Cleanup(server.Close)
			linear, err := ticket.NewLinearProviderWithEndpoint("key", server.URL, server.Client())
			if err != nil {
				t.Fatal(err)
			}

			_, err = linear.CreateTicket(context.Background(), createInput())
			d, limited := ticket.RateLimitDelay(err)
			if !limited || d < tc.low-time.Second || d > tc.high {
				t.Errorf("RateLimitDelay = %v, %v, want within [%v, %v]", d, limited, tc.low, tc.high)
			}
		})
	}
}

// Linear answers a rate-limited GraphQL request with a 400 and the RATELIMITED error code.
func TestLinearRateLimitedAnswerIsA400WithTheRateLimitedCode(t *testing.T) {
	t.Parallel()

	reset := time.Now().Add(20 * time.Minute)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-RateLimit-Requests-Reset", strconv.FormatInt(reset.UnixMilli(), 10))
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"errors":[{"message":"Rate limit exceeded","extensions":{"code":"RATELIMITED"}}]}`))
	}))
	t.Cleanup(server.Close)
	linear, err := ticket.NewLinearProviderWithEndpoint("key", server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}

	_, err = linear.CreateTicket(context.Background(), createInput())
	d, limited := ticket.RateLimitDelay(err)
	if !limited || d < 20*time.Minute-10*time.Second || d > 20*time.Minute {
		t.Fatalf("RateLimitDelay = %v, %v, want the 20 minutes to Linear's reset", d, limited)
	}
	if !ticket.CreateDefinitelyNotStored(err) {
		t.Error("a rate-limited create must be one that stored nothing")
	}

	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"errors":[{"message":"bad input","extensions":{"code":"INVALID_INPUT"}}]}`))
	}))
	t.Cleanup(plain.Close)
	other, err := ticket.NewLinearProviderWithEndpoint("key", plain.URL, plain.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, err = other.CreateTicket(context.Background(), createInput())
	if _, limited := ticket.RateLimitDelay(err); limited {
		t.Error("a 400 without the RATELIMITED code was taken for a rate limit")
	}
}
