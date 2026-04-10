package wyze

import (
	"context"
	"testing"
	"time"

	"cycling-coach/internal/storage"
)

type stubSidecar struct {
	records []ScaleRecord
	err     error
}

func (s *stubSidecar) QueryScaleRecords(_ context.Context, _, _ time.Time) ([]ScaleRecord, error) {
	return s.records, s.err
}

func TestImporter_Import_InsertAndSkip(t *testing.T) {
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	defer store.Close()

	rec := ScaleRecord{
		ExternalID:   "wyze:scale_record:abc123",
		MeasuredAt:   time.Date(2026, 4, 8, 7, 14, 22, 0, time.UTC),
		WeightKG:     floatPtr(77.4),
		BodyFatPct:   floatPtr(18.2),
		MuscleMassKG: floatPtr(36.8),
		BodyWaterPct: floatPtr(55.1),
		BMRKcal:      floatPtr(1684),
		RawSource:    "wyze",
	}

	importer := NewImporter(store.DB(), &stubSidecar{records: []ScaleRecord{rec}})
	r1, err := importer.Import(context.Background(), time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("Import #1: %v", err)
	}
	if r1.Inserted != 1 || r1.Skipped != 0 || r1.ConflictWithManual != 0 {
		t.Fatalf("unexpected result #1: %+v", r1)
	}

	r2, err := importer.Import(context.Background(), time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("Import #2: %v", err)
	}
	if r2.Inserted != 0 || r2.Skipped != 1 {
		t.Fatalf("unexpected result #2: %+v", r2)
	}
}

func TestImporter_Import_CreatesConflictWithManualDuplicate(t *testing.T) {
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	defer store.Close()
	db := store.DB()

	manualWeight := 77.5
	manualID, err := storage.InsertNote(db, &storage.AthleteNote{
		Timestamp: time.Date(2026, 4, 8, 7, 12, 0, 0, time.UTC),
		Type:      storage.NoteTypeWeight,
		WeightKG:  &manualWeight,
	})
	if err != nil {
		t.Fatalf("InsertNote(manual): %v", err)
	}

	rec := ScaleRecord{
		ExternalID:   "wyze:scale_record:abc123",
		MeasuredAt:   time.Date(2026, 4, 8, 7, 14, 22, 0, time.UTC),
		WeightKG:     floatPtr(77.4),
		BodyFatPct:   floatPtr(18.2),
		MuscleMassKG: floatPtr(36.8),
		BodyWaterPct: floatPtr(55.1),
		BMRKcal:      floatPtr(1684),
		RawSource:    "wyze",
	}

	importer := NewImporter(db, &stubSidecar{records: []ScaleRecord{rec}})
	result, err := importer.Import(context.Background(), time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Inserted != 1 || result.ConflictWithManual != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}

	conflicts, err := storage.ListWyzeScaleConflicts(db, 10)
	if err != nil {
		t.Fatalf("ListWyzeScaleConflicts: %v", err)
	}
	if len(conflicts) != 1 {
		t.Fatalf("expected 1 conflict, got %d", len(conflicts))
	}
	if conflicts[0].ManualNoteID != manualID {
		t.Fatalf("ManualNoteID = %d, want %d", conflicts[0].ManualNoteID, manualID)
	}
	if conflicts[0].WyzeNote.WeightKG == nil || *conflicts[0].WyzeNote.WeightKG != 77.4 {
		t.Fatalf("unexpected wyze note in conflict: %+v", conflicts[0].WyzeNote)
	}
}

func floatPtr(v float64) *float64 {
	return &v
}
