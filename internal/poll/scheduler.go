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

	"github.com/robfig/cron/v3"
	"github.com/mdg-labs/release-ops/internal/providers/source"
	"github.com/mdg-labs/release-ops/internal/providers/ticket"
	"github.com/mdg-labs/release-ops/internal/store"
)

// MinPollIntervalMinutes is the minimum allowed cron interval (specs §4.1).
const MinPollIntervalMinutes int64 = 5

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

	cron                 *cron.Cron
	entryID              cron.EntryID
	currentScheduleSpec  string
	pollScheduleSpec     string
	scheduleMu           sync.Mutex

	lifecycleCtx context.Context

	runMu        sync.Mutex
	running      bool
	currentRunID string
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
		engine:         cfg.Engine,
		settings:       cfg.Settings,
		repos:          cfg.Repos,
		ticketProjects: cfg.TicketProjects,
		integrations:   cfg.Integrations,
		poll:           cfg.Poll,
		notifier:       cfg.Notifier,
		httpClient:     client,
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
		return fmt.Errorf("list enabled repos: %w", err)
	}

	var (
		reposChecked      int64
		ticketsCreated    int64
		ticketsSuperseded int64
		runErrors         []RunErrorEntry
	)

	for i := range repos {
		repo := repos[i]
		reposChecked++

		eval, pollErr := s.pollOneRepo(ctx, runID, repo)
		if pollErr != nil {
			runErrors = append(runErrors, RunErrorEntry{
				RepoID:  repo.ID,
				Message: pollErr.Error(),
			})
			if eval != nil {
				s.recordEvaluation(ctx, runID, repo.ID, eval, &ticketsCreated, &ticketsSuperseded)
			}
			continue
		}
		if eval != nil {
			s.recordEvaluation(ctx, runID, repo.ID, eval, &ticketsCreated, &ticketsSuperseded)
		}
	}

	errorsJSON, err := EncodeRunErrors(runErrors)
	if err != nil {
		return fmt.Errorf("encode run errors: %w", err)
	}

	status := RunFinishStatus(reposChecked, int64(len(runErrors)))
	if _, err := s.poll.FinishRun(ctx, runID, status, reposChecked, ticketsCreated, ticketsSuperseded, errorsJSON); err != nil {
		return fmt.Errorf("finish poll run: %w", err)
	}
	return nil
}

func (s *Scheduler) pollOneRepo(ctx context.Context, runID string, repo store.MonitoredRepo) (*RepoEvaluation, error) {
	if s.pollRepoFn != nil {
		return s.pollRepoFn(ctx, runID, repo)
	}
	return s.defaultPollRepo(ctx, runID, repo)
}

func (s *Scheduler) defaultPollRepo(ctx context.Context, _ string, repo store.MonitoredRepo) (*RepoEvaluation, error) {
	if s.ticketProjects == nil || s.integrations == nil {
		return nil, errors.New("ticket projects and integrations repositories are required")
	}

	tpRow, err := s.ticketProjects.Get(ctx, repo.TicketProjectID)
	if err != nil {
		return nil, fmt.Errorf("load ticket project: %w", err)
	}

	ticketIntegration, err := s.integrations.Get(ctx, tpRow.IntegrationID)
	if err != nil {
		return nil, fmt.Errorf("load ticket integration: %w", err)
	}

	ticketProject, err := TicketProjectFromStore(*tpRow, ticketIntegration.Kind, "")
	if err != nil {
		return nil, err
	}

	ticketProvider, err := s.resolveTicketProvider(ctx, tpRow.IntegrationID)
	if err != nil {
		return nil, err
	}

	sourceIntegrationBaseURL, err := s.sourceIntegrationBaseURL(ctx, repo)
	if err != nil {
		return nil, err
	}

	repoWebURL, err := ResolveRepoWebURL(repo, sourceIntegrationBaseURL)
	if err != nil {
		return nil, fmt.Errorf("resolve repo web url: %w", err)
	}

	sourceProvider, err := s.resolveSourceProvider(ctx, repo)
	if err != nil {
		return nil, err
	}

	release, fetchErr := sourceProvider.GetLatestRelease(ctx, repo.ProjectPath, source.ReleaseOptions{
		IncludePrereleases: repo.IncludePrereleases,
	})
	return s.engine.EvaluateRepo(ctx, repo, release, fetchErr, ticketProject, ticketProvider, repoWebURL)
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
	return true
}

