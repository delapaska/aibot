package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/delapaska/aibot/internal/app/port"
)

const (
	pollRetryDelay = 3 * time.Second
	// handleTimeout bounds one update, including every LLM round-trip.
	// Free LLM tiers answer slowly, and a heavy request (a weekly
	// summary) needs two rounds — plus retries for rate limits and
	// blank answers — so the budget is generous: the user sees the
	// progress bar and typing status meanwhile.
	handleTimeout   = 300 * time.Second
	maxHandlers     = 4  // concurrent updates; a family needs few
	defaultPollSecs = 50 // long-poll window
	// The typing chat action expires after ~5s; renew it early so the
	// status never flashes away during a slow LLM round-trip.
	typingInterval = 4 * time.Second
	// The free LLM tiers can take tens of seconds; show an interim
	// progress message that gets edited a few times while the agent
	// works, then removed once the answer lands.
	progressDelay = 3 * time.Second   // fast answers get no progress message
	progressTick  = 4 * time.Second   // edits are rate-limited; keep it slow
	progressSteps = 8                 // bar width
	progressSpan  = 270 * time.Second // bar fills (to 7/8) over this span
	// Removing the placeholder must outlive the handler deadline and
	// survive a network blip: its own timeout plus one retry.
	cleanupTimeout = 10 * time.Second
	cleanupRetry   = 1 * time.Second
	// Delivering replies also outlives the handler deadline: when the
	// agent stalls past it, the usecase still returns an apology, and
	// that message must reach the chat. Transient network stalls get a
	// few attempts — a lost reply looks exactly like a silent bot.
	sendTimeout       = 15 * time.Second
	deliverAttempts   = 3
	deliverRetryDelay = 2 * time.Second
)

// Handler processes one update and returns the reply texts to deliver.
// Implemented by the application layer (usecase.BotService); the adapter
// owns no policy.
type Handler interface {
	HandleUpdate(ctx context.Context, u port.TelegramUpdate) []string
}

// Poller drives the long-polling loop until the context is cancelled.
// Transient fetch errors are retried with a delay; a panicking handler
// cannot take the loop down.
type Poller struct {
	api               port.TelegramAPI
	handler           Handler
	logger            *slog.Logger
	retryDelay        time.Duration // overridable in tests
	handleTimeout     time.Duration // overridable in tests
	typingInterval    time.Duration // overridable in tests
	progressDelay     time.Duration // overridable in tests
	progressTick      time.Duration // overridable in tests
	cleanupTimeout    time.Duration // overridable in tests
	cleanupRetry      time.Duration // overridable in tests
	deliverRetryDelay time.Duration // overridable in tests
}

func NewPoller(api port.TelegramAPI, handler Handler, logger *slog.Logger) *Poller {
	return &Poller{
		api:               api,
		handler:           handler,
		logger:            logger,
		retryDelay:        pollRetryDelay,
		handleTimeout:     handleTimeout,
		typingInterval:    typingInterval,
		progressDelay:     progressDelay,
		progressTick:      progressTick,
		cleanupTimeout:    cleanupTimeout,
		cleanupRetry:      cleanupRetry,
		deliverRetryDelay: deliverRetryDelay,
	}
}

// Run blocks until ctx is done and waits for in-flight updates.
func (p *Poller) Run(ctx context.Context) error {
	var (
		wg     sync.WaitGroup
		sem    = make(chan struct{}, maxHandlers)
		offset int64
	)
	for {
		if ctx.Err() != nil {
			break
		}
		updates, err := p.api.GetUpdates(ctx, offset, defaultPollSecs)
		if err != nil {
			if ctx.Err() != nil {
				break // shutdown, not a failure
			}
			p.logger.Warn("telegram getUpdates failed", "error", err)
			select {
			case <-ctx.Done():
			case <-time.After(p.retryDelay):
				continue
			}
			break
		}
		// Confirm receipt first: the offset skips past these updates even
		// if processing below fails, so they are not re-delivered forever.
		for _, u := range updates {
			if u.UpdateID >= offset {
				offset = u.UpdateID + 1
			}
		}
		for _, u := range updates {
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				wg.Wait()
				p.logger.Info("telegram poller stopped")
				return nil
			}
			wg.Add(1)
			go func(u port.TelegramUpdate) {
				defer wg.Done()
				defer func() { <-sem }()
				defer func() {
					if r := recover(); r != nil {
						p.logger.Error("update handler panicked", "update_id", u.UpdateID, "panic", r)
					}
				}()
				p.process(ctx, u)
			}(u)
		}
	}
	wg.Wait()
	p.logger.Info("telegram poller stopped")
	return nil
}

