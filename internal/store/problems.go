package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mdg-labs/release-ops/internal/store/db"
)

// ProblemFilter says whose problems ProblemReader reads (specs.html section 7.7).
type ProblemFilter struct {
	// Admin reads the installation-level inputs, the error text of ticket problems and the row
	// ids a clear action needs; anyone else gets none of them.
	Admin bool
	// AllRepositories reads every repository. Otherwise only the problems of RepositoryIDs are
	// read, and an empty list reads none.
	AllRepositories bool
	RepositoryIDs   []string
}

// JobProblem is a repository whose latest finished job failed.
type JobProblem struct {
	JobID, RepositoryID, RepositoryFullName string
	// Since is when the job finished; Message is the error of its last attempt, empty when it
	// has none.
	Since, Message string
}

// TicketProblem is an update ticket with an error or an unresolved pending marker.
type TicketProblem struct {
	UpdatePRID, UpdatePRURL, RepositoryID, RepositoryFullName string
	// Since is the time of the error, else of the marker.
	Since string
	// Message and UpdateTicketID are empty unless the filter is an admin's. UpdateTicketID is
	// set only for a row an admin may clear: a marker set and no ticket stored.
	Message, UpdateTicketID string
}

// InstallationProblemInput is the stored state the installation-level problems are derived
// from.
type InstallationProblemInput struct {
	ID, Status string
	// Permissions is the JSON object GitHub reported for the installation.
	Permissions string
	// RateLimitRemaining is nil, and RateLimitResetAt empty, while no budget was recorded.
	RateLimitRemaining *int64
	RateLimitResetAt   string
	UpdatedAt          string
	// LatestAttemptErrorCode and LatestAttemptFinishedAt describe the latest finished attempt
	// of the installation's repositories; empty when there is none.
	LatestAttemptErrorCode, LatestAttemptFinishedAt string
}

// ProblemSet is what the problems are derived from, read in one transaction.
type ProblemSet struct {
	Jobs    []JobProblem
	Tickets []TicketProblem
	// Installations is empty for a filter that is not an admin's. It holds the live
	// installation, or the newest removed one when there is none.
	Installations []InstallationProblemInput
}

// ProblemReader reads the stored state of the Problems view.
type ProblemReader interface {
	Read(ctx context.Context, filter ProblemFilter) (ProblemSet, error)
}

// Problems returns the Problems view reader.
func (s *Store) Problems() ProblemReader {
	return problemReader{store: s}
}

type problemReader struct {
	store *Store
}

func (p problemReader) Read(ctx context.Context, f ProblemFilter) (ProblemSet, error) {
	set := ProblemSet{Jobs: []JobProblem{}, Tickets: []TicketProblem{}, Installations: []InstallationProblemInput{}}
	if !f.Admin && !f.AllRepositories && len(f.RepositoryIDs) == 0 {
		return set, nil
	}
	scope, err := scopeIDsJSON(f.RepositoryIDs)
	if err != nil {
		return ProblemSet{}, err
	}
	all := boolInt(f.AllRepositories || f.Admin)

	tx, err := p.store.db.Reader.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return ProblemSet{}, fmt.Errorf("begin problems read: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	q := p.store.reads.WithTx(tx)

	jobs, err := q.ListScopedJobProblems(ctx, db.ListScopedJobProblemsParams{AllRepositories: all, ScopeIds: scope})
	if err != nil {
		return ProblemSet{}, fmt.Errorf("list job problems: %w", err)
	}
	for _, j := range jobs {
		set.Jobs = append(set.Jobs, JobProblem{
			JobID: j.JobID, RepositoryID: j.RepositoryID, RepositoryFullName: j.RepositoryFullName,
			Since: j.FinishedAt, Message: j.ErrorMessage.String,
		})
	}
	tickets, err := q.ListScopedTicketProblems(ctx, db.ListScopedTicketProblemsParams{AllRepositories: all, ScopeIds: scope})
	if err != nil {
		return ProblemSet{}, fmt.Errorf("list ticket problems: %w", err)
	}
	for _, t := range tickets {
		problem := TicketProblem{
			UpdatePRID: t.UpdatePrID, UpdatePRURL: t.UpdatePrUrl, RepositoryID: t.RepositoryID,
			RepositoryFullName: t.RepositoryFullName, Since: t.Since,
		}
		if f.Admin {
			problem.Message = t.LastError.String
			if t.Clearable == 1 {
				problem.UpdateTicketID = t.UpdateTicketID
			}
		}
		set.Tickets = append(set.Tickets, problem)
	}
	if !f.Admin {
		return set, nil
	}
	installations, err := q.ListInstallationProblemInputs(ctx)
	if err != nil {
		return ProblemSet{}, fmt.Errorf("list installation problem inputs: %w", err)
	}
	for _, i := range installations {
		if i.Status == "removed" && len(set.Installations) > 0 {
			continue
		}
		in := InstallationProblemInput{
			ID: i.ID, Status: i.Status, Permissions: i.Permissions, RateLimitResetAt: i.RateLimitResetAt.String,
			UpdatedAt: i.UpdatedAt, LatestAttemptErrorCode: i.LatestAttemptErrorCode.String,
			LatestAttemptFinishedAt: i.LatestAttemptFinishedAt.String,
		}
		if i.RateLimitRemaining.Valid {
			in.RateLimitRemaining = &i.RateLimitRemaining.Int64
		}
		set.Installations = append(set.Installations, in)
	}
	return set, nil
}
