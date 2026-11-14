package github_test

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/github"
	"github.com/mdg-labs/release-ops/internal/github/githubtest"
	"github.com/mdg-labs/release-ops/internal/store"
	"github.com/mdg-labs/release-ops/internal/store/storetest"
)

type syncRig struct {
	fake   *githubtest.Fake
	db     *store.DB
	syncer *github.Syncer
}

func newSyncRig(t *testing.T) *syncRig {
	t.Helper()
	return newSyncRigWithCreds(t, nil)
}

func newSyncRigWithCreds(t *testing.T, creds github.CredentialsSource) *syncRig {
	t.Helper()
	fake := githubtest.New(t, time.Now)
	fake.AddInstallation(1000, githubtest.AppID)
	if creds == nil {
		creds = staticCreds{appID: githubtest.AppID, pem: fake.PEM}
	}
	app := github.NewAppClient(github.NewClient(fake.URL, 5*time.Second), creds, github.AppClientOptions{})
	db := storetest.OpenMigrated(t)
	return &syncRig{fake: fake, db: db, syncer: github.NewSyncer(app, store.New(db, nil).InstallationSync())}
}

type repoRow struct {
	id            string
	fullName      string
	defaultBranch string
	enabled       int
	configState   string
	removed       bool
}

func (r *syncRig) repos(t *testing.T) map[int64]repoRow {
	t.Helper()
	rows, err := r.db.Reader.Query(`SELECT github_repository_id, id, full_name, default_branch, enabled, config_state, removed_at IS NOT NULL FROM repositories`)
	if err != nil {
		t.Fatalf("read repositories: %v", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[int64]repoRow{}
	for rows.Next() {
		var ghID int64
		var row repoRow
		if err := rows.Scan(&ghID, &row.id, &row.fullName, &row.defaultBranch, &row.enabled, &row.configState, &row.removed); err != nil {
			t.Fatalf("scan repository: %v", err)
		}
		out[ghID] = row
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read repositories: %v", err)
	}
	return out
}

type installRow struct {
	status, login, typ, selection, permissions, updatedAt string
	accountID                                             int64
}

func (r *syncRig) installation(t *testing.T, ghID int64) (installRow, bool) {
	t.Helper()
	var row installRow
	err := r.db.Reader.QueryRow(`SELECT status, account_login, account_type, repository_selection, permissions, updated_at, account_id FROM installations WHERE github_installation_id = ?`, ghID).
		Scan(&row.status, &row.login, &row.typ, &row.selection, &row.permissions, &row.updatedAt, &row.accountID)
	if errors.Is(err, sql.ErrNoRows) {
		return installRow{}, false
	}
	if err != nil {
		t.Fatalf("read installation: %v", err)
	}
	return row, true
}

func (r *syncRig) mustSync(t *testing.T) {
	t.Helper()
	if err := r.syncer.Sync(context.Background()); err != nil {
		t.Fatalf("Sync: %v", err)
	}
}

func seedWidgets(r *syncRig) {
	r.fake.SetRepositories(1000,
		githubtest.Repo{ID: 7001, FullName: "acme/widgets", DefaultBranch: "main"},
		githubtest.Repo{ID: 7002, FullName: "acme/gadgets", DefaultBranch: "trunk"},
		githubtest.Repo{ID: 7003, FullName: "acme/gizmos", DefaultBranch: "main"},
		githubtest.Repo{ID: 7004, FullName: "acme/doohickeys", DefaultBranch: "main"},
	)
	r.fake.SetFiles("acme/widgets", "README.md", "renovate.json")
	r.fake.SetFiles("acme/gadgets", "README.md", ".github/renovate.json5", ".github/workflows/ci.yml")
	r.fake.SetFiles("acme/gizmos", "README.md", "src/main.go")
	r.fake.SetFiles("acme/doohickeys", "README.md", ".github/CODEOWNERS", ".gitlab/ci.yml")
}

func TestSyncRecordsTheInstallationAndItsRepositoriesFromTheAPI(t *testing.T) {
	t.Parallel()

	r := newSyncRig(t)
	seedWidgets(r)
	r.mustSync(t)

	inst, ok := r.installation(t, 1000)
	if !ok || inst.status != "active" || inst.login != "acme" || inst.typ != "Organization" || inst.accountID != 4242 || inst.selection != "selected" {
		t.Fatalf("installation = %+v (found %v)", inst, ok)
	}
	if !strings.Contains(inst.permissions, `"contents":"write"`) || !strings.Contains(inst.permissions, `"pull_requests":"write"`) {
		t.Errorf("permissions = %s, want what GitHub reported", inst.permissions)
	}

	got := r.repos(t)
	want := map[int64]repoRow{
		7001: {fullName: "acme/widgets", defaultBranch: "main", enabled: 1, configState: "present"},
		7002: {fullName: "acme/gadgets", defaultBranch: "trunk", enabled: 1, configState: "present"},
		7003: {fullName: "acme/gizmos", defaultBranch: "main", enabled: 1, configState: "missing"},
		7004: {fullName: "acme/doohickeys", defaultBranch: "main", enabled: 1, configState: "missing"},
	}
	if len(got) != len(want) {
		t.Fatalf("repositories = %+v, want %d", got, len(want))
	}
	for id, w := range want {
		g := got[id]
		g.id = ""
		if g != w {
			t.Errorf("repository %d = %+v, want %+v", id, g, w)
		}
	}
}

func TestSyncRecognisesEveryRenovateConfigLocation(t *testing.T) {
	t.Parallel()

	locations := []string{
		"renovate.json", "renovate.jsonc", "renovate.json5",
		".renovaterc", ".renovaterc.json", ".renovaterc.jsonc", ".renovaterc.json5",
		".github/renovate.json", ".github/renovate.jsonc", ".github/renovate.json5",
		".gitlab/renovate.json", ".gitlab/renovate.jsonc", ".gitlab/renovate.json5",
	}
	r := newSyncRig(t)
	var repos []githubtest.Repo
	for i, loc := range locations {
		name := "acme/repo-" + string(rune('a'+i))
		repos = append(repos, githubtest.Repo{ID: int64(8000 + i), FullName: name, DefaultBranch: "main"})
		r.fake.SetFiles(name, "README.md", loc)
	}
	nearMisses := []string{"renovate.yml", "docs/renovate.json", ".github/workflows/renovate.json", "package.json", ".gitlab/renovate.yaml"}
	for i, loc := range nearMisses {
		name := "acme/miss-" + string(rune('a'+i))
		repos = append(repos, githubtest.Repo{ID: int64(9000 + i), FullName: name, DefaultBranch: "main"})
		r.fake.SetFiles(name, "README.md", loc)
	}
	r.fake.SetRepositories(1000, repos...)
	r.mustSync(t)

	got := r.repos(t)
	for i, loc := range locations {
		if s := got[int64(8000+i)].configState; s != "present" {
			t.Errorf("%s: config_state = %s, want present", loc, s)
		}
	}
	for i, loc := range nearMisses {
		if s := got[int64(9000+i)].configState; s != "missing" {
			t.Errorf("%s: config_state = %s, want missing", loc, s)
		}
	}
}

func TestSyncFollowsLinkHeadersAcrossPages(t *testing.T) {
	t.Parallel()

	r := newSyncRig(t)
	seedWidgets(r)
	r.fake.SetPageSize(1)
	r.mustSync(t)

	if got := len(r.repos(t)); got != 4 {
		t.Fatalf("repositories after a paged sync = %d, want 4", got)
	}
}

func TestSyncNeverFollowsANextLinkToAnotherHost(t *testing.T) {
	t.Parallel()

	var hits atomic.Int64
	elsewhere := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	t.Cleanup(elsewhere.Close)

	r := newSyncRig(t)
	seedWidgets(r)
	r.fake.SetPageSize(1)
	r.fake.LinkTo(elsewhere.URL)

	err := r.syncer.Sync(context.Background())
	if !errors.Is(err, github.ErrRequestFailed) {
		t.Fatalf("Sync = %v, want ErrRequestFailed", err)
	}
	if hits.Load() != 0 {
		t.Fatalf("the foreign host received %d requests, want none: a credential must not leave the API host", hits.Load())
	}
	if len(r.repos(t)) != 0 {
		t.Errorf("a partial listing was stored")
	}
}

func TestSyncSecondPassChangesNothingAndDoesNotReadRecordedConfigs(t *testing.T) {
	t.Parallel()

	r := newSyncRig(t)
	seedWidgets(r)
	r.mustSync(t)
	first, _ := r.installation(t, 1000)
	var updated string
	if err := r.db.Reader.QueryRow(`SELECT MAX(updated_at) FROM repositories`).Scan(&updated); err != nil {
		t.Fatal(err)
	}
	before := len(r.fake.Requests())

	r.mustSync(t)

	second, _ := r.installation(t, 1000)
	if second != first {
		t.Errorf("installation changed on an unchanged sync: %+v -> %+v", first, second)
	}
	var updatedAfter string
	if err := r.db.Reader.QueryRow(`SELECT MAX(updated_at) FROM repositories`).Scan(&updatedAfter); err != nil {
		t.Fatal(err)
	}
	if updatedAfter != updated {
		t.Errorf("repositories rewritten by an unchanged sync: %s -> %s", updated, updatedAfter)
	}
	again := 0
	for _, req := range r.fake.Requests()[before:] {
		if !strings.Contains(req.Path, "/contents") {
			continue
		}
		again++
		if strings.Contains(req.Path, "acme/widgets") || strings.Contains(req.Path, "acme/gadgets") {
			t.Errorf("a recorded config was read again: %s", req.Path)
		}
	}
	if again == 0 {
		t.Errorf("repositories without a config were not looked at again")
	}
}

func TestSyncKeepsAPausedRepositoryPaused(t *testing.T) {
	t.Parallel()

	r := newSyncRig(t)
	seedWidgets(r)
	r.mustSync(t)
	if _, err := r.db.Writer.Exec(`UPDATE repositories SET enabled = 0 WHERE github_repository_id = 7001`); err != nil {
		t.Fatal(err)
	}
	r.fake.SetRepositories(1000, githubtest.Repo{ID: 7001, FullName: "acme/widgets", DefaultBranch: "develop"})
	r.mustSync(t)

	got := r.repos(t)[7001]
	if got.enabled != 0 || got.defaultBranch != "develop" {
		t.Errorf("repository = %+v, want still paused with the new default branch", got)
	}
}

func TestSyncMarksARemovedRepositoryAndKeepsItsHistory(t *testing.T) {
	t.Parallel()

	r := newSyncRig(t)
	seedWidgets(r)
	r.mustSync(t)
	widgets := r.repos(t)[7001]
	if _, err := r.db.Writer.Exec(
		`INSERT INTO update_prs (id, repository_id, number, node_id, url, title, head_ref, state, last_seen_at, created_at, updated_at)
		 VALUES ('pr-1', ?, 12, 'PR_node', 'https://github.com/acme/widgets/pull/12', 'Update dep', 'renovate/dep', 'merged', '2026-11-14T12:00:00.000Z', '2026-11-14T12:00:00.000Z', '2026-11-14T12:00:00.000Z')`,
		widgets.id,
	); err != nil {
		t.Fatalf("seed update pr: %v", err)
	}

	r.fake.SetRepositories(1000, githubtest.Repo{ID: 7002, FullName: "acme/gadgets", DefaultBranch: "trunk"})
	r.mustSync(t)

	got := r.repos(t)
	if !got[7001].removed || got[7001].id != widgets.id {
		t.Fatalf("widgets = %+v, want the same row marked removed", got[7001])
	}
	if got[7002].removed {
		t.Errorf("gadgets is still listed but was marked removed")
	}
	if !got[7003].removed || !got[7004].removed {
		t.Errorf("repositories the installation no longer lists were not marked removed: %+v", got)
	}
	var prs int
	if err := r.db.Reader.QueryRow(`SELECT COUNT(*) FROM update_prs WHERE repository_id = ?`, widgets.id).Scan(&prs); err != nil || prs != 1 {
		t.Fatalf("update PR history = %d, %v, want it kept", prs, err)
	}

	r.fake.SetRepositories(1000, githubtest.Repo{ID: 7001, FullName: "acme/widgets", DefaultBranch: "main"})
	r.mustSync(t)
	back := r.repos(t)[7001]
	if back.removed || back.id != widgets.id {
		t.Errorf("returned repository = %+v, want the same row with removed_at cleared", back)
	}
}

func TestSyncRenameOntoANameAnotherRepositoryHeld(t *testing.T) {
	t.Parallel()

	r := newSyncRig(t)
	r.fake.SetRepositories(1000, githubtest.Repo{ID: 1, FullName: "acme/tool", DefaultBranch: "main"})
	r.fake.SetFiles("acme/tool", "renovate.json")
	r.mustSync(t)

	// The first repository is renamed, and a new one takes its old name.
	r.fake.SetRepositories(1000,
		githubtest.Repo{ID: 2, FullName: "acme/tool", DefaultBranch: "main"},
		githubtest.Repo{ID: 1, FullName: "acme/tool-legacy", DefaultBranch: "main"},
	)
	r.fake.SetFiles("acme/tool-legacy", "renovate.json")
	r.mustSync(t)

	got := r.repos(t)
	if got[1].fullName != "acme/tool-legacy" || got[2].fullName != "acme/tool" || got[1].removed || got[2].removed {
		t.Fatalf("repositories = %+v, want 1 renamed and 2 holding the old name", got)
	}
}

func TestSyncSuspendedInstallationKeepsItsRowsAndListsNoRepositories(t *testing.T) {
	t.Parallel()

	r := newSyncRig(t)
	seedWidgets(r)
	r.mustSync(t)

	r.fake.SuspendInstallation(1000, true)
	r.fake.FailTokens(http.StatusForbidden) // GitHub refuses a token for a suspended installation
	r.mustSync(t)

	inst, _ := r.installation(t, 1000)
	if inst.status != "suspended" {
		t.Fatalf("status = %s, want suspended", inst.status)
	}
	got := r.repos(t)
	if len(got) != 4 || got[7001].removed {
		t.Errorf("repositories of a suspended installation = %+v, want them all kept", got)
	}

	r.fake.SuspendInstallation(1000, false)
	r.fake.FailTokens(0)
	r.mustSync(t)
	if inst, _ := r.installation(t, 1000); inst.status != "active" {
		t.Errorf("status after unsuspend = %s, want active", inst.status)
	}
}

func TestSyncDeletedInstallationIsMarkedRemovedAndItsRepositoriesLeaveIt(t *testing.T) {
	t.Parallel()

	r := newSyncRig(t)
	seedWidgets(r)
	r.mustSync(t)

	r.fake.RemoveInstallation(1000)
	r.mustSync(t)

	if inst, ok := r.installation(t, 1000); !ok || inst.status != "removed" {
		t.Fatalf("installation = %+v (found %v), want the row kept as removed", inst, ok)
	}
	got := r.repos(t)
	if len(got) != 4 {
		t.Fatalf("repositories = %d, want the rows kept", len(got))
	}
	for id, row := range got {
		if !row.removed {
			t.Errorf("repository %d of a removed installation is not marked removed", id)
		}
	}
}

func TestSyncReinstalledAppTakesTheRemovedInstallationsPlaceInOnePass(t *testing.T) {
	t.Parallel()

	r := newSyncRig(t)
	seedWidgets(r)
	r.mustSync(t)

	r.fake.RemoveInstallation(1000)
	r.fake.AddInstallation(2000, githubtest.AppID)
	r.fake.SetRepositories(2000, githubtest.Repo{ID: 7001, FullName: "acme/widgets", DefaultBranch: "main"})
	r.fake.SetFiles("acme/widgets", "renovate.json")
	r.mustSync(t)

	if old, _ := r.installation(t, 1000); old.status != "removed" {
		t.Errorf("old installation = %s, want removed", old.status)
	}
	if now, ok := r.installation(t, 2000); !ok || now.status != "active" {
		t.Fatalf("new installation = %+v (found %v), want active", now, ok)
	}
	var installationOfWidgets, newInstallation string
	if err := r.db.Reader.QueryRow(`SELECT installation_id FROM repositories WHERE github_repository_id = 7001`).Scan(&installationOfWidgets); err != nil {
		t.Fatal(err)
	}
	if err := r.db.Reader.QueryRow(`SELECT id FROM installations WHERE github_installation_id = 2000`).Scan(&newInstallation); err != nil {
		t.Fatal(err)
	}
	if installationOfWidgets != newInstallation || r.repos(t)[7001].removed {
		t.Errorf("widgets belongs to %s (removed %v), want the new installation %s", installationOfWidgets, r.repos(t)[7001].removed, newInstallation)
	}
}

func TestSyncOnlyTheInstancesOneInstallationIsRecorded(t *testing.T) {
	t.Parallel()

	r := newSyncRig(t)
	seedWidgets(r)
	r.fake.AddInstallation(2000, githubtest.AppID)
	r.fake.SetRepositories(2000, githubtest.Repo{ID: 9999, FullName: "other/repo", DefaultBranch: "main"})
	r.mustSync(t)

	if _, ok := r.installation(t, 2000); ok {
		t.Errorf("a second installation was recorded")
	}
	if _, ok := r.repos(t)[9999]; ok {
		t.Errorf("a repository of the second installation was recorded")
	}
	if len(r.repos(t)) != 4 {
		t.Errorf("repositories = %d, want the first installation's four", len(r.repos(t)))
	}
}

func TestSyncDoesNothingWhileNoAppIsStored(t *testing.T) {
	t.Parallel()

	r := newSyncRigWithCreds(t, staticCreds{err: store.ErrGitHubAppNotFound})
	if err := r.syncer.Sync(context.Background()); err != nil {
		t.Fatalf("Sync without an App = %v, want nil", err)
	}
	if got := len(r.fake.Requests()); got != 0 {
		t.Errorf("requests = %d, want none", got)
	}
}

func TestSyncFailedInstallationListChangesNothingAndRemovesNothing(t *testing.T) {
	t.Parallel()

	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusInternalServerError, http.StatusBadGateway} {
		r := newSyncRig(t)
		seedWidgets(r)
		r.mustSync(t)

		r.fake.FailInstallationList(status)
		if err := r.syncer.Sync(context.Background()); err == nil {
			t.Fatalf("status %d: Sync = nil, want the failure", status)
		}
		if inst, _ := r.installation(t, 1000); inst.status != "active" {
			t.Errorf("status %d: installation = %s, want active: an unreadable list is not a deletion", status, inst.status)
		}
		for id, row := range r.repos(t) {
			if row.removed {
				t.Errorf("status %d: repository %d marked removed", status, id)
			}
		}
	}
}

func TestSyncFailedRepositoryListKeepsTheRowsAndDoesNotStopOtherInstallations(t *testing.T) {
	t.Parallel()

	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusInternalServerError} {
		r := newSyncRig(t)
		seedWidgets(r)
		r.mustSync(t)

		// The installation of a reinstalled App fails, while the removal of the old one in the
		// same pass still happens.
		r.fake.RemoveInstallation(1000)
		r.fake.AddInstallation(2000, githubtest.AppID)
		r.fake.FailRepositories(2000, status)
		err := r.syncer.Sync(context.Background())
		if err == nil || !strings.Contains(err.Error(), "installation 2000") {
			t.Fatalf("status %d: Sync = %v, want the failure of installation 2000", status, err)
		}
		if old, _ := r.installation(t, 1000); old.status != "removed" {
			t.Errorf("status %d: old installation = %s, want removed despite the other failing", status, old.status)
		}
	}

	r := newSyncRig(t)
	seedWidgets(r)
	r.mustSync(t)
	r.fake.FailRepositories(1000, http.StatusBadGateway)
	r.fake.SetRepositories(1000, githubtest.Repo{ID: 7001, FullName: "acme/widgets", DefaultBranch: "main"})
	if err := r.syncer.Sync(context.Background()); err == nil {
		t.Fatalf("Sync = nil, want the failure")
	}
	for id, row := range r.repos(t) {
		if row.removed {
			t.Errorf("repository %d marked removed by a failed listing", id)
		}
	}
}

