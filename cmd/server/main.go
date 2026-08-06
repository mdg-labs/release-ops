// Package main is the Release Ops HTTP API and poll scheduler entrypoint.
package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"github.com/mdguggenbichler/release-ops/internal/api"
	"github.com/mdguggenbichler/release-ops/internal/config"
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

	handler := api.NewRouter()
	addr := cfg.GoListenAddr()
	log.Printf("release-ops server listening on %s", addr)

	if err := api.Serve(ctx, addr, handler); err != nil {
		log.Fatalf("server: %v", err)
	}
}
