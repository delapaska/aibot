package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDotenv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := "# comment\n" +
		"LLM_API_KEY=sk-test\n" +
		"TELEGRAM_TOKEN=\"tok 123\"\n" + // quotes are stripped
		"ALLOWED_TELEGRAM_IDS=1, 2\n" + // spaces around values are trimmed
		"EMPTY=\n" + // empty value still counts as unset-default
		"  SPACED = spaced  \n" // stray spaces around the key and value
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("LLM_API_KEY", "from-env-wins")
	// Existing env vars win over the file: real environment takes priority.

	if err := loadDotenv(path); err != nil {
		t.Fatalf("loadDotenv: %v", err)
	}

	if got := os.Getenv("TELEGRAM_TOKEN"); got != "tok 123" {
		t.Errorf("TELEGRAM_TOKEN = %q", got)
	}
	if got := os.Getenv("ALLOWED_TELEGRAM_IDS"); got != "1, 2" {
		t.Errorf("ALLOWED_TELEGRAM_IDS = %q", got)
	}
	if got := os.Getenv("SPACED"); got != "spaced" {
		t.Errorf("SPACED = %q", got)
	}
	if got := os.Getenv("EMPTY"); got != "" {
		t.Errorf("EMPTY = %q, want empty (not the default)", got)
	}
	if got := os.Getenv("LLM_API_KEY"); got != "from-env-wins" {
		t.Errorf("LLM_API_KEY = %q, want existing env to win", got)
	}
}

func TestLoadDotenvMissingFile(t *testing.T) {
	// A missing .env is fine: env-only mode (containers, CI).
	if err := loadDotenv(t.TempDir() + "/nope.env"); err != nil {
		t.Fatalf("missing file must be ignored, got %v", err)
	}
}