func TestSyncUnreadableConfigOfOneRepositoryDoesNotStopTheOthers(t *testing.T) {
	t.Parallel()

	r := newSyncRig(t)
	seedWidgets(r)
	r.fake.FailContents("acme/gadgets", http.StatusInternalServerError)

	err := r.syncer.Sync(context.Background())
	if err == nil || !strings.Contains(err.Error(), "1 repositories") {
		t.Fatalf("Sync = %v, want the one unreadable config reported", err)
	}
	got := r.repos(t)
	if len(got) != 4 {
		t.Fatalf("repositories = %d, want all four recorded", len(got))
	}
	if got[7002].configState != "unknown" {
		t.Errorf("gadgets config_state = %s, want unknown: an unreadable config is not a missing one", got[7002].configState)
	}
	if got[7001].configState != "present" || got[7003].configState != "missing" {
		t.Errorf("the other repositories = %+v, want them checked", got)
	}

	r.fake.FailContents("acme/gadgets", 0)
	r.mustSync(t)
	if s := r.repos(t)[7002].configState; s != "present" {
		t.Errorf("gadgets config_state after recovery = %s, want present", s)
	}
}

func TestRunSyncsAtStartOnEveryTickAndOnTrigger(t *testing.T) {
	t.Parallel()

	r := newSyncRig(t)
	seedWidgets(r)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ticks := make(chan time.Time)
	reports := make(chan error)
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.syncer.Run(ctx, ticks, func(err error) { reports <- err })
	}()
	next := func() {
		t.Helper()
		select {
		case err := <-reports:
			if err != nil {
				t.Fatalf("pass failed: %v", err)
			}
		case <-time.After(20 * time.Second):
			t.Fatal("no pass reported")
		}
	}

	next()
	if got := len(r.repos(t)); got != 4 {
		t.Fatalf("repositories after the start-up pass = %d, want 4", got)
	}

	r.fake.SetRepositories(1000, githubtest.Repo{ID: 7001, FullName: "acme/widgets", DefaultBranch: "main"})
	ticks <- time.Now()
	next()
	if !r.repos(t)[7002].removed {
		t.Errorf("a tick did not run a pass")
	}

	r.fake.SetRepositories(1000,
		githubtest.Repo{ID: 7001, FullName: "acme/widgets", DefaultBranch: "main"},
		githubtest.Repo{ID: 7002, FullName: "acme/gadgets", DefaultBranch: "trunk"},
	)
	r.syncer.Trigger()
	next()
	if r.repos(t)[7002].removed {
		t.Errorf("a trigger did not run a pass")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("Run did not stop with its context")
	}
}
