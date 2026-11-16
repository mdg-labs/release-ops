package store

import (
	"context"
	"fmt"

	"github.com/mdg-labs/release-ops/internal/store/db"
)

// TicketProject is a ticket provider project/team target.
type TicketProject struct {
	ID                string
	IntegrationID     string
	ExternalProjectID string
	Name              string
	CreateConfig      string
	StatusMapping     string
	ContentTemplates  string
	// Filter is the JSON of which update PRs get a ticket (specs.html section 5.2).
	Filter    string
	CreatedAt string
	UpdatedAt string
}

// CreateTicketProjectInput holds fields for a new ticket project.
type CreateTicketProjectInput struct {
	IntegrationID     string
	ExternalProjectID string
	Name              string
	CreateConfig      string
	StatusMapping     string
	ContentTemplates  string
	// Filter is stored as given; empty stores the default, which lets every non-automerged PR
	// through.
	Filter string
}

// UpdateTicketProjectInput holds updatable ticket project fields.
type UpdateTicketProjectInput struct {
	Name             string
	CreateConfig     string
	StatusMapping    string
	ContentTemplates string
	// Filter is stored as given; empty stores the default.
	Filter string
}

// TicketProjectRepository manages ticket_projects rows.
type TicketProjectRepository interface {
	Create(ctx context.Context, input CreateTicketProjectInput) (*TicketProject, error)
	Get(ctx context.Context, id string) (*TicketProject, error)
	List(ctx context.Context) ([]TicketProject, error)
	ListByIntegration(ctx context.Context, integrationID string) ([]TicketProject, error)
	Update(ctx context.Context, id string, input UpdateTicketProjectInput) (*TicketProject, error)
	Delete(ctx context.Context, id string) error
}

type ticketProjectRepo struct {
	store *Store
}

func (r ticketProjectRepo) Create(ctx context.Context, input CreateTicketProjectInput) (*TicketProject, error) {
	now := nowUTC()
	contentTemplates := input.ContentTemplates
	if contentTemplates == "" {
		contentTemplates = defaultContentTemplatesJSON
	}

	row, err := r.store.writes.CreateTicketProject(ctx, db.CreateTicketProjectParams{
		ID:                newID(),
		IntegrationID:     input.IntegrationID,
		ExternalProjectID: input.ExternalProjectID,
		Name:              input.Name,
		CreateConfig:      input.CreateConfig,
		StatusMapping:     input.StatusMapping,
		ContentTemplates:  contentTemplates,
		CreatedAt:         now,
		UpdatedAt:         now,
		Filter:            orDefaultFilter(input.Filter),
	})
	if err != nil {
		return nil, err
	}
	return ticketProjectFromRow(row), nil
}

func (r ticketProjectRepo) Get(ctx context.Context, id string) (*TicketProject, error) {
	row, err := r.store.reads.GetTicketProject(ctx, id)
	if err != nil {
		return nil, err
	}
	return ticketProjectFromRow(row), nil
}

func (r ticketProjectRepo) List(ctx context.Context) ([]TicketProject, error) {
	rows, err := r.store.reads.ListTicketProjects(ctx)
	if err != nil {
		return nil, err
	}
	return ticketProjectsFromRows(rows), nil
}

func (r ticketProjectRepo) ListByIntegration(ctx context.Context, integrationID string) ([]TicketProject, error) {
	rows, err := r.store.reads.ListTicketProjectsByIntegration(ctx, integrationID)
	if err != nil {
		return nil, err
	}
	return ticketProjectsFromRows(rows), nil
}

func (r ticketProjectRepo) Update(ctx context.Context, id string, input UpdateTicketProjectInput) (*TicketProject, error) {
	contentTemplates := input.ContentTemplates
	if contentTemplates == "" {
		contentTemplates = defaultContentTemplatesJSON
	}

	row, err := r.store.writes.UpdateTicketProject(ctx, db.UpdateTicketProjectParams{
		Name:             input.Name,
		CreateConfig:     input.CreateConfig,
		StatusMapping:    input.StatusMapping,
		ContentTemplates: contentTemplates,
		Filter:           orDefaultFilter(input.Filter),
		UpdatedAt:        nowUTC(),
		ID:               id,
	})
	if err != nil {
		return nil, err
	}
	return ticketProjectFromRow(row), nil
}

// Delete removes a ticket project. It returns ErrInUse, with nothing changed, when a repository
// override, an installation default or an update ticket still references it. The foreign keys
// of the first two clear the reference instead of refusing, so the check runs in the same
// transaction as the delete.
func (r ticketProjectRepo) Delete(ctx context.Context, id string) error {
	tx, err := r.store.db.Writer.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin ticket project delete: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	q := r.store.writes.WithTx(tx)

	references, err := q.CountTicketProjectDefaultAndOverrideReferences(ctx, id)
	if err != nil {
		return fmt.Errorf("check ticket project references: %w", err)
	}
	if references > 0 {
		return ErrInUse
	}
	if err := q.DeleteTicketProject(ctx, id); err != nil {
		return mapForeignKeyError(err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit ticket project delete: %w", err)
	}
	return nil
}

const defaultContentTemplatesJSON = `{"title":"","description":"","updateComment":"","supersedeComment":""}`

// defaultFilterJSON lets every non-automerged update PR through (specs.html section 4.3).
const defaultFilterJSON = `{"updateTypes":[],"labels":[],"skipAutomerged":true}`

func orDefaultFilter(filter string) string {
	if filter == "" {
		return defaultFilterJSON
	}
	return filter
}

func ticketProjectFromRow(row db.TicketProject) *TicketProject {
	return &TicketProject{
		ID:                row.ID,
		IntegrationID:     row.IntegrationID,
		ExternalProjectID: row.ExternalProjectID,
		Name:              row.Name,
		CreateConfig:      row.CreateConfig,
		StatusMapping:     row.StatusMapping,
		ContentTemplates:  row.ContentTemplates,
		Filter:            row.Filter,
		CreatedAt:         row.CreatedAt,
		UpdatedAt:         row.UpdatedAt,
	}
}

func ticketProjectsFromRows(rows []db.TicketProject) []TicketProject {
	out := make([]TicketProject, len(rows))
	for i, row := range rows {
		out[i] = *ticketProjectFromRow(row)
	}
	return out
}