// process runs one update: handle it, deliver every reply chunk. While
// the handler works, the chat shows the "typing" status and, for slow
// replies, an interim progress message that is edited as time passes and
// removed once the answer is delivered.
func (p *Poller) process(ctx context.Context, u port.TelegramUpdate) {
	hctx, cancel := context.WithTimeout(ctx, p.handleTimeout)
	defer cancel()

	chatID := int64(0)
	if u.Message != nil {
		chatID = u.Message.Chat.ID
	}

	typingDone := make(chan struct{})
	defer close(typingDone)
	go p.showTyping(hctx, chatID, typingDone)

	var (
		progressDone     = make(chan struct{})
		progressFinished = make(chan struct{})
		progressMsgID    atomic.Int64
	)
	var stopOnce sync.Once
	stop := func() { stopOnce.Do(func() { close(progressDone) }) }
	defer stop()
	go func() {
		defer close(progressFinished)
		p.showProgress(hctx, chatID, progressDone, &progressMsgID)
	}()

	for _, text := range p.handler.HandleUpdate(hctx, u) {
		p.deliver(ctx, hctx, chatID, text)
	}

	// The answer is out: stop the bar and remove the placeholder. The
	// cleanup runs on a context independent of the handler deadline —
	// otherwise a slow reply eats the budget and the placeholder stays.
	stop()
	<-progressFinished
	if id := progressMsgID.Load(); id != 0 {
		p.deleteProgress(hctx, chatID, id)
	}
}

// deliver sends one reply chunk, retrying transient failures. Every
// attempt runs on a fresh context that ignores the handler deadline —
// a slow agent must not eat the reply the usecase produced — and stops
// early only when the whole poller is shutting down.
func (p *Poller) deliver(root, hctx context.Context, chatID int64, text string) {
	for attempt := 1; ; attempt++ {
		dctx, cancel := context.WithTimeout(context.WithoutCancel(hctx), sendTimeout)
		_, err := p.api.SendMessage(dctx, chatID, text)
		cancel()
		if err == nil {
			return
		}
		p.logger.Error("telegram sendMessage", "error", err, "chat_id", chatID, "attempt", attempt)
		if attempt >= deliverAttempts || root.Err() != nil {
			return
		}
		select {
		case <-time.After(p.deliverRetryDelay):
		case <-root.Done():
			return
		}
	}
}

// deleteProgress removes the placeholder message on a fresh context: by
// now the reply is delivered, so the handler's deadline (or a cancelled
// parent) must not leave the placeholder hanging in the chat. One retry
// covers a network blip.
func (p *Poller) deleteProgress(hctx context.Context, chatID, id int64) {
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			time.Sleep(p.cleanupRetry)
		}
		// WithoutCancel drops the handler's deadline; the fresh timeout
		// bounds the call itself.
		dctx, cancel := context.WithTimeout(context.WithoutCancel(hctx), p.cleanupTimeout)
		err := p.api.DeleteMessage(dctx, chatID, id)
		cancel()
		if err == nil {
			return
		}
		p.logger.Warn("telegram progress cleanup", "error", err, "chat_id", chatID, "attempt", attempt+1)
	}
}

// showTyping sends the typing chat action now and renews it until done
// is closed or ctx ends. A failure is logged and stops the renewing: the
// indicator is cosmetic and must never break message handling.
func (p *Poller) showTyping(ctx context.Context, chatID int64, done <-chan struct{}) {
	if chatID == 0 {
		return
	}
	send := func() {
		if err := p.api.SendChatAction(ctx, chatID, "typing"); err != nil {
			p.logger.Warn("telegram sendChatAction", "error", err, "chat_id", chatID)
		}
	}
	send()
	ticker := time.NewTicker(p.typingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			send()
		}
	}
}

// showProgress posts a placeholder message once the handler proves slow,
// edits it a few times, and lets the caller delete it afterwards. All
// failures are logged and swallowed: the bar is cosmetic.
func (p *Poller) showProgress(ctx context.Context, chatID int64, done <-chan struct{}, msgID *atomic.Int64) {
	if chatID == 0 {
		return
	}
	// Fast answers need no progress message; give the handler a moment.
	select {
	case <-done:
		return
	case <-ctx.Done():
		return
	case <-time.After(p.progressDelay):
	}

	id, err := p.api.SendMessage(ctx, chatID, progressText(0))
	if err != nil {
		p.logger.Warn("telegram progress message", "error", err, "chat_id", chatID)
		return
	}
	msgID.Store(id)

	start := time.Now()
	ticker := time.NewTicker(p.progressTick)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := p.api.EditMessageText(ctx, chatID, id, progressText(time.Since(start))); err != nil {
				p.logger.Warn("telegram progress edit", "error", err, "chat_id", chatID)
				return
			}
		}
	}
}

// progressText renders the interim status: elapsed seconds and a bar that
// fills over progressSpan but never quite completes — only the real
// answer completes the job.
func progressText(elapsed time.Duration) string {
	step := progressSpan / progressSteps
	filled := int(elapsed / step)
	if filled > progressSteps-1 {
		filled = progressSteps - 1
	}
	bar := strings.Repeat("▰", filled) + strings.Repeat("▱", progressSteps-filled)
	return fmt.Sprintf("⏳ Обрабатываю… %dс\n%s", int(elapsed.Seconds()), bar)
}
