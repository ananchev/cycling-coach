package analysis

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

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

func insertWorkout(t *testing.T, db *sql.DB, wahooID string, fitPath *string) int64 {
	t.Helper()
	w := &storage.Workout{
		WahooID:     wahooID,
		StartedAt:   time.Date(2026, 3, 10, 9, 0, 0, 0, time.UTC),
		Source:      "api",
		FITFilePath: fitPath,
	}
	id, _, err := storage.UpsertWorkout(db, w)
	if err != nil {
		t.Fatalf("insertWorkout: %v", err)
	}
	return id
}

func TestProcessor_ProcessAll_NoFITFile(t *testing.T) {
	db := openTestDB(t)
	insertWorkout(t, db, "99001", nil) // no FIT path — file will not be found

	proc := NewProcessor(db, t.TempDir())
	r := proc.ProcessAll(context.Background())

	// Missing file → counted as SkippedNoFIT, not a parse error or DB error.
	if r.SkippedNoFIT != 1 {
		t.Errorf("SkippedNoFIT = %d, want 1", r.SkippedNoFIT)
	}
	if r.Processed != 0 {
		t.Errorf("Processed = %d, want 0", r.Processed)
	}
	if len(r.ParseErrors) != 0 {
		t.Errorf("ParseErrors = %v, want none", r.ParseErrors)
	}

	// Workout should be marked processed so it isn't retried.
	workouts, err := storage.ListUnprocessedWorkouts(db)
	if err != nil {
		t.Fatalf("ListUnprocessedWorkouts: %v", err)
	}
	if len(workouts) != 0 {
		t.Errorf("unprocessed = %d, want 0", len(workouts))
	}
}

func TestProcessor_ProcessAll_CorruptFITFile(t *testing.T) {
	db := openTestDB(t)

	// Write a corrupt (non-FIT) file so the parser returns a parse error.
	dir := t.TempDir()
	corruptPath := dir + "/corrupt.fit"
	if err := writeFile(corruptPath, []byte("this is not a FIT file")); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}
	insertWorkout(t, db, "99005", &corruptPath)

	proc := NewProcessor(db, dir)
	r := proc.ProcessAll(context.Background())

	// Corrupt file → parse error, NOT marked processed (so re-sync + reset can fix it).
	if len(r.ParseErrors) != 1 {
		t.Errorf("ParseErrors = %d, want 1", len(r.ParseErrors))
	}
	if r.ParseErrors[0].WahooID != "99005" {
		t.Errorf("ParseErrors[0].WahooID = %q, want 99005", r.ParseErrors[0].WahooID)
	}
	if r.Processed != 0 {
		t.Errorf("Processed = %d, want 0", r.Processed)
	}

	// Workout should NOT be marked processed — so the user can fix and retry.
	workouts, err := storage.ListUnprocessedWorkouts(db)
	if err != nil {
		t.Fatalf("ListUnprocessedWorkouts: %v", err)
	}
	if len(workouts) != 1 {
		t.Errorf("unprocessed = %d, want 1 (parse-errored workout stays unprocessed)", len(workouts))
	}
}

func TestProcessor_ProcessAll_WithRealFIT(t *testing.T) {
	db := openTestDB(t)

	fitPath := "../../testdata/sample.fit"
	id := insertWorkout(t, db, "99002", &fitPath)

	proc := NewProcessor(db, t.TempDir())
	r := proc.ProcessAll(context.Background())

	if len(r.Errors) != 0 {
		t.Fatalf("Errors: %v", r.Errors)
	}
	if len(r.ParseErrors) != 0 {
		t.Fatalf("ParseErrors: %v", r.ParseErrors)
	}
	if r.Processed != 1 {
		t.Errorf("Processed = %d, want 1", r.Processed)
	}

	workouts, err := storage.ListUnprocessedWorkouts(db)
	if err != nil {
		t.Fatalf("ListUnprocessedWorkouts: %v", err)
	}
	if len(workouts) != 0 {
		t.Errorf("unprocessed = %d, want 0", len(workouts))
	}

	metrics, err := storage.GetRideMetrics(db, id)
	if err != nil {
		t.Fatalf("GetRideMetrics: %v", err)
	}
	if metrics.DurationMin == nil || *metrics.DurationMin <= 0 {
		t.Errorf("DurationMin = %v, want > 0", metrics.DurationMin)
	}
}

func TestProcessor_ProcessAll_EmptyDB(t *testing.T) {
	db := openTestDB(t)
	proc := NewProcessor(db, t.TempDir())
	r := proc.ProcessAll(context.Background())

	if r.Processed != 0 || len(r.Errors) != 0 || len(r.ParseErrors) != 0 {
		t.Errorf("expected all-zero result; got %+v", r)
	}
}

func TestProcessor_ProcessAll_IdempotentAfterProcessed(t *testing.T) {
	db := openTestDB(t)

	fitPath := "../../testdata/sample.fit"
	insertWorkout(t, db, "99003", &fitPath)

	proc := NewProcessor(db, t.TempDir())
	proc.ProcessAll(context.Background())

	// Second pass: nothing unprocessed.
	r := proc.ProcessAll(context.Background())
	if r.Processed != 0 || len(r.Errors) != 0 || len(r.ParseErrors) != 0 {
		t.Errorf("second pass: expected all-zero result; got %+v", r)
	}
}

func TestProcessor_ResetFIT(t *testing.T) {
	db := openTestDB(t)
	dir := t.TempDir()

	fitPath := dir + "/99004.fit"
	if err := writeFile(fitPath, []byte("dummy")); err != nil {
		t.Fatalf("write file: %v", err)
	}
	insertWorkout(t, db, "99004", &fitPath)
	// Mark processed manually to simulate a previously-processed workout.
	if _, err := db.Exec(`UPDATE workouts SET processed = 1 WHERE wahoo_id = ?`, "99004"); err != nil {
		t.Fatalf("mark processed: %v", err)
	}

	proc := NewProcessor(db, dir)
	if err := proc.ResetFIT(context.Background(), "99004"); err != nil {
		t.Fatalf("ResetFIT: %v", err)
	}

	// File should be gone.
	if _, err := os.Stat(fitPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("FIT file still exists after reset")
	}

	// Workout should be unprocessed again.
	workouts, err := storage.ListUnprocessedWorkouts(db)
	if err != nil {
		t.Fatalf("ListUnprocessedWorkouts: %v", err)
	}
	if len(workouts) != 1 {
		t.Errorf("unprocessed = %d, want 1", len(workouts))
	}
}

// writeFile is a small helper to create a file with given content.
func writeFile(path string, data []byte) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	f.Close()
	return err
}
