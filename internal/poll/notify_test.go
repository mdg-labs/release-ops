package poll_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/mdg-labs/release-ops/internal/poll"
	"github.com/mdg-labs/release-ops/internal/store"
)

type notifyMockRepo struct {
	enabled []store.NotificationTarget
	urls    map[string]string
	listErr error
	decrypt map[string]error
}

func (m *notifyMockRepo) Create(context.Context, store.CreateNotificationTargetInput) (*store.NotificationTarget, error) {
	return nil, errors.New("not implemented")
}

func (m *notifyMockRepo) Get(context.Context, string) (*store.NotificationTarget, error) {
	return nil, errors.New("not implemented")
}

func (m *notifyMockRepo) List(context.Context) ([]store.NotificationTarget, error) {
	return nil, errors.New("not implemented")
}

func (m *notifyMockRepo) ListEnabled(context.Context) ([]store.NotificationTarget, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return append([]store.NotificationTarget(nil), m.enabled...), nil
}

func (m *notifyMockRepo) Update(context.Context, string, store.UpdateNotificationTargetInput) (*store.NotificationTarget, error) {
	return nil, errors.New("not implemented")
}

func (m *notifyMockRepo) Delete(context.Context, string) error {
	return errors.New("not implemented")
}

func (m *notifyMockRepo) DecryptURL(_ context.Context, id string) (string, error) {
	if m.decrypt != nil {
		if err := m.decrypt[id]; err != nil {
			return "", err
		}
	}
	url, ok := m.urls[id]
	if !ok {
		return "", errors.New("unknown target")
	}
	return url, nil
}

type sendRecorder struct {
	mu      sync.Mutex
	calls   []sendCall
	sendErr error
}

type sendCall struct {
	url     string
	message string
}

func (r *sendRecorder) Send(url, message string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, sendCall{url: url, message: message})
	return r.sendErr
}

func (r *sendRecorder) callsSnapshot() []sendCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]sendCall, len(r.calls))
	copy(out, r.calls)
	return out
}

func sampleRepo() store.MonitoredRepo {
	tag := "v1.2.3"
	return store.MonitoredRepo{
		ID:            "repo-1",
		SourceKind:    "github",
		ProjectPath:   "org/app",
		OpenTicketTag: &tag,
	}
}

func TestNotifyRepoActionSendsForCreateErrorSupersede(t *testing.T) {
	repo := sampleRepo()
	targets := &notifyMockRepo{
		enabled: []store.NotificationTarget{
			{ID: "nt-1", Name: "Slack", Events: []string{"create", "error", "supersede"}, Enabled: true},
		},
		urls: map[string]string{"nt-1": "slack://token@channel"},
	}
	rec := &sendRecorder{}
	notifier := poll.NewNotifier(targets, rec.Send)

	for _, action := range []string{poll.ActionCreate, poll.ActionError, poll.ActionSupersede} {
		rec.calls = nil
		detail := ""
		if action == poll.ActionError {
			detail = "fetch failed"
		}
		notifier.NotifyRepoAction(context.Background(), repo, action, detail)
		calls := rec.callsSnapshot()
		if len(calls) != 1 {
			t.Fatalf("action %s: got %d sends, want 1", action, len(calls))
		}
		if calls[0].url != "slack://token@channel" {
			t.Fatalf("action %s: url leaked or wrong: %q", action, calls[0].url)
		}
		if !strings.Contains(calls[0].message, "org/app") {
			t.Fatalf("action %s: message = %q, want repo path", action, calls[0].message)
		}
	}
}

