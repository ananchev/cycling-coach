package internal

import (
	"fmt"
	"os"
	"strconv"
)

// Config holds all runtime configuration loaded from environment variables.
type Config struct {
	HTTPAddr             string
	PublicURL            string
	AppBaseURL           string
	AppClientCert        string
	AppClientKey         string
	OAuthUser            string
	OAuthPassword        string
	OAuthSigningKey      string
	DefaultWindowDays    int
	MaxRows              int
	BlockContextMaxChars int
}

// LoadConfig reads configuration from environment variables and returns a validated Config.
// MCP_APP_BASE_URL is the only required variable; all others have defaults.
func LoadConfig() (*Config, error) {
	c := &Config{
		HTTPAddr:             getEnv("MCP_HTTP_ADDR", ":8091"),
		PublicURL:            getEnv("MCP_PUBLIC_URL", ""),
		AppBaseURL:           getEnv("MCP_APP_BASE_URL", ""),
		AppClientCert:        getEnv("MCP_APP_CLIENT_CERT", ""),
		AppClientKey:         getEnv("MCP_APP_CLIENT_KEY", ""),
		OAuthUser:            getEnv("MCP_OAUTH_USER", ""),
		OAuthPassword:        getEnv("MCP_OAUTH_PASSWORD", ""),
		OAuthSigningKey:      getEnv("MCP_OAUTH_SIGNING_KEY", ""),
		DefaultWindowDays:    getEnvInt("MCP_DEFAULT_WINDOW_DAYS", 28),
		MaxRows:              getEnvInt("MCP_MAX_ROWS", 200),
		BlockContextMaxChars: getEnvInt("MCP_BLOCK_CONTEXT_MAX_CHARS", 60_000),
	}
	if c.AppBaseURL == "" {
		return nil, fmt.Errorf("MCP_APP_BASE_URL is required")
	}
	return c, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}
