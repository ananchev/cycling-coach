package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"cycling-coach/internal/analysis"
	"cycling-coach/internal/config"
	"cycling-coach/internal/reporting"
	"cycling-coach/internal/scheduler"
	"cycling-coach/internal/storage"
	"cycling-coach/internal/telegram"
	"cycling-coach/internal/wahoo"
	"cycling-coach/internal/web"
	wyzepkg "cycling-coach/internal/wyze"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("config.Load", "err", err)
		os.Exit(1)
	}

	// Wire up the log broadcaster so the admin UI can tail logs via SSE.
	logBroadcaster := web.NewLogBroadcaster()
	slog.SetDefault(slog.New(web.NewTeeHandler(
		slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}),
		logBroadcaster,
	)))

	db, err := storage.Open(cfg.DatabasePath)
	if err != nil {
		slog.Error("storage.Open", "err", err)
		os.Exit(1)
	}
	defer db.Close()

	if err := bootstrapAthleteProfile(cfg); err != nil {
		slog.Warn("bootstrapAthleteProfile", "err", err)
	}

	if err := os.MkdirAll(cfg.FITFilesPath, 0755); err != nil {
		slog.Warn("create fit files dir", "path", cfg.FITFilesPath, "err", err)
	}

	authHandler := wahoo.NewAuthHandler(db.DB(), cfg)
	client := wahoo.NewClient(db.DB(), cfg)
	syncer := wahoo.NewSyncer(client, db.DB(), cfg.FITFilesPath)

	webhookHandler := wahoo.NewWebhookHandler(cfg.WahooWebhookSecret, syncer)

	if err := seedAthleteConfig(db.DB()); err != nil {
		slog.Warn("seedAthleteConfig", "err", err)
	}

	processor := analysis.NewProcessor(db.DB(), cfg.FITFilesPath)

	var wyzeImporter *wyzepkg.Importer
	if cfg.WyzeSidecarURL != "" {
		wyzeClient := wyzepkg.NewClient(cfg.WyzeSidecarURL)
		wyzeImporter = wyzepkg.NewImporter(db.DB(), wyzeClient)
		slog.Info("wyze: sidecar importer enabled", "url", cfg.WyzeSidecarURL)
	} else {
		slog.Info("wyze: disabled (WYZE_SIDECAR_URL not set)")
	}

	claudeProvider := reporting.NewClaudeProvider(cfg.AnthropicAPIKey, cfg.AnthropicModel)
	orch := reporting.NewOrchestrator(db.DB(), cfg.AthleteProfilePath, claudeProvider)

	// Telegram: optional — disabled when bot token or chat ID is absent.
	var deliverySvc *reporting.DeliveryService
	var tgBot *telegram.Bot
	if cfg.TelegramBotToken != "" && cfg.TelegramChatID != "" {
		chatID, err := strconv.ParseInt(cfg.TelegramChatID, 10, 64)
		if err != nil {
			slog.Warn("invalid TELEGRAM_CHAT_ID, telegram disabled", "err", err)
		} else {
			sender, err := telegram.NewBotSender(cfg.TelegramBotToken)
			if err != nil {
				slog.Warn("telegram: init failed, telegram disabled", "err", err)
			} else {
				deliverySvc = reporting.NewDeliveryService(db.DB(), sender, chatID, cfg.BaseURL)
				slog.Info("telegram: delivery enabled", "chat_id", chatID)
			}

			bot, err := telegram.NewBot(cfg.TelegramBotToken, chatID, db.DB(), cfg.AthleteProfilePath)
			if err != nil {
				slog.Warn("telegram: bot init failed", "err", err)
			} else {
				tgBot = bot
			}
		}
	} else {
		slog.Info("telegram: disabled (TELEGRAM_BOT_TOKEN or TELEGRAM_CHAT_ID not set)")
	}

	sched, err := scheduler.NewScheduler(cfg, syncer, processor, orch, deliverySvc, wyzeImporter)
	if err != nil {
		slog.Error("scheduler.New", "err", err)
		os.Exit(1)
	}
	sched.Start()
	defer sched.Stop()

	// Start the Telegram bot goroutine (long-polling for inbound messages).
	botCtx, botCancel := context.WithCancel(context.Background())
	defer botCancel()
	if tgBot != nil {
		go tgBot.Run(botCtx)
	}

	router := web.NewRouter(cfg, db.DB(), authHandler, syncer, webhookHandler, orch, deliverySvc, processor, wyzeImporter, logBroadcaster)

	srv := &http.Server{Addr: cfg.ServerAddr, Handler: router}

	go func() {
		slog.Info("server starting", "addr", cfg.ServerAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
	<-quit

	slog.Info("shutting down...")

	// Stop the Telegram bot polling.
	botCancel()

	// Close the SSE broadcaster first so log-stream connections exit immediately
	// instead of blocking the HTTP shutdown timeout.
	logBroadcaster.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("server shutdown", "err", err)
	}
}

// seedAthleteConfig populates athlete_config with defaults on first startup.
// Existing keys are never overwritten (ON CONFLICT DO NOTHING via SetConfig only when key absent).
func seedAthleteConfig(db *sql.DB) error {
	defaults := []struct{ key, value string }{
		{"ftp_watts", "251"},
		{"hr_max", "184"},
		{"weight_kg", "90"},
		{"hr_z1_max", "109"},
		{"hr_z2_max", "127"},
		{"hr_z3_max", "145"},
		{"hr_z4_max", "164"},
		{"pwr_z1_max", "138"},
		{"pwr_z2_max", "188"},
		{"pwr_z3_max", "226"},
		{"pwr_z4_max", "263"},
	}
	for _, d := range defaults {
		// Only insert if the key does not already exist.
		if _, err := db.Exec(
			`INSERT INTO athlete_config(key, value) VALUES(?, ?) ON CONFLICT(key) DO NOTHING`,
			d.key, d.value,
		); err != nil {
			return fmt.Errorf("seedAthleteConfig %s: %w", d.key, err)
		}
	}
	return nil
}

// bootstrapAthleteProfile copies the seed profile to the data volume on first run.
func bootstrapAthleteProfile(cfg *config.Config) error {
	if _, err := os.Stat(cfg.AthleteProfilePath); err == nil {
		return nil // already exists
	}

	if err := os.MkdirAll(filepath.Dir(cfg.AthleteProfilePath), 0755); err != nil {
		return fmt.Errorf("bootstrapAthleteProfile: mkdir: %w", err)
	}

	data, err := os.ReadFile("config/athlete-profile.default.md")
	if err != nil {
		return fmt.Errorf("bootstrapAthleteProfile: read seed: %w", err)
	}

	if err := os.WriteFile(cfg.AthleteProfilePath, data, 0644); err != nil {
		return fmt.Errorf("bootstrapAthleteProfile: write: %w", err)
	}

	slog.Info("athlete profile bootstrapped", "path", cfg.AthleteProfilePath)
	return nil
}
