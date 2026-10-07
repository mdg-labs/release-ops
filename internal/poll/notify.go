package poll

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/containrrr/shoutrrr"
	"github.com/mdg-labs/release-ops/internal/store"
)

const testNotificationMessage = "Release Ops: test notification"

// Notification events (specs §4.5).
const (
	EventCreate    = "create"
	EventError     = "error"
	EventSupersede = "supersede"
)

// ShoutrrrSendFunc delivers a message via Shoutrrr.
type ShoutrrrSendFunc func(url, message string) error

// DefaultShoutrrrSend uses github.com/containrrr/shoutrrr (specs §6.5).
func DefaultShoutrrrSend(url, message string) error {
	return shoutrrr.Send(url, message)
}

// Notifier dispatches poll notifications to Shoutrrr targets (specs §5.5).
type Notifier struct {
	targets store.NotificationTargetRepository
	send    ShoutrrrSendFunc
}

// NewNotifier returns a notifier that decrypts URLs at send time.
func NewNotifier(targets store.NotificationTargetRepository, send ShoutrrrSendFunc) *Notifier {
	if send == nil {
		send = DefaultShoutrrrSend
	}
	return &Notifier{targets: targets, send: send}
}

// SendTest sends a probe message to a Shoutrrr URL (specs §6.5).
func (n *Notifier) SendTest(ctx context.Context, shoutrrrURL string) error {
	_ = ctx
	if n == nil {
		return fmt.Errorf("notification notifier not configured")
	}
	return n.send(shoutrrrURL, testNotificationMessage)
}

// NotifyRepoAction sends notifications for a poll action when configured.
// Delivery failures are logged without failing the poll (specs §5.5).
func (n *Notifier) NotifyRepoAction(ctx context.Context, repo store.MonitoredRepo, action, detail string) {
	if n == nil || n.targets == nil {
		return
	}

	event, ok := notificationEventForAction(action)
	if !ok {
		return
	}

	sendCtx := context.WithoutCancel(ctx)

	targets, err := n.targets.ListEnabled(sendCtx)
	if err != nil {
		slog.Error("list notification targets failed", "repoId", repo.ID, "event", event)
		return
	}

	message := buildNotificationMessage(event, repo, detail)
	for _, target := range resolveNotificationTargets(targets, repo.NotificationTargetIDs) {
		if !targetSubscribes(target.Events, event) {
			continue
		}
		n.sendToTarget(sendCtx, repo, event, target, message)
	}
}

func (n *Notifier) sendToTarget(
	ctx context.Context,
	repo store.MonitoredRepo,
	event string,
	target store.NotificationTarget,
	message string,
) {
	url, err := n.targets.DecryptURL(ctx, target.ID)
	if err != nil {
		slog.Error("notification decrypt failed",
			"targetId", target.ID,
			"targetName", target.Name,
			"repoId", repo.ID,
			"event", event,
		)
		return
	}

	if err := n.send(url, message); err != nil {
		slog.Error("notification send failed",
			"targetId", target.ID,
			"targetName", target.Name,
			"repoId", repo.ID,
			"event", event,
		)
	}
}

func notificationEventForAction(action string) (string, bool) {
	switch action {
	case ActionCreate:
		return EventCreate, true
	case ActionError:
		return EventError, true
	case ActionSupersede:
		return EventSupersede, true
	default:
		return "", false
	}
}

func resolveNotificationTargets(enabled []store.NotificationTarget, repoTargetIDs []string) []store.NotificationTarget {
	if len(repoTargetIDs) == 0 {
		return enabled
	}

	allowed := make(map[string]struct{}, len(repoTargetIDs))
	for _, id := range repoTargetIDs {
		allowed[id] = struct{}{}
	}

	out := make([]store.NotificationTarget, 0, len(enabled))
	for _, target := range enabled {
		if _, ok := allowed[target.ID]; ok {
			out = append(out, target)
		}
	}
	return out
}

func targetSubscribes(events []string, event string) bool {
	for _, subscribed := range events {
		if subscribed == event {
			return true
		}
	}
	return false
}

func buildNotificationMessage(event string, repo store.MonitoredRepo, detail string) string {
	repoLabel := fmt.Sprintf("%s/%s", repo.SourceKind, repo.ProjectPath)
	switch event {
	case EventCreate:
		tag := repoTag(repo)
		if tag == "" {
			return fmt.Sprintf("Release Ops: ticket created for %s", repoLabel)
		}
		return fmt.Sprintf("Release Ops: ticket created for %s (%s)", repoLabel, tag)
	case EventError:
		if detail == "" {
			return fmt.Sprintf("Release Ops: poll error for %s", repoLabel)
		}
		return fmt.Sprintf("Release Ops: poll error for %s: %s", repoLabel, detail)
	case EventSupersede:
		tag := repoTag(repo)
		if tag == "" {
			return fmt.Sprintf("Release Ops: ticket superseded for %s", repoLabel)
		}
		return fmt.Sprintf("Release Ops: ticket superseded for %s (%s)", repoLabel, tag)
	default:
		return fmt.Sprintf("Release Ops: %s for %s", event, repoLabel)
	}
}

func repoTag(repo store.MonitoredRepo) string {
	if repo.OpenTicketTag != nil && *repo.OpenTicketTag != "" {
		return *repo.OpenTicketTag
	}
	if repo.LastKnownTag != nil {
		return *repo.LastKnownTag
	}
	return ""
}
