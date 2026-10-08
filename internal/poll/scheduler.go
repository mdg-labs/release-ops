package poll

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/mdg-labs/release-ops/internal/providers/source"
	"github.com/mdg-labs/release-ops/internal/providers/ticket"
	"github.com/mdg-labs/release-ops/internal/store"
	"github.com/robfig/cron/v3"
)

// MinPollIntervalMinutes is the minimum allowed cron interval (specs §4.1).
const MinPollIntervalMinutes int64 = 5

// finishRunTimeout bounds the write that records a run's final state.
const finishRunTimeout = 5 * time.Second

// ShutdownWaitTimeout bounds how long shutdown waits for an in-flight run to write its
// finish: the finish-write bound plus time for the run to notice the cancellation.
const ShutdownWaitTimeout = finishRunTimeout + 3*time.Second

// ErrAlreadyRunning is returned when a poll is requested while another run is active.
var ErrAlreadyRunning = errors.New("poll already running")

// SchedulerConfig wires repositories and the poll engine for scheduled and manual runs.
type SchedulerConfig struct {
	Engine         *Engine
	Settings       store.SettingsRepository
	Repos          store.MonitoredRepoRepository
	TicketProjects store.TicketProjectRepository
	Integrations   store.IntegrationRepository
	Poll           store.PollRepository
	Notifier       *Notifier
	HTTPClient     *http.Client
	// PollRepo overrides per-repo polling (tests only). When nil, the default resolver runs.
	PollRepo func(ctx context.Context, runID string, repo store.MonitoredRepo) (*RepoEvaluation, error)
	// PollScheduleSpec overrides the cron expression when non-empty (tests only).
	PollScheduleSpec string
}

// Scheduler runs polls on a cron interval and accepts manual triggers (specs §5.6–§5.7).
type Scheduler struct {
	engine         *Engine
	settings       store.SettingsRepository
	repos          store.MonitoredRepoRepository
	ticketProjects store.TicketProjectRepository
	integrations   store.IntegrationRepository
	poll           store.PollRepository
	notifier       *Notifier
	httpClient     *http.Client
	pollRepoFn     func(ctx context.Context, runID string, repo store.MonitoredRepo) (*RepoEvaluation, error)

	cron                *cron.Cron
	entryID             cron.EntryID
	currentScheduleSpec string
	pollScheduleSpec    string
	scheduleMu          sync.Mutex

	lifecycleCtx context.Context

	runMu        sync.Mutex
	running      bool
	currentRunID string
	// activeRuns counts runs between tryStart and finish, scheduled and manual alike.
	activeRuns sync.WaitGroup
}

