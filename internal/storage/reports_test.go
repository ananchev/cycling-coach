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
		Type:        ReportTypeWeeklyReport,
		WeekStart:   ws,
		WeekEnd:     we,
		SummaryText: &summary,
		FullHTML:    &html,
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
