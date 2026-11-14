package store

import (
	"context"

	"github.com/mdg-labs/release-ops/internal/store/db"
)

// AppSettings is the singleton row (id=1) of the local-account token windows.
type AppSettings struct {
	ID                              int64
	InviteTokenExpiryHours          int64
	PasswordResetTokenExpiryMinutes int64
	UpdatedAt                       string
}

// SettingsRepository reads and updates app_settings.
type SettingsRepository interface {
	EnsureDefault(ctx context.Context) error
	Get(ctx context.Context) (*AppSettings, error)
	UpdateTokenExpiry(ctx context.Context, inviteTokenExpiryHours, passwordResetTokenExpiryMinutes int64) (*AppSettings, error)
}

type settingsRepo struct {
	store *Store
}

func (r settingsRepo) EnsureDefault(ctx context.Context) error {
	return r.store.writes.EnsureAppSettings(ctx, nowUTC())
}

func (r settingsRepo) Get(ctx context.Context) (*AppSettings, error) {
	row, err := r.store.reads.GetAppSettings(ctx)
	if err != nil {
		return nil, err
	}
	return appSettingsFromRow(row), nil
}

func (r settingsRepo) UpdateTokenExpiry(ctx context.Context, inviteTokenExpiryHours, passwordResetTokenExpiryMinutes int64) (*AppSettings, error) {
	row, err := r.store.writes.UpdateTokenExpirySettings(ctx, db.UpdateTokenExpirySettingsParams{
		InviteTokenExpiryHours:          inviteTokenExpiryHours,
		PasswordResetTokenExpiryMinutes: passwordResetTokenExpiryMinutes,
		UpdatedAt:                       nowUTC(),
	})
	if err != nil {
		return nil, err
	}
	return appSettingsFromRow(row), nil
}

func appSettingsFromRow(row db.AppSetting) *AppSettings {
	return &AppSettings{
		ID:                              row.ID,
		InviteTokenExpiryHours:          row.InviteTokenExpiryHours,
		PasswordResetTokenExpiryMinutes: row.PasswordResetTokenExpiryMinutes,
		UpdatedAt:                       row.UpdatedAt,
	}
}
