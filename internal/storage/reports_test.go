package storage

import (
	"database/sql"
	"errors"
	"testing"
	"time"
)

func weekOf(year int, month time.Month, day int) (start, end time.Time) {
	start = time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	end = start.AddDate(0, 0, 6)
	return
}

func TestUpsertReport_InsertAndGet(t *testing.T) {
	db := openTestDB(t)

	summary := "Great week"
	html := "<p>Full report</p>"
	ws, we := weekOf(2026, 3, 9)

	r := &Report{
		Type:         ReportTypeWeeklyReport,
		WeekStart:    ws,
		WeekEnd:      we,
		SummaryText:  &summary,
		SystemPrompt: "system prompt",
		UserPrompt:   "user prompt",
		FullHTML:     &html,
	}

	id, err := UpsertReport(db, r)
	if err != nil {
		t.Fatalf("UpsertReport: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero id")
	}

	got, err := GetReport(db, ReportTypeWeeklyReport, ws)
	if err != nil {
		t.Fatalf("GetReport: %v", err)
	}
	if got.Type != ReportTypeWeeklyReport {
		t.Errorf("Type = %q, want %q", got.Type, ReportTypeWeeklyReport)
	}
	if got.SummaryText == nil || *got.SummaryText != summary {
		t.Errorf("SummaryText = %v, want %q", got.SummaryText, summary)
	}
	if got.SystemPrompt != "system prompt" {
		t.Errorf("SystemPrompt = %q, want system prompt", got.SystemPrompt)
	}
	if got.UserPrompt != "user prompt" {
		t.Errorf("UserPrompt = %q, want user prompt", got.UserPrompt)
	}
}

func TestUpsertReport_UpdatesOnConflict(t *testing.T) {
	db := openTestDB(t)
	ws, we := weekOf(2026, 3, 9)

	r := &Report{
		Type:      ReportTypeWeeklyReport,
		WeekStart: ws,
		WeekEnd:   we,
	}
	id1, err := UpsertReport(db, r)
	if err != nil {
		t.Fatalf("first UpsertReport: %v", err)
	}

	newSummary := "Updated summary"
	r.SummaryText = &newSummary
	r.SystemPrompt = "updated system prompt"
	r.UserPrompt = "updated user prompt"
	id2, err := UpsertReport(db, r)
	if err != nil {
		t.Fatalf("second UpsertReport: %v", err)
	}
	if id1 != id2 {
		t.Errorf("row id changed on update: %d → %d", id1, id2)
	}

	got, err := GetReport(db, ReportTypeWeeklyReport, ws)
	if err != nil {
		t.Fatalf("GetReport: %v", err)
	}
	if got.SummaryText == nil || *got.SummaryText != newSummary {
		t.Errorf("SummaryText after update = %v, want %q", got.SummaryText, newSummary)
	}
	if got.SystemPrompt != "updated system prompt" {
		t.Errorf("SystemPrompt after update = %q", got.SystemPrompt)
	}
	if got.UserPrompt != "updated user prompt" {
		t.Errorf("UserPrompt after update = %q", got.UserPrompt)
	}
}

func TestGetReport_NotFound(t *testing.T) {
	db := openTestDB(t)
	ws, _ := weekOf(2026, 1, 5)
	_, err := GetReport(db, ReportTypeWeeklyReport, ws)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}

func TestListReports_OrderAndLimit(t *testing.T) {
	db := openTestDB(t)

	for _, startDay := range []int{2, 9, 16} {
		ws, we := weekOf(2026, 3, startDay)
		r := &Report{Type: ReportTypeWeeklyReport, WeekStart: ws, WeekEnd: we}
		if _, err := UpsertReport(db, r); err != nil {
			t.Fatalf("UpsertReport: %v", err)
		}
	}

	reports, err := ListReports(db, ReportTypeWeeklyReport, 2)
	if err != nil {
		t.Fatalf("ListReports: %v", err)
	}
	if len(reports) != 2 {
		t.Fatalf("expected 2 reports (limit), got %d", len(reports))
	}
	// Should be DESC by week_start: Mar 16 first, then Mar 9.
	if !reports[0].WeekStart.After(reports[1].WeekStart) {
		t.Errorf("expected DESC order by week_start")
	}
}

