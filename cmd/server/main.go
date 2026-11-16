// Package main is the Release Ops HTTP API entrypoint.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
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
	"github.com/mdg-labs/release-ops/internal/logging"
	"github.com/mdg-labs/release-ops/internal/mail"
	"github.com/mdg-labs/release-ops/internal/metrics"
	"github.com/mdg-labs/release-ops/internal/providers/integrationtester"
	"github.com/mdg-labs/release-ops/internal/providers/ticket"
	"github.com/mdg-labs/release-ops/internal/queue"
	"github.com/mdg-labs/release-ops/internal/queue/internalapi"
	"github.com/mdg-labs/release-ops/internal/scheduler"
	"github.com/mdg-labs/release-ops/internal/store"
	storedb "github.com/mdg-labs/release-ops/internal/store/db"
	"github.com/mdg-labs/release-ops/internal/updateprs"
	"github.com/mdg-labs/release-ops/internal/updatetickets"
)

// The intervals of the periodic jobs are variables only so a test can shorten them.
var (
	// installationSyncInterval is how often the app reconciles the installation and its
	// repositories with GitHub, after the sync at startup: hourly, as docs/specs.html
	// §3.4 fixes it. A pass over an unchanged installation is a handful of list calls.
	installationSyncInterval = time.Hour
	// pollingInterval replaces it when no webhook is configured, because the reconcile is
	// then the only way a change reaches the instance (docs/specs.html §2.11).
	pollingInterval = github.PollingInterval
	// redeliveryInterval is how often failed webhook deliveries are redelivered, after the
	// pass at startup.
	redeliveryInterval = github.RedeliveryInterval
	// schedulerInterval is how often the safety net enqueues a job per repository: hourly, as
	// docs/specs.html §5.7 fixes it. There is no setting for it.
	schedulerInterval = scheduler.Interval
)

// ticketRequestTimeout bounds each call to a ticket system, as for the connection test.
const ticketRequestTimeout = 30 * time.Second

// integrationTargets builds the provider of a ticket integration from its stored, decrypted
// credentials each time a ticket is created, so a changed credential applies at once. The
// credentials stay in this call: they are never logged or stored beside the provider.
type integrationTargets struct {
	integrations store.IntegrationRepository
	client       *http.Client
}

func (t integrationTargets) Target(ctx context.Context, integrationID string) (updatetickets.Target, error) {
	integration, err := t.integrations.Get(ctx, integrationID)
	if err != nil {
		return updatetickets.Target{}, fmt.Errorf("read integration: %w", err)
	}
	secret, err := t.integrations.DecryptPayload(ctx, integrationID)
	if err != nil {
		return updatetickets.Target{}, fmt.Errorf("read integration credentials: %w", err)
	}
	provider, err := ticket.NewProviderFromIntegration(integration.Kind, integration.BaseURL, secret, t.client)
	if err != nil {
		return updatetickets.Target{}, err
	}
	return updatetickets.Target{Provider: provider, Kind: integration.Kind}, nil
}

// reapInterval is how often the app requeues jobs whose lease has expired.
const reapInterval = 15 * time.Second

func main() {
	logging.Install(os.Stderr)

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
			fatal(err)
		}
		return
	}

	cfg, err := config.LoadServer()
	if err != nil {
		fatal(fmt.Errorf("config: %w", err))
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, cfg); err != nil {
		fatal(err)
	}
}

// fatal logs err, through the redacting handler, and ends the process with status 1.
func fatal(err error) {
	slog.Error("release-ops stopped", "err", err)
	os.Exit(1)
}

// setupLogf is where setup.EnsureToken tells the operator how to open /setup. A generated
// token is the call's second argument: it is printed in that one record, the only place the
// token is shown, and every later line has it removed.
func setupLogf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if len(args) == 2 {
		if token, ok := args[1].(string); ok {
			logging.PrintOnce(context.Background(), token, msg)
			return
		}
	}
	slog.Info(msg)
}

