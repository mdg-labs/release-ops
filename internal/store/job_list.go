package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/mdg-labs/release-ops/internal/store/db"
)

// ErrJobNotFound is returned for a job that is not stored, or that belongs to a repository
// outside the caller's scope: the two are indistinguishable.
var ErrJobNotFound = errors.New("job not found")

// JobListFilter selects and pages the jobs List returns.
type JobListFilter struct {
	// AllRepositories reads every repository (the admin scope). Otherwise only the jobs of
	// RepositoryIDs are read, and an empty list reads none.
	AllRepositories bool
	RepositoryIDs   []string
	// RepositoryID, when set, keeps the jobs of that repository; one outside the scope has none.
	RepositoryID string
	// State, when set, is queued, running, succeeded or failed.
	State string
	// Page counts from 1.
	Page, PageSize int
}

// JobListItem is a job as the Jobs page lists it (specs.html section 7.7).
type JobListItem struct {
	ID, RepositoryID, RepositoryFullName string
	Kind, State, Priority, Trigger       string
	// MatchedMarkers are the checkbox markers that caused the job; empty for other triggers.
	MatchedMarkers []string
	Attempts       int64
	// CreatedAt is a stored UTC timestamp; StartedAt and FinishedAt are empty until they happen.
	CreatedAt, StartedAt, FinishedAt string
	// StartLatency is the time from the webhook's arrival to the job's first heartbeat; nil for
	// a job no webhook caused or that has not started.
	StartLatency *time.Duration
}

// JobRunDetail is one attempt of a job. LogExcerpt is what the app stored, already redacted
// when it was written.
type JobRunDetail struct {
	Attempt                  int64
	StartedAt, FinishedAt    string
	Outcome                  string
	ExitCode                 *int64
	ErrorCode, ErrorMessage  string
	ReportStatus, LogExcerpt string
}

// JobEventDetail is one thing that happened to a job.
type JobEventDetail struct {
	At, Kind, Message string
}

// JobDetail is a job with its attempts and events.
type JobDetail struct {
	JobListItem
	Runs   []JobRunDetail
	Events []JobEventDetail
}

// JobLister reads the job history for the Jobs page.
type JobLister interface {
	// List returns one page of the jobs the filter selects, newest first, and the number it
	// selects in all, both read in one transaction.
	List(ctx context.Context, filter JobListFilter) ([]JobListItem, int, error)
	// Get returns the job with its runs and events, or ErrJobNotFound when the id is unknown or
	// its repository is outside the scope.
	Get(ctx context.Context, id string, allRepositories bool, repositoryIDs []string) (JobDetail, error)
}

// JobList returns the job history reader.
func (s *Store) JobList() JobLister {
	return jobLister{store: s}
}

type jobLister struct {
	store *Store
}

func scopeIDsJSON(ids []string) (string, error) {
	if ids == nil {
		ids = []string{}
	}
	raw, err := json.Marshal(ids)
	if err != nil {
		return "", fmt.Errorf("encode repository scope: %w", err)
	}
	return string(raw), nil
}

func boolInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

