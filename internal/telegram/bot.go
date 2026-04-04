package telegram

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"cycling-coach/internal/storage"
)

// Bot handles inbound Telegram messages via long polling.
// It only responds to messages from the configured chat ID.
type Bot struct {
	api         *tgbotapi.BotAPI
	chatID      int64
	db          *sql.DB
	profilePath string
}

// NewBot creates a Bot that listens for updates from the given chat.
func NewBot(token string, chatID int64, db *sql.DB, profilePath string) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, fmt.Errorf("telegram.NewBot: %w", err)
	}
	slog.Info("telegram: bot authorized", "username", api.Self.UserName)
	return &Bot{
		api:         api,
		chatID:      chatID,
		db:          db,
		profilePath: profilePath,
	}, nil
}

// Run starts the long-polling loop. It blocks until ctx is cancelled.
func (b *Bot) Run(ctx context.Context) {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 30

	updates := b.api.GetUpdatesChan(u)

	slog.Info("telegram: bot listening for updates")

	for {
		select {
		case <-ctx.Done():
			b.api.StopReceivingUpdates()
			slog.Info("telegram: bot stopped")
			return
		case update := <-updates:
			if update.Message == nil {
				continue
			}
			if update.Message.Chat.ID != b.chatID {
				slog.Warn("telegram: ignoring message from unknown chat",
					"chat_id", update.Message.Chat.ID)
				continue
			}
			b.handleMessage(update.Message)
		}
	}
}

func (b *Bot) handleMessage(msg *tgbotapi.Message) {
	text := strings.TrimSpace(msg.Text)
	if text == "" && msg.Document == nil {
		return
	}

	// Handle commands.
	if msg.IsCommand() {
		switch msg.Command() {
		case "help", "start":
			b.cmdHelp(msg)
		case "note":
			b.cmdNote(msg)
		case "ride":
			b.cmdRide(msg)
		case "weight":
			b.cmdWeight(msg)
		case "bodyfat":
			b.cmdBodyFat(msg)
		case "muscle":
			b.cmdMuscleMass(msg)
		case "profile":
			b.cmdProfile(msg)
		default:
			b.reply(msg, "Unknown command. Send /help for available commands.")
		}
		return
	}

	// Free-text messages without a command are not processed.
	b.reply(msg, "I only respond to commands. Send /help to see what's available.")
}

func (b *Bot) cmdHelp(msg *tgbotapi.Message) {
	help := `Available commands:

/ride <text> — Post-ride note (RPE, how you felt, etc.)
/note <text> — General training note
/weight <kg> — Log body weight
/bodyfat <pct> — Log body fat percentage
/muscle <kg> — Log muscle mass
/profile — View current athlete profile
/profile set — Replace profile (attach .md file)

RPE tip: include "rpe 7" anywhere in your ride or note and it'll be parsed automatically.`
	b.reply(msg, help)
}

func (b *Bot) cmdNote(msg *tgbotapi.Message) {
	text := strings.TrimSpace(msg.CommandArguments())
	if text == "" {
		b.reply(msg, "Usage: /note <your note text>")
		return
	}
	b.saveNote(msg, storage.NoteTypeNote, text)
}

func (b *Bot) cmdRide(msg *tgbotapi.Message) {
	text := strings.TrimSpace(msg.CommandArguments())
	if text == "" {
		b.reply(msg, "Usage: /ride <post-ride note>\nExample: /ride rpe 7, legs felt heavy, slept 6h")
		return
	}
	b.saveNote(msg, storage.NoteTypeRide, text)
}

func (b *Bot) cmdWeight(msg *tgbotapi.Message) {
	text := strings.TrimSpace(msg.CommandArguments())
	if text == "" {
		b.reply(msg, "Usage: /weight <kg>\nExample: /weight 90.5")
		return
	}

	w, err := strconv.ParseFloat(text, 64)
	if err != nil || w < 30 || w > 200 {
		b.reply(msg, "Please provide a valid weight in kg (30-200).")
		return
	}

	note := &storage.AthleteNote{
		Timestamp: time.Now(),
		Type:      storage.NoteTypeWeight,
		WeightKG:  &w,
	}

	id, err := storage.InsertNote(b.db, note)
	if err != nil {
		slog.Error("telegram: save weight", "err", err)
		b.reply(msg, "Failed to save weight.")
		return
	}

	slog.Info("telegram: weight logged", "note_id", id, "weight_kg", w)
	b.reply(msg, fmt.Sprintf("Weight logged: %.1f kg", w))
}

