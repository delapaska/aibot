package telegram

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/delapaska/aibot/internal/app/port"
)

// fakeAPI is a scripted port.TelegramAPI: each GetUpdates call pops the
// next batch (an error when failFirst is set), SendMessage records texts,
// SendChatAction records chat actions, edits and deletes are recorded too.
// DeleteMessage mirrors the real client: it refuses when its context is
// already dead, and can fail once (failFirstDelete) to exercise retries.
// SendMessage mirrors it too when onSendCtxErr is set.
type fakeAPI struct {
	mu              sync.Mutex
	batches         [][]port.TelegramUpdate
	failFirst       bool
	failFirstDelete bool
	failFirstSends  int
	onSendCtxErr    bool
	sent            []string
	actions         []string
	edited          []string
	deleted         []int64
	nextID          int64
	onSend          func(text string)
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

func (f *fakeAPI) SendMessage(ctx context.Context, _ int64, text string) (int64, error) {
	if f.onSendCtxErr && ctx.Err() != nil {
		return 0, ctx.Err()
	}
	f.mu.Lock()
	if f.failFirstSends > 0 {
		f.failFirstSends--
		f.mu.Unlock()
		return 0, errors.New("network blip")
	}
	f.nextID++
	id := f.nextID
	f.sent = append(f.sent, text)
	f.mu.Unlock()
	if f.onSend != nil {
		f.onSend(text)
	}
	return id, nil
}

func (f *fakeAPI) SendChatAction(_ context.Context, _ int64, action string) error {
	f.mu.Lock()
	f.actions = append(f.actions, action)
	f.mu.Unlock()
	return nil
}

func (f *fakeAPI) EditMessageText(_ context.Context, _ int64, _ int64, text string) error {
	f.mu.Lock()
	f.edited = append(f.edited, text)
	f.mu.Unlock()
	return nil
}

func (f *fakeAPI) DeleteMessage(ctx context.Context, _ int64, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if f.failFirstDelete {
		f.failFirstDelete = false
		return errors.New("network blip")
	}
	f.deleted = append(f.deleted, id)
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

	// Updates are handled concurrently, so the completion order is not
	// guaranteed; what matters is that both went through the handler.
	if got := handler.updates; len(got) != 2 {
		t.Fatalf("handled updates = %+v", got)
	}
	seen := map[int64]bool{}
	for _, u := range handler.updates {
		seen[u.UpdateID] = true
	}
	if !seen[5] || !seen[6] {
		t.Fatalf("handled updates = %+v, want ids 5 and 6", handler.updates)
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

// blockingHandler waits for release before replying, emulating a slow
// agent round-trip.
type blockingHandler struct {
	release chan struct{}
	reply   []string
}

func (h *blockingHandler) HandleUpdate(_ context.Context, u port.TelegramUpdate) []string {
	if u.Message == nil {
		return nil
	}
	<-h.release
	return h.reply
}

// deadlineHandler returns its reply only after its own context has
// expired, emulating an agent that outlived the handler deadline.
type deadlineHandler struct {
	reply []string
}

func (h *deadlineHandler) HandleUpdate(ctx context.Context, u port.TelegramUpdate) []string {
	if u.Message == nil {
		return nil
	}
	<-ctx.Done()
	return h.reply
}

func TestPollerShowsTypingWhileProcessing(t *testing.T) {
	api := &fakeAPI{batches: [][]port.TelegramUpdate{
		{{UpdateID: 5, Message: &port.TelegramMessage{MessageID: 1, Chat: port.TelegramChat{ID: 42, Type: "private"}, Text: "сводка за месяц"}}},
	}}
	unblock := make(chan struct{})
	handler := &blockingHandler{release: unblock, reply: []string{"Готово"}}
	sent := make(chan struct{}, 1)
	api.onSend = func(string) { sent <- struct{}{} }

	p := NewPoller(api, handler, testLogger(t))
	p.retryDelay = time.Millisecond
	p.typingInterval = time.Hour // no renew inside the test window
	p.progressDelay = time.Hour  // and no progress bar

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = p.Run(ctx)
		close(done)
	}()

	// The typing action is sent as soon as processing starts.
	deadline := time.After(2 * time.Second)
	for {
		api.mu.Lock()
		n := len(api.actions)
		api.mu.Unlock()
		if n > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("no typing action while processing")
		case <-time.After(5 * time.Millisecond):
		}
	}

	close(unblock)
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

	// Exactly one action: renewing stopped with the processing.
	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.actions) != 1 || api.actions[0] != "typing" {
		t.Fatalf("actions = %v, want exactly one typing", api.actions)
	}
}

