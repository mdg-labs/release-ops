// Package updatetickets creates the ticket of an update PR and keeps it in step with the PR
// (specs.html section 5.2). The engine decides from the stored PR, its dependencies and its
// ticket project whether the PR gets a ticket, and creates it with the at-most-once sequence of
// section 5.2.2: the pending marker is written before the tracker is called, a failure that
// proves nothing was stored clears it, and the write that stores the link clears it with the
// link. Afterwards the ticket follows its PR (section 5.2.1): a new version updates it and adds a
// note, a merge moves it to done, a close to won't do, a replacement supersedes it, and a ticket
// someone changed by hand is left alone. The engine runs on the events the PR tracker publishes
// once a transaction has committed, and on a periodic pass over the PRs that still lack a ticket
// and the tickets that may have an operation to run.
package updatetickets
