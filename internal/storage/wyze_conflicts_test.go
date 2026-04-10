package storage

import (
	"database/sql"
	"testing"
	"time"
)

func TestInsertAndListWyzeScaleConflicts(t *testing.T) {
	db := openTestDB(t)

	manualID := insertWeightNoteForConflict(t, db, time.Date(2026, 4, 8, 7, 10, 0, 0, time.UTC), 77.5)
	wyzeID := insertWeightNoteForConflict(t, db, time.Date(2026, 4, 8, 7, 14, 22, 0, time.UTC), 77.4)

	if _, err := InsertWyzeScaleConflict(db, &WyzeScaleConflict{
		WyzeRecordID: "wyze:scale_record:abc123",
		ManualNoteID: manualID,
		WyzeNoteID:   wyzeID,
		ConflictType: WyzeConflictTypeManual,
	}); err != nil {
		t.Fatalf("InsertWyzeScaleConflict: %v", err)
	}

	got, err := ListWyzeScaleConflicts(db, 10)
	if err != nil {
		t.Fatalf("ListWyzeScaleConflicts: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 conflict, got %d", len(got))
	}
	if got[0].ManualNoteID != manualID || got[0].WyzeNoteID != wyzeID {
		t.Fatalf("unexpected conflict row: %+v", got[0])
	}
}

func TestDeleteWyzeConflictEntry_Manual(t *testing.T) {
	db := openTestDB(t)

	manualID := insertWeightNoteForConflict(t, db, time.Date(2026, 4, 8, 7, 10, 0, 0, time.UTC), 77.5)
	wyzeID := insertWeightNoteForConflict(t, db, time.Date(2026, 4, 8, 7, 14, 22, 0, time.UTC), 77.4)

	if _, err := UpsertWyzeScaleImport(db, &WyzeScaleImport{
		WyzeRecordID:  "wyze:scale_record:abc123",
		AthleteNoteID: wyzeID,
		MeasuredAt:    time.Date(2026, 4, 8, 7, 14, 22, 0, time.UTC),
		PayloadHash:   "hash1",
	}); err != nil {
		t.Fatalf("UpsertWyzeScaleImport: %v", err)
	}

	conflictID, err := InsertWyzeScaleConflict(db, &WyzeScaleConflict{
		WyzeRecordID: "wyze:scale_record:abc123",
		ManualNoteID: manualID,
		WyzeNoteID:   wyzeID,
		ConflictType: WyzeConflictTypeManual,
	})
	if err != nil {
		t.Fatalf("InsertWyzeScaleConflict: %v", err)
	}

	if err := DeleteWyzeConflictEntry(db, conflictID, "manual"); err != nil {
		t.Fatalf("DeleteWyzeConflictEntry(manual): %v", err)
	}
	if _, err := GetWyzeScaleConflict(db, conflictID); err == nil {
		t.Fatal("expected conflict to be deleted")
	}
	if _, err := GetWyzeScaleImportByRecordID(db, "wyze:scale_record:abc123"); err != nil {
		t.Fatalf("expected wyze import to remain, got %v", err)
	}
	if _, err := getAthleteNoteByID(db, manualID); err == nil {
		t.Fatal("expected manual note to be deleted")
	}
	if _, err := getAthleteNoteByID(db, wyzeID); err != nil {
		t.Fatalf("expected wyze note to remain, got %v", err)
	}
}

