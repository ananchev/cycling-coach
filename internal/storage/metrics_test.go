package storage

import (
	"database/sql"
	"testing"
	"time"
)

func insertTestWorkout(t *testing.T, db *sql.DB, wahooID string) int64 {
	t.Helper()
	id, err := UpsertWorkout(db, &Workout{
		WahooID:   wahooID,
		StartedAt: time.Date(2026, 3, 10, 9, 0, 0, 0, time.UTC),
		Source:    "api",
	})
	if err != nil {
		t.Fatalf("insertTestWorkout: %v", err)
	}
	return id
}

func newTestMetrics(workoutID int64) *RideMetrics {
	f := func(v float64) *float64 { return &v }
	return &RideMetrics{
		WorkoutID:        workoutID,
		DurationMin:      f(60.0),
		AvgHR:            f(125.0),
		NormalizedPower:  f(185.0),
		IntensityFactor:  f(0.74),
		TSS:              f(65.0),
		TRIMP:            f(72.0),
		EfficiencyFactor: f(1.48),
		HRDriftPct:       f(3.5),
		HRZ2Pct:          f(75.0),
		HRZ3Pct:          f(15.0),
		PwrZ2Pct:         f(80.0),
	}
}

func TestUpsertRideMetrics_InsertAndGet(t *testing.T) {
	db := openTestDB(t)
	wid := insertTestWorkout(t, db, "metrics-w-001")

	m := newTestMetrics(wid)
	if err := UpsertRideMetrics(db, m); err != nil {
		t.Fatalf("UpsertRideMetrics: %v", err)
	}

	got, err := GetRideMetrics(db, wid)
	if err != nil {
		t.Fatalf("GetRideMetrics: %v", err)
	}
	if got.WorkoutID != wid {
		t.Errorf("WorkoutID = %d, want %d", got.WorkoutID, wid)
	}
	if got.DurationMin == nil || *got.DurationMin != 60.0 {
		t.Errorf("DurationMin = %v, want 60.0", got.DurationMin)
	}
	if got.TSS == nil || *got.TSS != 65.0 {
		t.Errorf("TSS = %v, want 65.0", got.TSS)
	}
}

func TestUpsertRideMetrics_UpdatesOnConflict(t *testing.T) {
	db := openTestDB(t)
	wid := insertTestWorkout(t, db, "metrics-w-002")

	m := newTestMetrics(wid)
	if err := UpsertRideMetrics(db, m); err != nil {
		t.Fatalf("first UpsertRideMetrics: %v", err)
	}

	// Update TSS value.
	newTSS := 90.0
	m.TSS = &newTSS
	if err := UpsertRideMetrics(db, m); err != nil {
		t.Fatalf("second UpsertRideMetrics: %v", err)
	}

	got, err := GetRideMetrics(db, wid)
	if err != nil {
		t.Fatalf("GetRideMetrics: %v", err)
	}
	if got.TSS == nil || *got.TSS != 90.0 {
		t.Errorf("TSS after update = %v, want 90.0", got.TSS)
	}
}

func TestGetRideMetrics_NotFound(t *testing.T) {
	db := openTestDB(t)
	_, err := GetRideMetrics(db, 99999)
	if err == nil {
		t.Fatal("expected error for missing workout_id")
	}
}