func (l jobLister) List(ctx context.Context, f JobListFilter) ([]JobListItem, int, error) {
	if !f.AllRepositories && len(f.RepositoryIDs) == 0 {
		return []JobListItem{}, 0, nil
	}
	scope, err := scopeIDsJSON(f.RepositoryIDs)
	if err != nil {
		return nil, 0, err
	}
	page, size := max(f.Page, 1), max(f.PageSize, 1)

	tx, err := l.store.db.Reader.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, 0, fmt.Errorf("begin job list read: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	q := l.store.reads.WithTx(tx)

	rows, err := q.ListScopedJobs(ctx, db.ListScopedJobsParams{
		AllRepositories: boolInt(f.AllRepositories), ScopeIds: scope, RepositoryID: f.RepositoryID, State: f.State,
		PageSize: int64(size), PageOffset: int64((page - 1) * size),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("list jobs: %w", err)
	}
	total, err := q.CountScopedJobs(ctx, db.CountScopedJobsParams{
		AllRepositories: boolInt(f.AllRepositories), ScopeIds: scope, RepositoryID: f.RepositoryID, State: f.State,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("count jobs: %w", err)
	}
	items := make([]JobListItem, 0, len(rows))
	for _, row := range rows {
		item, err := jobListItem(jobRow(row))
		if err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, int(total), nil
}

func (l jobLister) Get(ctx context.Context, id string, allRepositories bool, repositoryIDs []string) (JobDetail, error) {
	if !allRepositories && len(repositoryIDs) == 0 {
		return JobDetail{}, ErrJobNotFound
	}
	scope, err := scopeIDsJSON(repositoryIDs)
	if err != nil {
		return JobDetail{}, err
	}

	tx, err := l.store.db.Reader.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return JobDetail{}, fmt.Errorf("begin job read: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	q := l.store.reads.WithTx(tx)

	row, err := q.GetScopedJob(ctx, db.GetScopedJobParams{ID: id, AllRepositories: boolInt(allRepositories), ScopeIds: scope})
	if errors.Is(err, sql.ErrNoRows) {
		return JobDetail{}, ErrJobNotFound
	}
	if err != nil {
		return JobDetail{}, fmt.Errorf("get job: %w", err)
	}
	item, err := jobListItem(jobRow(row))
	if err != nil {
		return JobDetail{}, err
	}
	runs, err := q.ListJobRunsForJob(ctx, id)
	if err != nil {
		return JobDetail{}, fmt.Errorf("list job runs: %w", err)
	}
	events, err := q.ListJobEventsForJob(ctx, id)
	if err != nil {
		return JobDetail{}, fmt.Errorf("list job events: %w", err)
	}
	detail := JobDetail{JobListItem: item, Runs: make([]JobRunDetail, 0, len(runs)), Events: make([]JobEventDetail, 0, len(events))}
	for _, r := range runs {
		run := JobRunDetail{
			Attempt: r.Attempt, StartedAt: r.StartedAt, FinishedAt: r.FinishedAt.String, Outcome: r.Outcome,
			ErrorCode: r.ErrorCode.String, ErrorMessage: r.ErrorMessage.String,
			ReportStatus: r.ReportStatus.String, LogExcerpt: r.LogExcerpt.String,
		}
		if r.ExitCode.Valid {
			run.ExitCode = &r.ExitCode.Int64
		}
		detail.Runs = append(detail.Runs, run)
	}
	for _, e := range events {
		detail.Events = append(detail.Events, JobEventDetail{At: e.At, Kind: e.Kind, Message: e.Message.String})
	}
	return detail, nil
}

// jobRow is the shape ListScopedJobs and GetScopedJob share.
type jobRow db.ListScopedJobsRow

func jobListItem(row jobRow) (JobListItem, error) {
	item := JobListItem{
		ID: row.ID, RepositoryID: row.RepositoryID, RepositoryFullName: row.RepositoryFullName,
		Kind: row.Kind, State: row.State, Priority: row.Priority, Trigger: row.Trigger,
		MatchedMarkers: []string{}, Attempts: row.Attempts,
		CreatedAt: row.CreatedAt, StartedAt: row.StartedAt.String, FinishedAt: row.FinishedAt.String,
	}
	if row.MatchedMarkers.Valid {
		if err := json.Unmarshal([]byte(row.MatchedMarkers.String), &item.MatchedMarkers); err != nil {
			return JobListItem{}, fmt.Errorf("matched markers of job %s: %w", row.ID, err)
		}
	}
	if row.WebhookReceivedAt.Valid && row.StartedAt.Valid {
		received, err := time.Parse(timeLayout, row.WebhookReceivedAt.String)
		if err != nil {
			return JobListItem{}, fmt.Errorf("webhook arrival of job %s: %w", row.ID, err)
		}
		started, err := time.Parse(timeLayout, row.StartedAt.String)
		if err != nil {
			return JobListItem{}, fmt.Errorf("start of job %s: %w", row.ID, err)
		}
		latency := max(started.Sub(received), 0)
		item.StartLatency = &latency
	}
	return item, nil
}
