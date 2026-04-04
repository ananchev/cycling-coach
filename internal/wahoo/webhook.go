package wahoo

import (
	"crypto/subtle"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

const maxWebhookBodyBytes = 1 << 20 // 1 MiB

// WebhookHandler handles POST /wahoo/webhook.
// Wahoo includes a "webhook_token" field inside the JSON payload.
// Verification is a constant-time comparison of that field against the configured secret.
// If WAHOO_WEBHOOK_SECRET is empty, token checking is skipped with a warning
// (development convenience only — always set the secret in production).
type WebhookHandler struct {
	secret string
	syncer *Syncer
}

// NewWebhookHandler constructs a WebhookHandler.
func NewWebhookHandler(secret string, syncer *Syncer) *WebhookHandler {
	return &WebhookHandler{secret: secret, syncer: syncer}
}

// Handle processes a Wahoo webhook POST.
// It always returns 200 for well-formed requests so Wahoo does not retry
// events we have intentionally ignored (unknown event types, duplicates).
// It returns 400 for malformed payloads and 401 for failed verification.
func (h *WebhookHandler) Handle(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBodyBytes))
	if err != nil {
		slog.Warn("wahoo webhook: read body failed", "err", err)
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	var event WebhookEvent
	if err := json.Unmarshal(body, &event); err != nil {
		slog.Warn("wahoo webhook: decode failed", "err", err)
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	if !h.verifyToken(event.WebhookToken) {
		slog.Warn("wahoo webhook: token verification failed",
			"remote_addr", r.RemoteAddr,
		)
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}

	slog.Info("wahoo webhook: received", "event_type", event.EventType)

	switch strings.ToLower(event.EventType) {
	case "workout_summary":
		h.handleWorkoutSummary(r, &event)
	default:
		slog.Info("wahoo webhook: unhandled event type, acknowledging",
			"event_type", event.EventType,
		)
	}

	w.WriteHeader(http.StatusOK)
}

func (h *WebhookHandler) handleWorkoutSummary(r *http.Request, event *WebhookEvent) {
	if event.WorkoutSummary == nil {
		slog.Warn("wahoo webhook: workout_summary event has nil workout_summary field")
		return
	}

	w := event.WorkoutSummary
	slog.Info("wahoo webhook: ingesting workout", "wahoo_id", w.ID)

	inserted, err := h.syncer.IngestAPIWorkout(r.Context(), w)
	if err != nil {
		slog.Error("wahoo webhook: ingest failed",
			"wahoo_id", w.ID,
			"err", err,
		)
		return
	}

	if inserted {
		slog.Info("wahoo webhook: workout inserted", "wahoo_id", w.ID)
	} else {
		slog.Info("wahoo webhook: workout already present, skipped", "wahoo_id", w.ID)
	}
}

// verifyToken checks that the webhook_token field from the JSON body matches the configured secret.
// Returns true (no check) when the secret is not configured — logs a warning.
func (h *WebhookHandler) verifyToken(token string) bool {
	if h.secret == "" {
		slog.Warn("wahoo webhook: WAHOO_WEBHOOK_SECRET not set — skipping token verification")
		return true
	}
	if token == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(h.secret)) == 1
}
