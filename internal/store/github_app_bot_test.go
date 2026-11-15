package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/mdg-labs/release-ops/internal/crypto"
	"github.com/mdg-labs/release-ops/internal/queue"
	"github.com/mdg-labs/release-ops/internal/store"
)

func TestBotIdentityBeforeTheAppIsStored(t *testing.T) {
	t.Parallel()

	s, _, _ := newGitHubAppStore(t)
	if _, err := s.GitHubApp().BotIdentity(context.Background()); !errors.Is(err, store.ErrGitHubAppNotFound) {
		t.Fatalf("BotIdentity = %v, want ErrGitHubAppNotFound", err)
	}
}

func TestRecordBotUserIDStoresTheIdOnceAndNeverReplacesIt(t *testing.T) {
	t.Parallel()

	s, db, _ := newGitHubAppStore(t)
	ctx := context.Background()
	apps := s.GitHubApp()
	if err := apps.Create(ctx, sampleApp()); err != nil {
		t.Fatal(err)
	}

	bot, err := apps.BotIdentity(ctx)
	if err != nil || bot.Known || bot.UserID != 0 || bot.Slug != "release-ops-acme" {
		t.Fatalf("BotIdentity before = %+v, %v, want the slug with the id unknown", bot, err)
	}

	stored, err := apps.RecordBotUserID(ctx, 424242)
	if err != nil || !stored {
		t.Fatalf("RecordBotUserID = %v, %v, want stored", stored, err)
	}
	bot, err = apps.BotIdentity(ctx)
	if err != nil || !bot.Known || bot.UserID != 424242 {
		t.Fatalf("BotIdentity after = %+v, %v, want 424242", bot, err)
	}

	stored, err = apps.RecordBotUserID(ctx, 7)
	if err != nil || stored {
		t.Fatalf("second RecordBotUserID = %v, %v, want nothing stored", stored, err)
	}
	var id int64
	if err := db.Writer.QueryRow(`SELECT bot_user_id FROM github_app`).Scan(&id); err != nil || id != 424242 {
		t.Errorf("bot_user_id = %d, %v, want the first id kept", id, err)
	}
}

func TestRecordBotUserIDRefusesAnIdThatIsNotPositive(t *testing.T) {
	t.Parallel()

	s, db, _ := newGitHubAppStore(t)
	ctx := context.Background()
	apps := s.GitHubApp()
	if err := apps.Create(ctx, sampleApp()); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{0, -5} {
		if stored, err := apps.RecordBotUserID(ctx, id); err == nil || stored {
			t.Errorf("RecordBotUserID(%d) = %v, %v, want a refusal", id, stored, err)
		}
	}
	var n int
	if err := db.Writer.QueryRow(`SELECT COUNT(*) FROM github_app WHERE bot_user_id IS NOT NULL`).Scan(&n); err != nil || n != 0 {
		t.Errorf("rows with a bot user id = %d, %v, want none", n, err)
	}
}

func TestRecordBotUserIDWithoutAnAppStoresNothing(t *testing.T) {
	t.Parallel()

	s, db, _ := newGitHubAppStore(t)
	if stored, err := s.GitHubApp().RecordBotUserID(context.Background(), 5); err != nil || stored {
		t.Fatalf("RecordBotUserID = %v, %v, want nothing stored", stored, err)
	}
	if n := countRows(t, db, "github_app"); n != 0 {
		t.Errorf("github_app rows = %d, want none", n)
	}
}

// The id recorded through the accessor is what the claimed job's git author carries: the
// claim omits the author while the id is unknown and builds it from the slug and the id after.
func TestRecordedBotUserIDReachesTheClaimedJobsGitAuthor(t *testing.T) {
	e := newJobEnv(t, 2)
	ctx := context.Background()
	cipher, err := crypto.NewCipherFromHex(appStoreKeyHex)
	if err != nil {
		t.Fatal(err)
	}
	apps := store.New(e.db, cipher).GitHubApp()
	if err := apps.Create(ctx, sampleApp()); err != nil {
		t.Fatal(err)
	}

	e.enqueue(t, e.repos[0], queue.PriorityHigh)
	if job := e.claim(t); job.GitAuthor != "" {
		t.Fatalf("GitAuthor = %q before the id is recorded, want none", job.GitAuthor)
	}

	if stored, err := apps.RecordBotUserID(ctx, 424242); err != nil || !stored {
		t.Fatalf("RecordBotUserID = %v, %v", stored, err)
	}
	e.enqueue(t, e.repos[1], queue.PriorityHigh)
	job, err := e.q.Claim(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if want := "release-ops-acme[bot] <424242+release-ops-acme[bot]@users.noreply.github.com>"; job.GitAuthor != want {
		t.Errorf("GitAuthor = %q, want %q", job.GitAuthor, want)
	}
}
