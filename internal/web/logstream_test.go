package web

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// --- LogBroadcaster ---

func TestLogBroadcaster_WriteAndRecent(t *testing.T) {
	b := NewLogBroadcaster()
	b.write("line1")
	b.write("line2")

	got := b.recent()
	if len(got) != 2 {
		t.Fatalf("recent() len = %d, want 2", len(got))
	}
	if got[0] != "line1" || got[1] != "line2" {
		t.Errorf("recent() = %v, want [line1 line2]", got)
	}
}

func TestLogBroadcaster_RingBufferOverflow(t *testing.T) {
	b := NewLogBroadcaster()
	for i := range logBufSize + 10 {
		b.write(strings.Repeat("x", i+1)) // unique length per line
	}
	got := b.recent()
	if len(got) != logBufSize {
		t.Errorf("after overflow: len(recent) = %d, want %d", len(got), logBufSize)
	}
	// The last entry should be the most-recently written line.
	last := got[len(got)-1]
	if len(last) != logBufSize+10 {
		t.Errorf("last line length = %d, want %d", len(last), logBufSize+10)
	}
}

func TestLogBroadcaster_SubscribeReceivesLines(t *testing.T) {
	b := NewLogBroadcaster()
	ch := b.subscribe()
	defer b.unsubscribe(ch)

	b.write("hello")

	select {
	case got := <-ch:
		if got != "hello" {
			t.Errorf("received %q, want %q", got, "hello")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for broadcast line")
	}
}

func TestLogBroadcaster_UnsubscribeStopsDelivery(t *testing.T) {
	b := NewLogBroadcaster()
	ch := b.subscribe()
	b.unsubscribe(ch)

	b.write("should not arrive")

	select {
	case got := <-ch:
		t.Errorf("received %q after unsubscribe", got)
	case <-time.After(50 * time.Millisecond):
		// correct: nothing received
	}
}

func TestLogBroadcaster_MultipleSubscribers(t *testing.T) {
	b := NewLogBroadcaster()
	ch1 := b.subscribe()
	ch2 := b.subscribe()
	defer b.unsubscribe(ch1)
	defer b.unsubscribe(ch2)

	b.write("broadcast")

	for i, ch := range []chan string{ch1, ch2} {
		select {
		case got := <-ch:
			if got != "broadcast" {
				t.Errorf("subscriber %d: received %q, want %q", i+1, got, "broadcast")
			}
		case <-time.After(time.Second):
			t.Errorf("subscriber %d: timed out", i+1)
		}
	}
}

// --- TeeHandler ---

// captureHandler records every slog.Record it receives.
type captureHandler struct {
	records []slog.Record
}

func (h *captureHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	h.records = append(h.records, r)
	return nil
}
func (h *captureHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *captureHandler) WithGroup(_ string) slog.Handler      { return h }

func TestTeeHandler_ForwardsToInner(t *testing.T) {
	inner := &captureHandler{}
	b := NewLogBroadcaster()
	logger := slog.New(NewTeeHandler(inner, b))
	logger.Info("test message")

	if len(inner.records) != 1 {
		t.Fatalf("inner received %d records, want 1", len(inner.records))
	}
	if inner.records[0].Message != "test message" {
		t.Errorf("inner message = %q, want %q", inner.records[0].Message, "test message")
	}
}

func TestTeeHandler_BroadcastsFormattedLine(t *testing.T) {
	inner := &captureHandler{}
	b := NewLogBroadcaster()
	ch := b.subscribe()
	defer b.unsubscribe(ch)

	logger := slog.New(NewTeeHandler(inner, b))
	logger.Warn("disk full", "path", "/data", "free_mb", 0)

	select {
	case line := <-ch:
		checks := []string{"WARN", "disk full", "path=/data", "free_mb=0"}
		for _, want := range checks {
			if !strings.Contains(line, want) {
				t.Errorf("broadcast line %q missing %q", line, want)
			}
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for broadcast line")
	}
}

func TestTeeHandler_Enabled_DelegatesToInner(t *testing.T) {
	inner := slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelWarn})
	b := NewLogBroadcaster()
	tee := NewTeeHandler(inner, b)

	if tee.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("Enabled(Debug) should be false when inner level is Warn")
	}
	if !tee.Enabled(context.Background(), slog.LevelError) {
		t.Error("Enabled(Error) should be true when inner level is Warn")
	}
}

