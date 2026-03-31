package storage

import (
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestOpen_InMemory(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open(\":memory:\") error: %v", err)
	}
	defer db.Close()

	tables := []string{
		"wahoo_tokens",
		"workouts",
		"ride_metrics",
		"athlete_notes",
		"athlete_config",
		"reports",
	}

	for _, table := range tables {
		t.Run("table_"+table, func(t *testing.T) {
			if !tableExists(t, db.DB(), table) {
				t.Errorf("table %q not found after migration", table)
			}
		})
	}
}

func TestOpen_IdempotentMigrations(t *testing.T) {
	// Running Open twice on the same DB must not error (CREATE TABLE IF NOT EXISTS).
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("first Open error: %v", err)
	}

	// Run migrations again directly to verify idempotency.
	if err := migrate(db.DB()); err != nil {
		t.Fatalf("second migrate() error: %v", err)
	}
	db.Close()
}

func tableExists(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()
	var count int
	err := db.QueryRow(
		"SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?", name,
	).Scan(&count)
	if err != nil {
		t.Fatalf("tableExists query error: %v", err)
	}
	return count == 1
}
