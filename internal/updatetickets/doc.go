// Package updatetickets creates the ticket of an update PR (specs.html section 5.2). The engine
// decides from the stored PR, its dependencies and its ticket project whether the PR gets a
// ticket, and creates it with the at-most-once sequence of section 5.2.2: the pending marker is
// written before the tracker is called, a failure that proves nothing was stored clears it, and
// the write that stores the link clears it with the link. It runs on the events the PR tracker
// publishes once a transaction has committed, and on a periodic pass over the PRs that still
// lack a ticket.
package updatetickets
