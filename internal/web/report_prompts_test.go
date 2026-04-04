package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"cycling-coach/internal/storage"
	"github.com/go-chi/chi/v5"
)

func TestReportPromptsHandler_ReturnsSavedPrompts(t *testing.T) {
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	defer store.Close()
	db := store.DB()

	ws := time.Date(2026, 3, 9, 0, 0, 0, 0, time.UTC)
	we := ws.AddDate(0, 0, 6)
	id, err := storage.UpsertReport(db, &storage.Report{
		Type:         storage.ReportTypeWeeklyPlan,
		WeekStart:    ws,
		WeekEnd:      we,
		SystemPrompt: "Saved system prompt",
		UserPrompt:   "Saved user prompt",
	})
	if err != nil {
		t.Fatalf("UpsertReport: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/report/1/prompts", nil)
	rr := httptest.NewRecorder()
	r := chi.NewRouter()
	r.Get("/api/report/{id}/prompts", reportPromptsHandler(db))
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		ID     int64  `json:"id"`
		Type   string `json:"type"`
		System string `json:"system_prompt"`
		User   string `json:"user_prompt"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.ID != id {
		t.Errorf("id = %d, want %d", resp.ID, id)
	}
	if resp.Type != string(storage.ReportTypeWeeklyPlan) {
		t.Errorf("type = %q, want %q", resp.Type, storage.ReportTypeWeeklyPlan)
	}
	if resp.System != "Saved system prompt" {
		t.Errorf("system prompt = %q", resp.System)
	}
	if resp.User != "Saved user prompt" {
		t.Errorf("user prompt = %q", resp.User)
	}
}

func TestReportPromptsHandler_InvalidID(t *testing.T) {
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	defer store.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/report/bad/prompts", nil)
	rr := httptest.NewRecorder()
	r := chi.NewRouter()
	r.Get("/api/report/{id}/prompts", reportPromptsHandler(store.DB()))
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
}
