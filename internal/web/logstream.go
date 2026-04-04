package web

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// logBufSize is the number of recent log lines kept in memory for new subscribers.
const logBufSize = 200

// LogBroadcaster captures log lines and fans them out to connected SSE clients.
// It is safe for concurrent use.
type LogBroadcaster struct {
	mu      sync.Mutex
	buf     []string
	clients map[chan string]struct{}
	done    chan struct{} // closed by Close() to signal SSE handlers to exit
}

// NewLogBroadcaster creates a broadcaster with a 200-line history buffer.
func NewLogBroadcaster() *LogBroadcaster {
	return &LogBroadcaster{
		clients: make(map[chan string]struct{}),
		done:    make(chan struct{}),
	}
}

// Close signals all active SSE handlers to exit. Call this before
// http.Server.Shutdown so long-lived SSE connections drain immediately
// instead of blocking the shutdown timeout.
func (b *LogBroadcaster) Close() {
	close(b.done)
}

func (b *LogBroadcaster) write(line string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.buf) >= logBufSize {
		b.buf = b.buf[1:]
	}
	b.buf = append(b.buf, line)
	for ch := range b.clients {
		select {
		case ch <- line:
		default: // drop if the client channel is full (slow reader)
		}
	}
}

func (b *LogBroadcaster) subscribe() chan string {
	ch := make(chan string, 64)
	b.mu.Lock()
	b.clients[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

func (b *LogBroadcaster) unsubscribe(ch chan string) {
	b.mu.Lock()
	delete(b.clients, ch)
	b.mu.Unlock()
}

func (b *LogBroadcaster) recent() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]string, len(b.buf))
	copy(out, b.buf)
	return out
}

// TeeHandler is a slog.Handler that forwards records to an inner handler and
// also broadcasts a compact one-line summary to a LogBroadcaster for the
// live-log panel in the admin UI.
type TeeHandler struct {
	inner slog.Handler
	b     *LogBroadcaster
}

// NewTeeHandler wraps inner, sending a copy of each log record to b.
func NewTeeHandler(inner slog.Handler, b *LogBroadcaster) *TeeHandler {
	return &TeeHandler{inner: inner, b: b}
}

func (h *TeeHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *TeeHandler) Handle(ctx context.Context, r slog.Record) error {
	t := r.Time
	if t.IsZero() {
		t = time.Now()
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s %-5s %s", t.Format("15:04:05"), r.Level.String(), r.Message)
	r.Attrs(func(a slog.Attr) bool {
		fmt.Fprintf(&sb, " %s=%v", a.Key, a.Value.Any())
		return true
	})
	h.b.write(sb.String())
	return h.inner.Handle(ctx, r)
}

func (h *TeeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &TeeHandler{inner: h.inner.WithAttrs(attrs), b: h.b}
}

func (h *TeeHandler) WithGroup(name string) slog.Handler {
	return &TeeHandler{inner: h.inner.WithGroup(name), b: h.b}
}

// logStreamHandler streams log lines to the browser via Server-Sent Events.
// New connections receive the last 200 buffered lines immediately, then live updates.
func logStreamHandler(b *LogBroadcaster) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no") // disable nginx/proxy buffering

		// Send buffered history so the panel is populated immediately.
		for _, line := range b.recent() {
			fmt.Fprintf(w, "data: %s\n\n", line)
		}
		flusher.Flush()

		ch := b.subscribe()
		defer b.unsubscribe(ch)

		// Periodic keep-alive comment to survive proxy idle-connection timeouts.
		ticker := time.NewTicker(25 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case line := <-ch:
				fmt.Fprintf(w, "data: %s\n\n", line)
				flusher.Flush()
			case <-ticker.C:
				fmt.Fprintf(w, ": ping\n\n")
				flusher.Flush()
			case <-b.done:
				return
			case <-r.Context().Done():
				return
			}
		}
	}
}