func TestNotifyRepoActionSkipsNonNotifyActions(t *testing.T) {
	targets := &notifyMockRepo{
		enabled: []store.NotificationTarget{
			{ID: "nt-1", Name: "Slack", Events: []string{"create", "error", "supersede"}, Enabled: true},
		},
		urls: map[string]string{"nt-1": "slack://token@channel"},
	}
	rec := &sendRecorder{}
	notifier := poll.NewNotifier(targets, rec.Send)

	for _, action := range []string{poll.ActionBaseline, poll.ActionSkip, poll.ActionMerge, poll.ActionSkipOpen} {
		notifier.NotifyRepoAction(context.Background(), sampleRepo(), action, "")
	}
	if len(rec.callsSnapshot()) != 0 {
		t.Fatalf("expected no sends, got %d", len(rec.callsSnapshot()))
	}
}

func TestNotifyRepoActionFiltersByEventsJSON(t *testing.T) {
	targets := &notifyMockRepo{
		enabled: []store.NotificationTarget{
			{ID: "nt-create", Name: "Create only", Events: []string{"create"}, Enabled: true},
			{ID: "nt-error", Name: "Error only", Events: []string{"error"}, Enabled: true},
		},
		urls: map[string]string{
			"nt-create": "slack://create@channel",
			"nt-error":  "ntfy://example/error",
		},
	}
	rec := &sendRecorder{}
	notifier := poll.NewNotifier(targets, rec.Send)

	notifier.NotifyRepoAction(context.Background(), sampleRepo(), poll.ActionCreate, "")
	calls := rec.callsSnapshot()
	if len(calls) != 1 || calls[0].url != "slack://create@channel" {
		t.Fatalf("create filter: %+v", calls)
	}

	rec.calls = nil
	notifier.NotifyRepoAction(context.Background(), sampleRepo(), poll.ActionError, "boom")
	calls = rec.callsSnapshot()
	if len(calls) != 1 || calls[0].url != "ntfy://example/error" {
		t.Fatalf("error filter: %+v", calls)
	}
}

func TestNotifyRepoActionRepoTargetOverride(t *testing.T) {
	repo := sampleRepo()
	repo.NotificationTargetIDs = []string{"nt-b"}
	targets := &notifyMockRepo{
		enabled: []store.NotificationTarget{
			{ID: "nt-a", Name: "Global A", Events: []string{"create"}, Enabled: true},
			{ID: "nt-b", Name: "Repo B", Events: []string{"create"}, Enabled: true},
		},
		urls: map[string]string{
			"nt-a": "slack://a@channel",
			"nt-b": "generic://example/b",
		},
	}
	rec := &sendRecorder{}
	notifier := poll.NewNotifier(targets, rec.Send)

	notifier.NotifyRepoAction(context.Background(), repo, poll.ActionCreate, "")
	calls := rec.callsSnapshot()
	if len(calls) != 1 || calls[0].url != "generic://example/b" {
		t.Fatalf("repo override: %+v", calls)
	}
}

func TestNotifyRepoActionNoRepoOverrideUsesAllEnabled(t *testing.T) {
	targets := &notifyMockRepo{
		enabled: []store.NotificationTarget{
			{ID: "nt-a", Name: "A", Events: []string{"create"}, Enabled: true},
			{ID: "nt-b", Name: "B", Events: []string{"create"}, Enabled: true},
		},
		urls: map[string]string{
			"nt-a": "slack://a@channel",
			"nt-b": "discord://token@channel",
		},
	}
	rec := &sendRecorder{}
	notifier := poll.NewNotifier(targets, rec.Send)

	notifier.NotifyRepoAction(context.Background(), sampleRepo(), poll.ActionCreate, "")
	if len(rec.callsSnapshot()) != 2 {
		t.Fatalf("want 2 sends, got %d", len(rec.callsSnapshot()))
	}
}

func TestNotifyRepoActionSendFailureDoesNotPanic(t *testing.T) {
	targets := &notifyMockRepo{
		enabled: []store.NotificationTarget{
			{ID: "nt-1", Name: "Slack", Events: []string{"create"}, Enabled: true},
		},
		urls: map[string]string{"nt-1": "slack://token@channel"},
	}
	rec := &sendRecorder{sendErr: errors.New("slack://token@channel: delivery failed")}
	notifier := poll.NewNotifier(targets, rec.Send)

	notifier.NotifyRepoAction(context.Background(), sampleRepo(), poll.ActionCreate, "")
}

