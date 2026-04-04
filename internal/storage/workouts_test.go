package storage

import (
	"testing"
	"time"
)

func newTestWorkout(wahooID string) *Workout {
	dur := int64(3600)
	dist := 40000.0
	avgPwr := 180.0
	wtype := "cycling"
	return &Workout{
		WahooID:     wahooID,
		StartedAt:   time.Date(2026, 3, 10, 9, 0, 0, 0, time.UTC),
		DurationSec: &dur,
		DistanceM:   &dist,
		AvgPower:    &avgPwr,
		WorkoutType: &wtype,
		Source:      "api",
		Processed:   false,
	}
}

func TestUpsertWorkout_InsertAndGet(t *testing.T) {
	db := openTestDB(t)
	w := newTestWorkout("wahoo-001")

	id, inserted, err := UpsertWorkout(db, w)
	if err != nil {
		t.Fatalf("UpsertWorkout: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero id")
	}
	if !inserted {
		t.Error("expected inserted=true on first insert")
	}

	got, err := GetWorkoutByWahooID(db, "wahoo-001")
	if err != nil {
		t.Fatalf("GetWorkoutByWahooID: %v", err)
	}
	if got.WahooID != "wahoo-001" {
		t.Errorf("WahooID = %q, want %q", got.WahooID, "wahoo-001")
	}
	if got.Source != "api" {
		t.Errorf("Source = %q, want %q", got.Source, "api")
	}
	if got.Processed {
		t.Error("Processed should be false")
	}
}

func TestUpsertWorkout_Idempotent(t *testing.T) {
	db := openTestDB(t)
	w := newTestWorkout("wahoo-002")

	id1, inserted1, err := UpsertWorkout(db, w)
	if err != nil {
		t.Fatalf("first UpsertWorkout: %v", err)
	}
	if !inserted1 {
		t.Error("first upsert should report inserted=true")
	}

	id2, inserted2, err := UpsertWorkout(db, w)
	if err != nil {
		t.Fatalf("second UpsertWorkout: %v", err)
	}
	if inserted2 {
		t.Error("second upsert should report inserted=false")
	}
	if id1 != id2 {
		t.Errorf("expected same id on repeat upsert, got %d and %d", id1, id2)
	}
}

func TestGetWorkoutByWahooID_NotFound(t *testing.T) {
	db := openTestDB(t)
	_, err := GetWorkoutByWahooID(db, "does-not-exist")
	if err == nil {
		t.Fatal("expected error for missing wahoo_id")
	}
}

func TestMarkWorkoutProcessed(t *testing.T) {
	db := openTestDB(t)
	w := newTestWorkout("wahoo-003")
	id, _, err := UpsertWorkout(db, w)
	if err != nil {
		t.Fatalf("UpsertWorkout: %v", err)
	}

	if err := MarkWorkoutProcessed(db, id); err != nil {
		t.Fatalf("MarkWorkoutProcessed: %v", err)
	}

	got, err := GetWorkoutByWahooID(db, "wahoo-003")
	if err != nil {
		t.Fatalf("GetWorkoutByWahooID: %v", err)
	}
	if !got.Processed {
		t.Error("expected Processed = true after MarkWorkoutProcessed")
	}
}

func TestListUnprocessedWorkouts(t *testing.T) {
	db := openTestDB(t)

	for _, id := range []string{"w-a", "w-b", "w-c"} {
		if _, _, err := UpsertWorkout(db, newTestWorkout(id)); err != nil {
			t.Fatalf("UpsertWorkout %s: %v", id, err)
		}
	}

	// Mark one processed.
	wid, _, _ := UpsertWorkout(db, newTestWorkout("w-a"))
	if err := MarkWorkoutProcessed(db, wid); err != nil {
		t.Fatalf("MarkWorkoutProcessed: %v", err)
	}

	unprocessed, err := ListUnprocessedWorkouts(db)
	if err != nil {
		t.Fatalf("ListUnprocessedWorkouts: %v", err)
	}
	if len(unprocessed) != 2 {
		t.Errorf("expected 2 unprocessed, got %d", len(unprocessed))
	}
}

func TestListWorkoutsByDateRange(t *testing.T) {
	db := openTestDB(t)

	insert := func(wahooID string, day int) {
		w := &Workout{
			WahooID:   wahooID,
			StartedAt: time.Date(2026, 3, day, 9, 0, 0, 0, time.UTC),
			Source:    "api",
		}
		if _, _, err := UpsertWorkout(db, w); err != nil {
			t.Fatalf("UpsertWorkout: %v", err)
		}
	}

	insert("range-1", 5)
	insert("range-2", 10)
	insert("range-3", 20)

	from := time.Date(2026, 3, 8, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 3, 15, 23, 59, 59, 0, time.UTC)

	got, err := ListWorkoutsByDateRange(db, from, to)
	if err != nil {
		t.Fatalf("ListWorkoutsByDateRange: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("expected 1 workout in range, got %d", len(got))
	}
	if len(got) > 0 && got[0].WahooID != "range-2" {
		t.Errorf("expected range-2, got %s", got[0].WahooID)
	}
}
