package store

import (
	"context"
	"database/sql"

	"github.com/mdg-labs/release-ops/internal/store/db"
)

// Integration is an integration record safe for API exposure (no decrypted secrets).
type Integration struct {
	ID        string
	Kind      string
	Name      string
	BaseURL   *string
	HasSecret bool
	CreatedAt string
	UpdatedAt string
}

// CreateIntegrationInput holds plaintext credential payload before encryption.
type CreateIntegrationInput struct {
	Kind    string
	Name    string
	BaseURL *string
	Secret  []byte
}

// UpdateIntegrationInput updates mutable integration fields.
type UpdateIntegrationInput struct {
	Name    string
	BaseURL *string
	Secret  []byte // nil = leave existing encrypted payload unchanged
}

// IntegrationRepository manages integration credentials with encrypt-on-write.
type IntegrationRepository interface {
	Create(ctx context.Context, input CreateIntegrationInput) (*Integration, error)
	Get(ctx context.Context, id string) (*Integration, error)
	List(ctx context.Context) ([]Integration, error)
	ListByKind(ctx context.Context, kind string) ([]Integration, error)
	Update(ctx context.Context, id string, input UpdateIntegrationInput) (*Integration, error)
	Delete(ctx context.Context, id string) error
	CountReferences(ctx context.Context, id string) (int64, error)
	DecryptPayload(ctx context.Context, id string) ([]byte, error)
}

type integrationRepo struct {
	store *Store
}

func (r integrationRepo) Create(ctx context.Context, input CreateIntegrationInput) (*Integration, error) {
	encrypted, err := r.store.cipher.Encrypt(input.Secret)
	if err != nil {
		return nil, err
	}

	now := nowUTC()
	row, err := r.store.q.CreateIntegration(ctx, db.CreateIntegrationParams{
		ID:               newID(),
		Kind:             input.Kind,
		Name:             input.Name,
		BaseUrl:          stringPtrToNull(input.BaseURL),
		EncryptedPayload: encrypted,
		CreatedAt:        now,
		UpdatedAt:        now,
	})
	if err != nil {
		return nil, err
	}
	return integrationFromRow(row), nil
}

func (r integrationRepo) Get(ctx context.Context, id string) (*Integration, error) {
	row, err := r.store.q.GetIntegration(ctx, id)
	if err != nil {
		return nil, err
	}
	return integrationFromRow(row), nil
}

func (r integrationRepo) List(ctx context.Context) ([]Integration, error) {
	rows, err := r.store.q.ListIntegrations(ctx)
	if err != nil {
		return nil, err
	}
	return integrationsFromRows(rows), nil
}

func (r integrationRepo) ListByKind(ctx context.Context, kind string) ([]Integration, error) {
	rows, err := r.store.q.ListByKind(ctx, kind)
	if err != nil {
		return nil, err
	}
	return integrationsFromRows(rows), nil
}

func (r integrationRepo) Update(ctx context.Context, id string, input UpdateIntegrationInput) (*Integration, error) {
	existing, err := r.store.q.GetIntegration(ctx, id)
	if err != nil {
		return nil, err
	}

	now := nowUTC()
	var row db.Integration
	if input.Secret != nil {
		encrypted, encErr := r.store.cipher.Encrypt(input.Secret)
		if encErr != nil {
			return nil, encErr
		}
		row, err = r.store.q.UpdateIntegration(ctx, db.UpdateIntegrationParams{
			Name:             input.Name,
			BaseUrl:          stringPtrToNull(input.BaseURL),
			EncryptedPayload: encrypted,
			UpdatedAt:        now,
			ID:               id,
		})
	} else {
		row, err = r.store.q.UpdateIntegration(ctx, db.UpdateIntegrationParams{
			Name:             input.Name,
			BaseUrl:          stringPtrToNull(input.BaseURL),
			EncryptedPayload: existing.EncryptedPayload,
			UpdatedAt:        now,
			ID:               id,
		})
	}
	if err != nil {
		return nil, err
	}
	return integrationFromRow(row), nil
}

func (r integrationRepo) Delete(ctx context.Context, id string) error {
	return r.store.q.DeleteIntegration(ctx, id)
}

func (r integrationRepo) CountReferences(ctx context.Context, id string) (int64, error) {
	return r.store.q.CountIntegrationReferences(ctx, db.CountIntegrationReferencesParams{
		SourceIntegrationID: sql.NullString{String: id, Valid: true},
		IntegrationID:       id,
	})
}

func (r integrationRepo) DecryptPayload(ctx context.Context, id string) ([]byte, error) {
	row, err := r.store.q.GetIntegration(ctx, id)
	if err != nil {
		return nil, err
	}
	return r.store.cipher.Decrypt(row.EncryptedPayload)
}

func integrationFromRow(row db.Integration) *Integration {
	return &Integration{
		ID:        row.ID,
		Kind:      row.Kind,
		Name:      row.Name,
		BaseURL:   nullStringPtr(row.BaseUrl),
		HasSecret: row.EncryptedPayload != "",
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}
}

func integrationsFromRows(rows []db.Integration) []Integration {
	out := make([]Integration, len(rows))
	for i, row := range rows {
		out[i] = *integrationFromRow(row)
	}
	return out
}
