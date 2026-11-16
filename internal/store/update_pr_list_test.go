package store_test

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/mdg-labs/release-ops/internal/store"
)

type listPR struct {
	id, repo, state, opened, merged string
	number                          int
	types                           []string
}

// seedListPRs stores PRs for the list tests: repo-1 has pr-a (open, major then minor), pr-b
// (open, patch) and pr-c (merged); repo-2 has pr-d (open, major) and pr-e (merged).
func seedListPRs(t *testing.T, e *ticketEnv) {
	t.Helper()
	prs := []listPR{
		{"pr-a", e.repos[0], "open", "2026-11-10T08:00:00.000Z", "", 1, []string{"major", "minor"}},
		{"pr-b", e.repos[0], "open", "2026-11-12T08:00:00.000Z", "", 2, []string{"patch"}},
		{"pr-c", e.repos[0], "merged", "2026-11-01T08:00:00.000Z", "2026-11-13T08:00:00.000Z", 3, []string{"minor"}},
		{"pr-d", e.repos[1], "open", "2026-11-11T08:00:00.000Z", "", 4, []string{"major"}},
		{"pr-e", e.repos[1], "merged", "2026-11-02T08:00:00.000Z", "2026-11-14T08:00:00.000Z", 5, []string{"patch"}},
	}
	for _, p := range prs {
		var merged any
		if p.merged != "" {
			merged = p.merged
		}
		e.exec(t, `INSERT INTO update_prs (id, repository_id, number, node_id, url, title, head_ref, state, opened_at, merged_at, last_seen_at, created_at, updated_at)
			VALUES (?, ?, ?, 'n', ?, ?, 'renovate/x', ?, ?, ?, ?, ?, ?)`,
			p.id, p.repo, p.number, fmt.Sprintf("https://github.com/acme/x/pull/%d", p.number), "Update "+p.id, p.state, p.opened, merged, p.opened, p.opened, p.opened)
		for i, ut := range p.types {
			e.exec(t, `INSERT INTO update_pr_dependencies (id, update_pr_id, name, from_version, to_version, update_type, source) VALUES (?, ?, ?, '1', '2', ?, 'report')`,
				fmt.Sprintf("%s-dep%d", p.id, i), p.id, fmt.Sprintf("dep-%s-%d", p.id, i), ut)
		}
	}
}

func listIDs(items []store.UpdatePRListItem) []string {
	ids := make([]string, 0, len(items))
	for _, it := range items {
		ids = append(ids, it.ID)
	}
	return ids
}

func listFilter(f store.UpdatePRListFilter) store.UpdatePRListFilter {
	if f.State == "" {
		f.State = store.UpdatePROpen
	}
	if f.Sort == "" {
		f.Sort = store.UpdatePRSortOpenedAtDesc
	}
	f.Page, f.PageSize = 1, 25
	return f
}

func TestUpdatePRListReadsOnlyTheRepositoriesInScope(t *testing.T) {
	e := newTicketEnv(t)
	seedListPRs(t, e)
	l := e.st.UpdatePRList()
	ctx := context.Background()

	t.Run("a limited scope sees only its repositories, in the list and in the count", func(t *testing.T) {
		items, total, err := l.List(ctx, listFilter(store.UpdatePRListFilter{RepositoryIDs: []string{e.repos[0]}}))
		if err != nil {
			t.Fatal(err)
		}
		if got, want := listIDs(items), []string{"pr-b", "pr-a"}; !reflect.DeepEqual(got, want) || total != 2 {
			t.Fatalf("scoped list = %v (total %d), want %v (total 2)", got, total, want)
		}
	})

	t.Run("a repository filter outside the scope answers as an unknown repository does", func(t *testing.T) {
		outside, outsideTotal, err := l.List(ctx, listFilter(store.UpdatePRListFilter{RepositoryIDs: []string{e.repos[0]}, RepositoryID: e.repos[1]}))
		if err != nil {
			t.Fatal(err)
		}
		missing, missingTotal, err := l.List(ctx, listFilter(store.UpdatePRListFilter{RepositoryIDs: []string{e.repos[0]}, RepositoryID: "no-such-repository"}))
		if err != nil {
			t.Fatal(err)
		}
		if len(outside) != 0 || outsideTotal != 0 || !reflect.DeepEqual(outside, missing) || outsideTotal != missingTotal {
			t.Fatalf("outside = %v (%d), missing = %v (%d); both want an empty page", outside, outsideTotal, missing, missingTotal)
		}
	})

	t.Run("an empty scope sees nothing", func(t *testing.T) {
		for name, ids := range map[string][]string{"nil": nil, "empty": {}} {
			items, total, err := l.List(ctx, listFilter(store.UpdatePRListFilter{RepositoryIDs: ids}))
			if err != nil || len(items) != 0 || total != 0 {
				t.Fatalf("%s scope = %v (total %d, err %v), want nothing", name, items, total, err)
			}
		}
	})

	t.Run("the all-repositories scope sees every repository", func(t *testing.T) {
		items, total, err := l.List(ctx, listFilter(store.UpdatePRListFilter{AllRepositories: true}))
		if err != nil {
			t.Fatal(err)
		}
		if got, want := listIDs(items), []string{"pr-b", "pr-d", "pr-a"}; !reflect.DeepEqual(got, want) || total != 3 {
			t.Fatalf("admin list = %v (total %d), want %v (total 3)", got, total, want)
		}
	})

	t.Run("a scope id is data, never part of the statement", func(t *testing.T) {
		items, total, err := l.List(ctx, listFilter(store.UpdatePRListFilter{RepositoryIDs: []string{`x"] ) OR 1=1 --`}}))
		if err != nil || len(items) != 0 || total != 0 {
			t.Fatalf("hostile scope id = %v (total %d, err %v), want nothing", items, total, err)
		}
	})
}

