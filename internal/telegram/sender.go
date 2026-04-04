package telegram

import (
	"context"
	"fmt"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Sender is the interface for outbound Telegram message delivery.
// BotSender is the production implementation; StubSender is used in tests.
type Sender interface {
	SendMessage(ctx context.Context, chatID int64, text string) (messageID int, err error)
}

// BotSender sends messages via the Telegram Bot API.
type BotSender struct {
	bot *tgbotapi.BotAPI
}

// NewBotSender creates a BotSender using the given bot token.
// Returns an error if the token is invalid or the API is unreachable.
func NewBotSender(token string) (*BotSender, error) {
	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, fmt.Errorf("telegram.NewBotSender: %w", err)
	}
	return &BotSender{bot: bot}, nil
}

// SendMessage sends a plain-text message to the given chat ID.
// Returns the Telegram message ID assigned by the server.
func (s *BotSender) SendMessage(_ context.Context, chatID int64, text string) (int, error) {
	msg := tgbotapi.NewMessage(chatID, text)
	sent, err := s.bot.Send(msg)
	if err != nil {
		return 0, fmt.Errorf("telegram.BotSender.SendMessage: %w", err)
	}
	return sent.MessageID, nil
}

// StubSender is a test double for Sender that records sent messages in memory.
// Set Err to make SendMessage return an error on the next call.
type StubSender struct {
	Sent []SentMessage
	Err  error
}

// SentMessage records a single call to StubSender.SendMessage.
type SentMessage struct {
	ChatID int64
	Text   string
}

// SendMessage records the message and returns a synthetic message ID.
// Returns Err if set (and does not record the message).
func (s *StubSender) SendMessage(_ context.Context, chatID int64, text string) (int, error) {
	if s.Err != nil {
		return 0, s.Err
	}
	s.Sent = append(s.Sent, SentMessage{ChatID: chatID, Text: text})
	return len(s.Sent) * 1000, nil // deterministic synthetic message IDs
}
