package worker_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/queue/worker"
)

func TestTokenRequestRefusedForTheRateLimitCarriesTheAppsRetryAfter(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		header string
		want   time.Duration
	}{
		"seconds":          {"120", 2 * time.Minute},
		"missing":          {"", time.Minute},
		"not a number":     {"soon", time.Minute},
		"capped at a day":  {"99999999", 24 * time.Hour},
		"zero is a minute": {"0", time.Minute},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tc.header != "" {
					w.Header().Set("Retry-After", tc.header)
				}
				w.WriteHeader(http.StatusTooManyRequests)
			}))
			t.Cleanup(srv.Close)

			_, err := worker.NewClient(srv.URL, secret, nil).Token(context.Background(), "job-1", "0.owner")
			var limited *worker.RateLimitedError
			if !errors.As(err, &limited) || limited.RetryAfter != tc.want {
				t.Fatalf("Token error = %v, want a RateLimitedError asking for %v", err, tc.want)
			}
		})
	}
}
