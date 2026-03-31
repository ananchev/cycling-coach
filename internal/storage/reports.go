package storage

import (
	"database/sql"
	"fmt"
	"time"
)

// ReportType is the discriminator for the reports table.
type ReportType string

const (
	ReportTypeWeeklyReport ReportType = "weekly_report"
	ReportTypeWeeklyPlan   ReportType = "weekly_plan"
)

// Report represents a row in the reports table.
type Report struct {
	ID          int64
	Type        ReportType
	WeekStart   time.Time
	WeekEnd     time.Time
	SummaryText *string
	FullHTML    *string
	CreatedAt   time.Time
}

// UpsertReport inserts or updates a report for the given type + week.
// The unique index on (type, week_start) drives conflict resolution;
// week_end, summary_text, and full_html are updated on conflict.
// Returns the row ID.
func UpsertReport(db *sql.DB, r *Report) (int64, error) {
	_, err := db.Exec(`
		INSERT INTO reports(type, week_start, week_end, summary_text, full_html)
		VALUES(?, ?, ?, ?, ?)
		ON CONFLICT(type, week_start) DO UPDATE SET
			week_end=excluded.week_end,
			summary_text=excluded.summary_text,
			full_html=excluded.full_html`,
		string(r.Type), r.WeekStart, r.WeekEnd, r.SummaryText, r.FullHTML,
	)
	if err != nil {
		return 0, fmt.Errorf("storage.UpsertReport: %w", err)
	}

	var id int64
	err = db.QueryRow(
		`SELECT id FROM reports WHERE type = ? AND week_start = ?`,
		string(r.Type), r.WeekStart,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("storage.UpsertReport: select id: %w", err)
	}
	return id, nil
}

// GetReport returns the report for the given type and week start, or sql.ErrNoRows.
func GetReport(db *sql.DB, reportType ReportType, weekStart time.Time) (*Report, error) {
	var rep Report
	var repType string
	err := db.QueryRow(`
		SELECT id, type, week_start, week_end, summary_text, full_html, created_at
		FROM reports WHERE type = ? AND week_start = ?`,
		string(reportType), weekStart,
	).Scan(
		&rep.ID, &repType, &rep.WeekStart, &rep.WeekEnd,
		&rep.SummaryText, &rep.FullHTML, &rep.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("storage.GetReport: %w", err)
	}
	rep.Type = ReportType(repType)
	return &rep, nil
}

// ListReports returns reports of the given type ordered by week_start DESC, limited to n rows.
func ListReports(db *sql.DB, reportType ReportType, limit int) ([]Report, error) {
	rows, err := db.Query(`
		SELECT id, type, week_start, week_end, summary_text, full_html, created_at
		FROM reports WHERE type = ?
		ORDER BY week_start DESC LIMIT ?`,
		string(reportType), limit,
	)
	if err != nil {
		return nil, fmt.Errorf("storage.ListReports: %w", err)
	}
	defer rows.Close()

	var out []Report
	for rows.Next() {
		var rep Report
		var repType string
		err := rows.Scan(
			&rep.ID, &repType, &rep.WeekStart, &rep.WeekEnd,
			&rep.SummaryText, &rep.FullHTML, &rep.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("storage.ListReports: scan: %w", err)
		}
		rep.Type = ReportType(repType)
		out = append(out, rep)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage.ListReports: rows: %w", err)
	}
	return out, nil
}
