package hooks_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mdg-labs/release-ops/internal/api/hooks"
	"github.com/mdg-labs/release-ops/internal/store"
	"github.com/mdg-labs/release-ops/internal/store/storetest"
)

const secret = "fixture-webhook-secret-value"

type secrets struct{}

func (secrets) WebhookSecret(context.Context) ([]byte, error) { return []byte(secret), nil }

type rig struct {
	srv     *httptest.Server
	db      *store.DB
	changed atomic.Int64
}

func newRig(t *testing.T) *rig {
	t.Helper()
	db := storetest.OpenMigrated(t)
	st := store.New(db, nil)
	r := &rig{db: db}
	routes := hooks.NewRoutes()
	hooks.RegisterInstallationHandlers(routes, st.InstallationSync(), func() { r.changed.Add(1) })
	r.srv = httptest.NewServer(&hooks.Receiver{Secrets: secrets{}, Deliveries: st.WebhookDeliveries(), Routes: routes})
	t.Cleanup(r.srv.Close)
	return r
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return b
}

func (r *rig) deliver(t *testing.T, event, guid string, body []byte) int {
	t.Helper()
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	req, err := http.NewRequest(http.MethodPost, r.srv.URL, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	req.Header.Set("X-GitHub-Event", event)
	req.Header.Set("X-GitHub-Delivery", guid)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("deliver: %v", err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode
}

func (r *rig) mustDeliver(t *testing.T, event, guid, fixtureName string) {
	t.Helper()
	if got := r.deliver(t, event, guid, fixture(t, fixtureName)); got != http.StatusAccepted {
		t.Fatalf("%s %s = %d, want 202", event, fixtureName, got)
	}
}

type repoRow struct {
	fullName, defaultBranch, configState, removedAt, updatedAt string
	enabled                                                    int
}

func (r *rig) repos(t *testing.T) map[int64]repoRow {
	t.Helper()
	rows, err := r.db.Reader.Query(`SELECT github_repository_id, full_name, default_branch, config_state, COALESCE(removed_at, ''), updated_at, enabled FROM repositories`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	out := map[int64]repoRow{}
	for rows.Next() {
		var id int64
		var row repoRow
		if err := rows.Scan(&id, &row.fullName, &row.defaultBranch, &row.configState, &row.removedAt, &row.updatedAt, &row.enabled); err != nil {
			t.Fatal(err)
		}
		out[id] = row
	}
	return out
}

func (r *rig) installationStatus(t *testing.T, ghID int64) (status, permissions string, ok bool) {
	t.Helper()
	err := r.db.Reader.QueryRow(`SELECT status, permissions FROM installations WHERE github_installation_id = ?`, ghID).Scan(&status, &permissions)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", false
	}
	if err != nil {
		t.Fatal(err)
	}
	return status, permissions, true
}

func (r *rig) deliveries(t *testing.T) int {
	t.Helper()
	var n int
	if err := r.db.Reader.QueryRow(`SELECT COUNT(*) FROM webhook_deliveries`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestInstallationCreatedRecordsTheInstallationAndItsRepositories(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.mustDeliver(t, "installation", "g-1", "installation_created.json")

	status, permissions, ok := r.installationStatus(t, 1000)
	if !ok || status != "active" || !strings.Contains(permissions, `"pull_requests":"write"`) {
		t.Fatalf("installation = %q %q (found %v)", status, permissions, ok)
	}
	got := r.repos(t)[7001]
	if got.fullName != "acme/widgets" || got.enabled != 1 || got.configState != "unknown" || got.removedAt != "" {
		t.Errorf("repository = %+v, want a new enabled row with its config not yet known", got)
	}
	if r.changed.Load() != 1 {
		t.Errorf("sync nudged %d times, want once so the default branch and config get filled in", r.changed.Load())
	}
}

func TestInstallationRepositoriesAddedAndRemovedChangeTheRepositoryRows(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.mustDeliver(t, "installation", "g-1", "installation_created.json")
	r.mustDeliver(t, "installation_repositories", "g-2", "installation_repositories_added.json")

	got := r.repos(t)
	if len(got) != 2 || got[7002].fullName != "acme/gadgets" || got[7002].removedAt != "" {
		t.Fatalf("repositories after added = %+v, want widgets and gadgets", got)
	}

	r.mustDeliver(t, "installation_repositories", "g-3", "installation_repositories_removed.json")
	got = r.repos(t)
	if got[7002].removedAt == "" || got[7001].removedAt != "" {
		t.Errorf("repositories after removed = %+v, want only gadgets marked removed", got)
	}
}

func TestInstallationRepositoriesRemovalOnlyDeliveryRequestsASync(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.mustDeliver(t, "installation", "g-1", "installation_created.json")
	r.mustDeliver(t, "installation_repositories", "g-2", "installation_repositories_added.json")
	before := r.changed.Load()

	r.mustDeliver(t, "installation_repositories", "g-3", "installation_repositories_removed.json")
	if got := r.changed.Load() - before; got != 1 {
		t.Errorf("a removal-only delivery nudged the sync %d times, want once so a removal that arrived after a newer addition is reconciled", got)
	}
}

func TestInstallationRepositoriesReplayedUnderANewDeliveryChangesNothing(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.mustDeliver(t, "installation", "g-1", "installation_created.json")
	r.mustDeliver(t, "installation_repositories", "g-2", "installation_repositories_added.json")
	r.mustDeliver(t, "installation_repositories", "g-3", "installation_repositories_removed.json")
	afterOnce := r.repos(t)

	r.mustDeliver(t, "installation_repositories", "g-4", "installation_repositories_removed.json")
	if got := r.repos(t); !sameRows(got, afterOnce) {
		t.Errorf("a replayed removal changed the rows: %+v -> %+v", afterOnce, got)
	}

	r.mustDeliver(t, "installation_repositories", "g-5", "installation_repositories_added.json")
	afterAdd := r.repos(t)
	r.mustDeliver(t, "installation_repositories", "g-6", "installation_repositories_added.json")
	if got := r.repos(t); !sameRows(got, afterAdd) {
		t.Errorf("a replayed add changed the rows: %+v -> %+v", afterAdd, got)
	}
	if afterAdd[7002].removedAt != "" {
		t.Errorf("a repository that returned is still marked removed: %+v", afterAdd[7002])
	}
	if len(afterAdd) != 2 {
		t.Errorf("repositories = %d, want the returned one to be the same row", len(afterAdd))
	}
}

func sameRows(a, b map[int64]repoRow) bool {
	if len(a) != len(b) {
		return false
	}
	for id, row := range a {
		if b[id] != row {
			return false
		}
	}
	return true
}

func TestInstallationRepositoriesAddedKeepsWhatTheSyncRecordedAndAPause(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.mustDeliver(t, "installation", "g-1", "installation_created.json")
	if _, err := r.db.Writer.Exec(`UPDATE repositories SET default_branch = 'trunk', config_state = 'present', enabled = 0 WHERE github_repository_id = 7001`); err != nil {
		t.Fatal(err)
	}
	r.mustDeliver(t, "installation", "g-2", "installation_created.json")

	got := r.repos(t)[7001]
	if got.defaultBranch != "trunk" || got.configState != "present" || got.enabled != 0 {
		t.Errorf("repository = %+v, a payload without them must not reset the default branch, the config state or a pause", got)
	}
}

func TestInstallationDeletedKeepsTheRowsAndMarksThemRemoved(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.mustDeliver(t, "installation", "g-1", "installation_created.json")
	r.mustDeliver(t, "installation_repositories", "g-2", "installation_repositories_added.json")
	r.mustDeliver(t, "installation", "g-3", "installation_deleted.json")

	if status, _, ok := r.installationStatus(t, 1000); !ok || status != "removed" {
		t.Fatalf("installation = %q (found %v), want the row kept as removed", status, ok)
	}
	got := r.repos(t)
	if len(got) != 2 || got[7001].removedAt == "" || got[7002].removedAt == "" {
		t.Errorf("repositories = %+v, want both rows kept and marked removed", got)
	}

	before := r.repos(t)
	r.mustDeliver(t, "installation", "g-4", "installation_deleted.json")
	if !sameRows(r.repos(t), before) {
		t.Errorf("a replayed deletion changed the rows")
	}
}

func TestInstallationSuspendAndUnsuspendChangeOnlyTheStatus(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.mustDeliver(t, "installation", "g-1", "installation_created.json")
	before := r.repos(t)

	r.mustDeliver(t, "installation", "g-2", "installation_suspend.json")
	if status, _, _ := r.installationStatus(t, 1000); status != "suspended" {
		t.Fatalf("status after suspend = %q, want suspended", status)
	}
	if !sameRows(r.repos(t), before) {
		t.Errorf("a suspension changed the repositories")
	}

	r.mustDeliver(t, "installation", "g-3", "installation_unsuspend.json")
	if status, _, _ := r.installationStatus(t, 1000); status != "active" {
		t.Errorf("status after unsuspend = %q, want active", status)
	}
}

func TestInstallationEventsOfAnotherInstallationAreIgnored(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.mustDeliver(t, "installation", "g-1", "installation_created.json")

	other := bytes.ReplaceAll(fixture(t, "installation_created.json"), []byte(`"id": 1000`), []byte(`"id": 2000`))
	other = bytes.ReplaceAll(other, []byte(`"id": 7001`), []byte(`"id": 7777`))
	if got := r.deliver(t, "installation", "g-2", other); got != http.StatusAccepted {
		t.Fatalf("created of a second installation = %d, want 202", got)
	}
	if _, _, ok := r.installationStatus(t, 2000); ok {
		t.Errorf("a second installation was recorded")
	}
	if _, ok := r.repos(t)[7777]; ok {
		t.Errorf("a repository of the second installation was recorded")
	}

	added := bytes.ReplaceAll(fixture(t, "installation_repositories_added.json"), []byte(`"id": 1000`), []byte(`"id": 3000`))
	if got := r.deliver(t, "installation_repositories", "g-3", added); got != http.StatusAccepted {
		t.Fatalf("repositories of an unknown installation = %d, want 202", got)
	}
	if _, ok := r.repos(t)[7002]; ok {
		t.Errorf("a repository of an unknown installation was recorded")
	}
}

func TestInstallationEventWithAnUnusablePayloadIsRefusedAndNotRemembered(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.mustDeliver(t, "installation", "g-1", "installation_created.json")
	before := r.deliveries(t)

	bad := bytes.Replace(fixture(t, "installation_repositories_added.json"), []byte(`"full_name": "acme/gadgets"`), []byte(`"full_name": "gadgets"`), 1)
	if got := r.deliver(t, "installation_repositories", "g-2", bad); got != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 so GitHub shows the failure", got)
	}
	if r.deliveries(t) != before {
		t.Errorf("a refused delivery was remembered, so its redelivery would be dropped")
	}
	if len(r.repos(t)) != 1 {
		t.Errorf("repositories = %d, want nothing from the refused delivery", len(r.repos(t)))
	}
}

// rollsBack applies a delivery in a transaction of the real store and then rolls it back, as a
// failing commit does.
type rollsBack struct{ db *store.DB }

func (r rollsBack) Receive(ctx context.Context, _ store.WebhookDelivery, apply func(ctx context.Context, tx *sql.Tx) error) (bool, error) {
	tx, err := r.db.Writer.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := apply(ctx, tx); err != nil {
		return false, err
	}
	return false, errors.New("commit failed")
}

func TestInstallationEventDoesNotRequestASyncWhenItsTransactionRollsBack(t *testing.T) {
	t.Parallel()

	db := storetest.OpenMigrated(t)
	st := store.New(db, nil)
	var changed atomic.Int64
	routes := hooks.NewRoutes()
	hooks.RegisterInstallationHandlers(routes, st.InstallationSync(), func() { changed.Add(1) })
	srv := httptest.NewServer(&hooks.Receiver{Secrets: secrets{}, Deliveries: rollsBack{db: db}, Routes: routes})
	t.Cleanup(srv.Close)
	r := &rig{srv: srv, db: db}

	if got := r.deliver(t, "installation", "g-1", fixture(t, "installation_created.json")); got != http.StatusInternalServerError {
		t.Fatalf("installation delivery = %d, want 500 for the rolled-back transaction", got)
	}
	if _, _, ok := r.installationStatus(t, 1000); ok {
		t.Fatal("the installation was recorded although its transaction rolled back")
	}
	if changed.Load() != 0 {
		t.Errorf("sync requested %d times for a delivery that did not commit, want 0", changed.Load())
	}
}
