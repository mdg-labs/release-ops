package metrics

import (
	"strconv"

	"github.com/mdg-labs/release-ops/internal/ratelimit"
	"github.com/prometheus/client_golang/prometheus"
)

// RateLimits exposes the shared GitHub rate-limit budget of each installation (specs.html
// § 2.8, § 7.8): the gauges are read from the budget at scrape time, so they are never staler
// than the last response, and the counters count the hits and the deferrals the budget reports.
// The label installation is the GitHub installation id: a number, never a token or a login.
type RateLimits struct {
	hits     *prometheus.CounterVec
	deferred *prometheus.CounterVec

	budget    *ratelimit.Budget
	remaining *prometheus.Desc
	limit     *prometheus.Desc
	reset     *prometheus.Desc
}

var _ ratelimit.Recorder = (*RateLimits)(nil)

// RegisterRateLimits registers the rate-limit metrics on r, reading the gauges from b, and makes
// b count its hits and deferrals on them.
func RegisterRateLimits(r *Registry, b *ratelimit.Budget) (*RateLimits, error) {
	rl := &RateLimits{
		budget: b,
		hits: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "release_ops_github_rate_limit_hits_total",
			Help: "Answers of GitHub that refused a call because of a rate limit, by installation.",
		}, []string{"installation"}),
		deferred: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "release_ops_github_rate_limit_deferred_total",
			Help: "Work held back or refused because the installation's rate-limit budget was used up, by installation and consumer.",
		}, []string{"installation", "consumer"}),
		remaining: prometheus.NewDesc("release_ops_github_rate_limit_remaining",
			"Requests remaining of the installation's shared GitHub budget, as last seen in a response.", []string{"installation"}, nil),
		limit: prometheus.NewDesc("release_ops_github_rate_limit_limit",
			"Size of the installation's shared GitHub budget, as last seen in a response.", []string{"installation"}, nil),
		reset: prometheus.NewDesc("release_ops_github_rate_limit_reset_timestamp_seconds",
			"Unix time at which the installation's shared GitHub budget resets, as last seen in a response.", []string{"installation"}, nil),
	}
	for _, c := range []prometheus.Collector{rl.hits, rl.deferred, rl} {
		if err := r.Register(c); err != nil {
			return nil, err
		}
	}
	b.SetRecorder(rl)
	return rl, nil
}

// Hit implements ratelimit.Recorder.
func (rl *RateLimits) Hit(installationID int64) {
	rl.hits.WithLabelValues(installationLabel(installationID)).Inc()
}

// Deferred implements ratelimit.Recorder.
func (rl *RateLimits) Deferred(installationID int64, consumer string) {
	rl.deferred.WithLabelValues(installationLabel(installationID), consumer).Inc()
}

func installationLabel(id int64) string { return strconv.FormatInt(id, 10) }

// Describe implements prometheus.Collector for the gauges.
func (rl *RateLimits) Describe(ch chan<- *prometheus.Desc) {
	ch <- rl.remaining
	ch <- rl.limit
	ch <- rl.reset
}

// Collect implements prometheus.Collector for the gauges: one series of each per installation
// the budget has seen; the limit is left out until a response named it.
func (rl *RateLimits) Collect(ch chan<- prometheus.Metric) {
	for _, s := range rl.budget.Snapshot() {
		id := installationLabel(s.InstallationID)
		ch <- prometheus.MustNewConstMetric(rl.remaining, prometheus.GaugeValue, float64(s.Remaining), id)
		if s.Limit > 0 {
			ch <- prometheus.MustNewConstMetric(rl.limit, prometheus.GaugeValue, float64(s.Limit), id)
		}
		if !s.ResetAt.IsZero() {
			ch <- prometheus.MustNewConstMetric(rl.reset, prometheus.GaugeValue, float64(s.ResetAt.Unix()), id)
		}
	}
}
