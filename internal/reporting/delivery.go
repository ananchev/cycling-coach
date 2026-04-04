package reporting

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"cycling-coach/internal/storage"
	"cycling-coach/internal/telegram"
)

const deliveryChannel = "telegram"

// DeliveryService sends generated reports via Telegram and persists delivery state.
// Each report is delivered at most once per channel — idempotency is enforced through
// the report_deliveries table's UNIQUE(report_id, channel) constraint.
type DeliveryService struct {
	db      *sql.DB
	sender  telegram.Sender
	chatID  int64
	baseURL string
}

// NewDeliveryService creates a DeliveryService.
// chatID is the Telegram chat/user ID to send messages to.
// baseURL is used to construct the full-report link (e.g. "https://coach.tonio.cc").
func NewDeliveryService(db *sql.DB, sender telegram.Sender, chatID int64, baseURL string) *DeliveryService {
	return &DeliveryService{
		db:      db,
		sender:  sender,
		chatID:  chatID,
		baseURL: strings.TrimRight(baseURL, "/"),
	}
}

// Send delivers a single report to Telegram.
// It is safe to call multiple times for the same reportID — duplicate sends are silently skipped.
// On send failure the error is persisted so it can be inspected and retried later.
func (d *DeliveryService) Send(ctx context.Context, reportID int64) error {
	// 1. Ensure a delivery record exists (idempotent).
	if err := storage.InsertPendingDelivery(d.db, reportID, deliveryChannel); err != nil {
		return fmt.Errorf("reporting.DeliveryService.Send: claim delivery: %w", err)
	}

	// 2. Load current delivery state.
	del, err := storage.GetDelivery(d.db, reportID, deliveryChannel)
	if err != nil {
		return fmt.Errorf("reporting.DeliveryService.Send: get delivery: %w", err)
	}

	// 3. Already successfully sent — nothing to do.
	if del.Status == storage.DeliveryStatusSent {
		slog.Info("reporting: delivery already sent, skipping", "report_id", reportID)
		return nil
	}

	// 4. Load the report.
	report, err := storage.GetReportByID(d.db, reportID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("reporting.DeliveryService.Send: report %d not found", reportID)
		}
		return fmt.Errorf("reporting.DeliveryService.Send: load report: %w", err)
	}

	// 5. Format and send.
	text := d.formatMessage(report)
	slog.Info("reporting: delivering report",
		"report_id", reportID,
		"type", string(report.Type),
		"week_start", report.WeekStart.Format("2006-01-02"),
		"channel", deliveryChannel,
	)

	now := time.Now().UTC()
	msgID, err := d.sender.SendMessage(ctx, d.chatID, text)
	if err != nil {
		slog.Error("reporting: delivery failed",
			"report_id", reportID,
			"err", err,
		)
		if persistErr := storage.MarkDeliveryFailed(d.db, del.ID, err.Error(), now); persistErr != nil {
			slog.Warn("reporting: persist delivery failure", "err", persistErr)
		}
		return fmt.Errorf("reporting.DeliveryService.Send: send message: %w", err)
	}

	if err := storage.MarkDeliverySent(d.db, del.ID, msgID, now); err != nil {
		// Message was sent but we couldn't persist the status — log the inconsistency.
		slog.Error("reporting: sent but failed to persist delivery status",
			"report_id", reportID,
			"telegram_msg_id", msgID,
			"err", err,
		)
		return fmt.Errorf("reporting.DeliveryService.Send: persist sent status: %w", err)
	}

	slog.Info("reporting: delivered successfully",
		"report_id", reportID,
		"telegram_msg_id", msgID,
	)
	return nil
}

// SendAllUndelivered delivers all reports that have not yet been successfully sent.
// Failures for individual reports are logged and do not abort subsequent deliveries.
// Returns the count of newly delivered reports and any aggregate error.
func (d *DeliveryService) SendAllUndelivered(ctx context.Context) (int, error) {
	ids, err := storage.ListUndeliveredReports(d.db, deliveryChannel)
	if err != nil {
		return 0, fmt.Errorf("reporting.DeliveryService.SendAllUndelivered: list: %w", err)
	}

	if len(ids) == 0 {
		slog.Info("reporting: no undelivered reports")
		return 0, nil
	}

	slog.Info("reporting: sending undelivered reports", "count", len(ids))
	sent := 0
	for _, id := range ids {
		if err := d.Send(ctx, id); err != nil {
			slog.Error("reporting: delivery error", "report_id", id, "err", err)
			continue
		}
		sent++
	}
	return sent, nil
}

// formatMessage builds the Telegram message text for a report.
// Format: Claude's 5-line summary followed by a link to the full HTML report.
func (d *DeliveryService) formatMessage(r *storage.Report) string {
	var b strings.Builder

	if r.SummaryText != nil && *r.SummaryText != "" {
		b.WriteString(*r.SummaryText)
	} else {
		// Fallback header when no summary is available.
		b.WriteString("Weekly report generated.")
	}

	if d.baseURL != "" {
		kind := "reports"
		if r.Type == storage.ReportTypeWeeklyPlan {
			kind = "plans"
		}
		b.WriteString("\n\n")
		fmt.Fprintf(&b, "🔗 %s/%s/%d", d.baseURL, kind, r.ID)
	}

	return strings.TrimSpace(b.String())
}