func TestNotifyRepoActionListEnabledFailureDoesNotPanic(t *testing.T) {
	targets := &notifyMockRepo{listErr: errors.New("db down")}
	rec := &sendRecorder{}
	notifier := poll.NewNotifier(targets, rec.Send)

	notifier.NotifyRepoAction(context.Background(), sampleRepo(), poll.ActionCreate, "")
	if len(rec.callsSnapshot()) != 0 {
		t.Fatal("expected no sends when list fails")
	}
}

func TestNotifierSendTest(t *testing.T) {
	rec := &sendRecorder{}
	notifier := poll.NewNotifier(&notifyMockRepo{}, rec.Send)

	if err := notifier.SendTest(context.Background(), "generic://example/test"); err != nil {
		t.Fatalf("SendTest: %v", err)
	}
	calls := rec.callsSnapshot()
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	if calls[0].url != "generic://example/test" {
		t.Fatalf("url = %q", calls[0].url)
	}
	if !strings.Contains(calls[0].message, "test notification") {
		t.Fatalf("message = %q", calls[0].message)
	}
}

func TestDefaultShoutrrrSendAcceptsURLSchemes(t *testing.T) {
	// Verify shoutrrr can parse common URL schemes without sending (Locate only).
	schemes := []string{
		"slack://token@channel",
		"ntfy://example/topic",
		"generic://example/hook",
		"discord://token@channel",
	}
	for _, url := range schemes {
		t.Run(url, func(t *testing.T) {
			rec := &sendRecorder{}
			notifier := poll.NewNotifier(&notifyMockRepo{
				enabled: []store.NotificationTarget{
					{ID: "nt-1", Name: "t", Events: []string{"create"}, Enabled: true},
				},
				urls: map[string]string{"nt-1": url},
			}, rec.Send)
			notifier.NotifyRepoAction(context.Background(), sampleRepo(), poll.ActionCreate, "")
			if len(rec.callsSnapshot()) != 1 {
				t.Fatalf("expected send attempt for %s", url)
			}
		})
	}
}

func TestSchedulerRecordEvaluationDispatchesNotifications(t *testing.T) {
	repo := sampleRepo()
	repo.ID = "repo-sched"
	targets := &notifyMockRepo{
		enabled: []store.NotificationTarget{
			{ID: "nt-1", Name: "Slack", Events: []string{"create", "supersede"}, Enabled: true},
		},
		urls: map[string]string{"nt-1": "slack://token@channel"},
	}
	rec := &sendRecorder{}
	schedPoll := &schedulerMockPollRepo{}
	tag := "v2.0.0"
	eval := &poll.RepoEvaluation{
		Actions: []string{poll.ActionSupersede, poll.ActionCreate},
		Repo: &store.MonitoredRepo{
			ID:            repo.ID,
			SourceKind:    repo.SourceKind,
			ProjectPath:   repo.ProjectPath,
			OpenTicketTag: &tag,
		},
	}

	sched, err := poll.NewScheduler(poll.SchedulerConfig{
		Engine:   poll.NewEngine(schedPoll),
		Settings: &schedulerMockSettingsRepo{pollIntervalMinutes: 5},
		Repos:    &schedulerMockReposRepo{repos: []store.MonitoredRepo{repo}},
		Poll:     schedPoll,
		Notifier: poll.NewNotifier(targets, rec.Send),
		PollRepo: func(_ context.Context, _ string, _ store.MonitoredRepo) (*poll.RepoEvaluation, error) {
			return eval, nil
		},
	})
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}

	if err := sched.RunAll(context.Background(), "run-1"); err != nil {
		t.Fatalf("RunAll: %v", err)
	}
	if len(rec.callsSnapshot()) != 2 {
		t.Fatalf("want 2 notification sends (supersede + create), got %d", len(rec.callsSnapshot()))
	}
}
