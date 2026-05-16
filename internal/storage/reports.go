package storage

import (
	"database/sql"
	"errors"
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
	ID            int64
	Type          ReportType
	WeekStart     time.Time
	WeekEnd       time.Time
	SummaryText   *string // compact 5-line Telegram summary
	SystemPrompt  string
	UserPrompt    string
	NarrativeText *string // full markdown coaching narrative (used for plan vs. real comparison)
	FullHTML      *string
	CreatedAt     time.Time
}

// UpsertReport inserts or updates a report for the given type + week.
// The unique index on (type, week_start) drives conflict resolution;
// week_end, summary_text, and full_html are updated on conflict.
// Returns the row ID.
func UpsertReport(db *sql.DB, r *Report) (int64, error) {
	_, err := db.Exec(`
		INSERT INTO reports(type, week_start, week_end, summary_text, system_prompt, user_prompt, narrative_text, full_html)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(type, week_start) DO UPDATE SET
			week_end=excluded.week_end,
			summary_text=excluded.summary_text,
			system_prompt=excluded.system_prompt,
			user_prompt=excluded.user_prompt,
			narrative_text=excluded.narrative_text,
			full_html=excluded.full_html`,
		string(r.Type), r.WeekStart, r.WeekEnd, r.SummaryText, r.SystemPrompt, r.UserPrompt, r.NarrativeText, r.FullHTML,
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

// GetReportByID returns the report with the given ID, or sql.ErrNoRows.
func GetReportByID(db *sql.DB, id int64) (*Report, error) {
	var rep Report
	var repType string
	err := db.QueryRow(`
		SELECT id, type, week_start, week_end, summary_text, system_prompt, user_prompt, narrative_text, full_html, created_at
		FROM reports WHERE id = ?`, id,
	).Scan(
		&rep.ID, &repType, &rep.WeekStart, &rep.WeekEnd,
		&rep.SummaryText, &rep.SystemPrompt, &rep.UserPrompt, &rep.NarrativeText, &rep.FullHTML, &rep.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("storage.GetReportByID: %w", err)
	}
	rep.Type = ReportType(repType)
	return &rep, nil
}

// GetReport returns the report for the given type and week start, or sql.ErrNoRows.
func GetReport(db *sql.DB, reportType ReportType, weekStart time.Time) (*Report, error) {
	var rep Report
	var repType string
	err := db.QueryRow(`
		SELECT id, type, week_start, week_end, summary_text, system_prompt, user_prompt, narrative_text, full_html, created_at
		FROM reports WHERE type = ? AND week_start = ?`,
		string(reportType), weekStart,
	).Scan(
		&rep.ID, &repType, &rep.WeekStart, &rep.WeekEnd,
		&rep.SummaryText, &rep.SystemPrompt, &rep.UserPrompt, &rep.NarrativeText, &rep.FullHTML, &rep.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("storage.GetReport: %w", err)
	}
	rep.Type = ReportType(repType)
	return &rep, nil
}

// GetLatestReport returns the most recent report of the given type ordered by week_end DESC.
func GetLatestReport(db *sql.DB, reportType ReportType) (*Report, error) {
	var rep Report
	var repType string
	err := db.QueryRow(`
		SELECT id, type, week_start, week_end, summary_text, system_prompt, user_prompt, narrative_text, full_html, created_at
		FROM reports
		WHERE type = ?
		ORDER BY week_end DESC, week_start DESC
		LIMIT 1`,
		string(reportType),
	).Scan(
		&rep.ID, &repType, &rep.WeekStart, &rep.WeekEnd,
		&rep.SummaryText, &rep.SystemPrompt, &rep.UserPrompt, &rep.NarrativeText, &rep.FullHTML, &rep.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, sql.ErrNoRows
		}
		return nil, fmt.Errorf("storage.GetLatestReport: %w", err)
	}
	rep.Type = ReportType(repType)
	return &rep, nil
}

// ReportWithDelivery combines a Report with its optional Telegram delivery status.
type ReportWithDelivery struct {
	Report
	DeliveryStatus  *string // nil when no delivery record exists
	SentAt          *time.Time
	DeliveryError   *string
	HasSystemPrompt bool
	HasUserPrompt   bool
	HasNarrative    bool
}

// ListReportsWithDelivery returns all reports (both types) joined with their
// optional delivery status, filtered by week_start in [from, to].
// Zero from/to means no lower/upper bound. Results are ordered to read naturally
// in the coaching workflow: a finished-block weekly_report appears before the
// following weekly_plan it leads into.
func ListReportsWithDelivery(db *sql.DB, from, to time.Time, limit int) ([]ReportWithDelivery, error) {
	query := `
		SELECT r.id, r.type, r.week_start, r.week_end, r.summary_text, r.full_html, r.created_at,
		       CASE WHEN r.system_prompt <> '' THEN 1 ELSE 0 END,
		       CASE WHEN r.user_prompt <> '' THEN 1 ELSE 0 END,
		       CASE WHEN r.narrative_text IS NOT NULL AND r.narrative_text != '' THEN 1 ELSE 0 END,
		       d.status, d.sent_at, d.error
		FROM reports r
		LEFT JOIN report_deliveries d ON d.report_id = r.id AND d.channel = 'telegram'`

	args := []any{}
	conditions := []string{}
	if !from.IsZero() {
		conditions = append(conditions, "r.week_start >= ?")
		args = append(args, from)
	}
	if !to.IsZero() {
		conditions = append(conditions, "r.week_start <= ?")
		args = append(args, to)
	}
	if len(conditions) > 0 {
		query += " WHERE " + conditions[0]
		for _, c := range conditions[1:] {
			query += " AND " + c
		}
	}
	query += `
		ORDER BY
			CASE
				WHEN r.type = 'weekly_report' THEN r.week_end
				ELSE date(r.week_start, '-1 day')
			END DESC,
			CASE WHEN r.type = 'weekly_report' THEN 0 ELSE 1 END ASC,
			r.week_start DESC`
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("storage.ListReportsWithDelivery: %w", err)
	}
	defer rows.Close()

	var out []ReportWithDelivery
	for rows.Next() {
		var rwd ReportWithDelivery
		var repType string
		err := rows.Scan(
			&rwd.ID, &repType, &rwd.WeekStart, &rwd.WeekEnd,
			&rwd.SummaryText, &rwd.FullHTML, &rwd.CreatedAt,
			&rwd.HasSystemPrompt, &rwd.HasUserPrompt, &rwd.HasNarrative,
			&rwd.DeliveryStatus, &rwd.SentAt, &rwd.DeliveryError,
		)
		if err != nil {
			return nil, fmt.Errorf("storage.ListReportsWithDelivery: scan: %w", err)
		}
		rwd.Type = ReportType(repType)
		out = append(out, rwd)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage.ListReportsWithDelivery: rows: %w", err)
	}
	return out, nil
}

// DeleteReport deletes a report and its delivery record (cascade via FK) by ID.
func DeleteReport(db *sql.DB, id int64) error {
	// Delete delivery record first (no ON DELETE CASCADE defined in schema).
	if _, err := db.Exec(`DELETE FROM report_deliveries WHERE report_id = ?`, id); err != nil {
		return fmt.Errorf("storage.DeleteReport: deliveries: %w", err)
	}
	res, err := db.Exec(`DELETE FROM reports WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("storage.DeleteReport: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("storage.DeleteReport: %w", sql.ErrNoRows)
	}
	return nil
}

// ListReports returns reports of the given type ordered by week_start DESC, limited to n rows.
func ListReports(db *sql.DB, reportType ReportType, limit int) ([]Report, error) {
	rows, err := db.Query(`
		SELECT id, type, week_start, week_end, summary_text, system_prompt, user_prompt, narrative_text, full_html, created_at
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
			&rep.SummaryText, &rep.SystemPrompt, &rep.UserPrompt, &rep.NarrativeText, &rep.FullHTML, &rep.CreatedAt,
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
