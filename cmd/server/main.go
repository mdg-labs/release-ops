// Package main is the Release Ops HTTP API entrypoint.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/mdg-labs/release-ops/internal/api"
	"github.com/mdg-labs/release-ops/internal/api/auth"
	"github.com/mdg-labs/release-ops/internal/api/hooks"
	"github.com/mdg-labs/release-ops/internal/api/setup"
	"github.com/mdg-labs/release-ops/internal/config"
	"github.com/mdg-labs/release-ops/internal/crypto"
	"github.com/mdg-labs/release-ops/internal/github"
	"github.com/mdg-labs/release-ops/internal/mail"
	"github.com/mdg-labs/release-ops/internal/metrics"
	"github.com/mdg-labs/release-ops/internal/providers/integrationtester"
	"github.com/mdg-labs/release-ops/internal/queue"
	"github.com/mdg-labs/release-ops/internal/queue/internalapi"
	"github.com/mdg-labs/release-ops/internal/store"
	storedb "github.com/mdg-labs/release-ops/internal/store/db"
)

const (
	// reapInterval is how often the app requeues jobs whose lease has expired.
	reapInterval = 15 * time.Second
	// installationSyncInterval is how often the app reconciles the installation and its
	// repositories with GitHub, after the sync at startup: hourly, as docs/specs.html
	// §3.4 fixes it. A pass over an unchanged installation is a handful of list calls.
	installationSyncInterval = time.Hour
)

func main() {
	if len(os.Args) > 1 {
		var err error
		switch os.Args[1] {
		case "worker":
			err = runWorkerCommand()
		case "admin":
			err = runAdminCommand(context.Background(), os.Args[2:], os.Stdout)
		default:
			err = fmt.Errorf("unknown command %q: the commands are worker and admin", os.Args[1])
		}
		if err != nil {
			log.Fatal(err)
		}
		return
	}

	cfg, err := config.LoadServer()
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

	if err := setup.EnsureToken(ctx, db.Writer, cfg.SetupToken, cfg.AppURL, log.Printf); err != nil {
		return fmt.Errorf("setup token: %w", err)
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

	githubClient := github.NewClient(github.APIBaseURL(cfg.GitHubBaseURL), 0)
	appClient := github.NewAppClient(githubClient, appStore.GitHubApp(), github.AppClientOptions{})
	syncer := github.NewSyncer(appClient, appStore.InstallationSync())
	webhookRoutes := hooks.NewRoutes()
	hooks.RegisterInstallationHandlers(webhookRoutes, appStore.InstallationSync(), syncer.Trigger)

	registry := metrics.NewRegistry()
	jobMetrics, err := metrics.NewJobs(registry)
	if err != nil {
		return fmt.Errorf("metrics: %w", err)
	}
	webhookLag, err := metrics.NewWebhookLag(registry)
	if err != nil {
		return fmt.Errorf("metrics: %w", err)
	}
	jobs := store.NewJobQueue(db, store.JobQueueOptions{
		OnLeaseExpired: func(repository string) { jobMetrics.Ended(repository, metrics.OutcomeLeaseExpired) },
		OnJobStarted:   func(lag time.Duration) { webhookLag.Observe(lag.Seconds()) },
	})
	if err := metrics.RegisterQueue(registry, jobs); err != nil {
		return fmt.Errorf("metrics: %w", err)
	}

	sessionManager := auth.NewSessionManager(db.Writer, cfg.SessionSecret, cfg.SecureCookies())
	handler := api.NewServerRouter(&api.ServerDeps{
		DB:                db.Writer,
		Session:           sessionManager,
		Queries:           queries,
		Store:             appStore,
		IntegrationTester: integrationtester.New(nil),
		Mailer:            mailer,
		AppURL:            cfg.AppURL,
		WebhookURL:        cfg.WebhookURL,
		GitHubBaseURL:     cfg.GitHubBaseURL,
		GitHub:            githubClient,
		Roles: &auth.GitHubRoles{
			Access:     appStore.Access(),
			Users:      appStore.SignIn(),
			GitHub:     appClient,
			AdminTeam:  cfg.AdminTeam,
			ViewerTeam: cfg.ViewerTeam,
		},
		BootstrapAdminGitHubLogin: cfg.BootstrapAdminGitHubLogin,
		WebhookRoutes:             webhookRoutes,
		Queue:                     jobs,
	})

	appCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var background sync.WaitGroup
	background.Go(func() {
		queue.RunReaper(appCtx, jobs, reapInterval, func(err error) { log.Printf("job reaper: %v", err) })
	})
	background.Go(func() {
		ticker := time.NewTicker(installationSyncInterval)
		defer ticker.Stop()
		syncer.Run(appCtx, ticker.C, func(err error) {
			if err != nil {
				log.Printf("github sync: %v", err)
			}
		})
	})
	var internalErr, metricsErr error
	if cfg.MetricsAddr != "" {
		background.Go(func() {
			log.Printf("release-ops metrics listener on %s", cfg.MetricsAddr)
			if err := metrics.Serve(appCtx, cfg.MetricsAddr, registry); err != nil {
				metricsErr = fmt.Errorf("metrics listener: %w", err)
				cancel()
			}
		})
	}
	if cfg.InternalAddr != "" {
		background.Go(func() {
			log.Printf("release-ops internal listener on %s", cfg.InternalAddr)
			if err := serveInternal(appCtx, cfg, jobs, appClient); err != nil {
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
	return errors.Join(internalErr, metricsErr)
}

// serveInternal serves the worker routes on INTERNAL_ADDR until ctx ends.
// Its token route mints job tokens with the App's key, which stays in this process.
func serveInternal(ctx context.Context, cfg *config.Config, q queue.Queue, tokens internalapi.TokenIssuer) error {
	h := internalapi.NewHandler(q, internalapi.Options{
		Secret:            cfg.WorkerSecret,
		Tokens:            tokens,
		TokenRepositories: cfg.JobTokenRepositories(),
	})
	return internalapi.Serve(ctx, cfg.InternalAddr, h, internalapi.WriteTimeout)
}