func TestUpdatePRListFiltersSortsAndPages(t *testing.T) {
	e := newTicketEnv(t)
	seedListPRs(t, e)
	l := e.st.UpdatePRList()
	ctx := context.Background()
	all := store.UpdatePRListFilter{AllRepositories: true}

	cases := []struct {
		name   string
		mutate func(*store.UpdatePRListFilter)
		want   []string
		total  int
	}{
		{"open by default order, newest opened first", func(*store.UpdatePRListFilter) {}, []string{"pr-b", "pr-d", "pr-a"}, 3},
		{"oldest opened first", func(f *store.UpdatePRListFilter) { f.Sort = store.UpdatePRSortOpenedAt }, []string{"pr-a", "pr-d", "pr-b"}, 3},
		{"recently merged", func(f *store.UpdatePRListFilter) {
			f.State, f.Sort = store.UpdatePRMerged, store.UpdatePRSortMergedAtDesc
		}, []string{"pr-e", "pr-c"}, 2},
		{"merged, oldest merge first", func(f *store.UpdatePRListFilter) { f.State, f.Sort = store.UpdatePRMerged, store.UpdatePRSortMergedAt }, []string{"pr-c", "pr-e"}, 2},
		{"update type matches any dependency of a grouped PR", func(f *store.UpdatePRListFilter) { f.UpdateType = "minor" }, []string{"pr-a"}, 1},
		{"update type major", func(f *store.UpdatePRListFilter) { f.UpdateType = "major" }, []string{"pr-d", "pr-a"}, 2},
		{"repository", func(f *store.UpdatePRListFilter) { f.RepositoryID = e.repos[1] }, []string{"pr-d"}, 1},
		{"no closed PRs", func(f *store.UpdatePRListFilter) { f.State = store.UpdatePRClosed }, []string{}, 0},
		{"second page of one", func(f *store.UpdatePRListFilter) { f.Page, f.PageSize = 2, 1 }, []string{"pr-d"}, 3},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := listFilter(all)
			c.mutate(&f)
			items, total, err := l.List(ctx, f)
			if err != nil {
				t.Fatal(err)
			}
			if got := listIDs(items); !reflect.DeepEqual(got, c.want) || total != c.total {
				t.Fatalf("list = %v (total %d), want %v (total %d)", got, total, c.want, c.total)
			}
		})
	}
}

