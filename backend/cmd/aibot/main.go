// Command aibot is the entry point of the expense tracking service.
//
// It wires the application together: loads configuration, opens the
// database pool, builds use cases on top of repositories and starts the
// HTTP server (web panel + REST API) and the Telegram bot.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/delapaska/aibot/internal/infra/config"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger := newLogger(cfg.LogLevel)
	slog.SetDefault(logger)
	logger.Info("starting aibot", "version", version)
	// Log the configuration without secrets: tokens, keys and passwords
	// are deliberately never printed.
	logger.Info("configuration loaded",
		"http_addr", cfg.HTTPAddr,
		"telegram_enabled", cfg.TelegramToken != "",
		"llm_model", cfg.LLMModel,
		"llm_base_url", cfg.LLMBaseURL,
		"llm_key_set", cfg.LLMAPIKey != "",
		"allowed_users", len(cfg.AllowedTelegramIDs),
		"panel_auth", cfg.PanelToken != "",
		"tz", cfg.Location.String(),
	)

	// Scaffold stage: nothing to serve yet. Later stages start the HTTP
	// server and the Telegram bot here; for now just wait for shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	logger.Info("shutdown complete")
	return nil
}

func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: lvl}))
}
