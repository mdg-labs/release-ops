// Package updateprs keeps the update_prs rows current (specs.html section 5.1). GitHub is the
// source of truth: a pull_request webhook of a bot PR is applied in the delivery's transaction,
// and a reconcile compares the stored open PRs of a repository with what GitHub lists, so a
// missed webhook is repaired. Every PR that is applied also has its dependencies and release
// notes recorded from the newest Renovate report or the PR body, and the superseded rule
// applied, in the same transaction. A committed change wakes the ticket engine through a
// Notifier, and the events of the applied PRs are published to it, with Tracker.Publish, once the
// transaction has committed.
package updateprs
