// Package metrics is the Prometheus instrumentation of the app and the worker (specs.html
// § 7.8): one registry per process, the metric definitions the processes share and the
// listener on METRICS_ADDR that serves it. The listener serves /metrics and nothing else,
// and it is never reachable through the /api/go proxy.
//
// Label values are bounded: a repository full name, a priority or an outcome. A delivery,
// user or pull request id is never a label, and no secret is ever a metric or a label.
package metrics
