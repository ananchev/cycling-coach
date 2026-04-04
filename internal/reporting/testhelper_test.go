package reporting_test

import (
	"database/sql"
	"os"
	"testing"

	"cycling-coach/internal/storage"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("openTestDB: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store.DB()
}

func writeTempProfile(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "athlete-profile-*.md")
	if err != nil {
		t.Fatalf("writeTempProfile: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("writeTempProfile: write: %v", err)
	}
	f.Close()
	return f.Name()
}
