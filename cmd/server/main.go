// Package main is the Release Ops HTTP API and poll scheduler entrypoint.
package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"github.com/mdg-labs/release-ops/internal/api"
	"github.com/mdg-labs/release-ops/internal/api/auth"
	"github.com/mdg-labs/release-ops/internal/config"
	"github.com/mdg-labs/release-ops/internal/crypto"
	"github.com/mdg-labs/release-ops/internal/mail"
	"github.com/mdg-labs/release-ops/internal/poll"
	"github.com/mdg-labs/release-ops/internal/providers/integrationtester"
	"github.com/mdg-labs/release-ops/internal/store"
	storedb "github.com/mdg-labs/release-ops/internal/store/db"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	run(cfg)
}

func run(cfg *config.Config) {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	applied, err := store.Migrate(ctx, cfg.AppDBPath)
	if err != nil {
		log.Fatalf("migrations: %v", err)
	}
	for _, m := range applied {
		log.Printf("migrations: applied %s_%s", m.Version, m.Slug)
	}

	db, err := store.OpenPath(cfg.AppDBPath)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			log.Printf("database close: %v", err)
		}
	}()

	queries := storedb.New(db)
	if err := auth.BootstrapFromEnv(context.Background(), queries, cfg.BootstrapAdminEmail, cfg.BootstrapAdminPassword); err != nil {
		log.Fatalf("bootstrap admin: %v", err)
	}

	cipher, err := crypto.NewCipherFromHex(cfg.AppEncryptionKey)
	if err != nil {
		log.Fatalf("encryption: %v", err)
	}

	appStore := store.New(db, cipher)

	if err := appStore.Settings().EnsureDefault(context.Background()); err != nil {
		log.Fatalf("app settings: %v", err)
	}

	if err := reconcilePollRuns(ctx, appStore.Poll()); err != nil {
		log.Fatalf("poll runs: %v", err)
	}

	notifier := poll.NewNotifier(appStore.Notifications(), nil)
	engine := poll.NewEngine(appStore.Poll())
	scheduler, err := poll.NewScheduler(poll.SchedulerConfig{
		Engine:         engine,
		Settings:       appStore.Settings(),
		Repos:          appStore.Repos(),
		TicketProjects: appStore.TicketProjects(),
		Integrations:   appStore.Integrations(),
		Poll:           appStore.Poll(),
		Notifier:       notifier,
	})
	if err != nil {
		log.Fatalf("poll scheduler: %v", err)
	}
	if err := scheduler.Start(ctx); err != nil {
		log.Fatalf("poll scheduler start: %v", err)
	}

	mailer, err := mail.NewFromEnv()
	if err != nil {
		log.Fatalf("mail: %v", err)
	}

	sessionManager := auth.NewSessionManager(db, cfg.SessionSecret, cfg.SecureCookies())
	handler := api.NewServerRouter(&api.ServerDeps{
		DB:                 db,
		Session:            sessionManager,
		Queries:            queries,
		Store:              appStore,
		PollRunner:         scheduler,
		IntegrationTester:  integrationtester.New(nil),
		NotificationTester: notifier,
		Mailer:             mailer,
	})
	addr := cfg.GoListenAddr()
	log.Printf("release-ops server listening on %s", addr)

	serveErr := api.Serve(ctx, addr, handler)

	// Cancel first so a failed Serve also stops the scheduler, then let an in-flight
	// poll run write its finish before the deferred db.Close.
	stop()
	shutdownScheduler(scheduler)

	if serveErr != nil {
		log.Fatalf("server: %v", serveErr)
	}
}

// shutdownScheduler waits for in-flight poll runs to write their finish. A run that
// does not finish in time is left for reconcilePollRuns at the next start.
func shutdownScheduler(scheduler *poll.Scheduler) {
	waitCtx, cancel := context.WithTimeout(context.Background(), poll.ShutdownWaitTimeout)
	defer cancel()
	if err := scheduler.Shutdown(waitCtx); err != nil {
		log.Printf("poll scheduler shutdown: in-flight run did not finish within %s: %v", poll.ShutdownWaitTimeout, err)
	}
}

// reconcilePollRuns finishes the runs a previous process left running. It must run
// before the scheduler starts, so it can never touch a run of this process.
func reconcilePollRuns(ctx context.Context, polls store.PollRepository) error {
	n, err := poll.ReconcileInterruptedRuns(ctx, polls)
	if err != nil {
		return err
	}
	if n > 0 {
		log.Printf("poll runs: marked %d interrupted run(s) as failed", n)
	}
	return nil
}