func TestListReports_TypeFilter(t *testing.T) {
	db := openTestDB(t)
	ws, we := weekOf(2026, 3, 9)

	if _, err := UpsertReport(db, &Report{Type: ReportTypeWeeklyReport, WeekStart: ws, WeekEnd: we}); err != nil {
		t.Fatalf("UpsertReport weekly_report: %v", err)
	}
	ws2, we2 := weekOf(2026, 3, 16)
	if _, err := UpsertReport(db, &Report{Type: ReportTypeWeeklyPlan, WeekStart: ws2, WeekEnd: we2}); err != nil {
		t.Fatalf("UpsertReport weekly_plan: %v", err)
	}

	reports, err := ListReports(db, ReportTypeWeeklyPlan, 10)
	if err != nil {
		t.Fatalf("ListReports: %v", err)
	}
	if len(reports) != 1 {
		t.Errorf("expected 1 plan, got %d", len(reports))
	}
	if reports[0].Type != ReportTypeWeeklyPlan {
		t.Errorf("got type %q, want weekly_plan", reports[0].Type)
	}
}

func TestGetLatestReport_UsesLatestWeekEnd(t *testing.T) {
	db := openTestDB(t)

	ws1, we1 := weekOf(2026, 3, 9)
	ws2, we2 := weekOf(2026, 3, 16)
	ws3 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	we3 := time.Date(2026, 3, 30, 0, 0, 0, 0, time.UTC)

	if _, err := UpsertReport(db, &Report{Type: ReportTypeWeeklyReport, WeekStart: ws1, WeekEnd: we1}); err != nil {
		t.Fatalf("UpsertReport #1: %v", err)
	}
	if _, err := UpsertReport(db, &Report{Type: ReportTypeWeeklyReport, WeekStart: ws2, WeekEnd: we2}); err != nil {
		t.Fatalf("UpsertReport #2: %v", err)
	}
	if _, err := UpsertReport(db, &Report{Type: ReportTypeWeeklyReport, WeekStart: ws3, WeekEnd: we3}); err != nil {
		t.Fatalf("UpsertReport #3: %v", err)
	}

	got, err := GetLatestReport(db, ReportTypeWeeklyReport)
	if err != nil {
		t.Fatalf("GetLatestReport: %v", err)
	}
	if got.WeekStart.Format("2006-01-02") != "2026-03-01" {
		t.Errorf("WeekStart = %s, want 2026-03-01", got.WeekStart.Format("2006-01-02"))
	}
	if got.WeekEnd.Format("2006-01-02") != "2026-03-30" {
		t.Errorf("WeekEnd = %s, want 2026-03-30", got.WeekEnd.Format("2006-01-02"))
	}
}

func TestListReportsWithDelivery_NaturalReportThenPlanOrder(t *testing.T) {
	db := openTestDB(t)

	reportStart := time.Date(2026, 3, 23, 0, 0, 0, 0, time.UTC)
	reportEnd := time.Date(2026, 4, 4, 0, 0, 0, 0, time.UTC)
	planStart := time.Date(2026, 4, 5, 0, 0, 0, 0, time.UTC)
	planEnd := time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC)

	if _, err := UpsertReport(db, &Report{Type: ReportTypeWeeklyPlan, WeekStart: planStart, WeekEnd: planEnd}); err != nil {
		t.Fatalf("UpsertReport(plan): %v", err)
	}
	if _, err := UpsertReport(db, &Report{Type: ReportTypeWeeklyReport, WeekStart: reportStart, WeekEnd: reportEnd}); err != nil {
		t.Fatalf("UpsertReport(report): %v", err)
	}

	got, err := ListReportsWithDelivery(db, time.Time{}, time.Time{}, 10)
	if err != nil {
		t.Fatalf("ListReportsWithDelivery: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(got))
	}
	if got[0].Type != ReportTypeWeeklyReport {
		t.Errorf("first row type = %q, want weekly_report", got[0].Type)
	}
	if got[1].Type != ReportTypeWeeklyPlan {
		t.Errorf("second row type = %q, want weekly_plan", got[1].Type)
	}
}
