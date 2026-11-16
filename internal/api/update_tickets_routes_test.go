package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/mdg-labs/release-ops/internal/github"
	"github.com/mdg-labs/release-ops/internal/store"
)

// seedStuckTicket stores an update ticket of repo-1 whose create left its pending marker, and
// returns the row id.
func seedStuckTicket(t *testing.T, s *rolesServer) string {
	t.Helper()
	ctx := t.Context()
	base := "https://kaneo.example"
	integ, err := s.deps.Store.Integrations().Create(ctx, store.CreateIntegrationInput{Kind: "kaneo", Name: "Kaneo", BaseURL: &base, Secret: []byte(`{"api_key":"k"}`)})
	if err != nil {
		t.Fatal(err)
	}
	tp, err := s.deps.Store.TicketProjects().Create(ctx, store.CreateTicketProjectInput{
		IntegrationID: integ.ID, ExternalProjectID: "DEV", Name: "DEV", CreateConfig: `{}`, StatusMapping: `{}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	const at = "2026-11-16T08:00:00.000Z"
	for _, stmt := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO update_prs (id, repository_id, number, node_id, url, title, head_ref, state, last_seen_at, created_at, updated_at)
			VALUES ('pr-1', 'repo-1', 1, 'n', 'https://github.com/acme/repo-1/pull/1', 'Bump', 'renovate/x', 'open', ?, ?, ?)`, []any{at, at, at}},
		{`INSERT INTO update_tickets (id, update_pr_id, ticket_project_id, state, pending_since, attempts, next_attempt_at, last_error, last_error_at, created_at, updated_at)
			VALUES ('ut-1', 'pr-1', ?, 'pending', ?, 1, ?, 'create ticket: unexpected status 503', ?, ?, ?)`, []any{tp.ID, at, at, at, at, at}},
	} {
		if _, err := s.deps.DB.Exec(stmt.query, stmt.args...); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	return "ut-1"
}

func stuckMarkerCleared(t *testing.T, s *rolesServer, id string) bool {
	t.Helper()
	return rowCount(t, s.deps, `SELECT COUNT(*) FROM update_tickets WHERE id = '`+id+`' AND pending_since IS NULL AND last_error IS NULL AND attempts = 0 AND next_attempt_at IS NULL`) == 1
}

func TestAnAdminClearsAStuckPendingMarkerThroughTheRouter(t *testing.T) {
	t.Parallel()
	s := newRolesServer(t, github.User{ID: rolesOwnerID, Login: "acme-owner"})
	admin := s.signIn(t)
	id := seedStuckTicket(t, s)
	path := "/api/v1/update-tickets/" + id + "/clear-pending-marker"

	code, body := s.send(t, admin, http.MethodPost, path, "")
	if code != http.StatusOK || !strings.Contains(body, id) {
		t.Fatalf("clear = %d %s, want 200 naming the ticket", code, body)
	}
	if !stuckMarkerCleared(t, s, id) {
		t.Fatal("the marker, its error and its retry are still set after the clear")
	}
	if n := rowCount(t, s.deps, `SELECT COUNT(*) FROM update_tickets`); n != 1 {
		t.Fatalf("update_tickets rows = %d, want the same row kept", n)
	}

	code, body = s.send(t, admin, http.MethodPost, path, "")
	if code != http.StatusConflict || errorCodeOf(body) != "NO_PENDING_MARKER" {
		t.Fatalf("second clear = %d %s, want 409 NO_PENDING_MARKER", code, body)
	}
	code, body = s.send(t, admin, http.MethodPost, "/api/v1/update-tickets/missing/clear-pending-marker", "")
	if code != http.StatusNotFound || errorCodeOf(body) != "NOT_FOUND" {
		t.Fatalf("unknown ticket = %d %s, want 404", code, body)
	}
}

func TestAViewerCannotClearAPendingMarker(t *testing.T) {
	t.Parallel()
	s := newRolesServer(t, github.User{ID: rolesMemberID, Login: "acme-member"}, 1)
	viewer := s.signIn(t)
	id := seedStuckTicket(t, s)

	code, body := s.send(t, viewer, http.MethodPost, "/api/v1/update-tickets/"+id+"/clear-pending-marker", "")
	if code != http.StatusForbidden || errorCodeOf(body) != "FORBIDDEN" {
		t.Fatalf("viewer clear = %d %s, want 403 FORBIDDEN", code, body)
	}
	if stuckMarkerCleared(t, s, id) {
		t.Fatal("a viewer cleared the marker")
	}
}

func TestClearingAPendingMarkerNeedsASignedInUser(t *testing.T) {
	t.Parallel()
	s := newRolesServer(t, github.User{ID: rolesOwnerID, Login: "acme-owner"})
	id := seedStuckTicket(t, s)

	code, _ := s.send(t, noRedirects(t), http.MethodPost, "/api/v1/update-tickets/"+id+"/clear-pending-marker", "")
	if code != http.StatusUnauthorized {
		t.Fatalf("anonymous clear = %d, want 401", code)
	}
	if stuckMarkerCleared(t, s, id) {
		t.Fatal("an anonymous request cleared the marker")
	}
}
