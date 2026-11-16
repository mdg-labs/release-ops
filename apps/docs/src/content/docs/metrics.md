---
title: "Metrics and alert rules"
description: "Scrape the app and the worker with Prometheus or VictoriaMetrics, look up every Release Ops metric and copy example alert rules."
---

Release Ops sends no email, chat or webhook messages of its own. The ticket systems notify people about tickets, and GitHub notifies them about PRs. To be told when Release Ops itself has a problem, scrape its metrics and alert on them. This page shows how, lists every metric and gives example alert rules.

## Scrape the app and the worker

The app and the worker each serve their own `/metrics` endpoint in the Prometheus text format. The endpoint is on a separate listener that serves nothing else. It is not part of the web UI, and the web UI never proxies it.

You choose the listener's address with `METRICS_ADDR`. The default is `127.0.0.1:9100`, which only the container itself can reach. Set it in your `.env` file, and the Compose file passes the same value to both containers. To let a scraper on the Compose network reach them, bind beyond loopback:

```bash
METRICS_ADDR=:9100
```

Then restart both services. A scraper that runs in the same Compose network reaches the app at `app:9100` and the worker at `worker:9100`:

```yaml
scrape_configs:
  - job_name: release-ops-app
    static_configs:
      - targets: ["app:9100"]
  - job_name: release-ops-worker
    static_configs:
      - targets: ["worker:9100"]
```

The endpoint has no authentication. The protection is the bind address and the fact that you do not publish the port. Do not add a `ports` entry for it. If you open the listener beyond the Compose network, put your own authentication in front of it.

No secret, token or path to one appears in the output. Label values are bounded: a repository's full name, a priority, an outcome, the GitHub installation ID (a number, never the account name) and a fixed consumer name. A label never holds a delivery, a user or a PR ID.

Besides the Release Ops metrics below, both processes serve the standard Go runtime and process metrics (`go_*` and `process_*`).

## Which process serves what

The app and the worker each serve their own set. Scrape both, and add up the job metrics across the two.

| Process | Metrics |
| ------- | ------- |
| Worker | Job runs, job failures, job duration |
| App | Queue depth and age, webhook lag, open update PRs, GitHub rate limit, and job runs and failures for attempts that end without a result (`lease_expired`) |

## Reference

| Metric | Type and labels | Process | Meaning |
| ------ | --------------- | ------- | ------- |
| `release_ops_job_runs_total` | Counter: `repository`, `outcome` | Worker, and the app for `lease_expired` | Job attempts that finished, by outcome. |
| `release_ops_job_failures_total` | Counter: `repository` | Worker, and the app for `lease_expired` | Attempts that ended `failed` or `lease_expired`. |
| `release_ops_job_duration_seconds` | Histogram: `repository` | Worker | Wall time of an attempt, from the claim to the result. Buckets run from 1 second to 1 hour. |
| `release_ops_queue_depth` | Gauge: `priority` | App | Jobs that are queued and ready to run, by priority (`high` and `normal`). A job waiting for its retry time is not counted. |
| `release_ops_queue_oldest_age_seconds` | Gauge | App | Age of the oldest queued job that is ready to run. 0 when there is none. |
| `release_ops_webhook_lag_seconds` | Histogram | App | Time from a webhook's arrival to the worker's first heartbeat for the job it started. Release Ops aims for 10 seconds. Buckets run from 0.5 seconds to 5 minutes. |
| `release_ops_open_update_prs` | Gauge: `repository` | App | Open update PRs of each repository. |
| `release_ops_github_rate_limit_remaining` | Gauge: `installation` | App | Requests left in the installation's shared GitHub budget, as last seen in a GitHub response. |
| `release_ops_github_rate_limit_limit` | Gauge: `installation` | App | Size of that budget, as last seen in a response. |
| `release_ops_github_rate_limit_reset_timestamp_seconds` | Gauge: `installation` | App | Unix time at which that budget resets, as last seen in a response. Never more than 24 hours ahead. |
| `release_ops_github_rate_limit_hits_total` | Counter: `installation` | App | Answers from GitHub that refused a call because of a rate limit. |
| `release_ops_github_rate_limit_deferred_total` | Counter: `installation`, `consumer` | App | Work that was held back or refused because the budget was used up, by consumer. |

The `outcome` label of the job metrics has these values:

