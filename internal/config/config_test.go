package config

import (
	"os"
	"testing"
)

func TestLoad(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want Config
	}{
		{
			name: "defaults when no env vars set",
			env:  map[string]string{},
			want: Config{
				ServerAddr:         ":8080",
				DatabasePath:       "/data/cycling.db",
				FITFilesPath:       "/data/fit_files/",
				AthleteProfilePath: "/data/athlete-profile.md",
			},
		},
		{
			name: "env vars override defaults",
			env: map[string]string{
				"SERVER_ADDR":          ":9090",
				"DATABASE_PATH":        "/tmp/test.db",
				"FIT_FILES_PATH":       "/tmp/fit/",
				"ATHLETE_PROFILE_PATH": "/tmp/profile.md",
				"WAHOO_CLIENT_ID":      "wid",
				"ANTHROPIC_API_KEY":    "sk-test",
				"TELEGRAM_CHAT_ID":     "12345",
			},
			want: Config{
				ServerAddr:         ":9090",
				DatabasePath:       "/tmp/test.db",
				FITFilesPath:       "/tmp/fit/",
				AthleteProfilePath: "/tmp/profile.md",
				WahooClientID:      "wid",
				AnthropicAPIKey:    "sk-test",
				TelegramChatID:     "12345",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// isolate env state
			restore := setEnv(tc.env)
			defer restore()

			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load() error: %v", err)
			}

			if cfg.ServerAddr != tc.want.ServerAddr {
				t.Errorf("ServerAddr = %q, want %q", cfg.ServerAddr, tc.want.ServerAddr)
			}
			if cfg.DatabasePath != tc.want.DatabasePath {
				t.Errorf("DatabasePath = %q, want %q", cfg.DatabasePath, tc.want.DatabasePath)
			}
			if cfg.FITFilesPath != tc.want.FITFilesPath {
				t.Errorf("FITFilesPath = %q, want %q", cfg.FITFilesPath, tc.want.FITFilesPath)
			}
			if cfg.AthleteProfilePath != tc.want.AthleteProfilePath {
				t.Errorf("AthleteProfilePath = %q, want %q", cfg.AthleteProfilePath, tc.want.AthleteProfilePath)
			}
			if cfg.WahooClientID != tc.want.WahooClientID {
				t.Errorf("WahooClientID = %q, want %q", cfg.WahooClientID, tc.want.WahooClientID)
			}
			if cfg.AnthropicAPIKey != tc.want.AnthropicAPIKey {
				t.Errorf("AnthropicAPIKey = %q, want %q", cfg.AnthropicAPIKey, tc.want.AnthropicAPIKey)
			}
			if cfg.TelegramChatID != tc.want.TelegramChatID {
				t.Errorf("TelegramChatID = %q, want %q", cfg.TelegramChatID, tc.want.TelegramChatID)
			}
		})
	}
}

// setEnv sets the given env vars, clears all known config keys not in the map,
// and returns a function that restores the original state.
func setEnv(env map[string]string) func() {
	keys := []string{
		"WAHOO_CLIENT_ID", "WAHOO_CLIENT_SECRET", "WAHOO_REDIRECT_URI", "WAHOO_WEBHOOK_SECRET",
		"TELEGRAM_BOT_TOKEN", "TELEGRAM_CHAT_ID",
		"ANTHROPIC_API_KEY",
		"SERVER_ADDR", "BASE_URL", "DATABASE_PATH", "FIT_FILES_PATH", "ATHLETE_PROFILE_PATH",
	}

	saved := make(map[string]string, len(keys))
	for _, k := range keys {
		saved[k] = os.Getenv(k)
		os.Unsetenv(k)
	}
	for k, v := range env {
		os.Setenv(k, v)
	}

	return func() {
		for _, k := range keys {
			if v, ok := saved[k]; ok && v != "" {
				os.Setenv(k, v)
			} else {
				os.Unsetenv(k)
			}
		}
	}
}
