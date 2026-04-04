package storage

import (
	"testing"
	"time"
)

func TestUpsertDailyPlaceholderWorkout_InsertsWhenDayHasNoWorkout(t *testing.T) {
	db := openTestDB(t)
	loc := time.FixedZone("CET", 3600)

	id, inserted, err := UpsertDailyPlaceholderWorkout(db, time.Date(2026, 4, 4, 23, 50, 0, 0, loc), loc)
	if err != nil {
		t.Fatalf("UpsertDailyPlaceholderWorkout: %v", err)
	}
	if !inserted || id == 0 {
		t.Fatalf("expected placeholder insert, got inserted=%v id=%d", inserted, id)
	}

	w, err := GetWorkoutByWahooID(db, "manual-empty-2026-04-04")
	if err != nil {
		t.Fatalf("GetWorkoutByWahooID: %v", err)
	}
	if w.Source != "manual" || !w.Processed {
		t.Fatalf("unexpected placeholder workout: %+v", w)
	}
}

func TestUpsertDailyPlaceholderWorkout_SkipsWhenWorkoutExists(t *testing.T) {
	db := openTestDB(t)
	loc := time.FixedZone("CET", 3600)

	startedAt := time.Date(2026, 4, 4, 8, 0, 0, 0, loc).UTC()
	if _, _, err := UpsertWorkout(db, &Workout{
		WahooID:   "real-1",
		StartedAt: startedAt,
		Source:    "api",
	}); err != nil {
		t.Fatalf("UpsertWorkout: %v", err)
	}

	_, inserted, err := UpsertDailyPlaceholderWorkout(db, time.Date(2026, 4, 4, 23, 50, 0, 0, loc), loc)
	if err != nil {
		t.Fatalf("UpsertDailyPlaceholderWorkout: %v", err)
	}
	if inserted {
		t.Fatal("expected no placeholder insert when a workout already exists")
	}
}

func TestReconcilePlaceholderWorkout_MovesNotesAndDeletesPlaceholder(t *testing.T) {
	db := openTestDB(t)
	loc := time.FixedZone("CET", 3600)

	placeholderID, inserted, err := UpsertDailyPlaceholderWorkout(db, time.Date(2026, 4, 4, 23, 50, 0, 0, loc), loc)
	if err != nil || !inserted {
		t.Fatalf("UpsertDailyPlaceholderWorkout: inserted=%v err=%v", inserted, err)
	}

	note := "travel day"
	noteID, err := InsertNote(db, &AthleteNote{
		Timestamp: time.Date(2026, 4, 4, 12, 0, 0, 0, loc).UTC(),
		Type:      NoteTypeNote,
		Note:      &note,
		WorkoutID: &placeholderID,
	})
	if err != nil {
		t.Fatalf("InsertNote: %v", err)
	}

	actualID, _, err := UpsertWorkout(db, &Workout{
		WahooID:   "real-2",
		StartedAt: time.Date(2026, 4, 4, 18, 0, 0, 0, loc).UTC(),
		Source:    "api",
	})
	if err != nil {
		t.Fatalf("UpsertWorkout: %v", err)
	}

	if err := ReconcilePlaceholderWorkout(db, actualID, time.Date(2026, 4, 4, 18, 0, 0, 0, loc), loc); err != nil {
		t.Fatalf("ReconcilePlaceholderWorkout: %v", err)
	}

	if _, err := GetWorkoutByWahooID(db, "manual-empty-2026-04-04"); err == nil {
		t.Fatal("expected placeholder to be deleted")
	}

	notes, err := ListNotesByWorkout(db, actualID, "note")
	if err != nil {
		t.Fatalf("ListNotesByWorkout: %v", err)
	}
	if len(notes) != 1 || notes[0].ID != noteID {
		t.Fatalf("expected note to move to actual workout, got %+v", notes)
	}
}
