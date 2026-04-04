package wahoo

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"cycling-coach/internal/storage"
)

// webhookBody returns a JSON payload with the given event and webhook_token.
func webhookBody(t *testing.T, event WebhookEvent, secret string) []byte {
	t.Helper()
	event.WebhookToken = secret
	b, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	return b
}

func newWebhookRequest(t *testing.T, body []byte) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/wahoo/webhook", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

// --- token verification ---

func TestWebhook_ValidToken_Returns200(t *testing.T) {
	db := openTestDB(t)
	client := newClientForTest(http.DefaultClient, "http://unused")
	syncer := NewSyncer(client, db, t.TempDir())
	h := NewWebhookHandler("supersecret", syncer)

	body := webhookBody(t, WebhookEvent{EventType: "unknown_type"}, "supersecret")
	req := newWebhookRequest(t, body)
	w := httptest.NewRecorder()

	h.Handle(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestWebhook_InvalidToken_Returns401(t *testing.T) {
	db := openTestDB(t)
	client := newClientForTest(http.DefaultClient, "http://unused")
	syncer := NewSyncer(client, db, t.TempDir())
	h := NewWebhookHandler("supersecret", syncer)

	body := webhookBody(t, WebhookEvent{EventType: "workout_summary"}, "wrongtoken")
	req := newWebhookRequest(t, body)
	w := httptest.NewRecorder()

	h.Handle(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestWebhook_MissingToken_Returns401(t *testing.T) {
	db := openTestDB(t)
	client := newClientForTest(http.DefaultClient, "http://unused")
	syncer := NewSyncer(client, db, t.TempDir())
	h := NewWebhookHandler("supersecret", syncer)

	// No webhook_token in payload.
	body := []byte(`{"event_type":"workout_summary"}`)
	req := newWebhookRequest(t, body)
	w := httptest.NewRecorder()

	h.Handle(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestWebhook_EmptySecret_SkipsVerification(t *testing.T) {
	db := openTestDB(t)
	client := newClientForTest(http.DefaultClient, "http://unused")
	syncer := NewSyncer(client, db, t.TempDir())
	h := NewWebhookHandler("", syncer) // no secret configured

	body := []byte(`{"event_type":"unknown_type"}`)
	req := newWebhookRequest(t, body)
	w := httptest.NewRecorder()

	h.Handle(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 (no-secret mode), got %d", w.Code)
	}
}

// --- payload parsing ---

func TestWebhook_MalformedJSON_Returns400(t *testing.T) {
	db := openTestDB(t)
	client := newClientForTest(http.DefaultClient, "http://unused")
	syncer := NewSyncer(client, db, t.TempDir())
	h := NewWebhookHandler("", syncer)

	body := []byte(`not-json`)
	req := newWebhookRequest(t, body)
	w := httptest.NewRecorder()

	h.Handle(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestWebhook_UnknownEventType_Acknowledged(t *testing.T) {
	db := openTestDB(t)
	client := newClientForTest(http.DefaultClient, "http://unused")
	syncer := NewSyncer(client, db, t.TempDir())
	h := NewWebhookHandler("", syncer)

	body := []byte(`{"event_type":"user_updated"}`)
	req := newWebhookRequest(t, body)
	w := httptest.NewRecorder()

	h.Handle(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for unknown event type, got %d", w.Code)
	}
}

// --- ingestion triggering ---

func TestWebhook_WorkoutSummary_IngestsWorkout(t *testing.T) {
	db := openTestDB(t)
	client := newClientForTest(http.DefaultClient, "http://unused")
	syncer := NewSyncer(client, db, t.TempDir())
	h := NewWebhookHandler("secret", syncer)

	event := WebhookEvent{
		EventType: "workout_summary",
		WorkoutSummary: &APIWorkout{
			ID:            42001,
			WorkoutTypeID: 25,
			Starts:        time.Date(2026, 3, 20, 9, 0, 0, 0, time.UTC),
			Summary: &APISummary{
				DurationTotalAccum: 3600,
				HeartRateAvg:       130,
				PowerAvg:           210,
			},
		},
	}

	body := webhookBody(t, event, "secret")
	req := newWebhookRequest(t, body)
	w := httptest.NewRecorder()

	h.Handle(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Workout should now be in the DB.
	workout, err := storage.GetWorkoutByWahooID(db, "42001")
	if err != nil {
		t.Fatalf("GetWorkoutByWahooID: %v", err)
	}
	if workout.WahooID != "42001" {
		t.Errorf("unexpected wahoo_id: %q", workout.WahooID)
	}
}

// --- duplicate event handling ---

func TestWebhook_DuplicateEvent_Idempotent(t *testing.T) {
	db := openTestDB(t)
	client := newClientForTest(http.DefaultClient, "http://unused")
	syncer := NewSyncer(client, db, t.TempDir())
	h := NewWebhookHandler("", syncer)

	event := WebhookEvent{
		EventType: "workout_summary",
		WorkoutSummary: &APIWorkout{
			ID:            55001,
			WorkoutTypeID: 14,
			Starts:        time.Date(2026, 3, 21, 8, 0, 0, 0, time.UTC),
		},
	}
	body, _ := json.Marshal(event)

	for i := 0; i < 3; i++ {
		req := newWebhookRequest(t, body)
		w := httptest.NewRecorder()
		h.Handle(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("attempt %d: expected 200, got %d", i+1, w.Code)
		}
	}

	// Exactly one row should exist.
	rows, err := db.QueryContext(context.Background(), `SELECT COUNT(*) FROM workouts WHERE wahoo_id = '55001'`)
	if err != nil {
		t.Fatalf("count query: %v", err)
	}
	defer rows.Close()
	var count int
	rows.Next()
	rows.Scan(&count) //nolint:errcheck
	if count != 1 {
		t.Errorf("expected 1 row after 3 duplicate events, got %d", count)
	}
}

func TestWebhook_WorkoutSummary_NilWorkout_Returns200(t *testing.T) {
	db := openTestDB(t)
	client := newClientForTest(http.DefaultClient, "http://unused")
	syncer := NewSyncer(client, db, t.TempDir())
	h := NewWebhookHandler("", syncer)

	// workout_summary event but no workout_summary field — should log warning, not crash.
	body := []byte(`{"event_type":"workout_summary"}`)
	req := newWebhookRequest(t, body)
	w := httptest.NewRecorder()

	h.Handle(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}
