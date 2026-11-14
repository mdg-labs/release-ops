package store_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/crypto"
	"github.com/mdg-labs/release-ops/internal/queue"
	"github.com/mdg-labs/release-ops/internal/store"
)

func sampleInstallation(id int64) store.NewInstallation {
	return store.NewInstallation{
		GitHubInstallationID: id, AccountID: 4242, AccountLogin: "acme", AccountType: "Organization",
		Status: "active", RepositorySelection: "selected", Permissions: `{"contents":"write"}`,
	}
}

func TestInstallationRecordStoresWhatGitHubSaid(t *testing.T) {
	t.Parallel()

	s, db, _ := newGitHubAppStore(t)
	ctx := context.Background()
	if out, err := s.Installations().Record(ctx, sampleInstallation(1000)); err != nil || out != store.InstallationRecorded {
		t.Fatalf("Record = %v, %v, want InstallationRecorded", out, err)
	}

	var ghID, accountID int64
	var login, typ, status, selection, permissions string
	err := db.Writer.QueryRow(`SELECT github_installation_id, account_id, account_login, account_type, status, repository_selection, permissions FROM installations`).
		Scan(&ghID, &accountID, &login, &typ, &status, &selection, &permissions)
	if err != nil {
		t.Fatalf("read installation: %v", err)
	}
	if ghID != 1000 || accountID != 4242 || login != "acme" || typ != "Organization" || status != "active" || selection != "selected" || permissions != `{"contents":"write"}` {
		t.Errorf("row = %d %d %s %s %s %s %s", ghID, accountID, login, typ, status, selection, permissions)
	}

	ids, err := s.Installations().LiveIDs(ctx)
	if err != nil || len(ids) != 1 || ids[0] != 1000 {
		t.Errorf("LiveIDs = %v, %v", ids, err)
	}
}

func TestInstallationRecordKeepsExactlyOne(t *testing.T) {
	t.Parallel()

	s, db, _ := newGitHubAppStore(t)
	ctx := context.Background()
	if _, err := s.Installations().Record(ctx, sampleInstallation(1000)); err != nil {
		t.Fatal(err)
	}
	if out, err := s.Installations().Record(ctx, sampleInstallation(1000)); err != nil || out != store.InstallationKnown {
		t.Errorf("same installation again = %v, %v, want InstallationKnown", out, err)
	}
	other := sampleInstallation(2000)
	other.AccountLogin = "intruder"
	if out, err := s.Installations().Record(ctx, other); err != nil || out != store.InstallationRefused {
		t.Errorf("second installation = %v, %v, want InstallationRefused", out, err)
	}
	if n := countRows(t, db, "installations"); n != 1 {
		t.Errorf("installations = %d, want 1", n)
	}
	if n := countRows(t, db, "installations WHERE account_login = 'intruder'"); n != 0 {
		t.Error("a refused installation was written")
	}
}

func TestInstallationRecordReplacesARemovedOne(t *testing.T) {
	t.Parallel()

	s, db, _ := newGitHubAppStore(t)
	ctx := context.Background()
	if _, err := s.Installations().Record(ctx, sampleInstallation(1000)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Writer.Exec(`UPDATE installations SET status = 'removed'`); err != nil {
		t.Fatal(err)
	}
	if ids, err := s.Installations().LiveIDs(ctx); err != nil || len(ids) != 0 {
		t.Fatalf("LiveIDs with only a removed installation = %v, %v, want none", ids, err)
	}

	if out, err := s.Installations().Record(ctx, sampleInstallation(2000)); err != nil || out != store.InstallationRecorded {
		t.Fatalf("a new installation after removal = %v, %v", out, err)
	}
	if _, err := db.Writer.Exec(`UPDATE installations SET status = 'removed' WHERE github_installation_id = 2000`); err != nil {
		t.Fatal(err)
	}
	if out, err := s.Installations().Record(ctx, sampleInstallation(1000)); err != nil || out != store.InstallationRecorded {
		t.Fatalf("the removed id reinstalled = %v, %v", out, err)
	}
	if n := countRows(t, db, "installations WHERE status = 'active'"); n != 1 {
		t.Errorf("active installations = %d, want 1", n)
	}
}

func TestInstallationRecordConcurrentCallsStoreOne(t *testing.T) {
	t.Parallel()

	s, db, _ := newGitHubAppStore(t)
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			if _, err := s.Installations().Record(context.Background(), sampleInstallation(int64(1000+i))); err != nil {
				t.Errorf("Record: %v", err)
			}
		})
	}
	wg.Wait()
	if n := countRows(t, db, "installations"); n != 1 {
		t.Errorf("installations = %d, want 1", n)
	}
}