| Outcome | Meaning |
| ------- | ------- |
| `succeeded` | Renovate finished. |
| `failed` | The attempt failed. It is retried while attempts remain. |
| `aborted` | The worker did not report a result: it lost its lease, or it gave the job back while shutting down. |
| `lease_expired` | The worker stopped reporting and the app took the job back. |

The `consumer` label of `release_ops_github_rate_limit_deferred_total` has these values:

| Consumer | The work that was held back |
| -------- | --------------------------- |
| `renovate_run` | A Renovate run. |
| `reconcile` | The app reading repositories and PRs from GitHub. |
| `redelivery` | Redelivery of failed webhook deliveries. |
| `role_check` | A check of someone's role. |
| `pr_comment` | The comment with the ticket link on a PR. |

All of one installation's work shares a single GitHub rate-limit budget. Renovate runs, the reconcile, redelivery, role checks and PR comments all draw on it. While it is used up, no job is claimed before it resets.

### What to expect from the series

- A labelled counter has no series until its first event. A repository without failures has no `release_ops_job_failures_total` series.
- The rate-limit gauges have one series per installation. They appear after the first GitHub response. The limit series appears once a response has named it, and the reset series only while a reset time is known.
- The queue gauges and the open update PR gauge are read when you scrape. If that read fails, the scrape fails instead of returning partial data.
- Without a webhook URL, GitHub sends no webhooks and `release_ops_webhook_lag_seconds` records nothing. See [polling mode](../getting-started/#without-a-public-url-polling-mode).

## Example alert rules

These rules use the Prometheus rule format, which vmalert reads as well. Load them with `rule_files` in Prometheus or with the `-rule` flag of vmalert. They use only metrics from the table above. Adjust the thresholds to your installation.

```yaml
groups:
  - name: release-ops
    rules:
      - alert: ReleaseOpsJobFailures
        expr: sum by (repository) (increase(release_ops_job_failures_total[1h])) >= 3
        labels:
          severity: warning
        annotations:
          summary: "Renovate runs for {{ $labels.repository }} keep failing"
          description: "{{ $value }} job attempts for {{ $labels.repository }} failed or lost their lease in the last hour."

      - alert: ReleaseOpsQueueOld
        expr: release_ops_queue_oldest_age_seconds > 900
        for: 10m
        labels:
          severity: warning
        annotations:
          summary: "A Release Ops job has waited more than 15 minutes"
          description: "The oldest job that is ready to run has been queued for {{ $value | humanizeDuration }}. Check the worker, and the concurrency cap on the Settings page."

      - alert: ReleaseOpsWebhookLagHigh
        expr: histogram_quantile(0.95, sum by (le) (rate(release_ops_webhook_lag_seconds_bucket[30m]))) > 10
        for: 15m
        labels:
          severity: warning
        annotations:
          summary: "Runs start more than 10 seconds after their webhook"
          description: "The 95th percentile from webhook to job start is {{ $value | humanizeDuration }}."

      - alert: ReleaseOpsRateLimitLow
        expr: release_ops_github_rate_limit_remaining / release_ops_github_rate_limit_limit < 0.1
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Installation {{ $labels.installation }} has used up most of its GitHub rate limit"
          description: "{{ $value | humanizePercentage }} of the installation's GitHub budget is left."

      - alert: ReleaseOpsRateLimitHit
        expr: sum by (installation) (increase(release_ops_github_rate_limit_hits_total[15m])) > 0
        labels:
          severity: critical
        annotations:
          summary: "GitHub refused calls for installation {{ $labels.installation }} because of a rate limit"
          description: "Work for this installation is held back until the budget resets. The consumers that were held back appear in release_ops_github_rate_limit_deferred_total."
```

Notes on these rules:

- Prometheus does not count the first increment of a series that did not exist before. The first failure of a repository that never failed can therefore pass unnoticed by `ReleaseOpsJobFailures`, which looks for three in an hour.
- `ReleaseOpsQueueOld` also fires while a rate limit holds jobs back, because a job that waits for the budget is still ready to run. Read it together with the rate-limit rules.
- To see which kind of work a rate limit holds back, graph `sum by (consumer) (increase(release_ops_github_rate_limit_deferred_total[1h]))`.

## Related pages

- [How runs are triggered](../concepts/how-runs-are-triggered/)
- [Getting started](../getting-started/#operating-your-instance) for the instance's network access
