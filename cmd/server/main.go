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
	"github.com/mdg-labs/release-ops/internal/poll"
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

	engine := poll.NewEngine(appStore.Poll())
	scheduler, err := poll.NewScheduler(poll.SchedulerConfig{
		Engine:         engine,
		Settings:       appStore.Settings(),
		Repos:          appStore.Repos(),
		TicketProjects: appStore.TicketProjects(),
		Integrations:   appStore.Integrations(),
		Poll:           appStore.Poll(),
	})
	if err != nil {
		log.Fatalf("poll scheduler: %v", err)
	}
	if err := scheduler.Start(ctx); err != nil {
		log.Fatalf("poll scheduler start: %v", err)
	}

	sessionManager := auth.NewSessionManager(db, cfg.SessionSecret, cfg.SecureCookies())
	handler := api.NewServerRouter(&api.ServerDeps{
		DB:         db,
		Session:    sessionManager,
		Queries:    queries,
		Store:      appStore,
		PollRunner: scheduler,
	})
	addr := cfg.GoListenAddr()
	log.Printf("release-ops server listening on %s", addr)

	if err := api.Serve(ctx, addr, handler); err != nil {
		log.Fatalf("server: %v", err)
	}
}
