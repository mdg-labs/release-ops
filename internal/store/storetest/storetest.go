// Package storetest applies the embedded migrations to throwaway databases for tests.
package storetest

import (
	"context"
	"testing"

	"github.com/mdg-labs/release-ops/internal/store"
)

// Migrate creates the SQLite file at dbPath and applies every embedded migration to it.
func Migrate(t testing.TB, dbPath string) {
	t.Helper()

	if _, err := store.Migrate(context.Background(), dbPath); err != nil {
		t.Fatalf("migrate %s: %v", dbPath, err)
	}
}