func run(ctx context.Context, cfg *config.Config) error {
	applied, err := store.Migrate(ctx, cfg.AppDBPath)
	if err != nil {
		return fmt.Errorf("migrations: %w", err)
	}
	for _, m := range applied {
		slog.Info("migrations: applied", "version", m.Version, "slug", m.Slug)
	}

	db, err := store.OpenPath(cfg.AppDBPath)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			slog.Error("database close failed", "err", err)
		}
	}()

	queries := storedb.New(db.Writer)
	if err := auth.BootstrapFromEnv(context.Background(), queries, cfg.BootstrapAdminEmail, cfg.BootstrapAdminPassword); err != nil {
		return fmt.Errorf("bootstrap admin: %w", err)
	}

	if err := setup.EnsureToken(ctx, db.Writer, cfg.SetupToken, cfg.AppURL, setupLogf); err != nil {
		return fmt.Errorf("setup token: %w", err)
	}

	cipher, err := crypto.NewCipherFromHex(cfg.AppEncryptionKey)
	if err != nil {
		return fmt.Errorf("encryption: %w", err)
	}

	appStore := store.New(db, cipher)

	appStore.SetInstanceConfig(store.InstanceConfig{
		PresetRepositories: cfg.PresetRepositories,
		GlobalConfigRepo:   cfg.GlobalConfigRepo,
		GlobalConfigRef:    cfg.GlobalConfigRef,
		AdminTeam:          cfg.AdminTeam,
		ViewerTeam:         cfg.ViewerTeam,
	})

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
	recovery := github.NewRecovery(appClient, github.RecoveryOptions{})
	botResolver := github.NewBotResolver(githubClient, appStore.GitHubApp())
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
	if err := metrics.RegisterOpenUpdatePRs(registry, appStore.UpdatePRs()); err != nil {
		return fmt.Errorf("metrics: %w", err)
	}
	// ticketWake starts the ticket engine's pass over the PRs without a ticket: every committed
	// change of an update PR fires it. The engine also receives the tracker's events once the
	// transaction that wrote them has committed, from the webhook and from the reconcile alike.
	ticketWake := updateprs.NewNotifier()
	ticketEngine := &updatetickets.Engine{
		Tickets:  appStore.UpdateTickets(),
		Projects: appStore.TicketProjects(),
		Targets:  integrationTargets{integrations: appStore.Integrations(), client: &http.Client{Timeout: ticketRequestTimeout}},
		Wake:     ticketWake,
	}
	prTracker := updateprs.NewTracker(appStore.UpdatePRs(), nil, ticketWake)
	prTracker.Subscribe(ticketEngine.Handle)
	prRunner := &updateprs.Runner{
		Reconciler: &updateprs.Reconciler{
			Store: appStore.UpdatePRs(), Tracker: prTracker, API: githubClient, Tokens: appClient,
			Bot: appStore.GitHubApp(), DB: db.Writer, Wake: ticketWake,
		},
		Repositories: appStore.UpdatePRs(),
		Report: func(repositoryID string, err error) {
			if err != nil {
				slog.Error("update PR reconcile failed", "repository", repositoryID, "err", err)
			}
		},
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
		UpdatePRs:                 prTracker,
	})

	appCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var background sync.WaitGroup
	background.Go(func() {
		queue.RunReaper(appCtx, jobs, reapInterval, func(err error) { slog.Error("job reaper failed", "err", err) })
	})
	background.Go(func() {
		ticker := time.NewTicker(schedulerInterval)
		defer ticker.Stop()
		sched := scheduler.New(appStore.Repositories(), jobs)
		sched.AfterTick = func(ctx context.Context) { triggerReconciles(ctx, prRunner) }
		sched.Run(appCtx, ticker.C, func(res scheduler.Result, err error) {
			if err != nil {
				slog.Error("scheduler failed", "err", err)
			}
			if res.Enqueued > 0 {
				slog.Info("scheduler: enqueued repository runs", "count", res.Enqueued)
			}
		})
	})
	background.Go(func() { prRunner.Run(appCtx) })
	background.Go(func() { ticketEngine.Run(appCtx) })
	// The start-up reconcile needs the bot user id, which is what tells a bot PR, and the
	// repositories of the first sync, so it waits for both.
	startup := &startupReconcile{trigger: func() { triggerReconciles(appCtx, prRunner) }}
	syncEvery := installationSyncInterval
	if cfg.PollingMode() {
		syncEvery = pollingInterval
		slog.Warn("WEBHOOK_URL is not set: running in polling mode (degraded), re-reading installations and repositories from GitHub; changes reach this instance with minutes of latency", "interval", pollingInterval)
	} else {
		background.Go(func() {
			ticker := time.NewTicker(redeliveryInterval)
			defer ticker.Stop()
			recovery.Run(appCtx, ticker.C, func(redelivered int, err error) {
				if err != nil {
					slog.Error("github redelivery failed", "err", err)
				}
				if redelivered > 0 {
					slog.Info("github redelivery: asked GitHub to redeliver failed webhook deliveries", "count", redelivered)
				}
			})
		})
	}
	background.Go(func() {
		ticker := time.NewTicker(github.BotRetryInterval)
		defer ticker.Stop()
		botResolver.Run(appCtx, ticker.C, func(err error) {
			if err != nil {
				slog.Error("github bot user id failed", "err", err)
			}
			startup.botPass(appCtx, err, appStore.GitHubApp())
		})
	})
	background.Go(func() {
		ticker := time.NewTicker(syncEvery)
		defer ticker.Stop()
		syncer.Run(appCtx, ticker.C, func(err error) {
			if err != nil {
				slog.Error("github sync failed", "err", err)
			}
			startup.syncPass(appCtx, err, appStore.GitHubApp())
		})
	})
	var internalErr, metricsErr error
	if cfg.MetricsAddr != "" {
		background.Go(func() {
			slog.Info("release-ops metrics listener", "addr", cfg.MetricsAddr)
			if err := metrics.Serve(appCtx, cfg.MetricsAddr, registry); err != nil {
				metricsErr = fmt.Errorf("metrics listener: %w", err)
				cancel()
			}
		})
	}
	if cfg.InternalAddr != "" {
		background.Go(func() {
			slog.Info("release-ops internal listener", "addr", cfg.InternalAddr)
			if err := serveInternalWith(appCtx, cfg, jobs, appClient, prRunner.Trigger); err != nil {
				internalErr = fmt.Errorf("internal listener: %w", err)
				cancel()
			}
		})
	}

	addr := cfg.GoListenAddr()
	slog.Info("release-ops server listening", "addr", addr)

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
	return serveInternalWith(ctx, cfg, q, tokens, nil)
}

// serveInternalWith is serveInternal that tells afterResult, when not nil, the repository of
// every stored job result.
func serveInternalWith(ctx context.Context, cfg *config.Config, q queue.Queue, tokens internalapi.TokenIssuer, afterResult func(repositoryID string)) error {
	h := internalapi.NewHandler(q, internalapi.Options{
		Secret:            cfg.WorkerSecret,
		Tokens:            tokens,
		TokenRepositories: cfg.JobTokenRepositories(),
		GlobalConfigRepo:  cfg.GlobalConfigRepo,
		GlobalConfigRef:   cfg.GlobalConfigRef,
		AfterResult:       afterResult,
	})
	return internalapi.Serve(ctx, cfg.InternalAddr, h, internalapi.WriteTimeout)
}

// triggerReconciles asks for a PR reconcile of every repository that takes part.
func triggerReconciles(ctx context.Context, r *updateprs.Runner) {
	if err := r.TriggerAll(ctx); err != nil && ctx.Err() == nil {
		slog.Error("update PR reconcile failed", "err", err)
	}
}
