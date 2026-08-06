package store

import (
	"context"

	"github.com/mdg-labs/release-ops/internal/store/db"
)

// AppSettings is the singleton application settings row (id=1).
type AppSettings struct {
	ID                  int64
	PollIntervalMinutes int64
	UpdatedAt           string
}

// SettingsRepository reads and updates app_settings.
type SettingsRepository interface {
	Get(ctx context.Context) (*AppSettings, error)
	UpdatePollInterval(ctx context.Context, pollIntervalMinutes int64) (*AppSettings, error)
}

type settingsRepo struct {
	store *Store
}

func (r settingsRepo) Get(ctx context.Context) (*AppSettings, error) {
	row, err := r.store.q.GetAppSettings(ctx)
	if err != nil {
		return nil, err
	}
	return appSettingsFromRow(row), nil
}

func (r settingsRepo) UpdatePollInterval(ctx context.Context, pollIntervalMinutes int64) (*AppSettings, error) {
	row, err := r.store.q.UpdatePollInterval(ctx, db.UpdatePollIntervalParams{
		PollIntervalMinutes: pollIntervalMinutes,
		UpdatedAt:           nowUTC(),
	})
	if err != nil {
		return nil, err
	}
	return appSettingsFromRow(row), nil
}

func appSettingsFromRow(row db.AppSetting) *AppSettings {
	return &AppSettings{
		ID:                  row.ID,
		PollIntervalMinutes: row.PollIntervalMinutes,
		UpdatedAt:           row.UpdatedAt,
	}
}
