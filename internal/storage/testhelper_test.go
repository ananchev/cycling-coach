package storage

import (
	"database/sql"
	"testing"
)

// openTestDB opens an in-memory SQLite database with all migrations applied.
// It registers a cleanup function to close the DB when the test ends.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	store, err := Open(":memory:")
	if err != nil {
		t.Fatalf("openTestDB: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store.DB()
}
