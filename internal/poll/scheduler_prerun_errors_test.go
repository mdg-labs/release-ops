package poll_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mdg-labs/release-ops/internal/crypto"
	"github.com/mdg-labs/release-ops/internal/poll"
	"github.com/mdg-labs/release-ops/internal/providers/ticket"
	"github.com/mdg-labs/release-ops/internal/store"
)

const (
	validStatusMapping = `{"open":["ready"],"done":["done"],"cancelled":["cancelled"],"superseded":"cancelled"}`
	leakProbeSecret    = "probe-secret-4f9c1e"
)

// failingDecryptIntegrations fails DecryptPayload with an error that carries secret text.
type failingDecryptIntegrations struct {
	store.IntegrationRepository
}

func (failingDecryptIntegrations) DecryptPayload(context.Context, string) ([]byte, error) {
	return nil, errors.New("cipher: cannot open " + leakProbeSecret)
}

// TestSchedulerRecordsFailuresBeforeEngine runs the default per-repo path (no PollRepo
// override) against a real store; each case fails before the engine evaluates the repo.
func TestSchedulerRecordsFailuresBeforeEngine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		kind          string
		baseURL       string
		secret        string
		mapping       string
		failDecrypt   bool
		wantMessage   string
		wantMessagePf string
	}{
		{
			name:        "provider build fails on a Jira secret without email",
			kind:        "jira",
			baseURL:     "https://jira.example.com",
			secret:      `{"api_token":"` + leakProbeSecret + `"}`,
			mapping:     validStatusMapping,
			wantMessage: "jira: email is required in integration secret",
		},
		{
			name:          "status mapping does not parse",
			kind:          "kaneo",
			baseURL:       "https://api.kaneo.example",
			secret:        `{"api_key":"` + leakProbeSecret + `"}`,
			mapping:       `[]`,
			wantMessagePf: "parse status_mapping: ",
		},
		{
			name:        "secret is not valid JSON",
			kind:        "kaneo",
			baseURL:     "https://api.kaneo.example",
			secret:      leakProbeSecret,
			mapping:     validStatusMapping,
			wantMessage: "integration secret is malformed",
		},
		{
			name:        "secret cannot be decrypted",
			kind:        "kaneo",
			baseURL:     "https://api.kaneo.example",
			secret:      `{"api_key":"x"}`,
			mapping:     validStatusMapping,
			failDecrypt: true,
			wantMessage: "load ticket integration: decrypt integration: secret could not be decrypted",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()

			sqlDB := openIntegrationDB(t, filepath.Join(t.TempDir(), "app.db"))
			t.Cleanup(func() { _ = sqlDB.Close() })
			cipher, err := crypto.NewCipherFromHex(integrationTestKeyHex)
			if err != nil {
				t.Fatalf("NewCipherFromHex: %v", err)
			}
			seedIntegrationAppSettings(t, sqlDB)
			s := store.New(sqlDB, cipher)

			integration, err := s.Integrations().Create(ctx, store.CreateIntegrationInput{
				Kind:    tt.kind,
				Name:    "Tickets",
				BaseURL: integrationStrPtr(tt.baseURL),
				Secret:  []byte(tt.secret),
			})
			if err != nil {
				t.Fatalf("Create integration: %v", err)
			}
			project, err := s.TicketProjects().Create(ctx, store.CreateTicketProjectInput{
				IntegrationID:      integration.ID,
				ExternalProjectID:  "proj-1",
				Name:               "Project",
				CreateConfig:       `{}`,
				StatusMapping:      tt.mapping,
				OnOpenTicketPolicy: ticket.PolicySupersede,
			})
			if err != nil {
				t.Fatalf("Create ticket project: %v", err)
			}
			repo, err := s.Repos().Create(ctx, store.CreateMonitoredRepoInput{
				SourceKind:      "github",
				ProjectPath:     "org/app",
				Enabled:         true,
				TicketProjectID: project.ID,
			})
			if err != nil {
				t.Fatalf("Create repo: %v", err)
			}

			integrations := s.Integrations()
			if tt.failDecrypt {
				integrations = failingDecryptIntegrations{IntegrationRepository: integrations}
			}
			rec := &sendRecorder{}
			targets := &notifyMockRepo{
				enabled: []store.NotificationTarget{{ID: "t1", Name: "ops", Events: []string{poll.EventError}, Enabled: true}},
				urls:    map[string]string{"t1": "generic://example.invalid"},
			}
			scheduler, err := poll.NewScheduler(poll.SchedulerConfig{
				Engine:         poll.NewEngine(s.Poll()),
				Repos:          s.Repos(),
				TicketProjects: s.TicketProjects(),
				Integrations:   integrations,
				Poll:           s.Poll(),
				Notifier:       poll.NewNotifier(targets, rec.Send),
			})
			if err != nil {
				t.Fatalf("NewScheduler: %v", err)
			}

			run, err := s.Poll().InsertRun(ctx, store.PollTriggerSourceManual)
			if err != nil {
				t.Fatalf("InsertRun: %v", err)
			}
			if err := scheduler.RunAll(ctx, run.ID); err != nil {
				t.Fatalf("RunAll: %v", err)
			}

			gotRun, err := s.Poll().GetRun(ctx, run.ID)
			if err != nil {
				t.Fatalf("GetRun: %v", err)
			}
			if gotRun.Status != poll.RunStatusFailed || gotRun.ReposChecked != 1 {
				t.Fatalf("run status/repos_checked = %q/%d, want failed/1", gotRun.Status, gotRun.ReposChecked)
			}

			events, err := s.Poll().ListEventsByRunID(ctx, run.ID)
			if err != nil {
				t.Fatalf("ListEventsByRunID: %v", err)
			}
			if len(events) != 1 || events[0].Action != poll.ActionError {
				t.Fatalf("events = %+v, want one error event", events)
			}
			if events[0].MonitoredRepoID == nil || *events[0].MonitoredRepoID != repo.ID {
				t.Fatalf("event repo = %v, want %s", events[0].MonitoredRepoID, repo.ID)
			}
			if events[0].Detail == nil {
				t.Fatal("error event has no detail")
			}
			detail := *events[0].Detail
			assertPreEngineMessage(t, "event detail", detail, tt.wantMessage, tt.wantMessagePf)

			updated, err := s.Repos().Get(ctx, repo.ID)
			if err != nil {
				t.Fatalf("Get repo: %v", err)
			}
			if updated.LastError == nil || *updated.LastError != detail {
				t.Fatalf("last_error = %v, want %q", updated.LastError, detail)
			}

			calls := rec.callsSnapshot()
			if len(calls) != 1 {
				t.Fatalf("notifier calls = %d, want 1", len(calls))
			}
			assertPreEngineMessage(t, "notification", strings.TrimPrefix(calls[0].message, "Release Ops: poll error for github/org/app: "), tt.wantMessage, tt.wantMessagePf)

			for name, text := range map[string]string{
				"event detail": detail,
				"last_error":   *updated.LastError,
				"notification": calls[0].message,
				"errors_json":  gotRun.ErrorsJSON,
			} {
				if strings.Contains(text, leakProbeSecret) {
					t.Fatalf("%s leaks the integration secret: %q", name, text)
				}
			}
		})
	}
}

func assertPreEngineMessage(t *testing.T, what, got, want, wantPrefix string) {
	t.Helper()
	if want != "" && got != want {
		t.Fatalf("%s = %q, want %q", what, got, want)
	}
	if wantPrefix != "" && !strings.HasPrefix(got, wantPrefix) {
		t.Fatalf("%s = %q, want prefix %q", what, got, wantPrefix)
	}
}
