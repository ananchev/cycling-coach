package reporting

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cycling-coach/internal/storage"
)

func TestGenerateProgressAnalysis_SavesSingleInterpretation(t *testing.T) {
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	defer store.Close()
	db := store.DB()

	seedProgressReportingWorkout(t, db, "prog-1", time.Date(2026, 4, 1, 9, 0, 0, 0, time.UTC), 60*60, 70, 80, 0.78, 1.34, 0)
	seedProgressReportingWorkout(t, db, "prog-2", time.Date(2026, 4, 8, 9, 0, 0, 0, time.UTC), 100*60, 95, 105, 0.79, 1.42, 4.4)
	if _, err := storage.InsertNote(db, &storage.AthleteNote{
		Timestamp: time.Date(2026, 4, 8, 7, 0, 0, 0, time.UTC),
		Type:      storage.NoteTypeWeight,
		WeightKG:  reportingFloatPtr(74.4),
	}); err != nil {
		t.Fatalf("InsertNote(weight): %v", err)
	}

	profileDir := t.TempDir()
	profilePath := filepath.Join(profileDir, "athlete-profile.md")
	profileText := "# Athlete Profile\n\nCurrent phase: build.\n"
	if err := os.WriteFile(profilePath, []byte(profileText), 0644); err != nil {
		t.Fatalf("write profile: %v", err)
	}

	var gotSystem string
	var gotUser string
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		gotSystem, _ = body["system"].(string)
		msgs, _ := body["messages"].([]any)
		if len(msgs) > 0 {
			msg, _ := msgs[0].(map[string]any)
			gotUser, _ = msg["content"].(string)
		}
		respBody, _ := json.Marshal(map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": "## Executive Summary\n\nUseful progress snapshot."},
			},
		})
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(string(respBody))),
		}, nil
	})}

	provider := NewClaudeProviderForTest("test-key", "https://example.test", client)
	orch := NewOrchestrator(db, profilePath, provider)

	from := time.Date(2026, 4, 8, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 4, 14, 0, 0, 0, 0, time.UTC)
	got, err := orch.GenerateProgressAnalysis(context.Background(), from, to, true)
	if err != nil {
		t.Fatalf("GenerateProgressAnalysis: %v", err)
	}

	if !strings.Contains(gotSystem, "expert cycling coach") {
		t.Errorf("system prompt missing coaching guidance: %q", gotSystem)
	}
	if !strings.Contains(gotUser, "<athlete_profile>") || !strings.Contains(gotUser, "Current phase: build.") {
		t.Errorf("user prompt missing athlete profile: %q", gotUser)
	}
	if !strings.Contains(gotUser, `"aerobic_efficiency"`) || !strings.Contains(gotUser, `"completion_rate"`) {
		t.Errorf("user prompt missing KPI payload: %q", gotUser)
	}
	if got.Narrative != "## Executive Summary\n\nUseful progress snapshot." {
		t.Errorf("Narrative = %q", got.Narrative)
	}

	saved, err := storage.GetProgressAnalysis(db)
	if err != nil {
		t.Fatalf("GetProgressAnalysis: %v", err)
	}
	if saved.PeriodFrom.Format("2006-01-02") != "2026-04-08" {
		t.Errorf("saved.PeriodFrom = %s, want 2026-04-08", saved.PeriodFrom.Format("2006-01-02"))
	}
	if !strings.Contains(saved.SnapshotJSON, `"prior_period"`) {
		t.Errorf("snapshot json missing prior_period: %s", saved.SnapshotJSON)
	}
	if !strings.Contains(saved.SystemPrompt, "expert cycling coach") {
		t.Errorf("saved.SystemPrompt = %q", saved.SystemPrompt)
	}
	if !strings.Contains(saved.UserPrompt, "<athlete_profile>") || !strings.Contains(saved.UserPrompt, "<kpi_data>") {
		t.Errorf("saved.UserPrompt missing expected blocks: %q", saved.UserPrompt)
	}
}

func TestBuildProgressAnalysisUserPrompt_IncludesKPIExplanations(t *testing.T) {
	got := buildProgressAnalysisUserPrompt("profile", `{"period":"2026-04-01 to 2026-04-14"}`)
	for _, want := range []string{
		"Aerobic Efficiency (EF)",
		"Active Calorie Burn",
		"Completion Rate",
		"<athlete_profile>",
		"<kpi_data>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}

func seedProgressReportingWorkout(t *testing.T, db *sql.DB, wahooID string, startedAt time.Time, durationSec int64, tss, trimp, intensity, ef, decoupling float64) {
	t.Helper()
	id, _, err := storage.UpsertWorkout(db, &storage.Workout{
		WahooID:     wahooID,
		StartedAt:   startedAt,
		DurationSec: &durationSec,
		Calories:    reportingInt64Ptr(int64(tss * 10)),
		Source:      "api",
	})
	if err != nil {
		t.Fatalf("UpsertWorkout(%s): %v", wahooID, err)
	}
	if err := storage.UpsertRideMetrics(db, &storage.RideMetrics{
		WorkoutID:        id,
		DurationMin:      reportingFloatPtr(float64(durationSec) / 60),
		TSS:              reportingFloatPtr(tss),
		TRIMP:            reportingFloatPtr(trimp),
		IntensityFactor:  reportingFloatPtr(intensity),
		EfficiencyFactor: reportingFloatPtr(ef),
		DecouplingPct:    maybeFloatPtrReporting(decoupling),
	}); err != nil {
		t.Fatalf("UpsertRideMetrics(%s): %v", wahooID, err)
	}
}

func maybeFloatPtrReporting(v float64) *float64 {
	if v == 0 {
		return nil
	}
	return reportingFloatPtr(v)
}

func reportingFloatPtr(v float64) *float64 {
	return &v
}

func reportingInt64Ptr(v int64) *int64 {
	return &v
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
