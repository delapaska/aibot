// Command aibot is the entry point of the expense tracking service.
//
// It wires the application together: loads configuration, opens the
// database pool, builds use cases on top of repositories and starts the
// HTTP server (web panel + REST API) and the Telegram bot.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/delapaska/aibot/internal/app/usecase"
	"github.com/delapaska/aibot/internal/infra/clocksys"
	"github.com/delapaska/aibot/internal/infra/config"
	"github.com/delapaska/aibot/internal/infra/httpapi"
	"github.com/delapaska/aibot/internal/infra/llm"
	"github.com/delapaska/aibot/internal/infra/postgres"
	"github.com/delapaska/aibot/internal/infra/telegram"
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

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Storage: fail fast on an unreachable database — the service is
	// useless without it.
	pool, err := postgres.NewPool(ctx, cfg.PostgresDSN)
	if err != nil {
		return fmt.Errorf("open postgres: %w", err)
	}
	defer pool.Close()
	if err := postgres.RunMigrations(ctx, pool); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	// Application layer: use cases over ports.
	expenseRepo := postgres.NewExpenseRepo(pool)
	userRepo := postgres.NewUserRepo(pool)
	debtRepo := postgres.NewDebtRepo(pool)
	budgetRepo := postgres.NewBudgetRepo(pool)
	clock := clocksys.New(cfg.Location)
	expenses := usecase.NewExpenseService(expenseRepo, userRepo, clock)
	users := usecase.NewUserService(userRepo, clock)
	debts := usecase.NewDebtService(debtRepo, userRepo, clock)
	budgets := usecase.NewBudgetService(budgetRepo, expenses, clock)

	// LLM agent over the expense, debt and budget services.
	llmClient := llm.New(cfg.LLMBaseURL, cfg.LLMAPIKey, cfg.LLMModel, logger).
		WithFallbacks(cfg.LLMFallbackModels)
	agent := usecase.NewAgent(llmClient, expenses, debts, budgets, clock)

	// HTTP API of the panel.
	api := httpapi.NewServer(expenses, users, debts, logger, cfg.PanelToken)
	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           api.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("http server listening", "addr", cfg.HTTPAddr)
		errCh <- srv.ListenAndServe()
	}()

	// Telegram bot: a long-polling loop next to the HTTP server.
	if cfg.TelegramToken != "" {
		if len(cfg.AllowedTelegramIDs) == 0 {
			logger.Warn("ALLOWED_TELEGRAM_IDS is empty: the bot answers to any private chat; set it in production")
		}
		botSvc := usecase.NewBotService(users, agent, clock, cfg.AllowedTelegramIDs, logger)
		poller := telegram.NewPoller(telegram.New(cfg.TelegramToken, logger), botSvc, logger)
		go func() {
			if err := poller.Run(ctx); err != nil {
				errCh <- fmt.Errorf("telegram bot: %w", err)
			}
		}()
		logger.Info("telegram bot started", "allowed_users", len(cfg.AllowedTelegramIDs))
	} else {
		logger.Info("telegram bot disabled: TELEGRAM_TOKEN is not set")
	}

	// The web panel serves the REST API; the bot loop runs above.
	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("http server: %w", err)
		}
	case <-ctx.Done():
	}

	// Graceful shutdown: stop accepting, finish in-flight requests.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("http shutdown", "error", err)
	}

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