func (b *Bot) cmdBodyFat(msg *tgbotapi.Message) {
	text := strings.TrimSpace(msg.CommandArguments())
	if text == "" {
		b.reply(msg, "Usage: /bodyfat <percentage>\nExample: /bodyfat 18.5")
		return
	}

	bf, err := strconv.ParseFloat(text, 64)
	if err != nil || bf < 3 || bf > 60 {
		b.reply(msg, "Please provide a valid body fat percentage (3-60).")
		return
	}

	note := &storage.AthleteNote{
		Timestamp:  time.Now(),
		Type:       storage.NoteTypeWeight,
		BodyFatPct: &bf,
	}

	id, err := storage.InsertNote(b.db, note)
	if err != nil {
		slog.Error("telegram: save body fat", "err", err)
		b.reply(msg, "Failed to save body fat.")
		return
	}

	slog.Info("telegram: body fat logged", "note_id", id, "body_fat_pct", bf)
	b.reply(msg, fmt.Sprintf("Body fat logged: %.1f%%", bf))
}

func (b *Bot) cmdMuscleMass(msg *tgbotapi.Message) {
	text := strings.TrimSpace(msg.CommandArguments())
	if text == "" {
		b.reply(msg, "Usage: /muscle <kg>\nExample: /muscle 38.2")
		return
	}

	mm, err := strconv.ParseFloat(text, 64)
	if err != nil || mm < 10 || mm > 100 {
		b.reply(msg, "Please provide a valid muscle mass in kg (10-100).")
		return
	}

	note := &storage.AthleteNote{
		Timestamp:    time.Now(),
		Type:         storage.NoteTypeWeight,
		MuscleMassKG: &mm,
	}

	id, err := storage.InsertNote(b.db, note)
	if err != nil {
		slog.Error("telegram: save muscle mass", "err", err)
		b.reply(msg, "Failed to save muscle mass.")
		return
	}

	slog.Info("telegram: muscle mass logged", "note_id", id, "muscle_mass_kg", mm)
	b.reply(msg, fmt.Sprintf("Muscle mass logged: %.1f kg", mm))
}

func (b *Bot) cmdProfile(msg *tgbotapi.Message) {
	args := strings.TrimSpace(msg.CommandArguments())

	if strings.ToLower(args) == "set" {
		// Expect an attached .md file.
		if msg.Document == nil {
			b.reply(msg, "Attach a .md file with this command.\nSend /profile set with the file as attachment.")
			return
		}
		b.handleProfileUpload(msg)
		return
	}

	// Send current profile as a document.
	data, err := os.ReadFile(b.profilePath)
	if err != nil {
		slog.Error("telegram: read profile", "err", err)
		b.reply(msg, "Failed to read athlete profile.")
		return
	}

	doc := tgbotapi.NewDocument(b.chatID, tgbotapi.FileBytes{
		Name:  "athlete-profile.md",
		Bytes: data,
	})
	doc.Caption = "Current athlete profile"
	if _, err := b.api.Send(doc); err != nil {
		slog.Error("telegram: send profile document", "err", err)
		b.reply(msg, "Failed to send profile.")
	}
}