// NewScheduler returns a scheduler from cfg. Engine and Poll repositories are required.
func NewScheduler(cfg SchedulerConfig) (*Scheduler, error) {
	if cfg.Engine == nil {
		return nil, errors.New("poll scheduler: engine is required")
	}
	if cfg.Poll == nil {
		return nil, errors.New("poll scheduler: poll repository is required")
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &Scheduler{
		engine:           cfg.Engine,
		settings:         cfg.Settings,
		repos:            cfg.Repos,
		ticketProjects:   cfg.TicketProjects,
		integrations:     cfg.Integrations,
		poll:             cfg.Poll,
		notifier:         cfg.Notifier,
		httpClient:       client,
		pollRepoFn:       cfg.PollRepo,
		pollScheduleSpec: cfg.PollScheduleSpec,
		cron:             cron.New(),
	}, nil
}

// Start loads the cron schedule from app_settings and runs until ctx is cancelled.
// On shutdown the cron stops gracefully and waits for any in-flight scheduled job.
func (s *Scheduler) Start(ctx context.Context) error {
	s.lifecycleCtx = ctx

	if err := s.reloadSchedule(ctx); err != nil {
		return err
	}

	s.cron.Start()

	go func() {
		<-ctx.Done()
		stopCtx := s.cron.Stop()
		<-stopCtx.Done()
	}()

	go s.watchSettings(ctx)

	return nil
}

// Shutdown stops the cron and waits for every in-flight run, scheduled or manual, to
// finish. It returns ctx's error if ctx ends first. Call it after cancelling the context
// given to Start and after nothing can call Trigger any more.
func (s *Scheduler) Shutdown(ctx context.Context) error {
	select {
	case <-s.cron.Stop().Done():
	case <-ctx.Done():
		return ctx.Err()
	}

	done := make(chan struct{})
	go func() {
		s.activeRuns.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Trigger starts an asynchronous poll run and returns the new poll_runs id (specs §7.1).
func (s *Scheduler) Trigger(ctx context.Context) (string, error) {
	if !s.tryStart() {
		return "", ErrAlreadyRunning
	}

	run, err := s.poll.InsertRun(ctx, store.PollTriggerSourceManual)
	if err != nil {
		s.finish()
		return "", err
	}

	s.setCurrentRunID(run.ID)

	runCtx := s.lifecycleCtx
	if runCtx == nil {
		runCtx = context.Background()
	}
	go s.executeRun(runCtx, run.ID)

	return run.ID, nil
}

// IsPolling reports whether a poll run is currently active.
func (s *Scheduler) IsPolling() bool {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	return s.running
}

// RunAll executes one poll cycle: enabled repos, poll_runs row, repos_checked counter (specs §5.6).
func (s *Scheduler) RunAll(ctx context.Context, runID string) error {
	repos, err := s.repos.ListEnabled(ctx)
	if err != nil {
		listErr := fmt.Errorf("list enabled repos: %w", err)
		slog.Error("poll run failed before polling", "runId", runID, "error", listErr)
		if finishErr := s.finishRun(ctx, runID, RunStatusFailed, 0, 0, 0, []RunErrorEntry{{Message: "enabled repos could not be listed"}}); finishErr != nil {
			return errors.Join(listErr, finishErr)
		}
		return listErr
	}

	var (
		reposChecked      int64
		ticketsCreated    int64
		ticketsSuperseded int64
		runErrors         []RunErrorEntry
		interrupted       bool
	)

	integrations := newIntegrationCache(s.integrations)

	for i := range repos {
		if ctx.Err() != nil {
			interrupted = true
			break
		}
		repo := repos[i]
		reposChecked++

		eval, pollErr := s.pollOneRepo(ctx, runID, repo, integrations)
		if pollErr != nil {
			runErrors = append(runErrors, RunErrorEntry{
				RepoID:  repo.ID,
				Message: pollErr.Error(),
			})
			// A failure before the engine evaluated the repo is recorded like an
			// engine-reported error: error event, last_error, notification. Errors
			// returned by the engine itself are not: it owns the repo's poll state.
			var preErr *preEngineError
			if errors.As(pollErr, &preErr) {
				eval = s.recordPollFailure(ctx, repo, pollErr)
			}
		} else if eval.hasError() {
			// The engine reports fetch/ticket failures as an error action with a nil error.
			runErrors = append(runErrors, RunErrorEntry{
				RepoID:  repo.ID,
				Message: eval.Detail,
			})
		}
		if eval != nil {
			s.recordEvaluation(ctx, runID, repo.ID, eval, &ticketsCreated, &ticketsSuperseded)
		}
	}

	status := RunFinishStatus(reposChecked, int64(len(runErrors)))
	if interrupted {
		// Repos were left unpolled, so the run is never a success.
		runErrors = append(runErrors, RunErrorEntry{Message: interruptedRunMessage})
		if status == RunStatusSuccess {
			status = RunStatusPartial
		}
	}
	return s.finishRun(ctx, runID, status, reposChecked, ticketsCreated, ticketsSuperseded, runErrors)
}

// finishRun stores the run's final state. The write ignores ctx cancellation: a run cut
// short by shutdown would otherwise stay "running" with its errors lost.
func (s *Scheduler) finishRun(
	ctx context.Context,
	runID, status string,
	reposChecked, ticketsCreated, ticketsSuperseded int64,
	runErrors []RunErrorEntry,
) error {
	errorsJSON, err := EncodeRunErrors(runErrors)
	if err != nil {
		return fmt.Errorf("encode run errors: %w", err)
	}

	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), finishRunTimeout)
	defer cancel()
	if _, err := s.poll.FinishRun(writeCtx, runID, status, reposChecked, ticketsCreated, ticketsSuperseded, errorsJSON); err != nil {
		return fmt.Errorf("finish poll run: %w", err)
	}
	return nil
}

// recordPollFailure persists pollErr as the repo's last_error and returns the error
// evaluation to record. The evaluation is returned even when last_error cannot be
// stored, so the event and the notification are not lost with it.
func (s *Scheduler) recordPollFailure(ctx context.Context, repo store.MonitoredRepo, pollErr error) *RepoEvaluation {
	eval, err := s.engine.recordError(ctx, repo, pollErr)
	if err != nil {
		slog.Error("record poll failure", "repoId", repo.ID, "error", err)
		return &RepoEvaluation{
			Actions: []string{ActionError},
			Repo:    &repo,
			Detail:  pollErr.Error(),
		}
	}
	return eval
}

func (s *Scheduler) pollOneRepo(
	ctx context.Context,
	runID string,
	repo store.MonitoredRepo,
	integrations *integrationCache,
) (*RepoEvaluation, error) {
	if s.pollRepoFn != nil {
		return s.pollRepoFn(ctx, runID, repo)
	}
	return s.defaultPollRepo(ctx, repo, integrations)
}

// preEngineError marks a failure that happened before the engine evaluated the repo,
// so the engine has not touched the repo's poll state. Errors returned by
// EvaluateRepo are never wrapped: the engine may already have stored new state.
type preEngineError struct{ err error }

func (e *preEngineError) Error() string { return e.err.Error() }
func (e *preEngineError) Unwrap() error { return e.err }

// pollInputs is everything the engine needs to evaluate one repo.
type pollInputs struct {
	sourceProvider source.SourceProvider
	ticketProject  ticket.TicketProject
	ticketProvider ticket.TicketProvider
	repoWebURL     string
}

func (s *Scheduler) defaultPollRepo(ctx context.Context, repo store.MonitoredRepo, integrations *integrationCache) (*RepoEvaluation, error) {
	in, err := s.prepareRepoPoll(ctx, repo, integrations)
	if err != nil {
		return nil, &preEngineError{err: err}
	}
	release, fetchErr := in.sourceProvider.GetLatestRelease(ctx, repo.ProjectPath, source.ReleaseOptions{
		IncludePrereleases: repo.IncludePrereleases,
	})
	return s.engine.EvaluateRepo(ctx, repo, release, fetchErr, in.ticketProject, in.ticketProvider, in.repoWebURL)
}

func (s *Scheduler) prepareRepoPoll(ctx context.Context, repo store.MonitoredRepo, integrations *integrationCache) (*pollInputs, error) {
	if s.ticketProjects == nil || s.integrations == nil {
		return nil, errors.New("ticket projects and integrations repositories are required")
	}

	tpRow, err := s.ticketProjects.Get(ctx, repo.TicketProjectID)
	if err != nil {
		return nil, fmt.Errorf("load ticket project: %w", err)
	}

	ticketIntegration, err := integrations.get(ctx, tpRow.IntegrationID)
	if err != nil {
		return nil, fmt.Errorf("load ticket integration: %w", err)
	}

	ticketProject, err := TicketProjectFromStore(*tpRow, ticketIntegration.integration.Kind)
	if err != nil {
		return nil, err
	}

	ticketProvider, err := ticket.NewProviderFromIntegration(
		ticketIntegration.integration.Kind,
		ticketIntegration.integration.BaseURL,
		ticketIntegration.secret,
		s.httpClient,
	)
	if err != nil {
		return nil, redactSecretParseError(err)
	}

	var sourceIntegration *resolvedIntegration
	if repo.SourceIntegrationID != nil && *repo.SourceIntegrationID != "" {
		sourceIntegration, err = integrations.get(ctx, *repo.SourceIntegrationID)
		if err != nil {
			return nil, fmt.Errorf("load source integration: %w", err)
		}
		if sourceIntegration.integration.Kind != repo.SourceKind {
			return nil, fmt.Errorf("integration kind %q does not match repo source_kind %q", sourceIntegration.integration.Kind, repo.SourceKind)
		}
	}

	var sourceBaseURL *string
	if sourceIntegration != nil {
		sourceBaseURL = sourceIntegration.integration.BaseURL
	}
	repoWebURL, err := ResolveRepoWebURL(repo, sourceBaseURL)
	if err != nil {
		return nil, fmt.Errorf("resolve repo web url: %w", err)
	}

	sourceProvider, err := s.newSourceProvider(repo, sourceIntegration)
	if err != nil {
		return nil, err
	}

	return &pollInputs{
		sourceProvider: sourceProvider,
		ticketProject:  ticketProject,
		ticketProvider: ticketProvider,
		repoWebURL:     repoWebURL,
	}, nil
}

func (s *Scheduler) recordEvaluation(
	ctx context.Context,
	runID, repoID string,
	eval *RepoEvaluation,
	ticketsCreated, ticketsSuperseded *int64,
) {
	s.recorder().RecordEvaluation(ctx, runID, repoID, eval, ticketsCreated, ticketsSuperseded)
}

func (s *Scheduler) recorder() *RunRecorder {
	return NewRunRecorder(s.poll, s.notifier)
}

func (s *Scheduler) executeRun(ctx context.Context, runID string) {
	defer s.finish()
	if err := s.RunAll(ctx, runID); err != nil {
		slog.Error("poll run failed", "runId", runID, "error", err)
	}
}

func (s *Scheduler) runScheduled() {
	if !s.tryStart() {
		slog.Debug("skipping scheduled poll: already running")
		return
	}

	ctx := s.lifecycleCtx
	if ctx == nil {
		ctx = context.Background()
	}

	run, err := s.poll.InsertRun(ctx, store.PollTriggerSourceScheduled)
	if err != nil {
		s.finish()
		slog.Error("insert scheduled poll run", "error", err)
		return
	}

	s.setCurrentRunID(run.ID)
	s.executeRun(ctx, run.ID)
}

func (s *Scheduler) watchSettings(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.reloadSchedule(ctx); err != nil {
				slog.Error("reload poll schedule", "error", err)
			}
		}
	}
}

