package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"cycling-coach/internal/reporting"
	"cycling-coach/internal/storage"
)

func TestCloseBlockReportHandler_GeneratesReportAndPlan(t *testing.T) {
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	defer store.Close()
	db := store.DB()

	prevStart := time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)
	prevEnd := time.Date(2026, 3, 8, 0, 0, 0, 0, time.UTC)
	if _, err := storage.UpsertReport(db, &storage.Report{
		Type:      storage.ReportTypeWeeklyReport,
		WeekStart: prevStart,
		WeekEnd:   prevEnd,
	}); err != nil {
		t.Fatalf("UpsertReport(previous report): %v", err)
	}

	profileDir := t.TempDir()
	profilePath := filepath.Join(profileDir, "athlete-profile.md")
	if err := os.WriteFile(profilePath, []byte("# Athlete\n\nFTP: 250W\n"), 0644); err != nil {
		t.Fatalf("write profile: %v", err)
	}

	orch := reporting.NewOrchestrator(db, profilePath, &reporting.StubProvider{
		Output: &reporting.ReportOutput{
			Summary:   "Stub summary",
			Narrative: "# Stub narrative",
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/report/close-block", bytes.NewBufferString(`{"block_end":"2026-03-14","user_prompt":"Travel Tuesday"}`))
	rr := httptest.NewRecorder()
	runner := newCloseBlockRunner(orch)
	closeBlockReportHandler(runner)(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202: %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Status     string `json:"status"`
		BlockStart string `json:"block_start"`
		BlockEnd   string `json:"block_end"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "started" {
		t.Errorf("status = %q, want %q", resp.Status, "started")
	}
	if resp.BlockStart != "2026-03-09" || resp.BlockEnd != "2026-03-14" {
		t.Errorf("block window = %s to %s", resp.BlockStart, resp.BlockEnd)
	}

	runner.Wait()

	plan, err := storage.GetReport(db, storage.ReportTypeWeeklyPlan, time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("GetReport(plan): %v", err)
	}
	if plan.UserPrompt == "" {
		t.Error("expected saved plan prompt to be populated")
	}
	report, err := storage.GetReport(db, storage.ReportTypeWeeklyReport, time.Date(2026, 3, 9, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("GetReport(report): %v", err)
	}
	if report.ID == 0 {
		t.Error("expected report row to be persisted")
	}
}

func TestCloseBlockReportHandler_BadRequestWithoutAnchorReport(t *testing.T) {
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	defer store.Close()

	profileDir := t.TempDir()
	profilePath := filepath.Join(profileDir, "athlete-profile.md")
	if err := os.WriteFile(profilePath, []byte("# Athlete\n"), 0644); err != nil {
		t.Fatalf("write profile: %v", err)
	}
	orch := reporting.NewOrchestrator(store.DB(), profilePath, &reporting.StubProvider{})

	req := httptest.NewRequest(http.MethodPost, "/api/report/close-block", bytes.NewBufferString(`{"block_end":"2026-03-14"}`))
	rr := httptest.NewRecorder()
	r := chi.NewRouter()
	r.Post("/api/report/close-block", closeBlockReportHandler(newCloseBlockRunner(orch)))
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rr.Code, rr.Body.String())
	}
}

func TestCloseBlockReportHandler_FirstRunAcceptsInitialBlockStart(t *testing.T) {
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	defer store.Close()

	profileDir := t.TempDir()
	profilePath := filepath.Join(profileDir, "athlete-profile.md")
	if err := os.WriteFile(profilePath, []byte("# Athlete\n\nFTP: 250W\n"), 0644); err != nil {
		t.Fatalf("write profile: %v", err)
	}

	orch := reporting.NewOrchestrator(store.DB(), profilePath, &reporting.StubProvider{
		Output: &reporting.ReportOutput{
			Summary:   "Stub summary",
			Narrative: "# Stub narrative",
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/report/close-block", bytes.NewBufferString(`{"initial_block_start":"2026-03-03","block_end":"2026-03-14","user_prompt":"Travel Tuesday"}`))
	rr := httptest.NewRecorder()
	runner := newCloseBlockRunner(orch)
	r := chi.NewRouter()
	r.Post("/api/report/close-block", closeBlockReportHandler(runner))
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202: %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		BlockStart string `json:"block_start"`
		BlockEnd   string `json:"block_end"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.BlockStart != "2026-03-03" || resp.BlockEnd != "2026-03-14" {
		t.Errorf("block window = %s to %s", resp.BlockStart, resp.BlockEnd)
	}

	runner.Wait()
}