func TestPollerNoTypingWithoutChat(t *testing.T) {
	api := &fakeAPI{batches: [][]port.TelegramUpdate{
		{{UpdateID: 9}}, // no message -> no chat to type in
	}}
	handler := &recordingHandler{reply: []string{"x"}}

	p := NewPoller(api, handler, testLogger(t))
	p.retryDelay = time.Millisecond
	p.typingInterval = time.Hour

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = p.Run(ctx)
		close(done)
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop")
	}

	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.actions) != 0 {
		t.Fatalf("actions = %v, want none", api.actions)
	}
}

func TestPollerProgressMessageWhileSlow(t *testing.T) {
	api := &fakeAPI{batches: [][]port.TelegramUpdate{
		{{UpdateID: 5, Message: &port.TelegramMessage{MessageID: 1, Chat: port.TelegramChat{ID: 42, Type: "private"}, Text: "сводка за месяц"}}},
	}}
	unblock := make(chan struct{})
	handler := &blockingHandler{release: unblock, reply: []string{"Готово"}}
	sent := make(chan struct{}, 1)
	api.onSend = func(string) { sent <- struct{}{} }

	p := NewPoller(api, handler, testLogger(t))
	p.retryDelay = time.Millisecond
	p.typingInterval = time.Hour
	p.progressDelay = 5 * time.Millisecond
	p.progressTick = 10 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = p.Run(ctx)
		close(done)
	}()

	// The placeholder progress message goes out while the handler works.
	deadline := time.After(2 * time.Second)
	for {
		api.mu.Lock()
		n := len(api.sent)
		api.mu.Unlock()
		if n > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("no progress message while processing")
		case <-time.After(5 * time.Millisecond):
		}
	}

	// Hold the handler long enough for one progress edit (10ms tick).
	time.Sleep(30 * time.Millisecond)
	close(unblock)
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

	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.sent) < 2 || !strings.Contains(api.sent[0], "Обрабатываю") {
		t.Fatalf("sent = %v, want a progress placeholder first", api.sent)
	}
	if len(api.edited) < 1 {
		t.Errorf("edited = %v, want at least one progress update", api.edited)
	}
	// The placeholder (first message, id 1) is removed once the reply lands.
	if len(api.deleted) != 1 || api.deleted[0] != 1 {
		t.Fatalf("deleted = %v, want [1]", api.deleted)
	}
}

func TestPollerNoProgressForFastReply(t *testing.T) {
	api := &fakeAPI{batches: [][]port.TelegramUpdate{
		{{UpdateID: 5, Message: &port.TelegramMessage{MessageID: 1, Chat: port.TelegramChat{ID: 42, Type: "private"}, Text: "кофе 350"}}},
	}}
	handler := &recordingHandler{reply: []string{"Записал"}}
	sent := make(chan struct{}, 1)
	api.onSend = func(string) { sent <- struct{}{} }

	p := NewPoller(api, handler, testLogger(t))
	p.retryDelay = time.Millisecond
	p.progressDelay = time.Hour // reply is fast, the bar never shows

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

	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.sent) != 1 || api.sent[0] != "Записал" {
		t.Fatalf("sent = %v, want just the reply", api.sent)
	}
	if len(api.edited) != 0 || len(api.deleted) != 0 {
		t.Fatalf("edited = %v, deleted = %v, want none", api.edited, api.deleted)
	}
}

// TestPollerProgressCleanupSurvivesCancelledHandler: the handler deadline
// must not leave the placeholder behind — cleanup runs on its own context.
func TestPollerProgressCleanupSurvivesCancelledHandler(t *testing.T) {
	api := &fakeAPI{batches: [][]port.TelegramUpdate{
		{{UpdateID: 5, Message: &port.TelegramMessage{MessageID: 1, Chat: port.TelegramChat{ID: 42, Type: "private"}, Text: "сводка"}}},
	}}
	unblock := make(chan struct{})
	handler := &blockingHandler{release: unblock, reply: []string{"Готово"}}

	p := NewPoller(api, handler, testLogger(t))
	p.retryDelay = time.Millisecond
	p.typingInterval = time.Hour
	p.progressDelay = 5 * time.Millisecond
	p.progressTick = time.Hour
	p.cleanupRetry = time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = p.Run(ctx)
		close(done)
	}()

	deadline := time.After(2 * time.Second)
	for {
		api.mu.Lock()
		n := len(api.sent)
		api.mu.Unlock()
		if n > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("no progress message while processing")
		case <-time.After(5 * time.Millisecond):
		}
	}

	// Kill the root context — the handler deadline dies with it — then
	// finish the work. The placeholder must still be removed.
	cancel()
	close(unblock)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop")
	}

	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.deleted) != 1 || api.deleted[0] != 1 {
		t.Fatalf("deleted = %v, want [1]", api.deleted)
	}
}

