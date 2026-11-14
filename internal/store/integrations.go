package store

import (
	"context"

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
	Update(ctx context.Context, id string, input UpdateIntegrationInput) (*Integration, error)
	Delete(ctx context.Context, id string) error
	CountReferences(ctx context.Context, id string) (int64, error)
	DecryptPayload(ctx context.Context, id string) ([]byte, error)
}

type integrationRepo struct {
	store *Store
}

func (r integrationRepo) Create(ctx context.Context, input CreateIntegrationInput) (*Integration, error) {
	// An empty secret is stored as an empty payload (no credential), not as an encrypted
	// empty value, so the integration reports no secret.
	var encrypted string
	if len(input.Secret) > 0 {
		var err error
		encrypted, err = r.store.cipher.Encrypt(input.Secret)
		if err != nil {
			return nil, err
		}
	}

	now := nowUTC()
	row, err := r.store.writes.CreateIntegration(ctx, db.CreateIntegrationParams{
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
	row, err := r.store.reads.GetIntegration(ctx, id)
	if err != nil {
		return nil, err
	}
	return integrationFromRow(row), nil
}

func (r integrationRepo) List(ctx context.Context) ([]Integration, error) {
	rows, err := r.store.reads.ListIntegrations(ctx)
	if err != nil {
		return nil, err
	}
	return integrationsFromRows(rows), nil
}

func (r integrationRepo) Update(ctx context.Context, id string, input UpdateIntegrationInput) (*Integration, error) {
	tx, err := r.store.db.Writer.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = tx.Rollback()
	}()
	q := r.store.writes.WithTx(tx)

	existing, err := q.GetIntegration(ctx, id)
	if err != nil {
		return nil, err
	}

	encrypted := existing.EncryptedPayload
	if input.Secret != nil {
		encrypted, err = r.store.cipher.Encrypt(input.Secret)
		if err != nil {
			return nil, err
		}
	}

	row, err := q.UpdateIntegration(ctx, db.UpdateIntegrationParams{
		Name:             input.Name,
		BaseUrl:          stringPtrToNull(input.BaseURL),
		EncryptedPayload: encrypted,
		UpdatedAt:        nowUTC(),
		ID:               id,
	})
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return integrationFromRow(row), nil
}

// Delete removes an integration. It returns ErrInUse when a ticket project still references it.
func (r integrationRepo) Delete(ctx context.Context, id string) error {
	return mapForeignKeyError(r.store.writes.DeleteIntegration(ctx, id))
}

func (r integrationRepo) CountReferences(ctx context.Context, id string) (int64, error) {
	return r.store.reads.CountIntegrationReferences(ctx, id)
}

func (r integrationRepo) DecryptPayload(ctx context.Context, id string) ([]byte, error) {
	row, err := r.store.reads.GetIntegration(ctx, id)
	if err != nil {
		return nil, err
	}
	if row.EncryptedPayload == "" {
		return []byte{}, nil
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
