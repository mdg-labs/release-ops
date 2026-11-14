package main

import (
	"context"
	"fmt"
	"log"
	"os/signal"
	"sync"
	"syscall"

	"github.com/mdg-labs/release-ops/internal/config"
	"github.com/mdg-labs/release-ops/internal/metrics"
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
	return runWorker(ctx, cfg, registry, worker.Options{})
}

// runWorker claims and runs jobs until ctx ends, then shuts down gracefully. With a
// MetricsAddr it also serves the worker's /metrics, and the job metrics are recorded on that
// registry; a listener that cannot start or stops ends the worker with its error.
func runWorker(ctx context.Context, cfg *config.WorkerConfig, registry worker.Registry, opts worker.Options) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var metricsErr error
	var listener sync.WaitGroup
	if cfg.MetricsAddr != "" {
		reg := metrics.NewRegistry()
		jobs, err := metrics.NewJobs(reg)
		if err != nil {
			return fmt.Errorf("metrics: %w", err)
		}
		opts.Jobs = jobs
		listener.Go(func() {
			log.Printf("release-ops worker metrics listener on %s", cfg.MetricsAddr)
			if err := metrics.Serve(runCtx, cfg.MetricsAddr, reg); err != nil {
				metricsErr = fmt.Errorf("metrics listener: %w", err)
				cancel()
			}
		})
	}

	client := worker.NewClient(cfg.InternalURL, cfg.WorkerSecret, nil)
	worker.NewRunner(client, registry, opts).Run(runCtx)
	cancel()
	listener.Wait()
	return metricsErr
}
