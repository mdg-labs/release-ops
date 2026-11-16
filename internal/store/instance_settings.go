package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/mdg-labs/release-ops/internal/store/db"
)

// defaultConcurrencyCap is the cap while instance_settings holds no row; it is the column's
// default.
const defaultConcurrencyCap int64 = 2

// ErrInvalidConcurrencyCap is returned for a concurrency cap below 1.
var ErrInvalidConcurrencyCap = errors.New("the concurrency cap must be at least 1")

// InstanceConfig is the environment configuration the settings page shows read-only
// (specs.html sections 7.2 and 9). The server sets it once at start with SetInstanceConfig.
type InstanceConfig struct {
	// PresetRepositories is PRESET_REPOSITORIES.
	PresetRepositories []string
	// GlobalConfigRepo and GlobalConfigRef are GLOBAL_CONFIG_REPO and GLOBAL_CONFIG_REF.
	GlobalConfigRepo, GlobalConfigRef string
	// AdminTeam and ViewerTeam are ADMIN_TEAM and VIEWER_TEAM as org/team-slug.
	AdminTeam, ViewerTeam string
}

// SetInstanceConfig records the environment configuration that InstanceConfig returns.
func (s *Store) SetInstanceConfig(c InstanceConfig) {
	c.PresetRepositories = append([]string(nil), c.PresetRepositories...)
	s.instanceConfig.Store(&c)
}

// InstanceConfig returns the environment configuration, empty until SetInstanceConfig has
// been called.
func (s *Store) InstanceConfig() InstanceConfig {
	c := s.instanceConfig.Load()
	if c == nil {
		return InstanceConfig{}
	}
	out := *c
	out.PresetRepositories = append([]string(nil), c.PresetRepositories...)
	return out
}

// CapturedGlobalConfig is the global Renovate config as the worker last used it.
type CapturedGlobalConfig struct {
	// Content is the raw file text, never parsed; empty with Captured false until a job has
	// returned one.
	Content  string
	Captured bool
	// Source is "repository" or "mounted", and CapturedAt when the worker read it.
	Source     string
	CapturedAt string
}

// InstanceSettingsRepository reads and writes the settings of the whole instance.
type InstanceSettingsRepository interface {
	// ConcurrencyCap returns the cap, the default while no row exists.
	ConcurrencyCap(ctx context.Context) (int64, error)
	// SetConcurrencyCap stores the cap, creating the row when there is none and leaving its
	// other columns as they are. A cap below 1 is refused with ErrInvalidConcurrencyCap and
	// stores nothing.
	SetConcurrencyCap(ctx context.Context, n int64) error
	// GlobalConfig returns the global config captured for the installation that is not
	// removed, with Captured false when there is none.
	GlobalConfig(ctx context.Context) (CapturedGlobalConfig, error)
}

// InstanceSettings returns the instance settings repository.
func (s *Store) InstanceSettings() InstanceSettingsRepository {
	return instanceSettingsRepo{store: s}
}

type instanceSettingsRepo struct {
	store *Store
}

func (r instanceSettingsRepo) ConcurrencyCap(ctx context.Context) (int64, error) {
	n, err := r.store.reads.GetConcurrencyCap(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return defaultConcurrencyCap, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read concurrency cap: %w", err)
	}
	return n, nil
}

func (r instanceSettingsRepo) SetConcurrencyCap(ctx context.Context, n int64) error {
	if n < 1 {
		return ErrInvalidConcurrencyCap
	}
	if err := r.store.writes.UpsertConcurrencyCap(ctx, db.UpsertConcurrencyCapParams{ConcurrencyCap: n, UpdatedAt: nowUTC()}); err != nil {
		return fmt.Errorf("store concurrency cap: %w", err)
	}
	return nil
}

func (r instanceSettingsRepo) GlobalConfig(ctx context.Context) (CapturedGlobalConfig, error) {
	row, err := r.store.reads.GetLiveInstallationGlobalConfig(ctx)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !row.GlobalConfigContent.Valid) {
		return CapturedGlobalConfig{}, nil
	}
	if err != nil {
		return CapturedGlobalConfig{}, fmt.Errorf("read global config: %w", err)
	}
	return CapturedGlobalConfig{
		Content: row.GlobalConfigContent.String, Captured: true,
		Source: row.GlobalConfigSource.String, CapturedAt: row.GlobalConfigCapturedAt.String,
	}, nil
}

// GitHubAppInfo is the App's non-secret metadata and whether each of its secrets is stored.
type GitHubAppInfo struct {
	AppID                                            int64
	Slug, ClientID, HTMLURL, OwnerLogin, OwnerType   string
	WebhookActive                                    bool
	HasPrivateKey, HasClientSecret, HasWebhookSecret bool
}

// GitHubAppInfoRepository reads the App without any secret.
type GitHubAppInfoRepository interface {
	// Info returns ErrGitHubAppNotFound before an App is stored.
	Info(ctx context.Context) (GitHubAppInfo, error)
}

// GitHubAppInfo returns the repository that reads the App's metadata.
func (s *Store) GitHubAppInfo() GitHubAppInfoRepository {
	return githubAppInfoRepo{store: s}
}

type githubAppInfoRepo struct {
	store *Store
}

func (r githubAppInfoRepo) Info(ctx context.Context) (GitHubAppInfo, error) {
	row, err := r.store.reads.GetGitHubAppInfo(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return GitHubAppInfo{}, ErrGitHubAppNotFound
	}
	if err != nil {
		return GitHubAppInfo{}, fmt.Errorf("read github app: %w", err)
	}
	return GitHubAppInfo{
		AppID: row.AppID, Slug: row.Slug, ClientID: row.ClientID, HTMLURL: row.HtmlUrl,
		OwnerLogin: row.OwnerLogin, OwnerType: row.OwnerType, WebhookActive: row.WebhookActive == 1,
		HasPrivateKey: row.HasPrivateKey, HasClientSecret: row.HasClientSecret, HasWebhookSecret: row.HasWebhookSecret,
	}, nil
}