func (s *Scheduler) reloadSchedule(ctx context.Context) error {
	if s.settings == nil {
		return errors.New("settings repository is required")
	}

	settings, err := s.settings.Get(ctx)
	if err != nil {
		return fmt.Errorf("load app settings: %w", err)
	}

	minutes := ClampPollIntervalMinutes(settings.PollIntervalMinutes)
	spec := fmt.Sprintf("@every %dm", minutes)
	if s.pollScheduleSpec != "" {
		spec = s.pollScheduleSpec
	}

	s.scheduleMu.Lock()
	defer s.scheduleMu.Unlock()

	if spec == s.currentScheduleSpec && s.entryID != 0 {
		return nil
	}

	if s.entryID != 0 {
		s.cron.Remove(s.entryID)
		s.entryID = 0
	}

	entryID, err := s.cron.AddFunc(spec, s.runScheduled)
	if err != nil {
		return fmt.Errorf("add cron job %q: %w", spec, err)
	}
	s.entryID = entryID
	s.currentScheduleSpec = spec
	return nil
}

func (s *Scheduler) tryStart() bool {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	if s.running {
		return false
	}
	s.running = true
	s.activeRuns.Add(1)
	return true
}

func (s *Scheduler) finish() {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	s.running = false
	s.currentRunID = ""
	s.activeRuns.Done()
}