func (b *Bot) handleProfileUpload(msg *tgbotapi.Message) {
	if msg.Document == nil {
		b.reply(msg, "No file attached. Send /profile set with a .md file.")
		return
	}

	if !strings.HasSuffix(msg.Document.FileName, ".md") {
		b.reply(msg, "Please attach a .md (markdown) file.")
		return
	}

	fileConfig, err := b.api.GetFile(tgbotapi.FileConfig{FileID: msg.Document.FileID})
	if err != nil {
		slog.Error("telegram: get file", "err", err)
		b.reply(msg, "Failed to get file from Telegram.")
		return
	}

	fileURL := fileConfig.Link(b.api.Token)
	req, err := http.NewRequest(http.MethodGet, fileURL, nil)
	if err != nil {
		slog.Error("telegram: build download request", "err", err)
		b.reply(msg, "Failed to download file.")
		return
	}
	resp, err := b.api.Client.Do(req)
	if err != nil {
		slog.Error("telegram: download file", "err", err)
		b.reply(msg, "Failed to download file.")
		return
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Error("telegram: read file body", "err", err)
		b.reply(msg, "Failed to read file.")
		return
	}

	if len(data) < 100 {
		b.reply(msg, "File seems too short to be a valid profile. Minimum 100 bytes.")
		return
	}

	// Backup existing profile.
	backup := b.profilePath + "." + time.Now().Format("20060102-150405") + ".bak"
	if existing, err := os.ReadFile(b.profilePath); err == nil {
		if err := os.WriteFile(backup, existing, 0644); err != nil {
			slog.Error("telegram: backup profile", "err", err)
		}
	}

	if err := os.WriteFile(b.profilePath, data, 0644); err != nil {
		slog.Error("telegram: write profile", "err", err)
		b.reply(msg, "Failed to save new profile.")
		return
	}

	slog.Info("telegram: profile updated", "backup", backup, "bytes", len(data))
	b.reply(msg, fmt.Sprintf("Profile updated (%d bytes). Old profile backed up.", len(data)))
}

// saveNote parses the text for RPE, links to the most recent workout, and persists.
func (b *Bot) saveNote(msg *tgbotapi.Message, noteType storage.NoteType, text string) {
	note := &storage.AthleteNote{
		Timestamp: time.Now(),
		Type:      noteType,
		Note:      &text,
	}

	// Try to extract RPE from the text (e.g. "rpe 7" or "RPE 8").
	if rpe := parseRPE(text); rpe > 0 {
		rpeVal := int64(rpe)
		note.RPE = &rpeVal
	}

	id, err := storage.InsertNote(b.db, note)
	if err != nil {
		slog.Error("telegram: save note", "err", err)
		b.reply(msg, "Failed to save note.")
		return
	}

	// Try to link to the most recent workout (within the last 12 hours).
	if noteType == storage.NoteTypeRide || noteType == storage.NoteTypeNote {
		b.linkToRecentWorkout(id)
	}

	confirmation := "Noted"
	if note.RPE != nil {
		confirmation += fmt.Sprintf(" (RPE %d)", *note.RPE)
	}
	confirmation += "."
	slog.Info("telegram: note saved", "note_id", id, "type", string(noteType))
	b.reply(msg, confirmation)
}

// linkToRecentWorkout links the note to the most recent workout started within the last 12 hours.
func (b *Bot) linkToRecentWorkout(noteID int64) {
	cutoff := time.Now().Add(-12 * time.Hour)
	row := b.db.QueryRow(`
		SELECT id FROM workouts
		WHERE started_at >= ?
		ORDER BY started_at DESC LIMIT 1`, cutoff)

	var workoutID int64
	if err := row.Scan(&workoutID); err != nil {
		return // no recent workout — that's fine
	}

	if err := storage.LinkNoteToWorkout(b.db, noteID, workoutID); err != nil {
		slog.Warn("telegram: link note to workout", "err", err)
	}
}

func (b *Bot) reply(msg *tgbotapi.Message, text string) {
	reply := tgbotapi.NewMessage(b.chatID, text)
	reply.ReplyToMessageID = msg.MessageID
	if _, err := b.api.Send(reply); err != nil {
		slog.Error("telegram: reply", "err", err)
	}
}

// parseRPE extracts an RPE value (1-10) from text like "rpe 7" or "RPE 8".
// Returns 0 if no valid RPE found.
func parseRPE(text string) int {
	lower := strings.ToLower(text)
	idx := strings.Index(lower, "rpe")
	if idx < 0 {
		return 0
	}

	// Skip "rpe" and any whitespace/punctuation.
	rest := strings.TrimSpace(lower[idx+3:])
	rest = strings.TrimLeft(rest, ":= ")
	if len(rest) == 0 {
		return 0
	}

	// Parse the number (1 or 2 digits).
	end := 0
	for end < len(rest) && end < 2 && rest[end] >= '0' && rest[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0
	}

	v, err := strconv.Atoi(rest[:end])
	if err != nil || v < 1 || v > 10 {
		return 0
	}
	return v
}
