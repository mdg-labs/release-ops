package store

import (
	"context"

	"github.com/mdg-labs/release-ops/internal/store/db"
)

// TicketProject is a ticket provider project/team target.
type TicketProject struct {
	ID                 string
	IntegrationID      string
	ExternalProjectID  string
	Name               string
	CreateConfig       string
	StatusMapping      string
	ContentTemplates   string
	OnOpenTicketPolicy string
	CreatedAt          string
	UpdatedAt          string
}

// CreateTicketProjectInput holds fields for a new ticket project.
type CreateTicketProjectInput struct {
	IntegrationID      string
	ExternalProjectID  string
	Name               string
	CreateConfig       string
	StatusMapping      string
	ContentTemplates   string
	OnOpenTicketPolicy string
}

// UpdateTicketProjectInput holds updatable ticket project fields.
type UpdateTicketProjectInput struct {
	Name               string
	CreateConfig       string
	StatusMapping      string
	ContentTemplates   string
	OnOpenTicketPolicy string
}

// TicketProjectRepository manages ticket_projects rows.
type TicketProjectRepository interface {
	Create(ctx context.Context, input CreateTicketProjectInput) (*TicketProject, error)
	Get(ctx context.Context, id string) (*TicketProject, error)
	List(ctx context.Context) ([]TicketProject, error)
	ListByIntegration(ctx context.Context, integrationID string) ([]TicketProject, error)
	Update(ctx context.Context, id string, input UpdateTicketProjectInput) (*TicketProject, error)
	Delete(ctx context.Context, id string) error
	CountMonitoredRepos(ctx context.Context, id string) (int64, error)
}

type ticketProjectRepo struct {
	store *Store
}

func (r ticketProjectRepo) Create(ctx context.Context, input CreateTicketProjectInput) (*TicketProject, error) {
	policy := input.OnOpenTicketPolicy
	if policy == "" {
		policy = "supersede"
	}

	now := nowUTC()
	contentTemplates := input.ContentTemplates
	if contentTemplates == "" {
		contentTemplates = defaultContentTemplatesJSON
	}

	row, err := r.store.q.CreateTicketProject(ctx, db.CreateTicketProjectParams{
		ID:                 newID(),
		IntegrationID:      input.IntegrationID,
		ExternalProjectID:  input.ExternalProjectID,
		Name:               input.Name,
		CreateConfig:       input.CreateConfig,
		StatusMapping:      input.StatusMapping,
		ContentTemplates:   contentTemplates,
		OnOpenTicketPolicy: policy,
		CreatedAt:          now,
		UpdatedAt:          now,
	})
	if err != nil {
		return nil, err
	}
	return ticketProjectFromRow(row), nil
}

func (r ticketProjectRepo) Get(ctx context.Context, id string) (*TicketProject, error) {
	row, err := r.store.q.GetTicketProject(ctx, id)
	if err != nil {
		return nil, err
	}
	return ticketProjectFromRow(row), nil
}

func (r ticketProjectRepo) List(ctx context.Context) ([]TicketProject, error) {
	rows, err := r.store.q.ListTicketProjects(ctx)
	if err != nil {
		return nil, err
	}
	return ticketProjectsFromRows(rows), nil
}

func (r ticketProjectRepo) ListByIntegration(ctx context.Context, integrationID string) ([]TicketProject, error) {
	rows, err := r.store.q.ListTicketProjectsByIntegration(ctx, integrationID)
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

	row, err := r.store.q.UpdateTicketProject(ctx, db.UpdateTicketProjectParams{
		Name:               input.Name,
		CreateConfig:       input.CreateConfig,
		StatusMapping:      input.StatusMapping,
		ContentTemplates:   contentTemplates,
		OnOpenTicketPolicy: input.OnOpenTicketPolicy,
		UpdatedAt:          nowUTC(),
		ID:                 id,
	})
	if err != nil {
		return nil, err
	}
	return ticketProjectFromRow(row), nil
}

func (r ticketProjectRepo) Delete(ctx context.Context, id string) error {
	return r.store.q.DeleteTicketProject(ctx, id)
}

func (r ticketProjectRepo) CountMonitoredRepos(ctx context.Context, id string) (int64, error) {
	return r.store.q.CountMonitoredReposByTicketProject(ctx, id)
}

const defaultContentTemplatesJSON = `{"title":"","description":"","supersedeComment":""}`

func ticketProjectFromRow(row db.TicketProject) *TicketProject {
	return &TicketProject{
		ID:                 row.ID,
		IntegrationID:      row.IntegrationID,
		ExternalProjectID:  row.ExternalProjectID,
		Name:               row.Name,
		CreateConfig:       row.CreateConfig,
		StatusMapping:      row.StatusMapping,
		ContentTemplates:   row.ContentTemplates,
		OnOpenTicketPolicy: row.OnOpenTicketPolicy,
		CreatedAt:          row.CreatedAt,
		UpdatedAt:          row.UpdatedAt,
	}
}

func ticketProjectsFromRows(rows []db.TicketProject) []TicketProject {
	out := make([]TicketProject, len(rows))
	for i, row := range rows {
		out[i] = *ticketProjectFromRow(row)
	}
	return out
}
