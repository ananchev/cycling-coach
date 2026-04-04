package storage

import (
	"database/sql"
	"testing"
	"time"
)

// seedReport inserts a minimal report and returns its ID.
func seedReport(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	summary := "test summary"
	id, err := UpsertReport(db, &Report{
		Type:        ReportTypeWeeklyReport,
		WeekStart:   time.Date(2026, 3, 9, 0, 0, 0, 0, time.UTC),
		WeekEnd:     time.Date(2026, 3, 16, 0, 0, 0, 0, time.UTC),
		SummaryText: &summary,
	})
	if err != nil {
		t.Fatalf("seedReport: %v", err)
	}
	return id
}

func TestInsertPendingDelivery_CreatesRecord(t *testing.T) {
	db := openTestDB(t)
	reportID := seedReport(t, db)

	if err := InsertPendingDelivery(db, reportID, "telegram"); err != nil {
		t.Fatalf("InsertPendingDelivery: %v", err)
	}

	d, err := GetDelivery(db, reportID, "telegram")
	if err != nil {
		t.Fatalf("GetDelivery: %v", err)
	}
	if d.Status != DeliveryStatusPending {
		t.Errorf("expected pending, got %q", d.Status)
	}
	if d.ReportID != reportID {
		t.Errorf("expected report_id=%d, got %d", reportID, d.ReportID)
	}
}

func TestInsertPendingDelivery_Idempotent(t *testing.T) {
	db := openTestDB(t)
	reportID := seedReport(t, db)

	for i := 0; i < 3; i++ {
		if err := InsertPendingDelivery(db, reportID, "telegram"); err != nil {
			t.Fatalf("InsertPendingDelivery attempt %d: %v", i+1, err)
		}
	}

	var count int
	db.QueryRow(`SELECT COUNT(*) FROM report_deliveries WHERE report_id = ?`, reportID).Scan(&count) //nolint:errcheck
	if count != 1 {
		t.Errorf("expected 1 row, got %d", count)
	}
}

func TestMarkDeliverySent(t *testing.T) {
	db := openTestDB(t)
	reportID := seedReport(t, db)

	InsertPendingDelivery(db, reportID, "telegram") //nolint:errcheck
	d, _ := GetDelivery(db, reportID, "telegram")

	now := time.Now().UTC().Truncate(time.Second)
	if err := MarkDeliverySent(db, d.ID, 99001, now); err != nil {
		t.Fatalf("MarkDeliverySent: %v", err)
	}

	d2, err := GetDelivery(db, reportID, "telegram")
	if err != nil {
		t.Fatalf("GetDelivery after sent: %v", err)
	}
	if d2.Status != DeliveryStatusSent {
		t.Errorf("expected sent, got %q", d2.Status)
	}
	if d2.TelegramMsgID == nil || *d2.TelegramMsgID != 99001 {
		t.Errorf("unexpected telegram_msg_id: %v", d2.TelegramMsgID)
	}
	if d2.SentAt == nil {
		t.Error("expected sent_at to be set")
	}
}

func TestMarkDeliveryFailed(t *testing.T) {
	db := openTestDB(t)
	reportID := seedReport(t, db)

	InsertPendingDelivery(db, reportID, "telegram") //nolint:errcheck
	d, _ := GetDelivery(db, reportID, "telegram")

	now := time.Now().UTC()
	if err := MarkDeliveryFailed(db, d.ID, "connection refused", now); err != nil {
		t.Fatalf("MarkDeliveryFailed: %v", err)
	}

	d2, err := GetDelivery(db, reportID, "telegram")
	if err != nil {
		t.Fatalf("GetDelivery after failed: %v", err)
	}
	if d2.Status != DeliveryStatusFailed {
		t.Errorf("expected failed, got %q", d2.Status)
	}
	if d2.Error == nil || *d2.Error != "connection refused" {
		t.Errorf("unexpected error text: %v", d2.Error)
	}
}

func TestListUndeliveredReports(t *testing.T) {
	db := openTestDB(t)

	id1 := seedReport(t, db)

	summary := "second"
	id2, _ := UpsertReport(db, &Report{
		Type:        ReportTypeWeeklyReport,
		WeekStart:   time.Date(2026, 3, 16, 0, 0, 0, 0, time.UTC),
		WeekEnd:     time.Date(2026, 3, 23, 0, 0, 0, 0, time.UTC),
		SummaryText: &summary,
	})

	// Deliver report 1.
	InsertPendingDelivery(db, id1, "telegram") //nolint:errcheck
	d, _ := GetDelivery(db, id1, "telegram")
	MarkDeliverySent(db, d.ID, 1, time.Now()) //nolint:errcheck

	// Report 2 has no delivery record — should appear as undelivered.
	undelivered, err := ListUndeliveredReports(db, "telegram")
	if err != nil {
		t.Fatalf("ListUndeliveredReports: %v", err)
	}

	if len(undelivered) != 1 {
		t.Fatalf("expected 1 undelivered, got %d: %v", len(undelivered), undelivered)
	}
	if undelivered[0] != id2 {
		t.Errorf("expected report id %d, got %d", id2, undelivered[0])
	}
}

func TestGetDelivery_NotFound(t *testing.T) {
	db := openTestDB(t)
	_, err := GetDelivery(db, 9999, "telegram")
	if err == nil {
		t.Error("expected error for non-existent delivery")
	}
}
