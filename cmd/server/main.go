// Package main is the Release Ops HTTP API entrypoint.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/mdg-labs/release-ops/internal/api"
	"github.com/mdg-labs/release-ops/internal/api/auth"
	"github.com/mdg-labs/release-ops/internal/config"
	"github.com/mdg-labs/release-ops/internal/crypto"
	"github.com/mdg-labs/release-ops/internal/mail"
	"github.com/mdg-labs/release-ops/internal/providers/integrationtester"
	"github.com/mdg-labs/release-ops/internal/queue"
	"github.com/mdg-labs/release-ops/internal/queue/internalapi"
	"github.com/mdg-labs/release-ops/internal/store"
	storedb "github.com/mdg-labs/release-ops/internal/store/db"
)

// reapInterval is how often the app requeues jobs whose lease has expired.
const reapInterval = 15 * time.Second

func main() {
	if len(os.Args) > 1 {
		if os.Args[1] != "worker" {
			log.Fatalf("unknown command %q: the only command is worker", os.Args[1])
		}
		if err := runWorkerCommand(); err != nil {
			log.Fatal(err)
		}
		return
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, cfg); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, cfg *config.Config) error {
	applied, err := store.Migrate(ctx, cfg.AppDBPath)
	if err != nil {
		return fmt.Errorf("migrations: %w", err)
	}
	for _, m := range applied {
		log.Printf("migrations: applied %s_%s", m.Version, m.Slug)
	}

	db, err := store.OpenPath(cfg.AppDBPath)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			log.Printf("database close: %v", err)
		}
	}()

	queries := storedb.New(db.Writer)
	if err := auth.BootstrapFromEnv(context.Background(), queries, cfg.BootstrapAdminEmail, cfg.BootstrapAdminPassword); err != nil {
		return fmt.Errorf("bootstrap admin: %w", err)
	}

	cipher, err := crypto.NewCipherFromHex(cfg.AppEncryptionKey)
	if err != nil {
		return fmt.Errorf("encryption: %w", err)
	}

	appStore := store.New(db, cipher)

	if err := appStore.Settings().EnsureDefault(context.Background()); err != nil {
		return fmt.Errorf("app settings: %w", err)
	}

	mailer, err := mail.NewFromEnv()
	if err != nil {
		return fmt.Errorf("mail: %w", err)
	}

	sessionManager := auth.NewSessionManager(db.Writer, cfg.SessionSecret, cfg.SecureCookies())
	handler := api.NewServerRouter(&api.ServerDeps{
		DB:                db.Writer,
		Session:           sessionManager,
		Queries:           queries,
		Store:             appStore,
		IntegrationTester: integrationtester.New(nil),
		Mailer:            mailer,
	})

	appCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := store.NewJobQueue(db, store.JobQueueOptions{})
	var background sync.WaitGroup
	background.Go(func() {
		queue.RunReaper(appCtx, jobs, reapInterval, func(err error) { log.Printf("job reaper: %v", err) })
	})
	var internalErr error
	if cfg.InternalAddr != "" {
		background.Go(func() {
			log.Printf("release-ops internal listener on %s", cfg.InternalAddr)
			if err := serveInternal(appCtx, cfg, jobs); err != nil {
				internalErr = fmt.Errorf("internal listener: %w", err)
				cancel()
			}
		})
	}

	addr := cfg.GoListenAddr()
	log.Printf("release-ops server listening on %s", addr)

	serveErr := api.Serve(appCtx, addr, handler)
	cancel()
	background.Wait()
	if serveErr != nil {
		return fmt.Errorf("server: %w", serveErr)
	}
	return internalErr
}

// serveInternal serves the worker routes on INTERNAL_ADDR until ctx ends.
func serveInternal(ctx context.Context, cfg *config.Config, q queue.Queue) error {
	h := internalapi.NewHandler(q, internalapi.Options{Secret: cfg.WorkerSecret})
	return internalapi.Serve(ctx, cfg.InternalAddr, h, internalapi.DefaultClaimWait+30*time.Second)
}
