package config

import "os"

// Config holds all runtime configuration loaded from environment variables.
type Config struct {
	// Wahoo Cloud API
	WahooClientID      string
	WahooClientSecret  string
	WahooRedirectURI   string
	WahooWebhookSecret string

	// Telegram
	TelegramBotToken string
	TelegramChatID   string

	// Anthropic
	AnthropicAPIKey string

	// Server
	ServerAddr         string
	BaseURL            string
	DatabasePath       string
	FITFilesPath       string
	AthleteProfilePath string
}

// Load reads configuration from environment variables, applying defaults where defined.
func Load() (*Config, error) {
	return &Config{
		WahooClientID:      os.Getenv("WAHOO_CLIENT_ID"),
		WahooClientSecret:  os.Getenv("WAHOO_CLIENT_SECRET"),
		WahooRedirectURI:   os.Getenv("WAHOO_REDIRECT_URI"),
		WahooWebhookSecret: os.Getenv("WAHOO_WEBHOOK_SECRET"),

		TelegramBotToken: os.Getenv("TELEGRAM_BOT_TOKEN"),
		TelegramChatID:   os.Getenv("TELEGRAM_CHAT_ID"),

		AnthropicAPIKey: os.Getenv("ANTHROPIC_API_KEY"),

		ServerAddr:         envOrDefault("SERVER_ADDR", ":8080"),
		BaseURL:            os.Getenv("BASE_URL"),
		DatabasePath:       envOrDefault("DATABASE_PATH", "/data/cycling.db"),
		FITFilesPath:       envOrDefault("FIT_FILES_PATH", "/data/fit_files/"),
		AthleteProfilePath: envOrDefault("ATHLETE_PROFILE_PATH", "/data/athlete-profile.md"),
	}, nil
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
