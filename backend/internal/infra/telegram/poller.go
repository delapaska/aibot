package telegram

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/delapaska/aibot/internal/app/port"
)

const (
	pollRetryDelay  = 3 * time.Second
	handleTimeout   = 60 * time.Second // one update, including the LLM round-trip
	maxHandlers     = 4                // concurrent updates; a family needs few
	defaultPollSecs = 50               // long-poll window
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
	api        port.TelegramAPI
	handler    Handler
	logger     *slog.Logger
	retryDelay time.Duration // overridable in tests
}

func NewPoller(api port.TelegramAPI, handler Handler, logger *slog.Logger) *Poller {
	return &Poller{api: api, handler: handler, logger: logger, retryDelay: pollRetryDelay}
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

// process runs one update: handle it, deliver every reply chunk.
func (p *Poller) process(ctx context.Context, u port.TelegramUpdate) {
	hctx, cancel := context.WithTimeout(ctx, handleTimeout)
	defer cancel()

	for _, text := range p.handler.HandleUpdate(hctx, u) {
		chatID := int64(0)
		if u.Message != nil {
			chatID = u.Message.Chat.ID
		}
		if err := p.api.SendMessage(hctx, chatID, text); err != nil {
			p.logger.Error("telegram sendMessage", "error", err, "chat_id", chatID)
		}
	}
}
