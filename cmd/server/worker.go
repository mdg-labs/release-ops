package main

import (
	"context"
	"fmt"
	"log"
	"os/signal"
	"syscall"

	"github.com/mdg-labs/release-ops/internal/config"
	"github.com/mdg-labs/release-ops/internal/queue"
	"github.com/mdg-labs/release-ops/internal/queue/worker"
)

// runWorkerCommand is `release-ops worker`: it claims jobs from the app's internal listener
// until SIGINT or SIGTERM. The worker loads its own configuration and has no database.
func runWorkerCommand() error {
	cfg, err := config.LoadWorker()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// The Renovate handler replaces the no-op for this kind when it exists.
	registry := worker.Registry{queue.KindRenovateRun: worker.NoopHandler}
	log.Printf("release-ops worker claiming jobs from %s", cfg.InternalURL)
	runWorker(ctx, cfg, registry, worker.Options{})
	return nil
}

// runWorker claims and runs jobs until ctx ends, then shuts down gracefully.
func runWorker(ctx context.Context, cfg *config.WorkerConfig, registry worker.Registry, opts worker.Options) {
	client := worker.NewClient(cfg.InternalURL, cfg.WorkerSecret, nil)
	worker.NewRunner(client, registry, opts).Run(ctx)
}
