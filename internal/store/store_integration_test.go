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
		Kind:    "phasical",
		Name:    "Phasical",
		BaseURL: strPtr("https://api.phasical.example"),
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