func (s *Scheduler) finish() {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	s.running = false
	s.currentRunID = ""
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

type tokenPayload struct {
	Token string `json:"token"`
}

type phasicalPayload struct {
	APIKey string `json:"api_key"`
}

type jiraPayload struct {
	Email    string `json:"email"`
	APIToken string `json:"api_token"`
}

type linearPayload struct {
	APIKey string `json:"api_key"`
}

func (s *Scheduler) sourceIntegrationBaseURL(ctx context.Context, repo store.MonitoredRepo) (*string, error) {
	if repo.SourceIntegrationID == nil || *repo.SourceIntegrationID == "" {
		return nil, nil
	}
	integration, err := s.integrations.Get(ctx, *repo.SourceIntegrationID)
	if err != nil {
		return nil, fmt.Errorf("load source integration: %w", err)
	}
	if integration.Kind != repo.SourceKind {
		return nil, fmt.Errorf("integration kind %q does not match repo source_kind %q", integration.Kind, repo.SourceKind)
	}
	return integration.BaseURL, nil
}

func (s *Scheduler) resolveSourceProvider(ctx context.Context, repo store.MonitoredRepo) (source.SourceProvider, error) {
	var token string
	var baseURL *string

	if repo.SourceIntegrationID != nil && *repo.SourceIntegrationID != "" {
		integration, err := s.integrations.Get(ctx, *repo.SourceIntegrationID)
		if err != nil {
			return nil, fmt.Errorf("load source integration: %w", err)
		}
		if integration.Kind != repo.SourceKind {
			return nil, fmt.Errorf("integration kind %q does not match repo source_kind %q", integration.Kind, repo.SourceKind)
		}
		payload, err := s.integrations.DecryptPayload(ctx, integration.ID)
		if err != nil {
			return nil, fmt.Errorf("decrypt source integration: %w", err)
		}
		var creds tokenPayload
		if err := json.Unmarshal(payload, &creds); err != nil {
			return nil, fmt.Errorf("parse source integration payload: %w", err)
		}
		token = creds.Token
		baseURL = integration.BaseURL
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

func (s *Scheduler) resolveTicketProvider(ctx context.Context, integrationID string) (ticket.TicketProvider, error) {
	integration, err := s.integrations.Get(ctx, integrationID)
	if err != nil {
		return nil, fmt.Errorf("load ticket integration: %w", err)
	}

	payload, err := s.integrations.DecryptPayload(ctx, integration.ID)
	if err != nil {
		return nil, fmt.Errorf("decrypt ticket integration: %w", err)
	}

	switch integration.Kind {
	case "phasical":
		if integration.BaseURL == nil || *integration.BaseURL == "" {
			return nil, errors.New("phasical integration requires base_url")
		}
		var creds phasicalPayload
		if err := json.Unmarshal(payload, &creds); err != nil {
			return nil, fmt.Errorf("parse phasical payload: %w", err)
		}
		return ticket.NewPhasicalProvider(*integration.BaseURL, creds.APIKey, s.httpClient)
	case "jira":
		if integration.BaseURL == nil || *integration.BaseURL == "" {
			return nil, errors.New("jira integration requires base_url")
		}
		var creds jiraPayload
		if err := json.Unmarshal(payload, &creds); err != nil {
			return nil, fmt.Errorf("parse jira payload: %w", err)
		}
		return ticket.NewJiraProvider(*integration.BaseURL, creds.Email, creds.APIToken, s.httpClient)
	case "linear":
		var creds linearPayload
		if err := json.Unmarshal(payload, &creds); err != nil {
			return nil, fmt.Errorf("parse linear payload: %w", err)
		}
		return ticket.NewLinearProvider(creds.APIKey, s.httpClient)
	default:
		return nil, fmt.Errorf("unsupported ticket integration kind %q", integration.Kind)
	}
}
