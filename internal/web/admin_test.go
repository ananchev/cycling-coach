package web

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"cycling-coach/internal/storage"
)

func TestAdminHandler_RendersWyzeRecordsTable(t *testing.T) {
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
		Timestamp:    time.Date(2026, 4, 8, 7, 14, 22, 0, time.UTC),
		Type:         storage.NoteTypeWeight,
		WeightKG:     &wyzeWeight,
		BodyWaterPct: webAdminFloatPtr(55.1),
		BMRKcal:      webAdminFloatPtr(1684),
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

	req := httptest.NewRequest(http.MethodGet, "/admin?tab=wyze", nil)
	rr := httptest.NewRecorder()
	adminHandler(db)(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Wyze Records") {
		t.Fatalf("expected Wyze Records section in body")
	}
	if !strings.Contains(body, "<th>ID</th>") {
		t.Fatalf("expected ID column in body")
	}
	if !strings.Contains(body, "Wyze Sync") {
		t.Fatalf("expected Wyze Sync tab label in body")
	}
	if !strings.Contains(body, "wyze:scale_record:abc123") {
		t.Fatalf("expected wyze record id in body")
	}
	if !strings.Contains(body, "#"+strconv.FormatInt(conflictID, 10)+" conflict") {
		t.Fatalf("expected conflict id %d badge in body", conflictID)
	}
	if !strings.Contains(body, "Delete</button>") {
		t.Fatalf("expected delete button in body")
	}
	if !strings.Contains(body, `name="wyze_from"`) {
		t.Fatalf("expected wyze filter form in body")
	}
}

func TestAdminHandler_RendersEmptyWyzeRecordsTableState(t *testing.T) {
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	defer store.Close()

	req := httptest.NewRequest(http.MethodGet, "/admin?tab=wyze", nil)
	rr := httptest.NewRecorder()
	adminHandler(store.DB())(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Wyze Records") {
		t.Fatalf("expected Wyze Records section in body")
	}
	if !strings.Contains(body, "No body-metric records found for this filter.") {
		t.Fatalf("expected empty body-metric records message in body")
	}
	if !strings.Contains(body, `name="wyze_from"`) {
		t.Fatalf("expected wyze filter form in body")
	}
}

func webAdminFloatPtr(v float64) *float64 {
	return &v
}
