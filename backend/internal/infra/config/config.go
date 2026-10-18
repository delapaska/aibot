// Package config loads application settings from environment variables.
//
// Configuration follows the 12-factor style: every value comes from the
// process environment (docker compose injects them from .env), so secrets
// never live in the repository.
package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config carries every runtime setting of the application.
type Config struct {
	HTTPAddr    string
	PostgresDSN string

	// TelegramToken is empty when the bot is disabled (panel-only mode).
	TelegramToken string

	// LLM settings for an OpenAI-compatible chat completions API.
	LLMBaseURL string // e.g. https://api.openai.com/v1
	LLMAPIKey  string
	LLMModel   string

	// AllowedTelegramIDs restricts who may talk to the bot; empty means everyone.
	AllowedTelegramIDs []int64

	// PanelToken enables an optional bearer token on the panel API.
	// Empty disables it: the task explicitly requires no authorization.
	PanelToken string

	// Location is the family time zone; relative dates like "yesterday"
	// are resolved against it.
	Location *time.Location

	LogLevel string // debug|info|warn|error
}

// Load reads configuration from the environment and validates it. A
// .env file (repo root or backend/) is loaded first so `make run` works
// outside containers; real environment variables still win.
func Load() (Config, error) {
	for _, path := range []string{"../.env", ".env"} {
		if err := loadDotenv(path); err != nil {
			return Config{}, err
		}
	}

	cfg := Config{
		HTTPAddr:      env("HTTP_ADDR", ":8080"),
		TelegramToken: env("TELEGRAM_TOKEN", ""),
		LLMBaseURL:    strings.TrimRight(env("LLM_BASE_URL", "https://api.openai.com/v1"), "/"),
		LLMAPIKey:     env("LLM_API_KEY", ""),
		LLMModel:      env("LLM_MODEL", "gpt-4o-mini"),
		PanelToken:    env("PANEL_TOKEN", ""),
		LogLevel:      env("LOG_LEVEL", "info"),
	}

	cfg.PostgresDSN = dsnFromEnv()

	ids, err := parseIDs(env("ALLOWED_TELEGRAM_IDS", ""))
	if err != nil {
		return Config{}, fmt.Errorf("config: ALLOWED_TELEGRAM_IDS: %w", err)
	}
	cfg.AllowedTelegramIDs = ids

	loc, err := time.LoadLocation(env("APP_TZ", "Europe/Moscow"))
	if err != nil {
		return Config{}, fmt.Errorf("config: APP_TZ: %w", err)
	}
	cfg.Location = loc

	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) validate() error {
	// Emptiness checks are not needed here: env() substitutes defaults for
	// unset variables, so only values that can genuinely be invalid are
	// validated below.
	if u, err := url.Parse(c.LLMBaseURL); err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("config: LLM_BASE_URL must be a valid URL, got %q", c.LLMBaseURL)
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("config: LOG_LEVEL must be one of debug|info|warn|error, got %q", c.LogLevel)
	}
	return nil
}

// dsnFromEnv builds a pgx connection string either from POSTGRES_DSN
// or from individual POSTGRES_* variables.
func dsnFromEnv() string {
	if dsn := strings.TrimSpace(env("POSTGRES_DSN", "")); dsn != "" {
		return dsn
	}
	user := env("POSTGRES_USER", "aibot")
	pass := env("POSTGRES_PASSWORD", "")
	host := env("POSTGRES_HOST", "127.0.0.1")
	port := env("POSTGRES_PORT", "5432")
	db := env("POSTGRES_DB", "aibot")
	sslmode := env("POSTGRES_SSLMODE", "disable")

	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(user, pass),
		Host:   net.JoinHostPort(host, port),
		Path:   db,
	}
	q := u.Query()
	q.Set("sslmode", sslmode)
	u.RawQuery = q.Encode()
	return u.String()
}

// parseIDs parses a comma-separated list of Telegram user IDs.
func parseIDs(raw string) ([]int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	ids := make([]int64, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		id, err := strconv.ParseInt(p, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid id %q: %w", p, err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// env returns the environment variable value with surrounding spaces trimmed,
// falling back to def when the variable is unset or empty.
func env(key, def string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	return v
}
