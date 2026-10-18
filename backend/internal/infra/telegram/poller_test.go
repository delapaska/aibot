package telegram

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/delapaska/aibot/internal/app/port"
)

// fakeAPI is a scripted port.TelegramAPI: each GetUpdates call pops the
// next batch (an error when failFirst is set), SendMessage records texts.
type fakeAPI struct {
	mu        sync.Mutex
	batches   [][]port.TelegramUpdate
	failFirst bool
	sent      []string
	onSend    func(text string)
}

func (f *fakeAPI) GetUpdates(ctx context.Context, offset int64, _ int) ([]port.TelegramUpdate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failFirst {
		f.failFirst = false
		return nil, errors.New("poll failed once")
	}
	if len(f.batches) == 0 {
		return nil, nil
	}
	b := f.batches[0]
	f.batches = f.batches[1:]
	_ = offset
	return b, nil
}

func (f *fakeAPI) SendMessage(_ context.Context, _ int64, text string) error {
	f.mu.Lock()
	f.sent = append(f.sent, text)
	f.mu.Unlock()
	if f.onSend != nil {
		f.onSend(text)
	}
	return nil
}

func (f *fakeAPI) sentTexts() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.sent...)
}

// recordingHandler returns a fixed reply and records the updates it saw.
type recordingHandler struct {
	mu      sync.Mutex
	updates []port.TelegramUpdate
	reply   []string
	panicOn int64 // update id to panic on, 0 = never
}

func (h *recordingHandler) HandleUpdate(_ context.Context, u port.TelegramUpdate) []string {
	h.mu.Lock()
	h.updates = append(h.updates, u)
	boom := h.panicOn != 0 && u.UpdateID == h.panicOn
	h.mu.Unlock()
	if boom {
		panic("handler exploded")
	}
	// Mirror the real service: non-message updates produce no reply.
	if u.Message == nil {
		return nil
	}
	return h.reply
}

func TestPollerConsumesUpdatesAndSendsReplies(t *testing.T) {
	api := &fakeAPI{batches: [][]port.TelegramUpdate{
		{
			{UpdateID: 5, Message: &port.TelegramMessage{MessageID: 1, Chat: port.TelegramChat{ID: 42, Type: "private"}, Text: "кофе 350"}},
			{UpdateID: 6},
		},
	}}
	handler := &recordingHandler{reply: []string{"Записал"}}
	sent := make(chan struct{}, 1)
	api.onSend = func(string) { sent <- struct{}{} }

	p := NewPoller(api, handler, testLogger(t))
	p.retryDelay = time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = p.Run(ctx)
		close(done)
	}()

	select {
	case <-sent:
	case <-time.After(2 * time.Second):
		t.Fatal("no reply sent")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop")
	}

	if got := handler.updates; len(got) != 2 || got[0].UpdateID != 5 || got[1].UpdateID != 6 {
		t.Fatalf("handled updates = %+v", got)
	}
	if got := api.sentTexts(); len(got) != 1 || got[0] != "Записал" {
		t.Fatalf("sent = %v", got)
	}
}

func TestPollerRetriesAfterError(t *testing.T) {
	api := &fakeAPI{
		failFirst: true,
		batches: [][]port.TelegramUpdate{
			{{UpdateID: 3, Message: &port.TelegramMessage{MessageID: 1, Chat: port.TelegramChat{ID: 7, Type: "private"}, Text: "hi"}}},
		},
	}
	handler := &recordingHandler{reply: []string{"ok"}}
	sent := make(chan struct{}, 1)
	api.onSend = func(string) { sent <- struct{}{} }

	p := NewPoller(api, handler, testLogger(t))
	p.retryDelay = time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = p.Run(ctx)
		close(done)
	}()

	select {
	case <-sent:
	case <-time.After(2 * time.Second):
		t.Fatal("no reply after retry")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop")
	}
}

func TestPollerSurvivesHandlerPanic(t *testing.T) {
	api := &fakeAPI{batches: [][]port.TelegramUpdate{
		{{UpdateID: 1, Message: &port.TelegramMessage{MessageID: 1, Chat: port.TelegramChat{ID: 7, Type: "private"}, Text: "a"}}},
		{{UpdateID: 2, Message: &port.TelegramMessage{MessageID: 2, Chat: port.TelegramChat{ID: 7, Type: "private"}, Text: "b"}}},
	}}
	handler := &recordingHandler{reply: []string{"ok"}, panicOn: 1}
	sent := make(chan struct{}, 1)
	api.onSend = func(string) { sent <- struct{}{} }

	p := NewPoller(api, handler, testLogger(t))
	p.retryDelay = time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = p.Run(ctx)
		close(done)
	}()

	select {
	case <-sent:
	case <-time.After(2 * time.Second):
		t.Fatal("second update was not handled after the panic")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop")
	}

	if got := handler.updates; len(got) != 2 {
		t.Fatalf("handled updates = %d, want 2", len(got))
	}
}
