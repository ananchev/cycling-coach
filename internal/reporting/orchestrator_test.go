package reporting_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"cycling-coach/internal/reporting"
	"cycling-coach/internal/storage"
)

func TestOrchestrator_Generate_Success(t *testing.T) {
	db := openTestDB(t)
	profile := writeTempProfile(t, "FTP: 251W")

	weekStart := time.Date(2026, 3, 9, 0, 0, 0, 0, time.UTC)
	weekEnd := weekStart.Add(7 * 24 * time.Hour)

	stub := &reporting.StubProvider{
		Output: &reporting.ReportOutput{
			Summary:   "Great week.",
			Narrative: "# Week in review\n\nAll sessions completed.",
		},
	}

	orch := reporting.NewOrchestrator(db, profile, stub)
	id, err := orch.Generate(context.Background(), storage.ReportTypeWeeklyReport, weekStart, weekEnd, "")
	if err != nil {
		t.Fatalf("Generate error: %v", err)
	}
	if id <= 0 {
		t.Errorf("expected positive report ID, got %d", id)
	}

	// Verify it was persisted.
	rep, err := storage.GetReport(db, storage.ReportTypeWeeklyReport, weekStart)
	if err != nil {
		t.Fatalf("GetReport: %v", err)
	}
	if rep.SummaryText == nil || *rep.SummaryText != "Great week." {
		t.Errorf("unexpected summary: %v", rep.SummaryText)
	}
	if rep.FullHTML == nil || len(*rep.FullHTML) == 0 {
		t.Error("expected non-empty HTML")
	}
}

func TestOrchestrator_Generate_ProviderError(t *testing.T) {
	db := openTestDB(t)
	profile := writeTempProfile(t, "FTP: 251W")

	weekStart := time.Date(2026, 3, 9, 0, 0, 0, 0, time.UTC)
	weekEnd := weekStart.Add(7 * 24 * time.Hour)

	stub := &reporting.StubProvider{Err: errors.New("API unavailable")}

	orch := reporting.NewOrchestrator(db, profile, stub)
	_, err := orch.Generate(context.Background(), storage.ReportTypeWeeklyReport, weekStart, weekEnd, "")
	if err == nil {
		t.Fatal("expected error from provider, got nil")
	}
}

func TestOrchestrator_Generate_Idempotent(t *testing.T) {
	db := openTestDB(t)
	profile := writeTempProfile(t, "FTP: 251W")

	weekStart := time.Date(2026, 3, 9, 0, 0, 0, 0, time.UTC)
	weekEnd := weekStart.Add(7 * 24 * time.Hour)

	stub := &reporting.StubProvider{}
	orch := reporting.NewOrchestrator(db, profile, stub)

	id1, err := orch.Generate(context.Background(), storage.ReportTypeWeeklyReport, weekStart, weekEnd, "")
	if err != nil {
		t.Fatalf("first Generate: %v", err)
	}

	id2, err := orch.Generate(context.Background(), storage.ReportTypeWeeklyReport, weekStart, weekEnd, "")
	if err != nil {
		t.Fatalf("second Generate: %v", err)
	}

	// UpsertReport updates in place — same row ID expected.
	if id1 != id2 {
		t.Errorf("expected same report ID on re-generation; got %d then %d", id1, id2)
	}
}

func TestOrchestrator_Generate_MissingProfile(t *testing.T) {
	db := openTestDB(t)

	weekStart := time.Date(2026, 3, 9, 0, 0, 0, 0, time.UTC)
	weekEnd := weekStart.Add(7 * 24 * time.Hour)

	stub := &reporting.StubProvider{}
	orch := reporting.NewOrchestrator(db, "/nonexistent/athlete-profile.md", stub)

	_, err := orch.Generate(context.Background(), storage.ReportTypeWeeklyReport, weekStart, weekEnd, "")
	if err == nil {
		t.Fatal("expected error for missing profile, got nil")
	}
}

func TestOrchestrator_Generate_HTMLContainsContent(t *testing.T) {
	db := openTestDB(t)
	profile := writeTempProfile(t, "FTP: 251W")

	weekStart := time.Date(2026, 3, 9, 0, 0, 0, 0, time.UTC)
	weekEnd := weekStart.Add(7 * 24 * time.Hour)

	stub := &reporting.StubProvider{
		Output: &reporting.ReportOutput{
			Summary:   "Five line summary here.",
			Narrative: "Detailed narrative text.",
		},
	}

	orch := reporting.NewOrchestrator(db, profile, stub)
	id, err := orch.Generate(context.Background(), storage.ReportTypeWeeklyReport, weekStart, weekEnd, "")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	rep, err := storage.GetReport(db, storage.ReportTypeWeeklyReport, weekStart)
	if err != nil {
		t.Fatalf("GetReport: %v", err)
	}
	_ = id

	html := *rep.FullHTML
	for _, want := range []string{"Weekly Report", "Five line summary here.", "Detailed narrative text."} {
		if !contains(html, want) {
			t.Errorf("HTML missing %q", want)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStr(s, sub))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
