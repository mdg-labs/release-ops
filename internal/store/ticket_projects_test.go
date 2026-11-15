package store_test

import (
	"context"
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
