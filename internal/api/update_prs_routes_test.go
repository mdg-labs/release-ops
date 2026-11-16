package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/mdg-labs/release-ops/internal/github"
	"github.com/mdg-labs/release-ops/internal/store"
)

// seedListedPRs stores two open PRs, one in each of the two repositories, and a merged one in
// repo-2. The open PR of repo-1 has a ticket whose last failure carries a message.
func seedListedPRs(t *testing.T, s *rolesServer) {
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
		{`INSERT INTO update_prs (id, repository_id, number, node_id, url, title, head_ref, state, opened_at, last_seen_at, created_at, updated_at)
			VALUES ('pr-1', 'repo-1', 1, 'n', 'https://github.com/acme/repo-1/pull/1', 'Bump one', 'renovate/one', 'open', ?, ?, ?, ?)`, []any{at, at, at, at}},
		{`INSERT INTO update_prs (id, repository_id, number, node_id, url, title, head_ref, state, opened_at, last_seen_at, created_at, updated_at)
			VALUES ('pr-2', 'repo-2', 2, 'n', 'https://github.com/acme/repo-2/pull/2', 'Bump two', 'renovate/two', 'open', ?, ?, ?, ?)`, []any{at, at, at, at}},
		{`INSERT INTO update_prs (id, repository_id, number, node_id, url, title, head_ref, state, opened_at, merged_at, last_seen_at, created_at, updated_at)
			VALUES ('pr-3', 'repo-2', 3, 'n', 'https://github.com/acme/repo-2/pull/3', 'Bump three', 'renovate/three', 'merged', ?, ?, ?, ?, ?)`, []any{at, at, at, at, at}},
		{`INSERT INTO update_pr_dependencies (id, update_pr_id, name, from_version, to_version, update_type, source) VALUES ('d-1', 'pr-1', 'foo', '1.4.0', '2.0.0', 'major', 'report')`, nil},
		{`INSERT INTO update_pr_dependencies (id, update_pr_id, name, update_type, source) VALUES ('d-2', 'pr-2', 'bar', 'patch', 'body')`, nil},
		{`INSERT INTO update_tickets (id, update_pr_id, ticket_project_id, external_id, external_url, state, last_error, last_error_at, created_at, updated_at)
			VALUES ('ut-1', 'pr-1', ?, 'DEV-12', 'https://tickets.example/DEV-12', 'open', 'PR comment: forbidden for secret-looking-text', ?, ?, ?)`, []any{tp.ID, at, at, at}},
	} {
		if _, err := s.deps.DB.Exec(stmt.query, stmt.args...); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
}

type listedPage struct {
	Items []struct {
		ID         string `json:"id"`
		Repository struct {
			ID string `json:"id"`
		} `json:"repository"`
		UpdateTypes []string `json:"updateTypes"`
		Ticket      *struct {
			State      string  `json:"state"`
			ExternalID *string `json:"externalId"`
			HasError   bool    `json:"hasError"`
		} `json:"ticket"`
	} `json:"items"`
	Total int `json:"total"`
}

func listUpdatePRs(t *testing.T, s *rolesServer, c *http.Client, query string) (listedPage, string) {
	t.Helper()
	code, body := s.send(t, c, http.MethodGet, "/api/v1/update-prs"+query, "")
	if code != http.StatusOK {
		t.Fatalf("GET /update-prs%s = %d %s, want 200", query, code, body)
	}
	var page listedPage
	if err := json.Unmarshal([]byte(body), &page); err != nil {
		t.Fatalf("decode %q: %v", body, err)
	}
	return page, body
}

func listedIDs(p listedPage) []string {
	ids := []string{}
	for _, it := range p.Items {
		ids = append(ids, it.ID)
	}
	slices.Sort(ids)
	return ids
}

func TestAViewerListsOnlyTheUpdatePRsOfTheirRepositories(t *testing.T) {
	t.Parallel()
	// The member can access repository 1 on GitHub, not repository 2.
	s := newRolesServer(t, github.User{ID: rolesMemberID, Login: "acme-member"}, 1)
	viewer := s.signIn(t)
	seedListedPRs(t, s)

	page, body := listUpdatePRs(t, s, viewer, "")
	if got := listedIDs(page); !slices.Equal(got, []string{"pr-1"}) || page.Total != 1 {
		t.Fatalf("viewer open list = %v (total %d), want only pr-1", got, page.Total)
	}
	if strings.Contains(body, "repo-2") || strings.Contains(body, "Bump two") {
		t.Fatalf("a viewer's response mentions a repository outside their scope: %s", body)
	}

	if merged, _ := listUpdatePRs(t, s, viewer, "?state=merged&sort=-mergedAt"); len(merged.Items) != 0 || merged.Total != 0 {
		t.Fatalf("viewer merged list = %v (total %d), want nothing: the merged PR is in repo-2", listedIDs(merged), merged.Total)
	}

	outside, outsideBody := listUpdatePRs(t, s, viewer, "?repositoryId=repo-2")
	_, missingBody := listUpdatePRs(t, s, viewer, "?repositoryId=no-such-repository")
	if len(outside.Items) != 0 || outside.Total != 0 || outsideBody != missingBody {
		t.Fatalf("repositoryId outside the scope = %s, unknown = %s; want the same empty page", outsideBody, missingBody)
	}
}

