package reporting_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"cycling-coach/internal/reporting"
	"cycling-coach/internal/storage"
	"cycling-coach/internal/telegram"
)

// seedReportForDelivery inserts a report and returns its ID.
func seedReportForDelivery(t *testing.T, db *sql.DB, summary string) int64 {
	t.Helper()
	id, err := storage.UpsertReport(db, &storage.Report{
		Type:        storage.ReportTypeWeeklyReport,
		WeekStart:   time.Date(2026, 3, 9, 0, 0, 0, 0, time.UTC),
		WeekEnd:     time.Date(2026, 3, 16, 0, 0, 0, 0, time.UTC),
		SummaryText: &summary,
	})
	if err != nil {
		t.Fatalf("seedReportForDelivery: %v", err)
	}
	return id
}

func TestDeliveryService_Send_Success(t *testing.T) {
	db := openTestDB(t)
	stub := &telegram.StubSender{}
	svc := reporting.NewDeliveryService(db, stub, 12345, "https://coach.example.com")

	reportID := seedReportForDelivery(t, db, "Great week.\nAll sessions done.\nDrift avg 3.5%.\nWeight stable.\nReady for next week.")

	if err := svc.Send(context.Background(), reportID); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if len(stub.Sent) != 1 {
		t.Fatalf("expected 1 sent message, got %d", len(stub.Sent))
	}
	if stub.Sent[0].ChatID != 12345 {
		t.Errorf("unexpected chat ID: %d", stub.Sent[0].ChatID)
	}
	// Message should contain the summary and the report link.
	msg := stub.Sent[0].Text
	if !strings.Contains(msg, "Great week.") {
		t.Errorf("message missing summary text: %q", msg)
	}
	if !strings.Contains(msg, "https://coach.example.com/reports/") {
		t.Errorf("message missing report link: %q", msg)
	}
}

func TestDeliveryService_Send_PlanLinkUsesPlansPath(t *testing.T) {
	db := openTestDB(t)
	stub := &telegram.StubSender{}
	svc := reporting.NewDeliveryService(db, stub, 99, "https://coach.example.com")

	summary := "Plan for next week."
	id, err := storage.UpsertReport(db, &storage.Report{
		Type:        storage.ReportTypeWeeklyPlan,
		WeekStart:   time.Date(2026, 3, 16, 0, 0, 0, 0, time.UTC),
		WeekEnd:     time.Date(2026, 3, 23, 0, 0, 0, 0, time.UTC),
		SummaryText: &summary,
	})
	if err != nil {
		t.Fatalf("UpsertReport: %v", err)
	}

	if err := svc.Send(context.Background(), id); err != nil {
		t.Fatalf("Send: %v", err)
	}

	msg := stub.Sent[0].Text
	if !strings.Contains(msg, "/plans/") {
		t.Errorf("plan message should contain /plans/ path: %q", msg)
	}
}

func TestDeliveryService_Send_Idempotent(t *testing.T) {
	db := openTestDB(t)
	stub := &telegram.StubSender{}
	svc := reporting.NewDeliveryService(db, stub, 12345, "")

	reportID := seedReportForDelivery(t, db, "Summary.")

	// Send three times — only one actual Telegram message should be sent.
	for i := 0; i < 3; i++ {
		if err := svc.Send(context.Background(), reportID); err != nil {
			t.Fatalf("Send attempt %d: %v", i+1, err)
		}
	}

	if len(stub.Sent) != 1 {
		t.Errorf("expected exactly 1 sent message (idempotent), got %d", len(stub.Sent))
	}
}

func TestDeliveryService_Send_FailureRecorded(t *testing.T) {
	db := openTestDB(t)
	stub := &telegram.StubSender{Err: errors.New("telegram down")}
	svc := reporting.NewDeliveryService(db, stub, 12345, "")

	reportID := seedReportForDelivery(t, db, "Summary.")

	err := svc.Send(context.Background(), reportID)
	if err == nil {
		t.Fatal("expected error when sender fails")
	}

	// Delivery record should reflect the failure.
	del, err := storage.GetDelivery(db, reportID, "telegram")
	if err != nil {
		t.Fatalf("GetDelivery: %v", err)
	}
	if del.Status != storage.DeliveryStatusFailed {
		t.Errorf("expected failed status, got %q", del.Status)
	}
	if del.Error == nil || !strings.Contains(*del.Error, "telegram down") {
		t.Errorf("unexpected error in delivery record: %v", del.Error)
	}
}

func TestDeliveryService_Send_FailedThenRetried(t *testing.T) {
	db := openTestDB(t)
	stub := &telegram.StubSender{Err: errors.New("first attempt fails")}
	svc := reporting.NewDeliveryService(db, stub, 12345, "")

	reportID := seedReportForDelivery(t, db, "Summary.")

	// First attempt — fails.
	svc.Send(context.Background(), reportID) //nolint:errcheck

	// Second attempt — fix the sender and retry.
	stub.Err = nil
	if err := svc.Send(context.Background(), reportID); err != nil {
		t.Fatalf("retry Send: %v", err)
	}

	// Should now be sent.
	del, err := storage.GetDelivery(db, reportID, "telegram")
	if err != nil {
		t.Fatalf("GetDelivery: %v", err)
	}
	if del.Status != storage.DeliveryStatusSent {
		t.Errorf("expected sent after retry, got %q", del.Status)
	}
	if len(stub.Sent) != 1 {
		t.Errorf("expected 1 sent message after retry, got %d", len(stub.Sent))
	}
}

func TestDeliveryService_Send_MissingReport(t *testing.T) {
	db := openTestDB(t)
	stub := &telegram.StubSender{}
	svc := reporting.NewDeliveryService(db, stub, 12345, "")

	err := svc.Send(context.Background(), 9999)
	if err == nil {
		t.Fatal("expected error for non-existent report")
	}
}

func TestDeliveryService_SendAllUndelivered(t *testing.T) {
	db := openTestDB(t)
	stub := &telegram.StubSender{}
	svc := reporting.NewDeliveryService(db, stub, 12345, "")

	// Seed two reports; deliver one manually.
	id1 := seedReportForDelivery(t, db, "Week 1.")
	s2 := "Week 2."
	id2, _ := storage.UpsertReport(db, &storage.Report{
		Type:        storage.ReportTypeWeeklyReport,
		WeekStart:   time.Date(2026, 3, 16, 0, 0, 0, 0, time.UTC),
		WeekEnd:     time.Date(2026, 3, 23, 0, 0, 0, 0, time.UTC),
		SummaryText: &s2,
	})

	// Pre-deliver id1.
	svc.Send(context.Background(), id1) //nolint:errcheck

	_ = id2
	// Now send all undelivered — only id2 should be sent.
	n, err := svc.SendAllUndelivered(context.Background())
	if err != nil {
		t.Fatalf("SendAllUndelivered: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 new delivery, got %d", n)
	}
	if len(stub.Sent) != 2 { // 1 from first Send + 1 from SendAllUndelivered
		t.Errorf("expected 2 total sent messages, got %d", len(stub.Sent))
	}
}
