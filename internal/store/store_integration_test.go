package store_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"

	"github.com/mdg-labs/release-ops/internal/providers/ticket"
	"github.com/mdg-labs/release-ops/internal/store"
	"github.com/mdg-labs/release-ops/internal/store/db"
)

func TestIntegrationCRUDRoundtrip(t *testing.T) {
	t.Parallel()

	s, _ := testStore(t)
	ctx := context.Background()

	created, err := s.Integrations().Create(ctx, store.CreateIntegrationInput{
		Kind:   "linear",
		Name:   "Linear CI",
		Secret: []byte(`{"api_key":"lin_roundtrip"}`),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := s.Integrations().Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "Linear CI" || got.Kind != "linear" {
		t.Fatalf("Get = %+v, want Linear CI linear", got)
	}

	all, err := s.Integrations().List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 1 || all[0].ID != created.ID {
		t.Fatalf("List = %+v, want single integration %s", all, created.ID)
	}

	updated, err := s.Integrations().Update(ctx, created.ID, store.UpdateIntegrationInput{
		Name:   "Linear Production",
		Secret: []byte(`{"api_key":"lin_updated"}`),
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Name != "Linear Production" {
		t.Fatalf("updated Name = %q, want Linear Production", updated.Name)
	}

	if err := s.Integrations().Delete(ctx, created.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err = s.Integrations().Get(ctx, created.ID)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("Get after delete: %v, want sql.ErrNoRows", err)
	}
}

func TestIntegrationWithoutSecretReportsNoSecret(t *testing.T) {
	t.Parallel()

	s, _ := testStore(t)
	ctx := context.Background()

	created, err := s.Integrations().Create(ctx, store.CreateIntegrationInput{
		Kind: "linear",
		Name: "Linear",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.HasSecret {
		t.Fatal("created.HasSecret = true, want false for an integration without a secret")
	}

	listed, err := s.Integrations().List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(listed) != 1 || listed[0].HasSecret {
		t.Fatalf("List = %+v, want one integration without a secret", listed)
	}

	payload, err := s.Integrations().DecryptPayload(ctx, created.ID)
	if err != nil {
		t.Fatalf("DecryptPayload: %v", err)
	}
	if len(payload) != 0 {
		t.Fatalf("payload = %q, want empty", payload)
	}

	// A payload-less integration is never a usable ticket integration.
	if _, _, err := ticket.ParseJiraSecret(payload); err == nil {
		t.Fatal("ParseJiraSecret accepted an empty payload")
	}
	if _, err := ticket.ParseKaneoSecret(payload); err == nil {
		t.Fatal("ParseKaneoSecret accepted an empty payload")
	}
	if _, err := ticket.ParseLinearSecret(payload); err == nil {
		t.Fatal("ParseLinearSecret accepted an empty payload")
	}

	renamed, err := s.Integrations().Update(ctx, created.ID, store.UpdateIntegrationInput{Name: "Renamed"})
	if err != nil {
		t.Fatalf("Update without secret: %v", err)
	}
	if renamed.HasSecret {
		t.Fatal("HasSecret = true after an update without a secret, want false")
	}

	withKey, err := s.Integrations().Update(ctx, created.ID, store.UpdateIntegrationInput{
		Name:   "Renamed",
		Secret: []byte(`{"api_key":"lin_added"}`),
	})
	if err != nil {
		t.Fatalf("Update with secret: %v", err)
	}
	if !withKey.HasSecret {
		t.Fatal("HasSecret = false after adding a key, want true")
	}
	payload, err = s.Integrations().DecryptPayload(ctx, created.ID)
	if err != nil {
		t.Fatalf("DecryptPayload after update: %v", err)
	}
	if key, _ := ticket.ParseLinearSecret(payload); key != "lin_added" {
		t.Fatalf("key = %q, want lin_added", key)
	}
}

// updateTicketReferencing seeds the rows an update ticket needs (installation, repository,
// update PR) and the ticket itself, created in the given ticket project.
func updateTicketReferencing(t *testing.T, s *store.Store, ticketProjectID string) {
	t.Helper()
	ctx := context.Background()

	for _, stmt := range []string{
		`INSERT INTO installations (id, github_installation_id, account_id, account_login, account_type, status, repository_selection, created_at, updated_at)
		 VALUES ('inst-1', 1, 1, 'acme', 'Organization', 'active', 'all', 't', 't')`,
		`INSERT INTO repositories (id, installation_id, github_repository_id, full_name, default_branch, created_at, updated_at)
		 VALUES ('repo-1', 'inst-1', 1, 'acme/api', 'main', 't', 't')`,
		`INSERT INTO update_prs (id, repository_id, number, node_id, url, title, head_ref, state, last_seen_at, created_at, updated_at)
		 VALUES ('pr-1', 'repo-1', 1, 'node', 'https://example.test/pr/1', 'Update dep', 'renovate/dep', 'open', 't', 't', 't')`,
	} {
		if _, err := s.DB().Writer.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("seed %q: %v", stmt, err)
		}
	}
	if _, err := s.DB().Writer.ExecContext(ctx,
		`INSERT INTO update_tickets (id, update_pr_id, ticket_project_id, state, created_at, updated_at)
		 VALUES ('ut-1', 'pr-1', ?, 'pending', 't', 't')`, ticketProjectID); err != nil {
		t.Fatalf("seed update ticket: %v", err)
	}
}

func TestDeleteTicketProjectReferencedByUpdateTicketReturnsErrInUse(t *testing.T) {
	t.Parallel()

	s, _ := testStore(t)
	ctx := context.Background()
	fixture := seedTicketFixture(t, s, ctx)
	updateTicketReferencing(t, s, fixture.ticketProjectID)

	err := s.TicketProjects().Delete(ctx, fixture.ticketProjectID)
	if !errors.Is(err, store.ErrInUse) {
		t.Fatalf("Delete = %v, want store.ErrInUse", err)
	}
	if _, err := s.TicketProjects().Get(ctx, fixture.ticketProjectID); err != nil {
		t.Fatalf("ticket project gone after refused delete: %v", err)
	}
}

func TestDeleteIntegrationReferencedByTicketProjectIsCounted(t *testing.T) {
	t.Parallel()

	s, _ := testStore(t)
	ctx := context.Background()
	fixture := seedTicketFixture(t, s, ctx)

	refs, err := s.Integrations().CountReferences(ctx, fixture.integrationID)
	if err != nil {
		t.Fatalf("CountReferences: %v", err)
	}
	if refs != 1 {
		t.Fatalf("CountReferences = %d, want 1 ticket project", refs)
	}
	if err := s.Integrations().Delete(ctx, fixture.integrationID); !errors.Is(err, store.ErrInUse) {
		t.Fatalf("Delete = %v, want store.ErrInUse", err)
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

	row, err := db.New(s.DB().Reader).GetTicketProject(ctx, project.ID)
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
		Name:             project.Name,
		CreateConfig:     createConfig,
		StatusMapping:    statusMapping,
		ContentTemplates: updatedTemplates,
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
