// Package updateprs keeps the update_prs rows current (specs.html section 5.1). GitHub is the
// source of truth: a pull_request webhook of a bot PR is applied in the delivery's transaction,
// and a reconcile compares the stored open PRs of a repository with what GitHub lists, so a
// missed webhook is repaired. A committed change wakes the ticket engine through a Notifier.
package updateprs