func TestGitHubAppSigningKeyDecryptsTheStoredKey(t *testing.T) {
	t.Parallel()

	s, _, _ := newGitHubAppStore(t)
	ctx := context.Background()
	if _, _, err := s.GitHubApp().SigningKey(ctx); !errors.Is(err, store.ErrGitHubAppNotFound) {
		t.Fatalf("SigningKey without an App: err = %v, want ErrGitHubAppNotFound", err)
	}
	app := sampleApp()
	if err := s.GitHubApp().Create(ctx, app); err != nil {
		t.Fatal(err)
	}
	id, pem, err := s.GitHubApp().SigningKey(ctx)
	if err != nil || id != app.AppID || string(pem) != app.PrivateKey {
		t.Errorf("SigningKey = %d, %q, %v", id, pem, err)
	}
}

func TestGitHubAppSigningKeyWithAnotherEncryptionKeyFailsWithoutLeaking(t *testing.T) {
	t.Parallel()

	s, db, _ := newGitHubAppStore(t)
	if err := s.GitHubApp().Create(context.Background(), sampleApp()); err != nil {
		t.Fatal(err)
	}
	other, err := crypto.NewCipherFromHex(strings.Repeat("ab", 32))
	if err != nil {
		t.Fatal(err)
	}
	_, pem, err := store.New(db, other).GitHubApp().SigningKey(context.Background())
	if err == nil || pem != nil {
		t.Fatalf("SigningKey with the wrong key = %q, %v, want an error", pem, err)
	}
	if strings.Contains(err.Error(), "fixture-private-key-body") {
		t.Errorf("error quotes the key: %v", err)
	}
}

func TestHeldJobRequiresALiveLeaseOfTheCaller(t *testing.T) {
	t.Parallel()

	e := newJobEnv(t, 2)
	ctx := context.Background()
	e.enqueue(t, e.repos[0], queue.PriorityHigh)
	job := e.claim(t)

	held, err := e.q.HeldJob(ctx, job.ID, job.LeaseOwner)
	if err != nil {
		t.Fatalf("HeldJob: %v", err)
	}
	if held.ID != job.ID || held.RepositoryID != e.repos[0] || held.RepositoryFullName != "acme/repo-1" || held.InstallationID != 1 {
		t.Errorf("held = %+v", held)
	}

	lost := func(name, id, owner string) {
		t.Helper()
		if h, err := e.q.HeldJob(ctx, id, owner); !errors.Is(err, queue.ErrLeaseLost) || h != nil {
			t.Errorf("%s: HeldJob = %+v, %v, want ErrLeaseLost", name, h, err)
		}
	}
	lost("unknown job", "no-such-job", job.LeaseOwner)
	lost("another owner", job.ID, "0.not-the-owner")
	lost("empty owner", job.ID, "")

	e.enqueue(t, e.repos[1], queue.PriorityHigh)
	queued := e.claim(t)
	if err := e.q.Release(ctx, queued.ID, queued.LeaseOwner); err != nil {
		t.Fatal(err)
	}
	lost("a released job", queued.ID, queued.LeaseOwner)

	// The lease is live up to its expiry and not at it.
	e.clock.Advance(2*time.Minute - time.Millisecond)
	if _, err := e.q.HeldJob(ctx, job.ID, job.LeaseOwner); err != nil {
		t.Errorf("just before the lease expires: %v", err)
	}
	e.clock.Advance(time.Millisecond)
	lost("an expired lease", job.ID, job.LeaseOwner)

	// A heartbeat renews it.
	if _, err := e.q.Heartbeat(ctx, job.ID, job.LeaseOwner); err != nil {
		t.Fatal(err)
	}
	if _, err := e.q.HeldJob(ctx, job.ID, job.LeaseOwner); err != nil {
		t.Errorf("after a heartbeat: %v", err)
	}

	if err := e.q.Complete(ctx, job.ID, job.LeaseOwner); err != nil {
		t.Fatal(err)
	}
	lost("a finished job", job.ID, job.LeaseOwner)
}

func TestHeldJobRefusesAJobOfAnInstallationThatIsNotActive(t *testing.T) {
	t.Parallel()

	e := newJobEnv(t, 1)
	e.enqueue(t, e.repos[0], queue.PriorityHigh)
	job := e.claim(t)
	if _, err := e.db.Writer.Exec(`UPDATE installations SET status = 'suspended'`); err != nil {
		t.Fatal(err)
	}
	if _, err := e.q.HeldJob(context.Background(), job.ID, job.LeaseOwner); !errors.Is(err, queue.ErrLeaseLost) {
		t.Errorf("HeldJob for a suspended installation: err = %v, want ErrLeaseLost", err)
	}
}
