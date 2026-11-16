// Package pollwatch is the polling fallback of specs.html § 2.11: with no webhook, it reads
// from GitHub what the webhook would have delivered (a ticked checkbox in the Dependency
// Dashboard or in a bot PR, a closed bot PR, a new head of the default branch) and asks the
// queue for a normal-priority run. Every request is conditional, so a repository that did not
// change answers 304 and costs no rate-limit budget. What the watcher last saw is kept in
// memory only: a restart starts from a new baseline and the hourly safety net covers the gap.
package pollwatch
