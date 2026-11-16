package metrics

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/ratelimit"
)

func rateSignal(now time.Time, status, remaining int, reset time.Time) ratelimit.Signal {
	h := http.Header{}
	h.Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
	h.Set("X-RateLimit-Limit", "5000")
	h.Set("X-RateLimit-Reset", strconv.FormatInt(reset.Unix(), 10))
	return ratelimit.Parse(status, h, now)
}

// Each installation has its own series; the gauges follow the responses and the counters count
// the hits and the deferrals, by installation and consumer.
func TestRateLimitMetricsHaveOneSeriesPerInstallation(t *testing.T) {
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	reg := NewRegistry()
	budget := ratelimit.NewBudget(ratelimit.Options{Now: func() time.Time { return now }})
	if _, err := RegisterRateLimits(reg, budget); err != nil {
		t.Fatal(err)
	}
	resetA, resetB := now.Add(10*time.Minute), now.Add(40*time.Minute)

	budget.Observe(1001, rateSignal(now, http.StatusOK, 4200, resetA))
	budget.Observe(2002, rateSignal(now, http.StatusForbidden, 0, resetB))
	_ = budget.Gate(2002, ratelimit.ConsumerReconcile)
	_ = budget.Gate(2002, ratelimit.ConsumerRoleCheck)
	budget.Deferred(2002, ratelimit.ConsumerRenovateRun)

	code, body := scrape(t, reg)
	if code != http.StatusOK {
		t.Fatalf("scrape = %d: %s", code, body)
	}
	for _, want := range []string{
		`release_ops_github_rate_limit_remaining{installation="1001"} 4200`,
		`release_ops_github_rate_limit_remaining{installation="2002"} 0`,
		`release_ops_github_rate_limit_limit{installation="1001"} 5000`,
		`release_ops_github_rate_limit_limit{installation="2002"} 5000`,
		`release_ops_github_rate_limit_reset_timestamp_seconds{installation="1001"} ` + sample(resetA.Unix()),
		`release_ops_github_rate_limit_reset_timestamp_seconds{installation="2002"} ` + sample(resetB.Unix()),
		`release_ops_github_rate_limit_hits_total{installation="2002"} 1`,
		`release_ops_github_rate_limit_deferred_total{consumer="reconcile",installation="2002"} 1`,
		`release_ops_github_rate_limit_deferred_total{consumer="role_check",installation="2002"} 1`,
		`release_ops_github_rate_limit_deferred_total{consumer="renovate_run",installation="2002"} 1`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("scrape lacks %q:\n%s", want, grep(body, "rate_limit"))
		}
	}
	if strings.Contains(body, `hits_total{installation="1001"}`) {
		t.Error("installation 1001 has a hit counter series although it was never limited")
	}

	// A new window updates the same series.
	budget.Observe(1001, rateSignal(now, http.StatusOK, 4999, resetA.Add(time.Hour)))
	_, body = scrape(t, reg)
	if !strings.Contains(body, `release_ops_github_rate_limit_remaining{installation="1001"} 4999`) {
		t.Errorf("the gauge did not follow the next response:\n%s", grep(body, "rate_limit_remaining"))
	}
}

func TestRateLimitMetricsCarryNoSecretLabels(t *testing.T) {
	reg := NewRegistry()
	budget := ratelimit.NewBudget(ratelimit.Options{})
	if _, err := RegisterRateLimits(reg, budget); err != nil {
		t.Fatal(err)
	}
	budget.Bind("ghs_secretTokenValue", 7, time.Now().Add(time.Hour))
	budget.ObserveToken("ghs_secretTokenValue", rateSignal(time.Now(), http.StatusOK, 1, time.Now().Add(time.Hour)))
	_, body := scrape(t, reg)
	if strings.Contains(body, "ghs_") {
		t.Fatal("a token reached /metrics")
	}
	if !strings.Contains(body, `installation="7"`) {
		t.Errorf("the token's installation is not labelled by its id:\n%s", grep(body, "rate_limit"))
	}
}

// sample is a value as the text format writes a float.
func sample(v int64) string { return strconv.FormatFloat(float64(v), 'g', -1, 64) }

func grep(body, sub string) string {
	var out []string
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, sub) {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}
