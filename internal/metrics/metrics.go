package metrics

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/mdg-labs/release-ops/internal/queue"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Registry is the one registry of a process. It carries the Go runtime and process
// collectors; every other metric of the process is registered on it.
type Registry struct {
	reg *prometheus.Registry
}

// NewRegistry returns a registry with the Go runtime and process collectors.
func NewRegistry() *Registry {
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	return &Registry{reg: reg}
}

// Register adds a collector. It is the registration point for the metrics other features
// define; a name registered twice is an error.
func (r *Registry) Register(c prometheus.Collector) error {
	return r.reg.Register(c)
}

// Handler serves the registry in the Prometheus text format. A collector that fails makes
// the scrape fail with a 500 rather than serve a partial answer.
func (r *Registry) Handler() http.Handler {
	return promhttp.HandlerFor(r.reg, promhttp.HandlerOpts{ErrorHandling: promhttp.HTTPErrorOnError})
}

// Outcomes of a job attempt, the values of the outcome label of release_ops_job_runs_total.
const (
	OutcomeSucceeded    = "succeeded"
	OutcomeNoConfig     = "no_config"
	OutcomeFailed       = "failed"
	OutcomeAborted      = "aborted"
	OutcomeLeaseExpired = "lease_expired"
)

// Jobs holds the job metrics of both processes: the worker counts the attempts it finishes
// and times them, the app counts the attempts that end without a result.
type Jobs struct {
	runs     *prometheus.CounterVec
	failures *prometheus.CounterVec
	duration *prometheus.HistogramVec
}

// NewJobs registers the job metrics on r.
func NewJobs(r *Registry) (*Jobs, error) {
	j := &Jobs{
		runs: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "release_ops_job_runs_total",
			Help: "Job attempts finished, by repository and outcome.",
		}, []string{"repository", "outcome"}),
		failures: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "release_ops_job_failures_total",
			Help: "Job attempts that ended failed or lease_expired, by repository.",
		}, []string{"repository"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "release_ops_job_duration_seconds",
			Help:    "Wall time of a job attempt, from the claim to the result.",
			Buckets: []float64{1, 5, 15, 30, 60, 120, 300, 600, 1200, 1800, 3600},
		}, []string{"repository"}),
	}
	for _, c := range []prometheus.Collector{j.runs, j.failures, j.duration} {
		if err := r.Register(c); err != nil {
			return nil, err
		}
	}
	return j, nil
}

// Ended counts one finished attempt of a repository. A nil *Jobs counts nothing.
func (j *Jobs) Ended(repository, outcome string) {
	if j == nil {
		return
	}
	j.runs.WithLabelValues(repository, outcome).Inc()
	if outcome == OutcomeFailed || outcome == OutcomeLeaseExpired {
		j.failures.WithLabelValues(repository).Inc()
	}
}

// Took records the wall time of an attempt of a repository. A nil *Jobs records nothing.
func (j *Jobs) Took(repository string, d time.Duration) {
	if j == nil {
		return
	}
	j.duration.WithLabelValues(repository).Observe(d.Seconds())
}

// NewWebhookLag registers release_ops_webhook_lag_seconds on r and returns it for the code
// that receives webhooks to observe (the 10-second target of specs.html § 2.9).
func NewWebhookLag(r *Registry) (prometheus.Histogram, error) {
	h := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "release_ops_webhook_lag_seconds",
		Help:    "Time from a webhook's arrival to the worker's first heartbeat for the job it started.",
		Buckets: []float64{0.5, 1, 2, 5, 10, 20, 30, 60, 120, 300},
	})
	if err := r.Register(h); err != nil {
		return nil, err
	}
	return h, nil
}

// statsTimeout bounds the queue read of one scrape.
const statsTimeout = 5 * time.Second

// queueCollector reads the queue at scrape time, so the gauges are never staler than the
// scrape.
type queueCollector struct {
	q     queue.Queue
	depth *prometheus.Desc
	age   *prometheus.Desc
}

// RegisterQueue registers release_ops_queue_depth and release_ops_queue_oldest_age_seconds
// on r, read from q at each scrape. When the read fails, the scrape fails.
func RegisterQueue(r *Registry, q queue.Queue) error {
	return r.Register(&queueCollector{
		q: q,
		depth: prometheus.NewDesc("release_ops_queue_depth",
			"Jobs queued and eligible to run (run_after passed), by priority.", []string{"priority"}, nil),
		age: prometheus.NewDesc("release_ops_queue_oldest_age_seconds",
			"Age of the oldest eligible queued job in seconds; 0 when none.", nil, nil),
	})
}

func (c *queueCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.depth
	ch <- c.age
}

func (c *queueCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), statsTimeout)
	defer cancel()
	stats, err := c.q.Stats(ctx)
	if err != nil {
		err = fmt.Errorf("queue stats unavailable: %w", err)
		ch <- prometheus.NewInvalidMetric(c.depth, err)
		ch <- prometheus.NewInvalidMetric(c.age, err)
		return
	}
	for _, p := range []queue.Priority{queue.PriorityHigh, queue.PriorityNormal} {
		ch <- prometheus.MustNewConstMetric(c.depth, prometheus.GaugeValue, float64(stats.Depth[p]), string(p))
	}
	ch <- prometheus.MustNewConstMetric(c.age, prometheus.GaugeValue, stats.OldestAge.Seconds())
}

// OpenUpdatePRCounts reads the number of open update PRs of every repository.
type OpenUpdatePRCounts interface {
	// OpenCounts returns the count by repository full name; a repository without an open PR
	// has 0.
	OpenCounts(ctx context.Context) (map[string]int, error)
}

// openPRsCollector reads the counts at scrape time, so the gauge always equals the open rows.
type openPRsCollector struct {
	counts OpenUpdatePRCounts
	desc   *prometheus.Desc
}

// RegisterOpenUpdatePRs registers release_ops_open_update_prs on r, read from counts at each
// scrape and labelled by the repository's full name. When the read fails, the scrape fails.
func RegisterOpenUpdatePRs(r *Registry, counts OpenUpdatePRCounts) error {
	return r.Register(&openPRsCollector{
		counts: counts,
		desc: prometheus.NewDesc("release_ops_open_update_prs",
			"Open update PRs (specs.html section 5.1), by repository.", []string{"repository"}, nil),
	})
}

func (c *openPRsCollector) Describe(ch chan<- *prometheus.Desc) { ch <- c.desc }

func (c *openPRsCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), statsTimeout)
	defer cancel()
	counts, err := c.counts.OpenCounts(ctx)
	if err != nil {
		ch <- prometheus.NewInvalidMetric(c.desc, fmt.Errorf("open update PR counts unavailable: %w", err))
		return
	}
	for repository, n := range counts {
		ch <- prometheus.MustNewConstMetric(c.desc, prometheus.GaugeValue, float64(n), repository)
	}
}
