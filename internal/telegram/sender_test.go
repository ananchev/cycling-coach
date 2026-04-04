package telegram_test

import (
	"context"
	"errors"
	"testing"

	"cycling-coach/internal/telegram"
)

func TestStubSender_RecordsMessages(t *testing.T) {
	s := &telegram.StubSender{}

	msgID, err := s.SendMessage(context.Background(), 12345, "hello")
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if msgID <= 0 {
		t.Errorf("expected positive message ID, got %d", msgID)
	}
	if len(s.Sent) != 1 {
		t.Fatalf("expected 1 sent message, got %d", len(s.Sent))
	}
	if s.Sent[0].ChatID != 12345 {
		t.Errorf("unexpected chat ID: %d", s.Sent[0].ChatID)
	}
	if s.Sent[0].Text != "hello" {
		t.Errorf("unexpected text: %q", s.Sent[0].Text)
	}
}

func TestStubSender_ReturnsError(t *testing.T) {
	s := &telegram.StubSender{Err: errors.New("rate limited")}

	_, err := s.SendMessage(context.Background(), 12345, "hello")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if len(s.Sent) != 0 {
		t.Errorf("expected no recorded messages on error, got %d", len(s.Sent))
	}
}

func TestStubSender_MultipleSends(t *testing.T) {
	s := &telegram.StubSender{}

	for i := 0; i < 5; i++ {
		if _, err := s.SendMessage(context.Background(), 99, "msg"); err != nil {
			t.Fatalf("send %d: %v", i, err)
		}
	}
	if len(s.Sent) != 5 {
		t.Errorf("expected 5 sent, got %d", len(s.Sent))
	}
}

// TestStubSender_ImplementsSender verifies the interface at compile time.
var _ telegram.Sender = (*telegram.StubSender)(nil)
var _ telegram.Sender = (*telegram.BotSender)(nil)
