package storage

import (
	"database/sql"
	"fmt"
	"time"
)

// DeliveryStatus is the lifecycle state of a report delivery attempt.
type DeliveryStatus string

const (
	DeliveryStatusPending DeliveryStatus = "pending"
	DeliveryStatusSent    DeliveryStatus = "sent"
	DeliveryStatusFailed  DeliveryStatus = "failed"
)

// ReportDelivery represents a row in the report_deliveries table.
type ReportDelivery struct {
	ID            int64
	ReportID      int64
	Channel       string
	Status        DeliveryStatus
	TelegramMsgID *int64
	AttemptedAt   *time.Time
	SentAt        *time.Time
	Error         *string
	CreatedAt     time.Time
}

// InsertPendingDelivery creates a pending delivery record for the given report and channel.
// It is idempotent: if a record already exists it is left unchanged (ON CONFLICT DO NOTHING).
func InsertPendingDelivery(db *sql.DB, reportID int64, channel string) error {
	_, err := db.Exec(
		`INSERT INTO report_deliveries(report_id, channel) VALUES(?, ?) ON CONFLICT(report_id, channel) DO NOTHING`,
		reportID, channel,
	)
	if err != nil {
		return fmt.Errorf("storage.InsertPendingDelivery: %w", err)
	}
	return nil
}

// GetDelivery returns the delivery record for the given report and channel, or sql.ErrNoRows.
func GetDelivery(db *sql.DB, reportID int64, channel string) (*ReportDelivery, error) {
	var d ReportDelivery
	var status string
	err := db.QueryRow(`
		SELECT id, report_id, channel, status, telegram_msg_id, attempted_at, sent_at, error, created_at
		FROM report_deliveries WHERE report_id = ? AND channel = ?`,
		reportID, channel,
	).Scan(
		&d.ID, &d.ReportID, &d.Channel, &status,
		&d.TelegramMsgID, &d.AttemptedAt, &d.SentAt, &d.Error, &d.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("storage.GetDelivery: %w", err)
	}
	d.Status = DeliveryStatus(status)
	return &d, nil
}

// MarkDeliverySent updates the delivery record to reflect a successful send.
func MarkDeliverySent(db *sql.DB, id int64, telegramMsgID int, sentAt time.Time) error {
	msgID := int64(telegramMsgID)
	_, err := db.Exec(`
		UPDATE report_deliveries
		SET status='sent', telegram_msg_id=?, attempted_at=?, sent_at=?
		WHERE id=?`,
		msgID, sentAt, sentAt, id,
	)
	if err != nil {
		return fmt.Errorf("storage.MarkDeliverySent: %w", err)
	}
	return nil
}

// MarkDeliveryFailed updates the delivery record to reflect a failed send attempt.
func MarkDeliveryFailed(db *sql.DB, id int64, errMsg string, attemptedAt time.Time) error {
	_, err := db.Exec(`
		UPDATE report_deliveries
		SET status='failed', error=?, attempted_at=?
		WHERE id=?`,
		errMsg, attemptedAt, id,
	)
	if err != nil {
		return fmt.Errorf("storage.MarkDeliveryFailed: %w", err)
	}
	return nil
}

// ListUndeliveredReports returns report IDs that have no successful delivery for the given channel.
// This includes reports with a pending or failed delivery, and reports with no delivery record at all.
func ListUndeliveredReports(db *sql.DB, channel string) ([]int64, error) {
	rows, err := db.Query(`
		SELECT r.id FROM reports r
		LEFT JOIN report_deliveries d ON d.report_id = r.id AND d.channel = ?
		WHERE d.id IS NULL OR d.status != 'sent'
		ORDER BY r.week_start ASC`,
		channel,
	)
	if err != nil {
		return nil, fmt.Errorf("storage.ListUndeliveredReports: %w", err)
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("storage.ListUndeliveredReports: scan: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage.ListUndeliveredReports: rows: %w", err)
	}
	return ids, nil
}
