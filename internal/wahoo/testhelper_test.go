package wahoo

import (
	"database/sql"
	"testing"
	"time"

	"cycling-coach/internal/storage"
)

// openTestDB opens an in-memory SQLite database with all migrations applied.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("openTestDB: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store.DB()
}

// seedValidToken inserts a non-expired Wahoo token into db.
// Tests use this to avoid triggering token refresh during API calls.
func seedValidToken(t *testing.T, db *sql.DB) {
	t.Helper()
	err := storage.SaveToken(db, &storage.WahooToken{
		AccessToken:  "test-access-token",
		RefreshToken: "test-refresh-token",
		ExpiresAt:    time.Now().Add(2 * time.Hour),
	})
	if err != nil {
		t.Fatalf("seedValidToken: %v", err)
	}
}
