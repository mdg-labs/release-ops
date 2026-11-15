package store_test

import (
	"context"
	"slices"
	"testing"
)

func TestAccessLiveInstallationAndInstalledRepositories(t *testing.T) {
	t.Parallel()
	s, db, _ := newGitHubAppStore(t)
	ctx := context.Background()

	if _, ok, err := s.Access().LiveInstallation(ctx); err != nil || ok {
		t.Fatalf("LiveInstallation with none recorded = %v, %v; want not ok", ok, err)
	}
	if _, err := s.Installations().Record(ctx, sampleInstallation(1000)); err != nil {
		t.Fatalf("Record: %v", err)
	}
	inst, ok, err := s.Access().LiveInstallation(ctx)
	if err != nil || !ok || inst.GitHubInstallationID != 1000 || inst.AccountID != 4242 || inst.AccountLogin != "acme" || inst.AccountType != "Organization" || inst.ID == "" {
		t.Fatalf("LiveInstallation = %+v, %v, %v", inst, ok, err)
	}

	const at = "2026-11-14T12:00:00.000Z"
	for _, repo := range []struct {
		id      string
		ghID    int64
		removed string
	}{{"r1", 1, ""}, {"r2", 2, ""}, {"r3", 3, at}} {
		var removed any
		if repo.removed != "" {
			removed = repo.removed
		}
		if _, err := db.Writer.Exec(`INSERT INTO repositories (id, installation_id, github_repository_id, full_name, default_branch, removed_at, created_at, updated_at)
			VALUES (?, ?, ?, ?, 'main', ?, ?, ?)`, repo.id, inst.ID, repo.ghID, "acme/"+repo.id, removed, at, at); err != nil {
			t.Fatalf("seed %s: %v", repo.id, err)
		}
	}
	got, err := s.Access().InstalledRepositoryIDs(ctx, inst.ID)
	if err != nil || len(got) != 2 || got[1] != "r1" || got[2] != "r2" {
		t.Fatalf("InstalledRepositoryIDs = %v, %v; want github 1 and 2 mapped to r1 and r2, the removed one left out", got, err)
	}

	for _, stmt := range []string{
		`INSERT INTO sessions (token, data, expiry) VALUES ('tok', x'00', 2461000.5)`,
		`INSERT INTO session_repositories (session_token, repository_id) VALUES ('tok', 'r2')`,
		`INSERT INTO session_repositories (session_token, repository_id) VALUES ('tok', 'r1')`,
		`INSERT INTO session_repositories (session_token, repository_id) VALUES ('tok', 'r3')`,
	} {
		if _, err := db.Writer.Exec(stmt); err != nil {
			t.Fatalf("seed %q: %v", stmt, err)
		}
	}
	ids, err := s.Access().SessionRepositoryIDs(ctx, "tok")
	if err != nil || !slices.Equal(ids, []string{"r1", "r2"}) {
		t.Fatalf("SessionRepositoryIDs = %v, %v; want r1 and r2: the removed repository is not in scope", ids, err)
	}
	for _, token := range []string{"other-session", ""} {
		if ids, err := s.Access().SessionRepositoryIDs(ctx, token); err != nil || len(ids) != 0 {
			t.Errorf("SessionRepositoryIDs(%q) = %v, %v; want none: a session without a list sees nothing", token, ids, err)
		}
	}
}