func (s *Scheduler) setCurrentRunID(runID string) {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	s.currentRunID = runID
}

// ClampPollIntervalMinutes enforces the minimum poll interval from app_settings.
func ClampPollIntervalMinutes(minutes int64) int64 {
	if minutes < MinPollIntervalMinutes {
		return MinPollIntervalMinutes
	}
	return minutes
}

// resolvedIntegration is an integration row with its decrypted secret payload.
type resolvedIntegration struct {
	integration *store.Integration
	secret      []byte
}

// integrationCache loads and decrypts each integration at most once per poll run.
type integrationCache struct {
	repo    store.IntegrationRepository
	entries map[string]*resolvedIntegration
}

func newIntegrationCache(repo store.IntegrationRepository) *integrationCache {
	return &integrationCache{repo: repo, entries: make(map[string]*resolvedIntegration)}
}

func (c *integrationCache) get(ctx context.Context, id string) (*resolvedIntegration, error) {
	if entry, ok := c.entries[id]; ok {
		return entry, nil
	}
	integration, err := c.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	secret, err := c.repo.DecryptPayload(ctx, id)
	if err != nil {
		slog.Error("decrypt integration payload", "integrationId", id, "error", err)
		return nil, errors.New("decrypt integration: secret could not be decrypted")
	}
	entry := &resolvedIntegration{integration: integration, secret: secret}
	c.entries[id] = entry
	return entry, nil
}

// redactSecretParseError replaces a JSON decoding error raised while reading an
// integration secret: its text can quote a fragment of the decrypted payload, and
// the message is stored on the repo, in a poll event and sent to notification targets.
func redactSecretParseError(err error) error {
	var syntaxErr *json.SyntaxError
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &syntaxErr) || errors.As(err, &typeErr) {
		return errors.New("integration secret is malformed")
	}
	return err
}

func (s *Scheduler) newSourceProvider(repo store.MonitoredRepo, integration *resolvedIntegration) (source.SourceProvider, error) {
	var token string
	var baseURL *string
	if integration != nil {
		parsed, err := source.ParseTokenSecret(integration.secret)
		if err != nil {
			return nil, fmt.Errorf("parse source integration payload: %w", redactSecretParseError(err))
		}
		token = parsed
		baseURL = integration.integration.BaseURL
	}

	switch repo.SourceKind {
	case source.KindGitHub:
		return source.NewGitHubSource(token, s.httpClient), nil
	case source.KindCodeberg:
		return source.NewCodebergSource(token, s.httpClient), nil
	case source.KindGitLab:
		if baseURL == nil || *baseURL == "" {
			return nil, errors.New("gitlab source requires source_integration with base_url")
		}
		return source.NewGitLabSource(*baseURL, token, s.httpClient)
	case source.KindGitea:
		if baseURL == nil || *baseURL == "" {
			return nil, errors.New("gitea source requires source_integration with base_url")
		}
		return source.NewGiteaSource(*baseURL, token, s.httpClient)
	case source.KindForgejo:
		if baseURL == nil || *baseURL == "" {
			return nil, errors.New("forgejo source requires source_integration with base_url")
		}
		return source.NewForgejoSource(*baseURL, token, s.httpClient)
	default:
		return nil, fmt.Errorf("unsupported source_kind %q", repo.SourceKind)
	}
}