func TestUpdatePRListItemCarriesDependenciesTicketAndReplacement(t *testing.T) {
	e := newTicketEnv(t)
	seedListPRs(t, e)
	l := e.st.UpdatePRList()
	ctx := context.Background()
	const at = "2026-11-15T10:00:00.000Z"
	e.exec(t, `INSERT INTO update_tickets (id, update_pr_id, ticket_project_id, external_id, external_url, state, last_error, last_error_at, created_at, updated_at)
		VALUES ('ut-a', 'pr-a', ?, 'DEV-12', 'https://tickets.example.com/DEV-12', 'open', 'token abc rejected', ?, ?, ?)`, e.project, at, at, at)
	e.exec(t, `INSERT INTO update_tickets (id, update_pr_id, ticket_project_id, external_id, external_url, state, created_at, updated_at)
		VALUES ('ut-b', 'pr-b', ?, 'DEV-13', 'https://tickets.example.com/DEV-13', 'done', ?, ?)`, e.project, at, at)
	e.exec(t, `INSERT INTO update_tickets (id, update_pr_id, ticket_project_id, state, pending_since, created_at, updated_at)
		VALUES ('ut-d', 'pr-d', ?, 'pending', ?, ?, ?)`, e.project, at, at, at)
	e.exec(t, `INSERT INTO update_prs (id, repository_id, number, node_id, url, title, head_ref, state, superseded_by_id, last_seen_at, created_at, updated_at)
		VALUES ('pr-old', ?, 9, 'n', 'https://github.com/acme/x/pull/9', 'Old', 'renovate/old', 'superseded', 'pr-a', ?, ?, ?)`, e.repos[0], at, at, at)

	items, _, err := l.List(ctx, listFilter(store.UpdatePRListFilter{AllRepositories: true, Sort: store.UpdatePRSortOpenedAt}))
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]store.UpdatePRListItem{}
	for _, it := range items {
		byID[it.ID] = it
	}
	a := byID["pr-a"]
	if !reflect.DeepEqual(a.UpdateTypes, []string{"major", "minor"}) || len(a.Dependencies) != 2 || a.Dependencies[0].Name != "dep-pr-a-0" || a.Dependencies[0].From != "1" || a.Dependencies[0].To != "2" {
		t.Fatalf("pr-a dependencies = %+v types %v", a.Dependencies, a.UpdateTypes)
	}
	if a.Ticket == nil || *a.Ticket != (store.UpdatePRListTicket{State: "open", ExternalID: "DEV-12", ExternalURL: "https://tickets.example.com/DEV-12", HasError: true}) {
		t.Fatalf("pr-a ticket = %+v, want an open ticket with an error marker", a.Ticket)
	}
	if b := byID["pr-b"]; b.Ticket == nil || b.Ticket.HasError || b.Ticket.State != "done" {
		t.Fatalf("pr-b ticket = %+v, want done without error", b.Ticket)
	}
	if d := byID["pr-d"]; d.Ticket == nil || !d.Ticket.HasError || d.Ticket.State != "pending" || d.Ticket.ExternalID != "" {
		t.Fatalf("pr-d ticket = %+v, want a pending ticket with an unresolved marker", d.Ticket)
	}

	sup, _, err := l.List(ctx, listFilter(store.UpdatePRListFilter{AllRepositories: true, State: store.UpdatePRSuperseded}))
	if err != nil || len(sup) != 1 {
		t.Fatalf("superseded list = %v, err %v", sup, err)
	}
	if got := sup[0]; got.Ticket != nil || got.UpdateTypes == nil || got.Dependencies == nil || got.SupersededBy == nil || *got.SupersededBy != (store.UpdatePRRef{ID: "pr-a", Number: 1, URL: "https://github.com/acme/x/pull/1"}) {
		t.Fatalf("superseded item = %+v", got)
	}
}

func TestUpdatePRListSkipsRemovedRepositoriesAndInstallations(t *testing.T) {
	e := newTicketEnv(t)
	seedListPRs(t, e)
	l := e.st.UpdatePRList()
	ctx := context.Background()

	e.exec(t, `UPDATE repositories SET removed_at = '2026-11-15T10:00:00.000Z' WHERE id = ?`, e.repos[1])
	items, total, err := l.List(ctx, listFilter(store.UpdatePRListFilter{AllRepositories: true}))
	if err != nil || total != 2 || !reflect.DeepEqual(listIDs(items), []string{"pr-b", "pr-a"}) {
		t.Fatalf("after a repository was removed: %v (total %d, err %v)", listIDs(items), total, err)
	}
	e.exec(t, `UPDATE installations SET status = 'removed'`)
	items, total, err = l.List(ctx, listFilter(store.UpdatePRListFilter{AllRepositories: true}))
	if err != nil || total != 0 || len(items) != 0 {
		t.Fatalf("after the installation was removed: %v (total %d, err %v)", listIDs(items), total, err)
	}
}