func TestTeeHandler_WithAttrs_ReturnsTeeHandler(t *testing.T) {
	tee := NewTeeHandler(&captureHandler{}, NewLogBroadcaster())
	sub := tee.WithAttrs([]slog.Attr{slog.String("svc", "test")})
	if _, ok := sub.(*TeeHandler); !ok {
		t.Errorf("WithAttrs returned %T, want *TeeHandler", sub)
	}
}

func TestTeeHandler_WithGroup_ReturnsTeeHandler(t *testing.T) {
	tee := NewTeeHandler(&captureHandler{}, NewLogBroadcaster())
	sub := tee.WithGroup("grp")
	if _, ok := sub.(*TeeHandler); !ok {
		t.Errorf("WithGroup returned %T, want *TeeHandler", sub)
	}
}

// --- logStreamHandler (SSE endpoint) ---

func TestLogStreamHandler_ContentTypeAndHistory(t *testing.T) {
	b := NewLogBroadcaster()
	b.write("old line 1")
	b.write("old line 2")

	req := httptest.NewRequest(http.MethodGet, "/api/logs/stream", nil)
	ctx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		logStreamHandler(b)(rr, req)
	}()

	// Let the handler flush history, then cancel.
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	if ct := rr.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
	body := rr.Body.String()
	for _, want := range []string{"data: old line 1\n\n", "data: old line 2\n\n"} {
		if !strings.Contains(body, want) {
			t.Errorf("response missing %q; body: %q", want, body)
		}
	}
}

// pipeWriter buffers SSE writes and delivers complete events (delimited by
// "\n\n") to a channel so tests can read them without blocking.
type pipeWriter struct {
	ch  chan string
	buf strings.Builder
}

func (p *pipeWriter) Write(b []byte) (int, error) {
	p.buf.Write(b)
	for {
		s := p.buf.String()
		idx := strings.Index(s, "\n\n")
		if idx < 0 {
			break
		}
		p.ch <- s[:idx+2]
		p.buf.Reset()
		p.buf.WriteString(s[idx+2:])
	}
	return len(b), nil
}

// sseRW implements http.ResponseWriter + http.Flusher backed by a pipeWriter.
type sseRW struct {
	header http.Header
	pw     *pipeWriter
}

func (r *sseRW) Header() http.Header         { return r.header }
func (r *sseRW) WriteHeader(_ int)           {}
func (r *sseRW) Write(b []byte) (int, error) { return r.pw.Write(b) }
func (r *sseRW) Flush()                      {}

func TestLogStreamHandler_StreamsLiveLines(t *testing.T) {
	b := NewLogBroadcaster()

	req := httptest.NewRequest(http.MethodGet, "/api/logs/stream", nil)
	ctx, cancel := context.WithCancel(req.Context())
	defer cancel()
	req = req.WithContext(ctx)

	pw := &pipeWriter{ch: make(chan string, 32)}
	rw := &sseRW{header: make(http.Header), pw: pw}

	go logStreamHandler(b)(rw, req)

	// Allow the handler to send empty history and block on the select.
	time.Sleep(20 * time.Millisecond)
	b.write("hello from live")

	timeout := time.After(2 * time.Second)
	for {
		select {
		case s := <-pw.ch:
			if strings.Contains(s, "hello from live") {
				if !strings.HasPrefix(s, "data: ") {
					t.Errorf("SSE event %q does not start with 'data: '", s)
				}
				return // pass
			}
		case <-timeout:
			t.Fatal("timed out waiting for live SSE event")
		}
	}
}
