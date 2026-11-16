package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/mdg-labs/release-ops/internal/store"
	"github.com/mdg-labs/release-ops/internal/store/storetest"
)

func TestConcurrencyCapDefaultsBeforeAnyRowAndIsStoredAfter(t *testing.T) {
	t.Parallel()
	s, db, _ := newGitHubAppStore(t)
	ctx := context.Background()
	repo := s.InstanceSettings()

	if n, err := repo.ConcurrencyCap(ctx); err != nil || n != 2 {
		t.Fatalf("ConcurrencyCap with no row = %d, %v; want the default 2", n, err)
	}
	if err := repo.SetConcurrencyCap(ctx, 5); err != nil {
		t.Fatalf("SetConcurrencyCap: %v", err)
	}
	if n, err := repo.ConcurrencyCap(ctx); err != nil || n != 5 {
		t.Fatalf("ConcurrencyCap = %d, %v; want 5", n, err)
	}
	var rows int
	if err := db.Writer.QueryRow(`SELECT COUNT(*) FROM instance_settings`).Scan(&rows); err != nil || rows != 1 {
		t.Fatalf("instance_settings rows = %d, %v; want 1", rows, err)
	}
}

func TestSetConcurrencyCapKeepsTheBootstrapAdminOfTheRow(t *testing.T) {
	t.Parallel()
	s, db, _ := newGitHubAppStore(t)
	ctx := context.Background()
	const at = "2026-11-14T12:00:00.000Z"
	if _, err := db.Writer.Exec(`INSERT INTO users (id, created_at, updated_at) VALUES ('u1', ?, ?)`, at, at); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := db.Writer.Exec(`INSERT INTO instance_settings (id, bootstrap_admin_user_id, updated_at) VALUES (1, 'u1', ?)`, at); err != nil {
		t.Fatalf("seed instance settings: %v", err)
	}

	if err := s.InstanceSettings().SetConcurrencyCap(ctx, 7); err != nil {
		t.Fatalf("SetConcurrencyCap: %v", err)
	}

	var admin string
	var cap int64
	if err := db.Writer.QueryRow(`SELECT bootstrap_admin_user_id, concurrency_cap FROM instance_settings WHERE id = 1`).Scan(&admin, &cap); err != nil {
		t.Fatalf("read row: %v", err)
	}
	if admin != "u1" || cap != 7 {
		t.Errorf("row = admin %q cap %d, want u1 and 7: the write must change the cap only", admin, cap)
	}
}

func TestSetConcurrencyCapRefusesBelowOneAndKeepsTheStoredValue(t *testing.T) {
	t.Parallel()
	s, _, _ := newGitHubAppStore(t)
	ctx := context.Background()
	repo := s.InstanceSettings()
	if err := repo.SetConcurrencyCap(ctx, 4); err != nil {
		t.Fatalf("SetConcurrencyCap: %v", err)
	}

	for _, n := range []int64{0, -1} {
		if err := repo.SetConcurrencyCap(ctx, n); !errors.Is(err, store.ErrInvalidConcurrencyCap) {
			t.Errorf("SetConcurrencyCap(%d) = %v, want ErrInvalidConcurrencyCap", n, err)
		}
	}
	if n, err := repo.ConcurrencyCap(ctx); err != nil || n != 4 {
		t.Errorf("ConcurrencyCap = %d, %v; want the earlier 4 kept", n, err)
	}
}

func TestGlobalConfigIsTheLiveInstallationsAndNotARemovedOnes(t *testing.T) {
	t.Parallel()
	s, db, _ := newGitHubAppStore(t)
	ctx := context.Background()

	if got, err := s.InstanceSettings().GlobalConfig(ctx); err != nil || got.Captured {
		t.Fatalf("GlobalConfig with no installation = %+v, %v; want nothing captured", got, err)
	}

	storetest.SeedRepositories(t, db, 0)
	const at = "2026-11-14T12:00:00.000Z"
	if _, err := db.Writer.Exec(`INSERT INTO installation_settings (installation_id, updated_at) VALUES ('inst-1', ?)`, at); err != nil {
		t.Fatalf("seed settings: %v", err)
	}
	if got, err := s.InstanceSettings().GlobalConfig(ctx); err != nil || got.Captured {
		t.Fatalf("GlobalConfig before a capture = %+v, %v; want nothing captured", got, err)
	}

	raw := "module.exports = {\r\n  allowedCommands: ['^x$'],\r\n};\n"
	if _, err := db.Writer.Exec(`UPDATE installation_settings SET global_config_content = ?, global_config_source = 'repository', global_config_captured_at = ? WHERE installation_id = 'inst-1'`, raw, at); err != nil {
		t.Fatalf("capture: %v", err)
	}
	got, err := s.InstanceSettings().GlobalConfig(ctx)
	if err != nil || !got.Captured || got.Content != raw || got.Source != "repository" || got.CapturedAt != at {
		t.Fatalf("GlobalConfig = %+v, %v; want the raw text byte for byte", got, err)
	}

	if _, err := db.Writer.Exec(`UPDATE installations SET status = 'removed' WHERE id = 'inst-1'`); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if got, err := s.InstanceSettings().GlobalConfig(ctx); err != nil || got.Captured {
		t.Errorf("GlobalConfig of a removed installation = %+v, %v; want nothing captured", got, err)
	}
}

func TestGitHubAppInfoReportsWhichSecretsAreStoredAndNoneOfThem(t *testing.T) {
	t.Parallel()
	s, _, _ := newGitHubAppStore(t)
	ctx := context.Background()
	if _, err := s.GitHubAppInfo().Info(ctx); !errors.Is(err, store.ErrGitHubAppNotFound) {
		t.Fatalf("Info before an App = %v, want ErrGitHubAppNotFound", err)
	}
	seedSetupToken(t, s.DB())
	app := sampleApp()
	if err := s.GitHubApp().Create(ctx, app); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := s.GitHubAppInfo().Info(ctx)
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	want := store.GitHubAppInfo{
		AppID: app.AppID, Slug: app.Slug, ClientID: app.ClientID, HTMLURL: app.HTMLURL, OwnerLogin: "acme", OwnerType: "Organization",
		WebhookActive: true, HasPrivateKey: true, HasClientSecret: true, HasWebhookSecret: true,
	}
	if got != want {
		t.Errorf("Info = %+v, want %+v", got, want)
	}
}

func TestInstanceConfigIsACopy(t *testing.T) {
	t.Parallel()
	s, _, _ := newGitHubAppStore(t)
	if got := s.InstanceConfig(); got.GlobalConfigRepo != "" || len(got.PresetRepositories) != 0 {
		t.Fatalf("InstanceConfig before it is set = %+v, want empty", got)
	}
	in := store.InstanceConfig{PresetRepositories: []string{"acme/presets"}, GlobalConfigRepo: "acme/renovate", GlobalConfigRef: "main", AdminTeam: "acme/admins"}
	s.SetInstanceConfig(in)
	in.PresetRepositories[0] = "changed/after"

	got := s.InstanceConfig()
	if got.PresetRepositories[0] != "acme/presets" || got.GlobalConfigRepo != "acme/renovate" || got.GlobalConfigRef != "main" || got.AdminTeam != "acme/admins" {
		t.Errorf("InstanceConfig = %+v", got)
	}
	got.PresetRepositories[0] = "changed/after"
	if s.InstanceConfig().PresetRepositories[0] != "acme/presets" {
		t.Error("a caller changed the stored configuration through the returned slice")
	}
}
