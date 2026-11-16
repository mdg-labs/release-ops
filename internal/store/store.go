package store

import (
	"database/sql"
	"errors"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/mdg-labs/release-ops/internal/crypto"
	"github.com/mdg-labs/release-ops/internal/store/db"
	"modernc.org/sqlite"
)

// ErrInUse is returned when a delete is refused because another row still references it.
var ErrInUse = errors.New("row is referenced by another row")

// sqliteConstraint is the primary result code of every constraint failure. A broken
// foreign key reports an extended code under it: SQLITE_CONSTRAINT_FOREIGNKEY for a plain
// reference and SQLITE_CONSTRAINT_TRIGGER for ON DELETE RESTRICT, so the message decides.
const sqliteConstraint = 19

// mapForeignKeyError turns a foreign key failure into ErrInUse and passes any other
// error through.
func mapForeignKeyError(err error) error {
	var sqliteErr *sqlite.Error
	if errors.As(err, &sqliteErr) &&
		sqliteErr.Code()&0xff == sqliteConstraint &&
		strings.Contains(sqliteErr.Error(), "FOREIGN KEY constraint failed") {
		return ErrInUse
	}
	return err
}

// Store wraps sqlc queries with domain repositories and credential encryption.
// Its repositories send every write and transaction through the writer connection
// and every plain read through the read pool; the raw handles stay inside the store.
type Store struct {
	db     *DB
	reads  *db.Queries
	writes *db.Queries
	cipher *crypto.Cipher

	// keyVersion counts the replacements of the GitHub App's private key made through this
	// Store, so in-memory state derived from the old key can tell it is stale.
	keyVersion atomic.Int64

	// instanceConfig is the environment configuration the settings page shows.
	instanceConfig atomic.Pointer[InstanceConfig]
}

// New creates a Store from an open database and encryption cipher.
func New(sqlDB *DB, cipher *crypto.Cipher) *Store {
	return &Store{
		db:     sqlDB,
		reads:  db.New(sqlDB.Reader),
		writes: db.New(sqlDB.Writer),
		cipher: cipher,
	}
}

// DB returns the underlying database handle.
func (s *Store) DB() *DB {
	return s.db
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
