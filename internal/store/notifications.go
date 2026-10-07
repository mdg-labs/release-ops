package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mdg-labs/release-ops/internal/store/db"
)

// NotificationTarget is a Shoutrrr notification target safe for API exposure.
type NotificationTarget struct {
	ID        string
	Name      string
	HasSecret bool
	Events    []string
	Enabled   bool
	CreatedAt string
	UpdatedAt string
}

// CreateNotificationTargetInput holds plaintext Shoutrrr URL before encryption.
type CreateNotificationTargetInput struct {
	Name        string
	ShoutrrrURL string
	Events      []string
	Enabled     bool
}

// UpdateNotificationTargetInput updates mutable notification target fields.
type UpdateNotificationTargetInput struct {
	Name        string
	ShoutrrrURL *string // nil = leave existing encrypted URL unchanged
	Events      []string
	Enabled     bool
}

// NotificationTargetRepository manages notification_targets with encrypted URLs.
type NotificationTargetRepository interface {
	Create(ctx context.Context, input CreateNotificationTargetInput) (*NotificationTarget, error)
	Get(ctx context.Context, id string) (*NotificationTarget, error)
	List(ctx context.Context) ([]NotificationTarget, error)
	ListEnabled(ctx context.Context) ([]NotificationTarget, error)
	Update(ctx context.Context, id string, input UpdateNotificationTargetInput) (*NotificationTarget, error)
	Delete(ctx context.Context, id string) error
	DecryptURL(ctx context.Context, id string) (string, error)
}

type notificationTargetRepo struct {
	store *Store
}

func (r notificationTargetRepo) Create(ctx context.Context, input CreateNotificationTargetInput) (*NotificationTarget, error) {
	events := input.Events
	if len(events) == 0 {
		events = DefaultNotificationEvents
	}
	eventsJSON, err := marshalEventsJSON(events)
	if err != nil {
		return nil, err
	}

	encrypted, err := r.store.cipher.Encrypt([]byte(input.ShoutrrrURL))
	if err != nil {
		return nil, err
	}

	now := nowUTC()
	row, err := r.store.q.CreateNotificationTarget(ctx, db.CreateNotificationTargetParams{
		ID:                   newID(),
		Name:                 input.Name,
		ShoutrrrUrlEncrypted: encrypted,
		EventsJson:           eventsJSON,
		Enabled:              boolToInt64(input.Enabled),
		CreatedAt:            now,
		UpdatedAt:            now,
	})
	if err != nil {
		return nil, err
	}
	return notificationTargetFromRow(row)
}

func (r notificationTargetRepo) Get(ctx context.Context, id string) (*NotificationTarget, error) {
	row, err := r.store.q.GetNotificationTarget(ctx, id)
	if err != nil {
		return nil, err
	}
	return notificationTargetFromRow(row)
}

func (r notificationTargetRepo) List(ctx context.Context) ([]NotificationTarget, error) {
	rows, err := r.store.q.ListNotificationTargets(ctx)
	if err != nil {
		return nil, err
	}
	return notificationTargetsFromRows(rows)
}

func (r notificationTargetRepo) ListEnabled(ctx context.Context) ([]NotificationTarget, error) {
	rows, err := r.store.q.ListEnabledNotificationTargets(ctx)
	if err != nil {
		return nil, err
	}
	return notificationTargetsFromRows(rows)
}

func (r notificationTargetRepo) Update(ctx context.Context, id string, input UpdateNotificationTargetInput) (*NotificationTarget, error) {
	existing, err := r.store.q.GetNotificationTarget(ctx, id)
	if err != nil {
		return nil, err
	}

	eventsJSON, err := marshalEventsJSON(input.Events)
	if err != nil {
		return nil, err
	}

	encrypted := existing.ShoutrrrUrlEncrypted
	if input.ShoutrrrURL != nil {
		encrypted, err = r.store.cipher.Encrypt([]byte(*input.ShoutrrrURL))
		if err != nil {
			return nil, err
		}
	}

	row, err := r.store.q.UpdateNotificationTarget(ctx, db.UpdateNotificationTargetParams{
		Name:                 input.Name,
		ShoutrrrUrlEncrypted: encrypted,
		EventsJson:           eventsJSON,
		Enabled:              boolToInt64(input.Enabled),
		UpdatedAt:            nowUTC(),
		ID:                   id,
	})
	if err != nil {
		return nil, err
	}
	return notificationTargetFromRow(row)
}

func (r notificationTargetRepo) Delete(ctx context.Context, id string) error {
	return r.store.q.DeleteNotificationTarget(ctx, id)
}

func (r notificationTargetRepo) DecryptURL(ctx context.Context, id string) (string, error) {
	row, err := r.store.q.GetNotificationTarget(ctx, id)
	if err != nil {
		return "", err
	}
	plaintext, err := r.store.cipher.Decrypt(row.ShoutrrrUrlEncrypted)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func marshalEventsJSON(events []string) (string, error) {
	if len(events) == 0 {
		return "", fmt.Errorf("%w: events must not be empty", ErrInvalidJSON)
	}
	b, err := json.Marshal(events)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidJSON, err)
	}
	return string(b), nil
}

func parseEventsJSON(raw string) ([]string, error) {
	var events []string
	if err := json.Unmarshal([]byte(raw), &events); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidJSON, err)
	}
	return events, nil
}

func notificationTargetFromRow(row db.NotificationTarget) (*NotificationTarget, error) {
	events, err := parseEventsJSON(row.EventsJson)
	if err != nil {
		return nil, err
	}
	return &NotificationTarget{
		ID:        row.ID,
		Name:      row.Name,
		HasSecret: row.ShoutrrrUrlEncrypted != "",
		Events:    events,
		Enabled:   int64ToBool(row.Enabled),
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}, nil
}

func notificationTargetsFromRows(rows []db.NotificationTarget) ([]NotificationTarget, error) {
	out := make([]NotificationTarget, len(rows))
	for i, row := range rows {
		target, err := notificationTargetFromRow(row)
		if err != nil {
			return nil, err
		}
		out[i] = *target
	}
	return out, nil
}