func TestAnAdminListsTheUpdatePRsOfEveryRepository(t *testing.T) {
	t.Parallel()
	s := newRolesServer(t, github.User{ID: rolesOwnerID, Login: "acme-owner"})
	admin := s.signIn(t)
	seedListedPRs(t, s)

	if page, _ := listUpdatePRs(t, s, admin, ""); !slices.Equal(listedIDs(page), []string{"pr-1", "pr-2"}) || page.Total != 2 {
		t.Fatalf("admin open list = %v (total %d), want pr-1 and pr-2", listedIDs(page), page.Total)
	}
	if page, _ := listUpdatePRs(t, s, admin, "?state=merged&sort=-mergedAt&pageSize=10"); !slices.Equal(listedIDs(page), []string{"pr-3"}) {
		t.Fatalf("admin merged list = %v, want pr-3", listedIDs(page))
	}
	if page, _ := listUpdatePRs(t, s, admin, "?updateType=patch"); !slices.Equal(listedIDs(page), []string{"pr-2"}) {
		t.Fatalf("admin patch list = %v, want pr-2", listedIDs(page))
	}
	if page, _ := listUpdatePRs(t, s, admin, "?repositoryId=repo-1"); !slices.Equal(listedIDs(page), []string{"pr-1"}) {
		t.Fatalf("admin repo-1 list = %v, want pr-1", listedIDs(page))
	}
}

func TestTheUpdatePRListShowsTheTicketButNeverItsErrorText(t *testing.T) {
	t.Parallel()
	for name, user := range map[string]struct {
		user       github.User
		accessible []int64
	}{
		"admin":  {github.User{ID: rolesOwnerID, Login: "acme-owner"}, nil},
		"viewer": {github.User{ID: rolesMemberID, Login: "acme-member"}, []int64{1}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := newRolesServer(t, user.user, user.accessible...)
			c := s.signIn(t)
			seedListedPRs(t, s)

			page, body := listUpdatePRs(t, s, c, "?repositoryId=repo-1")
			if len(page.Items) != 1 || page.Items[0].Ticket == nil {
				t.Fatalf("list = %s, want pr-1 with its ticket", body)
			}
			tk := page.Items[0].Ticket
			if tk.State != "open" || tk.ExternalID == nil || *tk.ExternalID != "DEV-12" || !tk.HasError {
				t.Fatalf("ticket = %+v, want open DEV-12 with an error marker", tk)
			}
			if strings.Contains(body, "secret-looking-text") || strings.Contains(body, "lastError") {
				t.Fatalf("the list exposes the ticket's error text: %s", body)
			}
		})
	}
}

func TestTheUpdatePRListRejectsAMalformedQuery(t *testing.T) {
	t.Parallel()
	s := newRolesServer(t, github.User{ID: rolesMemberID, Login: "acme-member"}, 1)
	viewer := s.signIn(t)
	for _, query := range []string{
		"?state=gone", "?updateType=huge", "?sort=title", "?page=0", "?page=x", "?pageSize=0", "?pageSize=101",
		"?sort=" + url.QueryEscape("openedAt; DROP TABLE update_prs"),
	} {
		code, body := s.send(t, viewer, http.MethodGet, "/api/v1/update-prs"+query, "")
		if code != http.StatusBadRequest || errorCodeOf(body) != "VALIDATION_ERROR" {
			t.Errorf("GET /update-prs%s = %d %s, want 400 VALIDATION_ERROR", query, code, body)
		}
	}
}

func TestTheUpdatePRListNeedsASignedInUser(t *testing.T) {
	t.Parallel()
	s := newRolesServer(t, github.User{ID: rolesOwnerID, Login: "acme-owner"})
	seedListedPRs(t, s)

	code, body := s.send(t, noRedirects(t), http.MethodGet, "/api/v1/update-prs", "")
	if code != http.StatusUnauthorized {
		t.Fatalf("anonymous GET /update-prs = %d %s, want 401", code, body)
	}
}