// TestPollerDeliversReplyAfterHandlerDeadline covers the free-LLM stall:
// the agent outlives the handler deadline, the usecase still returns a
// reply (an apology), and it must reach the chat even though the handler
// context is already dead.
func TestPollerDeliversReplyAfterHandlerDeadline(t *testing.T) {
	api := &fakeAPI{batches: [][]port.TelegramUpdate{
		{{UpdateID: 5, Message: &port.TelegramMessage{MessageID: 1, Chat: port.TelegramChat{ID: 42, Type: "private"}, Text: "траты за неделю"}}},
	}}
	// The fake refuses to send on a dead context, like the real client.
	api.onSendCtxErr = true
	handler := &deadlineHandler{reply: []string{"Не успел, попробуй ещё раз"}}

	p := NewPoller(api, handler, testLogger(t))
	p.retryDelay = time.Millisecond
	p.handleTimeout = 100 * time.Millisecond
	p.typingInterval = time.Hour
	p.progressDelay = time.Hour // keep the progress bar out of the test

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = p.Run(ctx)
		close(done)
	}()

	select {
	case <-time.After(2 * time.Second):
		t.Fatal("no reply sent after handler deadline")
	case <-func() chan struct{} {
		c := make(chan struct{})
		go func() {
			for {
				api.mu.Lock()
				n := len(api.sent)
				api.mu.Unlock()
				if n > 0 {
					close(c)
					return
				}
				time.Sleep(5 * time.Millisecond)
			}
		}()
		return c
	}():
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop")
	}

	if got := api.sentTexts(); len(got) != 1 || got[0] != "Не успел, попробуй ещё раз" {
		t.Fatalf("sent = %v", got)
	}
}

// TestPollerRetriesDeliveryAfterFailure covers a transient network stall
// on the first delivery attempt: the reply must still reach the chat.
func TestPollerRetriesDeliveryAfterFailure(t *testing.T) {
	api := &fakeAPI{batches: [][]port.TelegramUpdate{
		{{UpdateID: 5, Message: &port.TelegramMessage{MessageID: 1, Chat: port.TelegramChat{ID: 42, Type: "private"}, Text: "кофе 350"}}},
	}}
	api.failFirstSends = 1
	handler := &recordingHandler{reply: []string{"Записал: кофе 350"}}

	p := NewPoller(api, handler, testLogger(t))
	p.retryDelay = time.Millisecond
	p.typingInterval = time.Hour
	p.progressDelay = time.Hour
	p.deliverRetryDelay = time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = p.Run(ctx)
		close(done)
	}()

	deadline := time.After(2 * time.Second)
	for {
		api.mu.Lock()
		n := len(api.sent)
		api.mu.Unlock()
		if n > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("no reply sent after delivery retry")
		case <-time.After(5 * time.Millisecond):
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop")
	}

	if got := api.sentTexts(); len(got) != 1 || got[0] != "Записал: кофе 350" {
		t.Fatalf("sent = %v", got)
	}
}

// TestPollerProgressCleanupRetriesAfterFailure covers a network blip on
// the first delete attempt.
func TestPollerProgressCleanupRetriesAfterFailure(t *testing.T) {
	api := &fakeAPI{
		failFirstDelete: true,
		batches: [][]port.TelegramUpdate{
			{{UpdateID: 5, Message: &port.TelegramMessage{MessageID: 1, Chat: port.TelegramChat{ID: 42, Type: "private"}, Text: "сводка"}}},
		},
	}
	unblock := make(chan struct{})
	handler := &blockingHandler{release: unblock, reply: []string{"Готово"}}
	sent := make(chan struct{}, 1)
	api.onSend = func(string) { sent <- struct{}{} }

	p := NewPoller(api, handler, testLogger(t))
	p.retryDelay = time.Millisecond
	p.typingInterval = time.Hour
	p.progressDelay = 5 * time.Millisecond
	p.progressTick = time.Hour
	p.cleanupRetry = 5 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = p.Run(ctx)
		close(done)
	}()

	deadline := time.After(2 * time.Second)
	for {
		api.mu.Lock()
		n := len(api.sent)
		api.mu.Unlock()
		if n > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("no progress message while processing")
		case <-time.After(5 * time.Millisecond):
		}
	}

	close(unblock)
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

	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.deleted) != 1 || api.deleted[0] != 1 {
		t.Fatalf("deleted = %v, want [1] after the retry", api.deleted)
	}
}
