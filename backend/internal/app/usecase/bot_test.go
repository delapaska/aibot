package usecase

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/delapaska/aibot/internal/app/port"
	"github.com/delapaska/aibot/internal/app/port/fakes"
	"github.com/delapaska/aibot/internal/domain"
)

// newBot builds a BotService on fake ports; returns the fakes for
// assertions.
func newBot(t *testing.T, allowed []int64) (*BotService, *fakes.Users, *fakes.Expenses, *fakes.LLM, *fakes.Clock) {
	t.Helper()
	users := fakes.NewUsers()
	expenses := fakes.NewExpenses()
	clock := &fakes.Clock{T: now}
	llm := &fakes.LLM{}
	svc := NewExpenseService(expenses, users, clock)
	agent := NewAgent(llm, svc, NewDebtService(fakes.NewDebts(), users, clock), NewBudgetService(fakes.NewBudgets(), svc, clock), clock)
	bot := NewBotService(NewUserService(users, clock), agent, clock, allowed, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return bot, users, expenses, llm, clock
}

// tgUpdate builds a message update: the chat id mirrors the sender id,
// as Telegram does in private chats.
func tgUpdate(id, fromID int64, chatType, text string) port.TelegramUpdate {
	return port.TelegramUpdate{
		UpdateID: id,
		Message: &port.TelegramMessage{
			MessageID: id,
			From:      port.TelegramUser{ID: fromID, FirstName: "Аня", Username: "anya"},
			Chat:      port.TelegramChat{ID: fromID, Type: chatType},
			Text:      text,
		},
	}
}

func TestBotAnswersPrivateMessage(t *testing.T) {
	bot, users, expenses, llm, _ := newBot(t, nil)
	llm.Queue = []port.ChatResponse{
		{ToolCalls: []port.ToolCall{{ID: "t1", Name: "add_expense", ArgsJSON: `{"amount_rubles":350,"category":"food","spent_at":"2026-10-18"}`}}},
	}

	replies := bot.HandleUpdate(ctx, tgUpdate(1, 42, "private", "кофе 350"))

	if len(replies) != 1 || replies[0] != "Записал: Еда — 350 ₽, 18.10.2026." {
		t.Fatalf("replies = %v", replies)
	}
	if users.Len() != 1 {
		t.Fatalf("users.Len() = %d, want 1", users.Len())
	}
	if u, ok := users.Get(42); !ok || u.FirstName != "Аня" || u.Username != "anya" {
		t.Fatalf("stored user = %+v ok=%v", u, ok)
	}
	// The agent ran for the Telegram sender: the created expense is owned
	// by them, no matter what the model would have preferred.
	if expenses.Len() != 1 {
		t.Fatalf("expenses.Len() = %d, want 1", expenses.Len())
	}
	e, ok := expenses.Peek("exp-1")
	if !ok || e.UserID != 42 || e.Category != domain.CategoryFood {
		t.Fatalf("stored expense = %+v ok=%v", e, ok)
	}
}

func TestBotIgnoresNonPrivateChats(t *testing.T) {
	bot, users, _, llm, _ := newBot(t, nil)

	replies := bot.HandleUpdate(ctx, tgUpdate(1, 42, "group", "кофе 350"))

	if replies != nil {
		t.Fatalf("replies = %v, want none", replies)
	}
	if users.Len() != 0 || len(llm.Requests) != 0 {
		t.Fatalf("side effects: users=%d llm=%d", users.Len(), len(llm.Requests))
	}
}

func TestBotIgnoresNonMessageUpdates(t *testing.T) {
	bot, users, _, llm, _ := newBot(t, nil)

	replies := bot.HandleUpdate(ctx, port.TelegramUpdate{UpdateID: 1})

	if replies != nil {
		t.Fatalf("replies = %v, want none", replies)
	}
	if users.Len() != 0 || len(llm.Requests) != 0 {
		t.Fatalf("side effects: users=%d llm=%d", users.Len(), len(llm.Requests))
	}
}

func TestBotDeniesNonAllowedUser(t *testing.T) {
	bot, users, _, llm, _ := newBot(t, []int64{1})

	replies := bot.HandleUpdate(ctx, tgUpdate(1, 42, "private", "кофе 350"))

	if replies != nil {
		t.Fatalf("replies = %v, want silence (no existence leak)", replies)
	}
	if users.Len() != 0 || len(llm.Requests) != 0 {
		t.Fatalf("side effects: users=%d llm=%d", users.Len(), len(llm.Requests))
	}
}

func TestBotOpenWhenAllowListEmpty(t *testing.T) {
	// Covered by TestBotAnswersPrivateMessage running with a nil
	// allow-list; here a non-empty list admits a listed member.
	bot, users, _, _, _ := newBot(t, []int64{42})

	replies := bot.HandleUpdate(ctx, tgUpdate(1, 42, "private", "/start"))

	if len(replies) != 1 || replies[0] != botHelp {
		t.Fatalf("replies = %v", replies)
	}
	if users.Len() != 1 {
		t.Fatalf("users.Len() = %d, want 1", users.Len())
	}
}

func TestBotRateLimit(t *testing.T) {
	bot, _, _, llm, clock := newBot(t, nil)

	var last []string
	for i := 0; i < botRatePerMinute; i++ {
		last = bot.HandleUpdate(ctx, tgUpdate(int64(i+1), 42, "private", "привет"))
	}
	// Every message so far reached the agent (which fails on the empty
	// scripted queue, producing the internal-error reply).
	if len(llm.Requests) != botRatePerMinute {
		t.Fatalf("llm requests = %d, want %d", len(llm.Requests), botRatePerMinute)
	}
	if last[0] != msgBotInternal {
		t.Fatalf("reply = %q", last[0])
	}

	// The next message within the window is rejected without reaching the
	// agent, with a friendly rate-limit reply.
	replies := bot.HandleUpdate(ctx, tgUpdate(99, 42, "private", "привет"))
	if len(replies) != 1 || replies[0] != msgBotRateLimit {
		t.Fatalf("replies = %v, want rate limit", replies)
	}
	if len(llm.Requests) != botRatePerMinute {
		t.Fatalf("llm requests = %d, agent must not run", len(llm.Requests))
	}

	// A new window lets the user talk again.
	clock.T = clock.T.Add(time.Minute + time.Second)
	replies = bot.HandleUpdate(ctx, tgUpdate(100, 42, "private", "привет"))
	if len(replies) != 1 || replies[0] != msgBotInternal {
		t.Fatalf("after window: replies = %v", replies)
	}
}

func TestBotRejectsLongMessage(t *testing.T) {
	bot, _, _, llm, _ := newBot(t, nil)

	replies := bot.HandleUpdate(ctx, tgUpdate(1, 42, "private", strings.Repeat("а", maxBotInputLen+1)))

	if len(replies) != 1 || replies[0] != msgBotTooLong {
		t.Fatalf("replies = %v", replies)
	}
	if len(llm.Requests) != 0 {
		t.Fatalf("llm requests = %d, want 0", len(llm.Requests))
	}
}

func TestBotAcceptsMessageAtLimit(t *testing.T) {
	bot, _, _, llm, _ := newBot(t, nil)

	bot.HandleUpdate(ctx, tgUpdate(1, 42, "private", strings.Repeat("а", maxBotInputLen)))

	if len(llm.Requests) != 1 {
		t.Fatalf("llm requests = %d, want 1", len(llm.Requests))
	}
}

func TestBotNonTextMessage(t *testing.T) {
	bot, _, _, llm, _ := newBot(t, nil)

	replies := bot.HandleUpdate(ctx, tgUpdate(1, 42, "private", "   "))

	if len(replies) != 1 || replies[0] != msgBotNotText {
		t.Fatalf("replies = %v", replies)
	}
	if len(llm.Requests) != 0 {
		t.Fatalf("llm requests = %d, want 0", len(llm.Requests))
	}
}

func TestBotCommandGetsGreeting(t *testing.T) {
	bot, users, _, llm, _ := newBot(t, nil)

	replies := bot.HandleUpdate(ctx, tgUpdate(1, 42, "private", "/start"))

	if len(replies) != 1 || replies[0] != botHelp {
		t.Fatalf("replies = %v", replies)
	}
	if len(llm.Requests) != 0 {
		t.Fatalf("llm requests = %d, want 0", len(llm.Requests))
	}
	if users.Len() != 1 {
		t.Fatalf("users.Len() = %d, want 1 (member registered even by /start)", users.Len())
	}
}

func TestBotHelpCommand(t *testing.T) {
	bot, _, _, llm, _ := newBot(t, nil)

	replies := bot.HandleUpdate(ctx, tgUpdate(1, 42, "private", "/help"))

	if len(replies) != 1 || replies[0] != botHelp {
		t.Fatalf("replies = %v", replies)
	}
	if len(llm.Requests) != 0 {
		t.Fatalf("llm requests = %d, want 0", len(llm.Requests))
	}
}

func TestBotUnknownCommand(t *testing.T) {
	bot, _, _, llm, _ := newBot(t, nil)

	replies := bot.HandleUpdate(ctx, tgUpdate(1, 42, "private", "/party"))

	if len(replies) != 1 || replies[0] != msgBotUnknownCommand {
		t.Fatalf("replies = %v", replies)
	}
	if len(llm.Requests) != 0 {
		t.Fatalf("llm requests = %d, want 0", len(llm.Requests))
	}
}

func TestBotAgentError(t *testing.T) {
	bot, _, _, llm, _ := newBot(t, nil)
	llm.Err = errors.New("llm down")

	replies := bot.HandleUpdate(ctx, tgUpdate(1, 42, "private", "кофе 350"))

	if len(replies) != 1 || replies[0] != msgBotInternal {
		t.Fatalf("replies = %v", replies)
	}
}

func TestBotAgentLimit(t *testing.T) {
	bot, _, _, llm, _ := newBot(t, nil)
	llm.Err = ErrAgentLimit

	replies := bot.HandleUpdate(ctx, tgUpdate(1, 42, "private", "кофе 350"))

	if len(replies) != 1 || replies[0] != msgBotTooComplex {
		t.Fatalf("replies = %v", replies)
	}
}

func TestBotLongReplyIsChunked(t *testing.T) {
	bot, _, _, llm, _ := newBot(t, nil)
	llm.Queue = []port.ChatResponse{{Content: strings.Repeat("ы", 9000)}}

	replies := bot.HandleUpdate(ctx, tgUpdate(1, 42, "private", "покажи всё"))

	total := 0
	for _, r := range replies {
		if utf8.RuneCountInString(r) > maxBotReplyLen {
			t.Fatalf("reply chunk of %d runes exceeds %d", utf8.RuneCountInString(r), maxBotReplyLen)
		}
		total += utf8.RuneCountInString(r)
	}
	if total != 9000 {
		t.Fatalf("chunked total = %d, want 9000", total)
	}
	if len(replies) < 2 {
		t.Fatalf("replies = %d chunks, want >1", len(replies))
	}
}

// The model sometimes returns an empty final answer (weak/free tiers do
// this after tool rounds). Silence would look like the bot ignored the
// message, so the user gets an apology instead.
func TestBotEmptyReplyApologizes(t *testing.T) {
	bot, _, _, llm, _ := newBot(t, nil)
	llm.Queue = []port.ChatResponse{{Content: "   "}}

	replies := bot.HandleUpdate(ctx, tgUpdate(1, 42, "private", "привет"))

	if len(replies) != 1 || replies[0] != msgBotInternal {
		t.Fatalf("replies = %v, want the internal-error apology", replies)
	}
}

// The agent outliving its deadline must not stay silent either: the user
// waited for the answer and deserves to know it did not arrive.
func TestBotTimeoutReply(t *testing.T) {
	bot, _, _, llm, _ := newBot(t, nil)
	llm.Hang = true

	hctx, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	replies := bot.HandleUpdate(hctx, tgUpdate(1, 42, "private", "покажи траты за неделю"))

	if len(replies) != 1 || replies[0] != msgBotTimeout {
		t.Fatalf("replies = %v, want the timeout apology", replies)
	}
}

// A cancelled context is a shutdown, not a timeout: stay silent.
func TestBotShutdownIsSilent(t *testing.T) {
	bot, _, _, llm, _ := newBot(t, nil)
	llm.Hang = true

	hctx, cancel := context.WithCancel(ctx)
	cancel()
	replies := bot.HandleUpdate(hctx, tgUpdate(1, 42, "private", "покажи траты"))

	if replies != nil {
		t.Fatalf("replies = %v, want none on shutdown", replies)
	}
}
