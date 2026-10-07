package store

import (
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/mdg-labs/release-ops/internal/crypto"
	"github.com/mdg-labs/release-ops/internal/store/db"
)

// ErrInvalidJSON is returned when a JSON column fails validation.
var ErrInvalidJSON = errors.New("invalid JSON")

// DefaultNotificationEvents is the default events_json value per specs §4.5.
var DefaultNotificationEvents = []string{"create", "error", "supersede"}

// Store wraps sqlc queries with domain repositories and credential encryption.
type Store struct {
	db     *sql.DB
	q      *db.Queries
	cipher *crypto.Cipher
}

// New creates a Store from an open database and encryption cipher.
func New(sqlDB *sql.DB, cipher *crypto.Cipher) *Store {
	return &Store{
		db:     sqlDB,
		q:      db.New(sqlDB),
		cipher: cipher,
	}
}

// DB returns the underlying database handle.
func (s *Store) DB() *sql.DB {
	return s.db
}

// Queries returns the raw sqlc query handle (for auth and other low-level callers).
func (s *Store) Queries() *db.Queries {
	return s.q
}

// Settings returns the app settings repository.
func (s *Store) Settings() SettingsRepository {
	return settingsRepo{store: s}
}

// Integrations returns the integrations repository.
func (s *Store) Integrations() IntegrationRepository {
	return integrationRepo{store: s}
}

// TicketProjects returns the ticket projects repository.
func (s *Store) TicketProjects() TicketProjectRepository {
	return ticketProjectRepo{store: s}
}

// Repos returns the monitored repos repository.
func (s *Store) Repos() MonitoredRepoRepository {
	return monitoredRepoRepo{store: s}
}

// Notifications returns the notification targets repository.
func (s *Store) Notifications() NotificationTargetRepository {
	return notificationTargetRepo{store: s}
}

// Poll returns poll-state and poll-run repository methods.
func (s *Store) Poll() PollRepository {
	return pollRepo{store: s}
}

func newID() string {
	return uuid.NewString()
}

func nowUTC() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
}

func nullStringPtr(s sql.NullString) *string {
	if !s.Valid {
		return nil
	}
	v := s.String
	return &v
}

func stringPtrToNull(s *string) sql.NullString {
	if s == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *s, Valid: true}
}

func boolToInt64(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

func int64ToBool(v int64) bool {
	return v != 0
}
