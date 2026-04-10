package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"cycling-coach/internal/storage"
)

func TestBodyMetricsHandler_ReturnsHydrationBMRAndTimestamp(t *testing.T) {
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	defer store.Close()
	db := store.DB()

	weight := 77.4
	bodyFat := 18.2
	muscle := 36.8
	water := 55.1
	bmr := 1684.0
	ts := time.Date(2026, 4, 8, 7, 14, 22, 0, time.UTC)
	if _, err := storage.InsertNote(db, &storage.AthleteNote{
		Timestamp:    ts,
		Type:         storage.NoteTypeWeight,
		WeightKG:     &weight,
		BodyFatPct:   &bodyFat,
		MuscleMassKG: &muscle,
		BodyWaterPct: &water,
		BMRKcal:      &bmr,
	}); err != nil {
		t.Fatalf("InsertNote: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/body-metrics?from=2026-04-08&to=2026-04-08", nil)
	rr := httptest.NewRecorder()
	bodyMetricsHandler(db)(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rr.Code, rr.Body.String())
	}

	var resp []struct {
		Timestamp    string   `json:"timestamp"`
		Date         string   `json:"date"`
		WeightKG     *float64 `json:"weight_kg"`
		BodyFatPct   *float64 `json:"body_fat_pct"`
		MuscleMassKG *float64 `json:"muscle_mass_kg"`
		BodyWaterPct *float64 `json:"body_water_pct"`
		BMRKcal      *float64 `json:"bmr_kcal"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp) != 1 {
		t.Fatalf("expected 1 point, got %d", len(resp))
	}
	if resp[0].Timestamp != ts.Format(time.RFC3339) {
		t.Fatalf("timestamp = %q, want %q", resp[0].Timestamp, ts.Format(time.RFC3339))
	}
	if resp[0].BodyWaterPct == nil || *resp[0].BodyWaterPct != water {
		t.Fatalf("BodyWaterPct = %v, want %.1f", resp[0].BodyWaterPct, water)
	}
	if resp[0].BMRKcal == nil || *resp[0].BMRKcal != bmr {
		t.Fatalf("BMRKcal = %v, want %.1f", resp[0].BMRKcal, bmr)
	}
}

func TestBodyMetricsHandler_ExcludesManualSideOfWyzeConflict(t *testing.T) {
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
	if _, err := storage.InsertWyzeScaleConflict(db, &storage.WyzeScaleConflict{
		WyzeRecordID: "wyze:scale_record:abc123",
		ManualNoteID: manualID,
		WyzeNoteID:   wyzeID,
		ConflictType: storage.WyzeConflictTypeManual,
	}); err != nil {
		t.Fatalf("InsertWyzeScaleConflict: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/body-metrics?from=2026-04-08&to=2026-04-08", nil)
	rr := httptest.NewRecorder()
	bodyMetricsHandler(db)(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rr.Code, rr.Body.String())
	}

	var resp []struct {
		WeightKG *float64 `json:"weight_kg"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp) != 1 {
		t.Fatalf("expected 1 chart point, got %d", len(resp))
	}
	if resp[0].WeightKG == nil || *resp[0].WeightKG != wyzeWeight {
		t.Fatalf("expected wyze point to remain, got %+v", resp[0])
	}
}
