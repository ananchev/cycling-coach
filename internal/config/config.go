package config

import "os"

// Config holds all runtime configuration loaded from environment variables.
type Config struct {
	// Wahoo Cloud API
	WahooClientID      string
	WahooClientSecret  string
	WahooRedirectURI   string
	WahooWebhookSecret string

	// Wyze sidecar
	WyzeSidecarURL string

	// Telegram
	TelegramBotToken string
	TelegramChatID   string

	// Anthropic
	AnthropicAPIKey string
	AnthropicModel  string

	// Server
	ServerAddr         string
	BaseURL            string
	DatabasePath       string
	FITFilesPath       string
	AthleteProfilePath string

	// Scheduler cron expressions (empty = disabled)
	CronSync          string // Wahoo sync, e.g. "0 */4 * * *"
	CronFITProcessing string // FIT file processing, e.g. "*/15 * * * *"
	CronWeeklyReport  string // Weekly report + plan + delivery, e.g. "0 20 * * 0"
	CronWyzeScaleSync string // Wyze scale sync via sidecar
}

// Load reads configuration from environment variables, applying defaults where defined.
func Load() (*Config, error) {
	return &Config{
		WahooClientID:      os.Getenv("WAHOO_CLIENT_ID"),
		WahooClientSecret:  os.Getenv("WAHOO_CLIENT_SECRET"),
		WahooRedirectURI:   os.Getenv("WAHOO_REDIRECT_URI"),
		WahooWebhookSecret: os.Getenv("WAHOO_WEBHOOK_SECRET"),
		WyzeSidecarURL:     os.Getenv("WYZE_SIDECAR_URL"),

		TelegramBotToken: os.Getenv("TELEGRAM_BOT_TOKEN"),
		TelegramChatID:   os.Getenv("TELEGRAM_CHAT_ID"),

		AnthropicAPIKey: os.Getenv("ANTHROPIC_API_KEY"),
		AnthropicModel:  envOrDefault("ANTHROPIC_MODEL", "claude-sonnet-4-20250514"),

		ServerAddr:         envOrDefault("SERVER_ADDR", ":8080"),
		BaseURL:            os.Getenv("BASE_URL"),
		DatabasePath:       envOrDefault("DATABASE_PATH", "/data/cycling.db"),
		FITFilesPath:       envOrDefault("FIT_FILES_PATH", "/data/fit_files/"),
		AthleteProfilePath: envOrDefault("ATHLETE_PROFILE_PATH", "/data/athlete-profile.md"),

		CronSync:          os.Getenv("CRON_SYNC"),
		CronFITProcessing: os.Getenv("CRON_FIT_PROCESSING"),
		CronWeeklyReport:  os.Getenv("CRON_WEEKLY_REPORT"),
		CronWyzeScaleSync: os.Getenv("CRON_WYZE_SCALE_SYNC"),
	}, nil
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
