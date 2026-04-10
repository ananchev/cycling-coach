package web

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"cycling-coach/internal/storage"
	wyzepkg "cycling-coach/internal/wyze"
)

type stubWyzeSidecar struct {
	records []wyzepkg.ScaleRecord
	err     error
}

func (s *stubWyzeSidecar) QueryScaleRecords(_ context.Context, _, _ time.Time) ([]wyzepkg.ScaleRecord, error) {
	return s.records, s.err
}

func TestWyzeSyncHandler_ImportsAndReturnsConflictCount(t *testing.T) {
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	defer store.Close()
	db := store.DB()

	manualWeight := 77.5
	if _, err := storage.InsertNote(db, &storage.AthleteNote{
		Timestamp: time.Date(2026, 4, 8, 7, 12, 0, 0, time.UTC),
		Type:      storage.NoteTypeWeight,
		WeightKG:  &manualWeight,
	}); err != nil {
		t.Fatalf("InsertNote(manual): %v", err)
	}

	importer := wyzepkg.NewImporter(db, &stubWyzeSidecar{
		records: []wyzepkg.ScaleRecord{
			{
				ExternalID:   "wyze:scale_record:abc123",
				MeasuredAt:   time.Date(2026, 4, 8, 7, 14, 22, 0, time.UTC),
				WeightKG:     wyzeFloatPtr(77.4),
				BodyWaterPct: wyzeFloatPtr(55.1),
				BMRKcal:      wyzeFloatPtr(1684),
				RawSource:    "wyze",
			},
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/wyze/sync", bytes.NewBufferString(`{"from":"2026-04-01","to":"2026-04-09"}`))
	rr := httptest.NewRecorder()
	wyzeSyncHandler(importer)(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Inserted           int `json:"inserted"`
		ConflictWithManual int `json:"conflict_with_manual"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Inserted != 1 || resp.ConflictWithManual != 1 {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestListWyzeConflictsHandler_ReturnsConflicts(t *testing.T) {
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	defer store.Close()
	db := store.DB()

	conflictID := seedWyzeConflict(t, db)

	req := httptest.NewRequest(http.MethodGet, "/api/wyze/conflicts", nil)
	rr := httptest.NewRecorder()
	listWyzeConflictsHandler(db)(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rr.Code, rr.Body.String())
	}

	var resp []struct {
		ID     int64 `json:"id"`
		Manual struct {
			ID int64 `json:"id"`
		} `json:"manual"`
		Wyze struct {
			ID int64 `json:"id"`
		} `json:"wyze"`
		ConflictType string `json:"conflict_type"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp) != 1 || resp[0].ID != conflictID || resp[0].ConflictType != storage.WyzeConflictTypeManual {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestListWyzeRecordsHandler_ReturnsManualAndWyzeRows(t *testing.T) {
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	defer store.Close()
	db := store.DB()

	conflictID := seedWyzeConflict(t, db)

	req := httptest.NewRequest(http.MethodGet, "/api/wyze/records?from=2026-04-01&to=2026-04-09", nil)
	rr := httptest.NewRecorder()
	listWyzeRecordsHandler(db)(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rr.Code, rr.Body.String())
	}

	var resp []struct {
		Source     string `json:"source"`
		NoteID     int64  `json:"note_id"`
		ConflictID *int64 `json:"conflict_id"`
		WyzeRecordID *string `json:"wyze_record_id"`
		Wyze       struct {
			ID int64 `json:"id"`
		} `json:"wyze"`
		Manual *struct {
			ID int64 `json:"id"`
		} `json:"manual"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp) != 2 {
		t.Fatalf("expected 2 records, got %d", len(resp))
	}

	var sawManual, sawWyze bool
	for _, row := range resp {
		if row.ConflictID == nil || *row.ConflictID != conflictID {
			t.Fatalf("expected conflict id %d on row: %+v", conflictID, row)
		}
		if row.WyzeRecordID == nil || *row.WyzeRecordID != "wyze:scale_record:abc123" {
			t.Fatalf("expected wyze record id on row: %+v", row)
		}
		if row.Source == "manual" {
			sawManual = true
		}
		if row.Source == "wyze" {
			sawWyze = true
		}
	}
	if !sawManual || !sawWyze {
		t.Fatalf("expected both manual and wyze rows, got %+v", resp)
	}
}

func TestListWyzeRecordsHandler_InfersSplitManualDuplicates(t *testing.T) {
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	defer store.Close()
	db := store.DB()

	weight := 89.9
	bodyFat := 24.8
	muscle := 63.4
	if _, err := storage.InsertNote(db, &storage.AthleteNote{
		Timestamp:  time.Date(2026, 4, 9, 7, 0, 0, 0, time.UTC),
		Type:       storage.NoteTypeWeight,
		WeightKG:   &weight,
	}); err != nil {
		t.Fatalf("InsertNote(weight): %v", err)
	}
	if _, err := storage.InsertNote(db, &storage.AthleteNote{
		Timestamp:    time.Date(2026, 4, 9, 7, 1, 0, 0, time.UTC),
		Type:         storage.NoteTypeWeight,
		BodyFatPct:   &bodyFat,
	}); err != nil {
		t.Fatalf("InsertNote(bodyfat): %v", err)
	}
	if _, err := storage.InsertNote(db, &storage.AthleteNote{
		Timestamp:     time.Date(2026, 4, 9, 7, 2, 0, 0, time.UTC),
		Type:          storage.NoteTypeWeight,
		MuscleMassKG:  &muscle,
	}); err != nil {
		t.Fatalf("InsertNote(muscle): %v", err)
	}

	wyzeID, err := storage.InsertNote(db, &storage.AthleteNote{
		Timestamp:     time.Date(2026, 4, 9, 17, 59, 1, 0, time.UTC),
		Type:          storage.NoteTypeWeight,
		WeightKG:      &weight,
		BodyFatPct:    &bodyFat,
		MuscleMassKG:  &muscle,
	})
	if err != nil {
		t.Fatalf("InsertNote(wyze): %v", err)
	}
	if _, err := storage.UpsertWyzeScaleImport(db, &storage.WyzeScaleImport{
		WyzeRecordID:  "wyze:scale_record:xyz",
		AthleteNoteID: wyzeID,
		MeasuredAt:    time.Date(2026, 4, 9, 17, 59, 1, 0, time.UTC),
		PayloadHash:   "hash-x",
	}); err != nil {
		t.Fatalf("UpsertWyzeScaleImport: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/wyze/records?from=2026-04-09&to=2026-04-09", nil)
	rr := httptest.NewRecorder()
	listWyzeRecordsHandler(db)(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rr.Code, rr.Body.String())
	}

	var resp []struct {
		Source       string  `json:"source"`
		ConflictID   *int64  `json:"conflict_id"`
		ConflictType *string `json:"conflict_type"`
		WyzeRecordID *string `json:"wyze_record_id"`
		Manual       *struct {
			ID int64 `json:"id"`
		} `json:"manual"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp) != 4 {
		t.Fatalf("expected 4 rows, got %d", len(resp))
	}
	var inferred int
	for _, row := range resp {
		if row.Source == "manual" && row.ConflictID == nil && row.ConflictType != nil && *row.ConflictType == "duplicate_with_wyze" {
			inferred++
			if row.WyzeRecordID == nil || *row.WyzeRecordID != "wyze:scale_record:xyz" {
				t.Fatalf("expected inferred duplicate to carry wyze record id: %+v", row)
			}
		}
	}
	if inferred != 3 {
		t.Fatalf("expected 3 inferred manual duplicates, got %d", inferred)
	}
}

func TestDeleteWyzeConflictEntryHandler_DeletesSelectedSide(t *testing.T) {
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	defer store.Close()
	db := store.DB()

	conflictID := seedWyzeConflict(t, db)

	req := httptest.NewRequest(http.MethodDelete, "/api/wyze/conflicts/1?side=wyze", nil)
	rr := httptest.NewRecorder()
	r := chi.NewRouter()
	r.Delete("/api/wyze/conflicts/{id}", deleteWyzeConflictEntryHandler(db))
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	if _, err := storage.GetWyzeScaleConflict(db, conflictID); err == nil {
		t.Fatal("expected conflict to be deleted")
	}
}

func TestDeleteWyzeRecordHandler_DeletesWyzeRowAndImport(t *testing.T) {
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	defer store.Close()
	db := store.DB()

	seedWyzeConflict(t, db)

	req := httptest.NewRequest(http.MethodDelete, "/api/wyze/records/2?source=wyze", nil)
	rr := httptest.NewRecorder()
	r := chi.NewRouter()
	r.Delete("/api/wyze/records/{id}", deleteWyzeRecordHandler(db))
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	if _, err := storage.GetWyzeScaleImportByRecordID(db, "wyze:scale_record:abc123"); err == nil {
		t.Fatal("expected wyze import to be deleted")
	}
}

func TestDeleteWyzeRecordHandler_DeletesManualRow(t *testing.T) {
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	defer store.Close()
	db := store.DB()

	seedWyzeConflict(t, db)

	req := httptest.NewRequest(http.MethodDelete, "/api/wyze/records/1?source=manual", nil)
	rr := httptest.NewRecorder()
	r := chi.NewRouter()
	r.Delete("/api/wyze/records/{id}", deleteWyzeRecordHandler(db))
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	if err := storage.DeleteNote(db, 1); err == nil {
		t.Fatal("expected manual note to already be deleted")
	}
}

func seedWyzeConflict(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	manualWeight := 77.5
	manualID, err := storage.InsertNote(db, &storage.AthleteNote{
		Timestamp: time.Date(2026, 4, 8, 7, 12, 0, 0, time.UTC),
		Type:      storage.NoteTypeWeight,
		WeightKG:  &manualWeight,
	})
	if err != nil {
		t.Fatalf("InsertNote(manual): %v", err)
	}

	wyzeWeight := 77.4
	wyzeID, err := storage.InsertNote(db, &storage.AthleteNote{
		Timestamp: time.Date(2026, 4, 8, 7, 14, 22, 0, time.UTC),
		Type:      storage.NoteTypeWeight,
		WeightKG:  &wyzeWeight,
	})
	if err != nil {
		t.Fatalf("InsertNote(wyze): %v", err)
	}
	if _, err := storage.UpsertWyzeScaleImport(db, &storage.WyzeScaleImport{
		WyzeRecordID:  "wyze:scale_record:abc123",
		AthleteNoteID: wyzeID,
		MeasuredAt:    time.Date(2026, 4, 8, 7, 14, 22, 0, time.UTC),
		PayloadHash:   "hash1",
	}); err != nil {
		t.Fatalf("UpsertWyzeScaleImport: %v", err)
	}
	conflictID, err := storage.InsertWyzeScaleConflict(db, &storage.WyzeScaleConflict{
		WyzeRecordID: "wyze:scale_record:abc123",
		ManualNoteID: manualID,
		WyzeNoteID:   wyzeID,
		ConflictType: storage.WyzeConflictTypeManual,
	})
	if err != nil {
		t.Fatalf("InsertWyzeScaleConflict: %v", err)
	}
	return conflictID
}

func wyzeFloatPtr(v float64) *float64 {
	return &v
}