func TestDeleteWyzeConflictEntry_Wyze(t *testing.T) {
	db := openTestDB(t)

	manualID := insertWeightNoteForConflict(t, db, time.Date(2026, 4, 8, 7, 10, 0, 0, time.UTC), 77.5)
	wyzeID := insertWeightNoteForConflict(t, db, time.Date(2026, 4, 8, 7, 14, 22, 0, time.UTC), 77.4)

	if _, err := UpsertWyzeScaleImport(db, &WyzeScaleImport{
		WyzeRecordID:  "wyze:scale_record:abc123",
		AthleteNoteID: wyzeID,
		MeasuredAt:    time.Date(2026, 4, 8, 7, 14, 22, 0, time.UTC),
		PayloadHash:   "hash1",
	}); err != nil {
		t.Fatalf("UpsertWyzeScaleImport: %v", err)
	}

	conflictID, err := InsertWyzeScaleConflict(db, &WyzeScaleConflict{
		WyzeRecordID: "wyze:scale_record:abc123",
		ManualNoteID: manualID,
		WyzeNoteID:   wyzeID,
		ConflictType: WyzeConflictTypeManual,
	})
	if err != nil {
		t.Fatalf("InsertWyzeScaleConflict: %v", err)
	}

	if err := DeleteWyzeConflictEntry(db, conflictID, "wyze"); err != nil {
		t.Fatalf("DeleteWyzeConflictEntry(wyze): %v", err)
	}
	if _, err := GetWyzeScaleConflict(db, conflictID); err == nil {
		t.Fatal("expected conflict to be deleted")
	}
	if _, err := GetWyzeScaleImportByRecordID(db, "wyze:scale_record:abc123"); err == nil {
		t.Fatal("expected wyze import to be deleted")
	}
	if _, err := getAthleteNoteByID(db, manualID); err != nil {
		t.Fatalf("expected manual note to remain, got %v", err)
	}
	if _, err := getAthleteNoteByID(db, wyzeID); err == nil {
		t.Fatal("expected wyze note to be deleted")
	}
}

func TestFindClosestManualBodyMetric(t *testing.T) {
	db := openTestDB(t)

	manualTime := time.Date(2026, 4, 8, 7, 10, 0, 0, time.UTC)
	manualID := insertWeightNoteForConflict(t, db, manualTime, 77.5)
	wyzeID := insertWeightNoteForConflict(t, db, time.Date(2026, 4, 8, 7, 14, 22, 0, time.UTC), 77.4)

	if _, err := UpsertWyzeScaleImport(db, &WyzeScaleImport{
		WyzeRecordID:  "wyze:scale_record:existing",
		AthleteNoteID: wyzeID,
		MeasuredAt:    time.Date(2026, 4, 8, 7, 14, 22, 0, time.UTC),
		PayloadHash:   "hash-existing",
	}); err != nil {
		t.Fatalf("UpsertWyzeScaleImport: %v", err)
	}

	got, err := FindClosestManualBodyMetric(db, time.Date(2026, 4, 8, 7, 12, 0, 0, time.UTC), 30*time.Minute)
	if err != nil {
		t.Fatalf("FindClosestManualBodyMetric: %v", err)
	}
	if got.ID != manualID {
		t.Fatalf("got note id %d, want %d", got.ID, manualID)
	}
}

func insertWeightNoteForConflict(t *testing.T, db *sql.DB, ts time.Time, weight float64) int64 {
	t.Helper()
	id, err := InsertNote(db, &AthleteNote{
		Timestamp: ts,
		Type:      NoteTypeWeight,
		WeightKG:  &weight,
	})
	if err != nil {
		t.Fatalf("InsertNote: %v", err)
	}
	return id
}

func getAthleteNoteByID(db *sql.DB, id int64) (*AthleteNote, error) {
	row := db.QueryRow(`
		SELECT id, timestamp, type, rpe, weight_kg, body_fat_pct, muscle_mass_kg, body_water_pct, bmr_kcal, note, workout_id, created_at
		FROM athlete_notes
		WHERE id = ?`, id)

	var note AthleteNote
	var noteType string
	if err := row.Scan(
		&note.ID, &note.Timestamp, &noteType, &note.RPE, &note.WeightKG, &note.BodyFatPct, &note.MuscleMassKG, &note.BodyWaterPct, &note.BMRKcal, &note.Note, &note.WorkoutID, &note.CreatedAt,
	); err != nil {
		return nil, err
	}
	note.Type = NoteType(noteType)
	return &note, nil
}
