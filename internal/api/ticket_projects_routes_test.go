package api

import (
	"net/http"
	"testing"

	"github.com/mdg-labs/release-ops/internal/github"
)

func TestDeletingTheInstallationDefaultTicketProjectIsRefusedAndClearsNothing(t *testing.T) {
	t.Parallel()
	s := newRolesServer(t, github.User{ID: rolesOwnerID, Login: "acme-owner"})
	admin := s.signIn(t)
	dev := seedTicketProject(t, s, "DEV")
	if code, body := s.send(t, admin, http.MethodPatch, "/api/v1/installations/inst-1", `{"defaultTicketProjectId":"`+dev+`"}`); code != http.StatusOK {
		t.Fatalf("set default = %d %s", code, body)
	}

	code, body := s.send(t, admin, http.MethodDelete, "/api/v1/ticket-projects/"+dev, "")
	if code != http.StatusConflict || errorCodeOf(body) != "CONFLICT" {
		t.Fatalf("DELETE = %d %s, want 409 CONFLICT", code, body)
	}
	if n := rowCount(t, s.deps, `SELECT COUNT(*) FROM ticket_projects WHERE id = '`+dev+`'`); n != 1 {
		t.Errorf("ticket project rows = %d, want it kept", n)
	}
	if n := rowCount(t, s.deps, `SELECT COUNT(*) FROM installation_settings WHERE default_ticket_project_id = '`+dev+`'`); n != 1 {
		t.Errorf("installations defaulting to the project = %d, want 1: the refused delete cleared the default", n)
	}
}

func TestDeletingARepositoryOverrideTicketProjectIsRefusedAndClearsNothing(t *testing.T) {
	t.Parallel()
	s := newRolesServer(t, github.User{ID: rolesOwnerID, Login: "acme-owner"})
	admin := s.signIn(t)
	ops := seedTicketProject(t, s, "OPS")
	if code, body := s.send(t, admin, http.MethodPatch, "/api/v1/repositories/repo-2", `{"ticketProjectId":"`+ops+`"}`); code != http.StatusOK {
		t.Fatalf("set override = %d %s", code, body)
	}

	code, body := s.send(t, admin, http.MethodDelete, "/api/v1/ticket-projects/"+ops, "")
	if code != http.StatusConflict || errorCodeOf(body) != "CONFLICT" {
		t.Fatalf("DELETE = %d %s, want 409 CONFLICT", code, body)
	}
	if n := rowCount(t, s.deps, `SELECT COUNT(*) FROM ticket_projects WHERE id = '`+ops+`'`); n != 1 {
		t.Errorf("ticket project rows = %d, want it kept", n)
	}
	if n := rowCount(t, s.deps, `SELECT COUNT(*) FROM repositories WHERE ticket_project_id = '`+ops+`'`); n != 1 {
		t.Errorf("repositories overriding to the project = %d, want 1: the refused delete cleared the override", n)
	}
}

func TestDeletingAnUnreferencedTicketProjectSucceedsAndLeavesOthersAlone(t *testing.T) {
	t.Parallel()
	s := newRolesServer(t, github.User{ID: rolesOwnerID, Login: "acme-owner"})
	admin := s.signIn(t)
	dev, ops := seedTicketProject(t, s, "DEV"), seedTicketProject(t, s, "OPS")
	if code, body := s.send(t, admin, http.MethodPatch, "/api/v1/installations/inst-1", `{"defaultTicketProjectId":"`+dev+`"}`); code != http.StatusOK {
		t.Fatalf("set default = %d %s", code, body)
	}

	if code, body := s.send(t, admin, http.MethodDelete, "/api/v1/ticket-projects/"+ops, ""); code != http.StatusNoContent {
		t.Fatalf("DELETE = %d %s, want 204", code, body)
	}
	if n := rowCount(t, s.deps, `SELECT COUNT(*) FROM ticket_projects WHERE id = '`+ops+`'`); n != 0 {
		t.Errorf("ticket project rows = %d, want it deleted", n)
	}
	if n := rowCount(t, s.deps, `SELECT COUNT(*) FROM installation_settings WHERE default_ticket_project_id = '`+dev+`'`); n != 1 {
		t.Errorf("the default of another project was touched: %d", n)
	}
}
