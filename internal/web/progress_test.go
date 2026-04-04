package web

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cycling-coach/internal/reporting"
	"cycling-coach/internal/storage"
)

func TestProgressHandler_ReturnsSnapshotAndSavedAnalysis(t *testing.T) {
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	defer store.Close()
	db := store.DB()

	seedWebProgressWorkout(t, db, "web-prog-1", time.Date(2026, 3, 28, 9, 0, 0, 0, time.UTC), 95)
	if _, err := storage.InsertNote(db, &storage.AthleteNote{
		Timestamp: time.Date(2026, 3, 29, 7, 0, 0, 0, time.UTC),
		Type:      storage.NoteTypeWeight,
		WeightKG:  webFloatPtr(74.5),
	}); err != nil {
		t.Fatalf("InsertNote(weight): %v", err)
	}
	if err := storage.UpsertProgressAnalysis(db, &storage.ProgressAnalysis{
		PeriodFrom:   time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
		PeriodTo:     time.Date(2026, 4, 4, 23, 59, 59, 0, time.UTC),
		SnapshotJSON: `{"period":"saved"}`,
		SystemPrompt: "Saved system prompt",
		UserPrompt:   "Saved user prompt",
		Narrative:    "Saved analysis markdown",
	}); err != nil {
		t.Fatalf("UpsertProgressAnalysis: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/progress?from=2026-03-28", nil)
	rr := httptest.NewRecorder()
	progressHandler(db)(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		SelectedRange struct {
			From string `json:"from"`
		} `json:"selected_range"`
		KPIs []struct {
			Key         string `json:"key"`
			Explanation string `json:"explanation"`
		} `json:"kpis"`
		SavedAnalysis *struct {
			Narrative string `json:"narrative"`
			System    string `json:"system_prompt"`
			User      string `json:"user_prompt"`
		} `json:"saved_analysis"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.SelectedRange.From != "2026-03-28" {
		t.Errorf("selected_range.from = %q, want 2026-03-28", resp.SelectedRange.From)
	}
	if len(resp.KPIs) < 8 {
		t.Fatalf("len(kpis) = %d, want at least 8", len(resp.KPIs))
	}
	if resp.KPIs[0].Explanation == "" {
		t.Error("expected KPI explanation to be populated")
	}
	if resp.SavedAnalysis == nil || resp.SavedAnalysis.Narrative != "Saved analysis markdown" {
		t.Errorf("saved analysis = %+v, want saved narrative", resp.SavedAnalysis)
	}
	if resp.SavedAnalysis == nil || resp.SavedAnalysis.System != "Saved system prompt" {
		t.Errorf("saved system prompt = %+v, want Saved system prompt", resp.SavedAnalysis)
	}
	if resp.SavedAnalysis == nil || resp.SavedAnalysis.User != "Saved user prompt" {
		t.Errorf("saved user prompt = %+v, want Saved user prompt", resp.SavedAnalysis)
	}
}

func TestProgressHandler_InvalidFrom(t *testing.T) {
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	defer store.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/progress?from=bad-date", nil)
	rr := httptest.NewRecorder()
	progressHandler(store.DB())(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
}

func TestProgressInterpretHandler_SavesInterpretation(t *testing.T) {
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	defer store.Close()
	db := store.DB()

	seedWebProgressWorkout(t, db, "web-prog-2", time.Date(2026, 3, 28, 9, 0, 0, 0, time.UTC), 100)

	profileDir := t.TempDir()
	profilePath := filepath.Join(profileDir, "athlete-profile.md")
	if err := os.WriteFile(profilePath, []byte("# Athlete\n\nCurrent phase: build.\n"), 0644); err != nil {
		t.Fatalf("write profile: %v", err)
	}

	client := &http.Client{Transport: webRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		respBody, _ := json.Marshal(map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": "## Executive Summary\n\nFresh interpretation."},
			},
		})
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(string(respBody))),
		}, nil
	})}
	orch := reporting.NewOrchestrator(db, profilePath, reporting.NewClaudeProviderForTest("test-key", "https://example.test", client))

	reqBody := bytes.NewBufferString(`{"from":"2026-03-28","aerobic_only_ef":true}`)
	req := httptest.NewRequest(http.MethodPost, "/api/progress/interpret", reqBody)
	rr := httptest.NewRecorder()
	progressInterpretHandler(orch)(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rr.Code, rr.Body.String())
	}

	saved, err := storage.GetProgressAnalysis(db)
	if err != nil {
		t.Fatalf("GetProgressAnalysis: %v", err)
	}
	if !strings.Contains(saved.Narrative, "Fresh interpretation.") {
		t.Errorf("Narrative = %q", saved.Narrative)
	}
	if saved.SystemPrompt == "" {
		t.Error("expected system prompt to be saved")
	}
	if !strings.Contains(saved.UserPrompt, "<athlete_profile>") {
		t.Errorf("expected user prompt to include athlete profile block, got %q", saved.UserPrompt)
	}
}

func seedWebProgressWorkout(t *testing.T, db *sql.DB, wahooID string, startedAt time.Time, tss float64) {
	t.Helper()
	durationSec := int64(60 * 60)
	id, _, err := storage.UpsertWorkout(db, &storage.Workout{
		WahooID:     wahooID,
		StartedAt:   startedAt,
		DurationSec: &durationSec,
		Calories:    int64Ptr(int64(tss * 10)),
		Source:      "api",
	})
	if err != nil {
		t.Fatalf("UpsertWorkout(%s): %v", wahooID, err)
	}
	if err := storage.UpsertRideMetrics(db, &storage.RideMetrics{
		WorkoutID:        id,
		DurationMin:      webFloatPtr(60),
		TSS:              webFloatPtr(tss),
		TRIMP:            webFloatPtr(tss + 10),
		IntensityFactor:  webFloatPtr(0.76),
		EfficiencyFactor: webFloatPtr(1.41),
	}); err != nil {
		t.Fatalf("UpsertRideMetrics(%s): %v", wahooID, err)
	}
}

func webFloatPtr(v float64) *float64 {
	return &v
}

func int64Ptr(v int64) *int64 {
	return &v
}

type webRoundTripFunc func(*http.Request) (*http.Response, error)

func (f webRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
