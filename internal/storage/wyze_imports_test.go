package storage

import (
	"testing"
	"time"
)

func TestUpsertWyzeScaleImport_InsertAndGet(t *testing.T) {
	db := openTestDB(t)

	weight := 77.4
	noteID, err := InsertNote(db, &AthleteNote{
		Timestamp: time.Date(2026, 4, 8, 7, 14, 22, 0, time.UTC),
		Type:      NoteTypeWeight,
		WeightKG:  &weight,
	})
	if err != nil {
		t.Fatalf("InsertNote: %v", err)
	}

	raw := `{"weight_kg":77.4}`
	measuredAt := time.Date(2026, 4, 8, 7, 14, 22, 0, time.UTC)
	id, err := UpsertWyzeScaleImport(db, &WyzeScaleImport{
		WyzeRecordID:   "wyze:scale_record:abc123",
		AthleteNoteID:  noteID,
		MeasuredAt:     measuredAt,
		PayloadHash:    "hash1",
		RawPayloadJSON: &raw,
	})
	if err != nil {
		t.Fatalf("UpsertWyzeScaleImport: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero id")
	}

	got, err := GetWyzeScaleImportByRecordID(db, "wyze:scale_record:abc123")
	if err != nil {
		t.Fatalf("GetWyzeScaleImportByRecordID: %v", err)
	}
	if got.AthleteNoteID != noteID {
		t.Fatalf("AthleteNoteID = %d, want %d", got.AthleteNoteID, noteID)
	}
	if got.PayloadHash != "hash1" {
		t.Fatalf("PayloadHash = %q, want hash1", got.PayloadHash)
	}
}

func TestUpsertWyzeScaleImport_UpdatesOnConflict(t *testing.T) {
	db := openTestDB(t)

	weight1 := 77.4
	noteID1, err := InsertNote(db, &AthleteNote{
		Timestamp: time.Date(2026, 4, 8, 7, 14, 22, 0, time.UTC),
		Type:      NoteTypeWeight,
		WeightKG:  &weight1,
	})
	if err != nil {
		t.Fatalf("InsertNote #1: %v", err)
	}

	measuredAt1 := time.Date(2026, 4, 8, 7, 14, 22, 0, time.UTC)
	id1, err := UpsertWyzeScaleImport(db, &WyzeScaleImport{
		WyzeRecordID:  "wyze:scale_record:abc123",
		AthleteNoteID: noteID1,
		MeasuredAt:    measuredAt1,
		PayloadHash:   "hash1",
	})
	if err != nil {
		t.Fatalf("UpsertWyzeScaleImport #1: %v", err)
	}

	weight2 := 77.1
	noteID2, err := InsertNote(db, &AthleteNote{
		Timestamp: time.Date(2026, 4, 8, 7, 30, 0, 0, time.UTC),
		Type:      NoteTypeWeight,
		WeightKG:  &weight2,
	})
	if err != nil {
		t.Fatalf("InsertNote #2: %v", err)
	}

	measuredAt2 := time.Date(2026, 4, 8, 7, 30, 0, 0, time.UTC)
	id2, err := UpsertWyzeScaleImport(db, &WyzeScaleImport{
		WyzeRecordID:  "wyze:scale_record:abc123",
		AthleteNoteID: noteID2,
		MeasuredAt:    measuredAt2,
		PayloadHash:   "hash2",
	})
	if err != nil {
		t.Fatalf("UpsertWyzeScaleImport #2: %v", err)
	}
	if id2 != id1 {
		t.Fatalf("expected same row id after conflict update, got %d want %d", id2, id1)
	}

	got, err := GetWyzeScaleImportByRecordID(db, "wyze:scale_record:abc123")
	if err != nil {
		t.Fatalf("GetWyzeScaleImportByRecordID: %v", err)
	}
	if got.AthleteNoteID != noteID2 {
		t.Fatalf("AthleteNoteID = %d, want %d", got.AthleteNoteID, noteID2)
	}
	if got.PayloadHash != "hash2" {
		t.Fatalf("PayloadHash = %q, want hash2", got.PayloadHash)
	}
	if !got.MeasuredAt.Equal(measuredAt2) {
		t.Fatalf("MeasuredAt = %s, want %s", got.MeasuredAt, measuredAt2)
	}
}
