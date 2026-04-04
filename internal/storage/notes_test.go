package storage

import (
	"testing"
	"time"
)

func TestInsertNote_RideNote(t *testing.T) {
	db := openTestDB(t)

	rpe := int64(7)
	note := "legs felt heavy"
	n := &AthleteNote{
		Timestamp: time.Date(2026, 3, 10, 10, 0, 0, 0, time.UTC),
		Type:      NoteTypeRide,
		RPE:       &rpe,
		Note:      &note,
	}

	id, err := InsertNote(db, n)
	if err != nil {
		t.Fatalf("InsertNote: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero id")
	}
}

func TestInsertNote_WeightNote(t *testing.T) {
	db := openTestDB(t)

	kg := 90.3
	n := &AthleteNote{
		Timestamp: time.Date(2026, 3, 10, 8, 0, 0, 0, time.UTC),
		Type:      NoteTypeWeight,
		WeightKG:  &kg,
	}

	_, err := InsertNote(db, n)
	if err != nil {
		t.Fatalf("InsertNote weight: %v", err)
	}
}

func TestListNotesByDateRange(t *testing.T) {
	db := openTestDB(t)

	insertNote := func(day int, ntype NoteType) {
		n := &AthleteNote{
			Timestamp: time.Date(2026, 3, day, 9, 0, 0, 0, time.UTC),
			Type:      ntype,
		}
		if _, err := InsertNote(db, n); err != nil {
			t.Fatalf("InsertNote day %d: %v", day, err)
		}
	}

	insertNote(5, NoteTypeNote)
	insertNote(10, NoteTypeRide)
	insertNote(20, NoteTypeNote)

	from := time.Date(2026, 3, 8, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 3, 15, 23, 59, 59, 0, time.UTC)

	notes, err := ListNotesByDateRange(db, from, to)
	if err != nil {
		t.Fatalf("ListNotesByDateRange: %v", err)
	}
	if len(notes) != 1 {
		t.Errorf("expected 1 note in range, got %d", len(notes))
	}
	if len(notes) > 0 && notes[0].Type != NoteTypeRide {
		t.Errorf("expected NoteTypeRide, got %s", notes[0].Type)
	}
}

func TestLinkNoteToWorkout(t *testing.T) {
	db := openTestDB(t)

	wid := insertTestWorkout(t, db, "notes-w-001")

	noteID, err := InsertNote(db, &AthleteNote{
		Timestamp: time.Date(2026, 3, 10, 10, 0, 0, 0, time.UTC),
		Type:      NoteTypeRide,
	})
	if err != nil {
		t.Fatalf("InsertNote: %v", err)
	}

	if err := LinkNoteToWorkout(db, noteID, wid); err != nil {
		t.Fatalf("LinkNoteToWorkout: %v", err)
	}

	notes, err := ListNotesByDateRange(db,
		time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("ListNotesByDateRange: %v", err)
	}
	if len(notes) == 0 {
		t.Fatal("expected at least one note")
	}
	if notes[0].WorkoutID == nil || *notes[0].WorkoutID != wid {
		t.Errorf("WorkoutID = %v, want %d", notes[0].WorkoutID, wid)
	}
}

func TestListBodyMetrics_ByDateRange(t *testing.T) {
	db := openTestDB(t)

	insertMetric := func(day int, weight float64) {
		if _, err := InsertNote(db, &AthleteNote{
			Timestamp: time.Date(2026, 3, day, 8, 0, 0, 0, time.UTC),
			Type:      NoteTypeWeight,
			WeightKG:  &weight,
		}); err != nil {
			t.Fatalf("InsertNote day %d: %v", day, err)
		}
	}

	insertMetric(5, 90.1)
	insertMetric(10, 89.8)
	insertMetric(20, 89.4)

	from := time.Date(2026, 3, 8, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 3, 15, 23, 59, 59, 0, time.UTC)

	got, err := ListBodyMetrics(db, from, to, 100)
	if err != nil {
		t.Fatalf("ListBodyMetrics: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 body metric in range, got %d", len(got))
	}
	if got[0].WeightKG == nil || *got[0].WeightKG != 89.8 {
		t.Fatalf("unexpected metric returned: %+v", got[0])
	}
}
