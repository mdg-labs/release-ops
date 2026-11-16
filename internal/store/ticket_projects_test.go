package store_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/mdg-labs/release-ops/internal/store"
)

func TestDeleteTicketProjectRefusedWhileARepositoryOverridesOrAnInstallationDefaultsToIt(t *testing.T) {
	t.Parallel()

	for name, reference := range map[string]string{
		"repository override":  `UPDATE repositories SET ticket_project_id = ? WHERE id = 'repo-1'`,
		"installation default": `INSERT INTO installation_settings (installation_id, default_ticket_project_id, updated_at) VALUES ('inst-1', ?, 't')`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s, _ := testStore(t)
			ctx := context.Background()
			fixture := seedTicketFixture(t, s, ctx)
			for _, stmt := range []string{
				`INSERT INTO installations (id, github_installation_id, account_id, account_login, account_type, status, repository_selection, created_at, updated_at)
				 VALUES ('inst-1', 1, 1, 'acme', 'Organization', 'active', 'all', 't', 't')`,
				`INSERT INTO repositories (id, installation_id, github_repository_id, full_name, default_branch, created_at, updated_at)
				 VALUES ('repo-1', 'inst-1', 1, 'acme/api', 'main', 't', 't')`,
			} {
				if _, err := s.DB().Writer.ExecContext(ctx, stmt); err != nil {
					t.Fatalf("seed %q: %v", stmt, err)
				}
			}
			if _, err := s.DB().Writer.ExecContext(ctx, reference, fixture.ticketProjectID); err != nil {
				t.Fatalf("seed reference: %v", err)
			}

			if err := s.TicketProjects().Delete(ctx, fixture.ticketProjectID); !errors.Is(err, store.ErrInUse) {
				t.Fatalf("Delete = %v, want store.ErrInUse", err)
			}
			if _, err := s.TicketProjects().Get(ctx, fixture.ticketProjectID); err != nil {
				t.Fatalf("ticket project gone after refused delete: %v", err)
			}
			var references int
			if err := s.DB().Reader.QueryRowContext(ctx,
				`SELECT (SELECT COUNT(*) FROM repositories WHERE ticket_project_id = ?1)
				      + (SELECT COUNT(*) FROM installation_settings WHERE default_ticket_project_id = ?1)`,
				fixture.ticketProjectID).Scan(&references); err != nil || references != 1 {
				t.Fatalf("references after refused delete = %d (%v), want 1: the reference was cleared", references, err)
			}
		})
	}
}

func TestDeleteUnreferencedTicketProject(t *testing.T) {
	t.Parallel()

	s, _ := testStore(t)
	ctx := context.Background()
	fixture := seedTicketFixture(t, s, ctx)

	if err := s.TicketProjects().Delete(ctx, fixture.ticketProjectID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.TicketProjects().Get(ctx, fixture.ticketProjectID); err == nil {
		t.Fatal("ticket project still present after delete")
	}
}

func TestUpdateTicketProjectWritesOnlyWhatIsSet(t *testing.T) {
	t.Parallel()

	s, _ := testStore(t)
	ctx := context.Background()
	fixture := seedTicketFixture(t, s, ctx)
	const (
		templates = `{"title":"T","description":"D","updateComment":"","supersedeComment":"S"}`
		filter    = `{"updateTypes":["major"],"labels":[],"skipAutomerged":false}`
	)
	if _, err := s.TicketProjects().Update(ctx, fixture.ticketProjectID, store.UpdateTicketProjectInput{
		Name: "One", CreateConfig: `{}`, StatusMapping: `{}`,
		ContentTemplates: func(string) (string, error) { return templates, nil },
		Filter:           strPtr(filter),
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := s.TicketProjects().Update(ctx, fixture.ticketProjectID, store.UpdateTicketProjectInput{Name: "Two", CreateConfig: `{"a":1}`, StatusMapping: `{}`})
	if err != nil {
		t.Fatalf("Update without templates or filter: %v", err)
	}
	if got.Name != "Two" || got.CreateConfig != `{"a":1}` || got.ContentTemplates != templates || got.Filter != filter {
		t.Fatalf("project = %+v, want the name and create config written and the templates and filter kept", got)
	}

	var seen string
	got, err = s.TicketProjects().Update(ctx, fixture.ticketProjectID, store.UpdateTicketProjectInput{
		Name: "Two", CreateConfig: `{}`, StatusMapping: `{}`,
		ContentTemplates: func(stored string) (string, error) { seen = stored; return `{"title":"N"}`, nil },
	})
	if err != nil || seen != templates || got.ContentTemplates != `{"title":"N"}` || got.Filter != filter {
		t.Fatalf("Update = %+v, %v; callback saw %q, want it to see the stored templates and leave the filter", got, err, seen)
	}
}

func TestUpdateTicketProjectLeavesTheRowAloneWhenTheTemplateStepFailsOrTheProjectIsMissing(t *testing.T) {
	t.Parallel()

	s, _ := testStore(t)
	ctx := context.Background()
	fixture := seedTicketFixture(t, s, ctx)
	before, err := s.TicketProjects().Get(ctx, fixture.ticketProjectID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	refused := errors.New("refused")
	_, err = s.TicketProjects().Update(ctx, fixture.ticketProjectID, store.UpdateTicketProjectInput{
		Name: "Changed", CreateConfig: `{}`, StatusMapping: `{}`, Filter: strPtr(`{"updateTypes":["major"],"labels":[],"skipAutomerged":false}`),
		ContentTemplates: func(string) (string, error) { return "", refused },
	})
	if !errors.Is(err, refused) {
		t.Fatalf("Update = %v, want the callback's error", err)
	}
	after, err := s.TicketProjects().Get(ctx, fixture.ticketProjectID)
	if err != nil || *after != *before {
		t.Fatalf("project = %+v (%v), want %+v unchanged", after, err, before)
	}

	called := false
	_, err = s.TicketProjects().Update(ctx, "no-such", store.UpdateTicketProjectInput{
		Name: "X", CreateConfig: `{}`, StatusMapping: `{}`,
		ContentTemplates: func(string) (string, error) { called = true; return "{}", nil },
	})
	if !errors.Is(err, sql.ErrNoRows) || called {
		t.Fatalf("Update of a missing project = %v (callback called: %v), want sql.ErrNoRows", err, called)
	}
}
