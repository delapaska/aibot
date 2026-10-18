package config

import (
	"strings"
	"testing"
)

// clearEnv unsets every variable the config reads so tests are immune
// to the developer's shell environment.
func clearEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"HTTP_ADDR", "POSTGRES_DSN", "POSTGRES_HOST", "POSTGRES_PORT",
		"POSTGRES_USER", "POSTGRES_PASSWORD", "POSTGRES_DB", "POSTGRES_SSLMODE",
		"TELEGRAM_TOKEN", "LLM_BASE_URL", "LLM_API_KEY", "LLM_MODEL",
		"ALLOWED_TELEGRAM_IDS", "PANEL_TOKEN", "APP_TZ", "LOG_LEVEL",
	} {
		t.Setenv(key, "")
	}
}

func TestLoadDefaults(t *testing.T) {
	clearEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}

	if cfg.HTTPAddr != ":8080" {
		t.Errorf("HTTPAddr = %q, want %q", cfg.HTTPAddr, ":8080")
	}
	// Password defaults to empty, so the userinfo part is "aibot:".
	wantDSN := "postgres://aibot:@127.0.0.1:5432/aibot?sslmode=disable"
	if cfg.PostgresDSN != wantDSN {
		t.Errorf("PostgresDSN = %q, want %q", cfg.PostgresDSN, wantDSN)
	}
	if cfg.LLMBaseURL != "https://api.openai.com/v1" {
		t.Errorf("LLMBaseURL = %q, want default", cfg.LLMBaseURL)
	}
	if cfg.LLMModel != "gpt-4o-mini" {
		t.Errorf("LLMModel = %q, want default", cfg.LLMModel)
	}
	if cfg.Location.String() != "Europe/Moscow" {
		t.Errorf("Location = %q, want Europe/Moscow", cfg.Location.String())
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want info", cfg.LogLevel)
	}
	if cfg.TelegramToken != "" {
		t.Errorf("TelegramToken = %q, want empty", cfg.TelegramToken)
	}
}

func TestLoadFromEnv(t *testing.T) {
	clearEnv(t)
	t.Setenv("HTTP_ADDR", ":9090")
	t.Setenv("POSTGRES_HOST", "db.internal")
	t.Setenv("POSTGRES_PASSWORD", "s3cret/паss")
	t.Setenv("ALLOWED_TELEGRAM_IDS", " 111, 222 ,333")
	t.Setenv("APP_TZ", "Asia/Almaty")
	t.Setenv("LOG_LEVEL", "debug")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}

	if cfg.HTTPAddr != ":9090" {
		t.Errorf("HTTPAddr = %q, want :9090", cfg.HTTPAddr)
	}
	// Special characters in the password must be percent-encoded in the DSN.
	if !strings.Contains(cfg.PostgresDSN, "s3cret%2F%D0%BF%D0%B0ss") {
		t.Errorf("PostgresDSN does not escape password: %q", cfg.PostgresDSN)
	}
	if !strings.Contains(cfg.PostgresDSN, "@db.internal:5432/") {
		t.Errorf("PostgresDSN host missing: %q", cfg.PostgresDSN)
	}
	if len(cfg.AllowedTelegramIDs) != 3 ||
		cfg.AllowedTelegramIDs[0] != 111 ||
		cfg.AllowedTelegramIDs[1] != 222 ||
		cfg.AllowedTelegramIDs[2] != 333 {
		t.Errorf("AllowedTelegramIDs = %v, want [111 222 333]", cfg.AllowedTelegramIDs)
	}
	if cfg.Location.String() != "Asia/Almaty" {
		t.Errorf("Location = %q, want Asia/Almaty", cfg.Location.String())
	}
}

func TestLoadValidationErrors(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
	}{
		{"bad log level", map[string]string{"LOG_LEVEL": "verbose"}},
		{"bad llm url", map[string]string{"LLM_BASE_URL": "not a url"}},
		{"llm url without host", map[string]string{"LLM_BASE_URL": "http://"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearEnv(t)
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			if _, err := Load(); err == nil {
				t.Fatalf("Load() expected error for %s", tt.name)
			}
		})
	}
}

func TestParseIDsInvalid(t *testing.T) {
	if _, err := parseIDs("12,abc"); err == nil {
		t.Fatal("parseIDs(12,abc) expected error")
	}
	if _, err := parseIDs(""); err != nil {
		t.Fatalf("parseIDs(empty) unexpected error: %v", err)
	}
}
