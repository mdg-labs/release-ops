package store_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/mdg-labs/release-ops/internal/store"
)

func TestGetAppSettingsPollInterval(t *testing.T) {
	t.Parallel()

	s, _ := testStore(t)
	ctx := context.Background()

	got, err := s.Settings().Get(ctx)
	if err != nil {
		t.Fatalf("GetAppSettings: %v", err)
	}
	if got.ID != 1 {
		t.Fatalf("ID = %d, want 1", got.ID)
	}
	if got.PollIntervalMinutes < 5 {
		t.Fatalf("poll_interval_minutes = %d, want >= 5", got.PollIntervalMinutes)
	}
	if got.PollIntervalMinutes != 360 {
		t.Fatalf("poll_interval_minutes = %d, want default 360", got.PollIntervalMinutes)
	}
}

func TestIntegrationCRUDRoundtrip(t *testing.T) {
	t.Parallel()

	s, _ := testStore(t)
	ctx := context.Background()

	created, err := s.Integrations().Create(ctx, store.CreateIntegrationInput{
		Kind:   "github",
		Name:   "GitHub CI",
		Secret: []byte(`{"token":"ghp_roundtrip"}`),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := s.Integrations().Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "GitHub CI" || got.Kind != "github" {
		t.Fatalf("Get = %+v, want GitHub CI github", got)
	}

	all, err := s.Integrations().List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 1 || all[0].ID != created.ID {
		t.Fatalf("List = %+v, want single integration %s", all, created.ID)
	}

	updated, err := s.Integrations().Update(ctx, created.ID, store.UpdateIntegrationInput{
		Name:   "GitHub Production",
		Secret: []byte(`{"token":"ghp_updated"}`),
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Name != "GitHub Production" {
		t.Fatalf("updated Name = %q, want GitHub Production", updated.Name)
	}

	if err := s.Integrations().Delete(ctx, created.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err = s.Integrations().Get(ctx, created.ID)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("Get after delete: %v, want sql.ErrNoRows", err)
	}
}

func TestTicketProjectCreateWithValidJSONConfigs(t *testing.T) {
	t.Parallel()

	s, _ := testStore(t)
	ctx := context.Background()

	integration, err := s.Integrations().Create(ctx, store.CreateIntegrationInput{
		Kind:    "kaneo",
		Name:    "Kaneo",
		BaseURL: strPtr("https://api.kaneo.example"),
		Secret:  []byte(`{"api_key":"test"}`),
	})
	if err != nil {
		t.Fatalf("Create integration: %v", err)
	}

	createConfig := `{"status":"ready","priority":"medium"}`
	statusMapping := `{"open":["To Do"],"done":["Done"],"cancelled":["Cancelled"],"superseded":"Cancelled"}`
	contentTemplates := `{"title":"Release: {{ .Release.Tag }}","description":"Body","supersedeComment":"Old → new"}`

	project, err := s.TicketProjects().Create(ctx, store.CreateTicketProjectInput{
		IntegrationID:     integration.ID,
		ExternalProjectID: "proj-json-valid",
		Name:              "JSON Valid Project",
		CreateConfig:      createConfig,
		StatusMapping:     statusMapping,
		ContentTemplates:  contentTemplates,
	})
	if err != nil {
		t.Fatalf("Create ticket project: %v", err)
	}
	if project.CreateConfig != createConfig {
		t.Fatalf("CreateConfig = %q, want %q", project.CreateConfig, createConfig)
	}
	if project.StatusMapping != statusMapping {
		t.Fatalf("StatusMapping = %q, want %q", project.StatusMapping, statusMapping)
	}
	if project.ContentTemplates != contentTemplates {
		t.Fatalf("ContentTemplates = %q, want %q", project.ContentTemplates, contentTemplates)
	}

	row, err := s.Queries().GetTicketProject(ctx, project.ID)
	if err != nil {
		t.Fatalf("GetTicketProject raw: %v", err)
	}
	if !json.Valid([]byte(row.CreateConfig)) {
		t.Fatalf("create_config is not valid JSON: %q", row.CreateConfig)
	}
	if !json.Valid([]byte(row.StatusMapping)) {
		t.Fatalf("status_mapping is not valid JSON: %q", row.StatusMapping)
	}
	if !json.Valid([]byte(row.ContentTemplates)) {
		t.Fatalf("content_templates is not valid JSON: %q", row.ContentTemplates)
	}
	if row.ContentTemplates != contentTemplates {
		t.Fatalf("content_templates = %q, want %q", row.ContentTemplates, contentTemplates)
	}

	updatedTemplates := `{"title":"","description":"","supersedeComment":""}`
	updated, err := s.TicketProjects().Update(ctx, project.ID, store.UpdateTicketProjectInput{
		Name:               project.Name,
		CreateConfig:       createConfig,
		StatusMapping:      statusMapping,
		ContentTemplates:   updatedTemplates,
		OnOpenTicketPolicy: project.OnOpenTicketPolicy,
	})
	if err != nil {
		t.Fatalf("Update ticket project: %v", err)
	}
	if updated.ContentTemplates != updatedTemplates {
		t.Fatalf("updated ContentTemplates = %q, want %q", updated.ContentTemplates, updatedTemplates)
	}

	_, err = s.TicketProjects().Create(ctx, store.CreateTicketProjectInput{
		IntegrationID:     integration.ID,
		ExternalProjectID: "proj-invalid",
		Name:              "Invalid JSON",
		CreateConfig:      "not-json",
		StatusMapping:     statusMapping,
	})
	if err == nil {
		t.Fatal("Create with invalid create_config: want error, got nil")
	}
}

func TestMonitoredRepoUniqueSourceKindProjectPath(t *testing.T) {
	t.Parallel()

	s, _ := testStore(t)
	ctx := context.Background()

	fixture := seedRepoFixture(t, s, ctx)

	_, err := s.Repos().Create(ctx, store.CreateMonitoredRepoInput{
		SourceKind:      "github",
		ProjectPath:     "org/repo",
		Enabled:         true,
		TicketProjectID: fixture.ticketProjectID,
	})
	if err == nil {
		t.Fatal("duplicate Create: want UNIQUE error, got nil")
	}
	if !strings.Contains(err.Error(), "UNIQUE") {
		t.Fatalf("duplicate Create error = %v, want UNIQUE constraint failure", err)
	}

	_, err = s.Repos().Create(ctx, store.CreateMonitoredRepoInput{
		SourceKind:      "gitlab",
		ProjectPath:     "org/repo",
		Enabled:         true,
		TicketProjectID: fixture.ticketProjectID,
	})
	if err != nil {
		t.Fatalf("Create with different source_kind: %v", err)
	}
}

func TestIntegrationDefaultMovesWithinKind(t *testing.T) {
	t.Parallel()

	s, _ := testStore(t)
	ctx := context.Background()
	repo := s.Integrations()

	create := func(kind, name string, baseURL *string, isDefault bool) *store.Integration {
		t.Helper()
		got, err := repo.Create(ctx, store.CreateIntegrationInput{
			Kind: kind, Name: name, BaseURL: baseURL, Secret: []byte(`{"token":"x"}`), IsDefault: isDefault,
		})
		if err != nil {
			t.Fatalf("Create %s: %v", name, err)
		}
		return got
	}
	defaults := func() map[string]bool {
		t.Helper()
		all, err := repo.List(ctx)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		out := make(map[string]bool, len(all))
		for _, it := range all {
			out[it.Name] = it.IsDefault
		}
		return out
	}

	plain := create("github", "plain", nil, false)
	if plain.IsDefault {
		t.Fatal("new integration must not be default unless requested")
	}
	first := create("github", "first", nil, true)
	if !first.IsDefault {
		t.Fatal("Create with IsDefault did not set the flag")
	}
	glURL := "https://gitlab.example"
	gitlab := create("gitlab", "gl", &glURL, true)

	// Creating a second default moves the flag; other kinds are untouched.
	second := create("github", "second", nil, true)
	if got := defaults(); got["first"] || !got["second"] || got["plain"] || !got["gl"] {
		t.Fatalf("after create-as-default: %v", got)
	}

	// Updating with true moves it back.
	yes, no := true, false
	if _, err := repo.Update(ctx, first.ID, store.UpdateIntegrationInput{Name: "first", IsDefault: &yes}); err != nil {
		t.Fatalf("Update true: %v", err)
	}
	if got := defaults(); !got["first"] || got["second"] || !got["gl"] {
		t.Fatalf("after update-as-default: %v", got)
	}

	// nil leaves the flag alone, including on a secret rotation.
	if _, err := repo.Update(ctx, first.ID, store.UpdateIntegrationInput{Name: "first renamed", Secret: []byte(`{"token":"y"}`)}); err != nil {
		t.Fatalf("Update nil: %v", err)
	}
	if got := defaults(); !got["first renamed"] {
		t.Fatalf("flag lost on unrelated update: %v", got)
	}

	// false clears it and leaves the kind without a default.
	if _, err := repo.Update(ctx, first.ID, store.UpdateIntegrationInput{Name: "first", IsDefault: &no}); err != nil {
		t.Fatalf("Update false: %v", err)
	}
	if got := defaults(); got["first"] || got["second"] || got["plain"] || !got["gl"] {
		t.Fatalf("after clear: %v", got)
	}

	// Deleting the default leaves the kind without one; nothing is promoted.
	if err := repo.Delete(ctx, gitlab.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := repo.Delete(ctx, second.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	for name, isDefault := range defaults() {
		if isDefault {
			t.Fatalf("%s is default after deleting defaults", name)
		}
	}
}

func TestIntegrationDefaultEnforcedByDatabase(t *testing.T) {
	t.Parallel()

	s, _ := testStore(t)
	ctx := context.Background()
	repo := s.Integrations()

	a, err := repo.Create(ctx, store.CreateIntegrationInput{Kind: "github", Name: "a", Secret: []byte("x"), IsDefault: true})
	if err != nil {
		t.Fatalf("Create a: %v", err)
	}
	b, err := repo.Create(ctx, store.CreateIntegrationInput{Kind: "github", Name: "b", Secret: []byte("x")})
	if err != nil {
		t.Fatalf("Create b: %v", err)
	}
	linear, err := repo.Create(ctx, store.CreateIntegrationInput{Kind: "linear", Name: "lin", Secret: []byte("x")})
	if err != nil {
		t.Fatalf("Create linear: %v", err)
	}

	// Bypass the repository: the schema alone must refuse a second default of a kind...
	if _, err := s.DB().ExecContext(ctx, `UPDATE integrations SET is_default = 1 WHERE id = ?`, b.ID); err == nil {
		t.Fatal("second default for the same kind was accepted")
	}
	// ...a default on a ticket kind...
	if _, err := s.DB().ExecContext(ctx, `UPDATE integrations SET is_default = 1 WHERE id = ?`, linear.ID); err == nil {
		t.Fatal("default on a ticket kind was accepted")
	}
	// ...and a value other than 0/1.
	if _, err := s.DB().ExecContext(ctx, `UPDATE integrations SET is_default = 2 WHERE id = ?`, a.ID); err == nil {
		t.Fatal("is_default = 2 was accepted")
	}
}

func TestIntegrationCreateDefaultRollsBackOnFailure(t *testing.T) {
	t.Parallel()

	s, _ := testStore(t)
	ctx := context.Background()
	repo := s.Integrations()

	old, err := repo.Create(ctx, store.CreateIntegrationInput{Kind: "github", Name: "old", Secret: []byte("x"), IsDefault: true})
	if err != nil {
		t.Fatalf("Create old: %v", err)
	}
	// A github integration with a base URL violates the table CHECK, so the insert fails
	// after the previous default was cleared.
	bad := "https://example.test"
	if _, err := repo.Create(ctx, store.CreateIntegrationInput{Kind: "github", Name: "bad", BaseURL: &bad, Secret: []byte("x"), IsDefault: true}); err == nil {
		t.Fatal("Create with invalid base URL succeeded")
	}
	got, err := repo.Get(ctx, old.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !got.IsDefault {
		t.Fatal("previous default was cleared although the new default was not created")
	}
}
