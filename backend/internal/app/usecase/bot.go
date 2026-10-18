// Bot part of the application layer: applies the access policy to
// Telegram updates and forwards messages to the agent. Everything
// user-facing is decided here — who may talk, rate limits, reply texts —
// so the Telegram adapter stays a thin delivery pipe.
package usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/delapaska/aibot/internal/app/port"
	"github.com/delapaska/aibot/internal/domain"
)

const (
	// maxBotInputLen caps the incoming text in runes; Telegram itself
	// caps messages at 4096 chars, this leaves a margin.
	maxBotInputLen = 4000
	// maxBotReplyLen caps each outgoing message; long agent answers are
	// split into several Telegram messages.
	maxBotReplyLen = 4000
	// botRatePerMinute is the per-user message budget per fixed window.
	botRatePerMinute = 20
)

// User-facing replies, in Russian per the project conventions.
const (
	msgBotRateLimit  = "Слишком много сообщений, подожди минуту."
	msgBotTooLong    = "Сообщение слишком длинное, раздели его на части."
	msgBotNotText    = "Я понимаю только текстовые сообщения."
	msgBotTooComplex = "Слишком сложный запрос, попробуй разбить его на шаги."
	msgBotInternal   = "Не получилось обработать запрос, попробуй ещё раз."
)

const botGreeting = "Привет! Я помогаю вести семейные расходы. Пиши траты текстом — например «кофе 350» или «вчера такси 520» — и я запишу. Можно спросить: «покажи расходы за неделю», «сколько потратил на еду в этом месяце»."

// BotService turns Telegram updates into agent conversations.
type BotService struct {
	users   *UserService
	agent   *Agent
	clock   port.Clock
	allowed map[int64]struct{} // empty set = open to all private chats
	limiter *botLimiter
	logger  *slog.Logger
}

func NewBotService(users *UserService, agent *Agent, clock port.Clock, allowed []int64, logger *slog.Logger) *BotService {
	set := make(map[int64]struct{}, len(allowed))
	for _, id := range allowed {
		set[id] = struct{}{}
	}
	return &BotService{
		users:   users,
		agent:   agent,
		clock:   clock,
		allowed: set,
		limiter: newBotLimiter(botRatePerMinute),
		logger:  logger,
	}
}

// HandleUpdate inspects one update and returns the reply texts to deliver
// (nil = stay silent). Errors never escape: they are logged and converted
// to a friendly reply, so the adapter has no policy of its own.
func (s *BotService) HandleUpdate(ctx context.Context, u port.TelegramUpdate) []string {
	if u.Message == nil {
		return nil // edits, callback queries and other events we ignore
	}
	m := u.Message
	if m.Chat.Type != "private" {
		s.logger.Debug("ignored non-private update", "chat_type", m.Chat.Type, "update_id", u.UpdateID)
		return nil
	}
	if !s.allowedUser(m.From.ID) {
		// Silent, like ErrNotFound: an outsider learns nothing, not even
		// that the bot exists.
		s.logger.Warn("ignored update from non-allowed user", "telegram_id", m.From.ID)
		return nil
	}
	if !s.limiter.allow(m.From.ID, s.clock.Now()) {
		return []string{msgBotRateLimit}
	}

	user, err := s.registerUser(ctx, m.From)
	if err != nil {
		s.logger.Error("register user", "error", err, "telegram_id", m.From.ID)
		return []string{msgBotInternal}
	}

	text := strings.TrimSpace(m.Text)
	if text == "" {
		return []string{msgBotNotText}
	}
	if strings.HasPrefix(text, "/") {
		return []string{botGreeting}
	}
	if utf8.RuneCountInString(text) > maxBotInputLen {
		return []string{msgBotTooLong}
	}

	reply, err := s.agent.Handle(ctx, user.ID, text)
	if err != nil {
		switch {
		case errors.Is(err, ErrAgentLimit):
			return []string{msgBotTooComplex}
		case ctx.Err() != nil:
			return nil // shutting down, nobody is listening
		default:
			s.logger.Error("agent handle", "error", err, "telegram_id", user.ID)
			return []string{msgBotInternal}
		}
	}
	return splitReply(reply, maxBotReplyLen)
}

// allowedUser reports whether the sender may talk to the bot. An empty
// allow-list means everyone (the family configures it in production).
func (s *BotService) allowedUser(id int64) bool {
	if len(s.allowed) == 0 {
		return true
	}
	_, ok := s.allowed[id]
	return ok
}

// registerUser upserts the member profile on every message so the panel
// sees current names. Telegram names can exceed the domain cap, so they
// are clamped first.
func (s *BotService) registerUser(ctx context.Context, from port.TelegramUser) (domain.User, error) {
	u, err := domain.NewUser(from.ID, clampName(from.FirstName), clampName(from.Username), s.clock.Now())
	if err != nil {
		return domain.User{}, fmt.Errorf("build user: %w", err)
	}
	stored, err := s.users.EnsureProfile(ctx, u)
	if err != nil {
		return domain.User{}, fmt.Errorf("upsert user: %w", err)
	}
	return stored, nil
}

func clampName(s string) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) > domain.MaxNameLen {
		r = r[:domain.MaxNameLen]
	}
	return string(r)
}

// splitReply cuts a long reply into chunks of at most max runes each,
// preferring to break at a newline.
func splitReply(s string, max int) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	r := []rune(s)
	if len(r) <= max {
		return []string{s}
	}
	var out []string
	for len(r) > max {
		cut := max
		for i := max - 1; i > max/2; i-- {
			if r[i] == '\n' {
				cut = i + 1
				break
			}
		}
		out = append(out, string(r[:cut]))
		r = r[cut:]
	}
	if len(r) > 0 {
		out = append(out, string(r))
	}
	return out
}

// botLimiter is a per-user fixed-window counter. Safe for concurrent use:
// the poller handles updates in parallel.
type botLimiter struct {
	mu      sync.Mutex
	perMin  int
	windows map[int64]*botWindow
}

type botWindow struct {
	start time.Time
	count int
}

func newBotLimiter(perMinute int) *botLimiter {
	return &botLimiter{perMin: perMinute, windows: map[int64]*botWindow{}}
}

func (l *botLimiter) allow(id int64, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	w := l.windows[id]
	if w == nil || now.Sub(w.start) >= time.Minute {
		w = &botWindow{start: now}
		l.windows[id] = w
	}
	if w.count >= l.perMin {
		return false
	}
	w.count++
	return true
}
