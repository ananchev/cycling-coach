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
	if rep.SystemPrompt != "FTP: 251W" {
		t.Errorf("unexpected system prompt: %q", rep.SystemPrompt)
	}
	if rep.UserPrompt == "" {
		t.Error("expected saved user prompt to be populated")
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
	for _, want := range []string{"Training Report", "Period:", "Five line summary here.", "Detailed narrative text."} {
		if !contains(html, want) {
			t.Errorf("HTML missing %q", want)
		}
	}
}

func TestOrchestrator_GenerateCloseBlock_Success(t *testing.T) {
	db := openTestDB(t)
	profile := writeTempProfile(t, "FTP: 251W")

	prevStart := time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)
	prevEnd := time.Date(2026, 3, 8, 0, 0, 0, 0, time.UTC)
	prevNarrative := "# Prior plan\n\nKeep the week aerobic."
	if _, err := storage.UpsertReport(db, &storage.Report{
		Type:          storage.ReportTypeWeeklyPlan,
		WeekStart:     prevStart.AddDate(0, 0, 7),
		WeekEnd:       prevEnd.AddDate(0, 0, 7),
		NarrativeText: &prevNarrative,
	}); err != nil {
		t.Fatalf("UpsertReport(previous plan): %v", err)
	}
	if _, err := storage.UpsertReport(db, &storage.Report{
		Type:      storage.ReportTypeWeeklyReport,
		WeekStart: prevStart,
		WeekEnd:   prevEnd,
	}); err != nil {
		t.Fatalf("UpsertReport(previous report): %v", err)
	}

	provider := &recordingProvider{}
	orch := reporting.NewOrchestrator(db, profile, provider)

	got, err := orch.GenerateCloseBlock(context.Background(), time.Date(2026, 3, 14, 0, 0, 0, 0, time.UTC), nil, "Travel Tuesday")
	if err != nil {
		t.Fatalf("GenerateCloseBlock: %v", err)
	}

	if got.BlockStart.Format("2006-01-02") != "2026-03-09" {
		t.Errorf("BlockStart = %s, want 2026-03-09", got.BlockStart.Format("2006-01-02"))
	}
	if got.BlockEnd.Format("2006-01-02") != "2026-03-14" {
		t.Errorf("BlockEnd = %s, want 2026-03-14", got.BlockEnd.Format("2006-01-02"))
	}
	if got.PlanStart.Format("2006-01-02") != "2026-03-15" {
		t.Errorf("PlanStart = %s, want 2026-03-15", got.PlanStart.Format("2006-01-02"))
	}
	if got.PlanEnd.Format("2006-01-02") != "2026-03-21" {
		t.Errorf("PlanEnd = %s, want 2026-03-21", got.PlanEnd.Format("2006-01-02"))
	}
	if len(provider.inputs) != 2 {
		t.Fatalf("provider calls = %d, want 2", len(provider.inputs))
	}
	if provider.inputs[0].Type != storage.ReportTypeWeeklyReport {
		t.Errorf("first provider type = %q, want weekly_report", provider.inputs[0].Type)
	}
	if provider.inputs[1].Type != storage.ReportTypeWeeklyPlan {
		t.Errorf("second provider type = %q, want weekly_plan", provider.inputs[1].Type)
	}
	if provider.inputs[1].UserPrompt != "Travel Tuesday" {
		t.Errorf("plan user prompt = %q, want Travel Tuesday", provider.inputs[1].UserPrompt)
	}

	// Profile patch is skipped when provider is not *ClaudeProvider.
	if got.ProfilePatched {
		t.Error("ProfilePatched should be false with a non-Claude provider")
	}

	rep, err := storage.GetReport(db, storage.ReportTypeWeeklyReport, time.Date(2026, 3, 9, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("GetReport(closed block report): %v", err)
	}
	if rep.WeekEnd.Format("2006-01-02") != "2026-03-14" {
		t.Errorf("closed report week_end = %s, want 2026-03-14", rep.WeekEnd.Format("2006-01-02"))
	}

	plan, err := storage.GetReport(db, storage.ReportTypeWeeklyPlan, time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("GetReport(next plan): %v", err)
	}
	if plan.WeekEnd.Format("2006-01-02") != "2026-03-21" {
		t.Errorf("plan week_end = %s, want 2026-03-21", plan.WeekEnd.Format("2006-01-02"))
	}
}

func TestOrchestrator_GenerateCloseBlock_NoPreviousReport(t *testing.T) {
	db := openTestDB(t)
	profile := writeTempProfile(t, "FTP: 251W")

	orch := reporting.NewOrchestrator(db, profile, &recordingProvider{})
	_, err := orch.GenerateCloseBlock(context.Background(), time.Date(2026, 3, 14, 0, 0, 0, 0, time.UTC), nil, "")
	if err == nil {
		t.Fatal("expected error when no previous weekly report exists")
	}
}

func TestOrchestrator_GenerateCloseBlock_FirstRunUsesManualInitialStart(t *testing.T) {
	db := openTestDB(t)
	profile := writeTempProfile(t, "FTP: 251W")

	provider := &recordingProvider{}
	orch := reporting.NewOrchestrator(db, profile, provider)
	initialStart := time.Date(2026, 3, 3, 0, 0, 0, 0, time.UTC)

	got, err := orch.GenerateCloseBlock(context.Background(), time.Date(2026, 3, 14, 0, 0, 0, 0, time.UTC), &initialStart, "Keep Wednesday light")
	if err != nil {
		t.Fatalf("GenerateCloseBlock: %v", err)
	}

	if got.BlockStart.Format("2006-01-02") != "2026-03-03" {
		t.Errorf("BlockStart = %s, want 2026-03-03", got.BlockStart.Format("2006-01-02"))
	}
	if len(provider.inputs) != 2 {
		t.Fatalf("provider calls = %d, want 2", len(provider.inputs))
	}
	if provider.inputs[0].WeekStart.Format("2006-01-02") != "2026-03-03" {
		t.Errorf("report WeekStart = %s, want 2026-03-03", provider.inputs[0].WeekStart.Format("2006-01-02"))
	}
}

type recordingProvider struct {
	inputs []*reporting.ReportInput
}

func (r *recordingProvider) Generate(_ context.Context, input *reporting.ReportInput) (*reporting.ReportOutput, error) {
	clone := *input
	r.inputs = append(r.inputs, &clone)
	if input.Type == storage.ReportTypeWeeklyReport {
		return &reporting.ReportOutput{
			Summary:   "Closed block summary",
			Narrative: "# Closed block report",
		}, nil
	}
	return &reporting.ReportOutput{
		Summary:   "Next plan summary",
		Narrative: "# Next plan",
	}, nil
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
