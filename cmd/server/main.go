// Package main is the Release Ops HTTP API entrypoint.
package main

import (
	"context"
	"fmt"
	"log"
	"os/signal"
	"syscall"

	"github.com/mdg-labs/release-ops/internal/api"
	"github.com/mdg-labs/release-ops/internal/api/auth"
	"github.com/mdg-labs/release-ops/internal/config"
	"github.com/mdg-labs/release-ops/internal/crypto"
	"github.com/mdg-labs/release-ops/internal/mail"
	"github.com/mdg-labs/release-ops/internal/providers/integrationtester"
	"github.com/mdg-labs/release-ops/internal/store"
	storedb "github.com/mdg-labs/release-ops/internal/store/db"
)

func main() {
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
	addr := cfg.GoListenAddr()
	log.Printf("release-ops server listening on %s", addr)

	if err := api.Serve(ctx, addr, handler); err != nil {
		return fmt.Errorf("server: %w", err)
	}
	return nil
}
