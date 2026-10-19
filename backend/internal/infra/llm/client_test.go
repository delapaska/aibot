package llm

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/delapaska/aibot/internal/app/port"
)

// testClient builds a client against url with instant retries.
func testClient(t *testing.T, url, key string) *Client {
	t.Helper()
	c := New(url, key, "test-model", slog.New(slog.NewTextHandler(io.Discard, nil)))
	c.wait = func(int) time.Duration { return 0 }
	return c
}

func TestChatCompleteBasic(t *testing.T) {
	var gotPath, gotAuth, gotCT string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotCT = r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"Какой расход добавить?"}}]}`)
	}))
	defer srv.Close()

	c := testClient(t, srv.URL, "test-key")
	resp, err := c.ChatComplete(context.Background(), port.ChatRequest{
		Messages: []port.ChatMessage{
			{Role: port.RoleSystem, Content: "Ты помощник по расходам"},
			{Role: port.RoleUser, Content: "кофе 350"},
		},
	})
	if err != nil {
		t.Fatalf("ChatComplete: %v", err)
	}
	if gotPath != "/chat/completions" {
		t.Errorf("path = %q, want /chat/completions", gotPath)
	}
	if gotAuth != "Bearer test-key" {
		t.Errorf("authorization = %q", gotAuth)
	}
	if gotCT != "application/json" {
		t.Errorf("content-type = %q", gotCT)
	}
	if resp.Content != "Какой расход добавить?" {
		t.Errorf("content = %q", resp.Content)
	}
	if gotBody["model"] != "test-model" {
		t.Errorf("model = %v", gotBody["model"])
	}
	msgs, _ := gotBody["messages"].([]any)
	if len(msgs) != 2 {
		t.Errorf("messages = %d, want 2", len(msgs))
	}
	if _, has := gotBody["tools"]; has {
		t.Error("tools must be omitted when empty")
	}
}

// TestFallbackModelsSentInRequest: fallback models travel in the OpenRouter
// "models" field so a rate-limited primary is rerouted server-side.
func TestFallbackModelsSentInRequest(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"ok"}}]}`)
	}))
	defer srv.Close()

	c := New(srv.URL, "key", "primary", slog.New(slog.NewTextHandler(io.Discard, nil)))
	c.wait = func(int) time.Duration { return 0 }
	c.WithFallbacks([]string{"fallback-a", "fallback-b"})
	_, err := c.ChatComplete(context.Background(), port.ChatRequest{
		Messages: []port.ChatMessage{{Role: port.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("ChatComplete: %v", err)
	}
	if gotBody["model"] != "primary" {
		t.Errorf("model = %v, want primary", gotBody["model"])
	}
	got, _ := gotBody["models"].([]any)
	want := []any{"primary", "fallback-a", "fallback-b"}
	if len(got) != len(want) {
		t.Fatalf("models = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("models = %v, want %v", got, want)
		}
	}
}

func TestNoFallbackFieldWithoutFallbacks(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"ok"}}]}`)
	}))
	defer srv.Close()

	c := testClient(t, srv.URL, "key")
	_, err := c.ChatComplete(context.Background(), port.ChatRequest{
		Messages: []port.ChatMessage{{Role: port.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("ChatComplete: %v", err)
	}
	if _, has := gotBody["models"]; has {
		t.Error("models must be omitted when no fallbacks configured")
	}
}

func TestChatCompleteEmptyKeyOmitsAuth(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"ok"}}]}`)
	}))
	defer srv.Close()

	c := testClient(t, srv.URL, "")
	if _, err := c.ChatComplete(context.Background(), port.ChatRequest{}); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "" {
		t.Errorf("authorization = %q, want no header for an empty key", gotAuth)
	}
}

func TestChatCompleteToolCalls(t *testing.T) {
	var gotTools []any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotTools, _ = body["tools"].([]any)
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","tool_calls":[
			{"id":"call_1","type":"function","function":{"name":"add_expense","arguments":"{\"amount\":35000}"}}
		]},"finish_reason":"tool_calls"}]}`)
	}))
	defer srv.Close()

	c := testClient(t, srv.URL, "k")
	resp, err := c.ChatComplete(context.Background(), port.ChatRequest{
		Messages: []port.ChatMessage{{Role: port.RoleUser, Content: "кофе 350"}},
		Tools: []port.Tool{{
			Name:        "add_expense",
			Description: "Добавить расход",
			ParamsJSON:  `{"type":"object","properties":{"amount":{"type":"integer"}}}`,
		}},
	})
	if err != nil {
		t.Fatalf("ChatComplete: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("tool calls = %d, want 1", len(resp.ToolCalls))
	}
	tc := resp.ToolCalls[0]
	if tc.ID != "call_1" || tc.Name != "add_expense" || tc.ArgsJSON != `{"amount":35000}` {
		t.Errorf("tool call = %+v", tc)
	}
	if resp.Content != "" {
		t.Errorf("content = %q, want empty", resp.Content)
	}
	// The tools traveled in the OpenAI wire shape.
	if len(gotTools) != 1 {
		t.Fatalf("tools sent = %d, want 1", len(gotTools))
	}
	tool := gotTools[0].(map[string]any)
	if tool["type"] != "function" {
		t.Errorf("tool type = %v", tool["type"])
	}
	fn := tool["function"].(map[string]any)
	if fn["name"] != "add_expense" || fn["parameters"] == nil {
		t.Errorf("function = %v", fn)
	}
}

func TestToolRoundTrip(t *testing.T) {
	// A follow-up turn carries the assistant's tool calls and the tool
	// reply back to the provider; the wire format must survive it.
	var gotMsgs []any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotMsgs, _ = body["messages"].([]any)
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"Готово"}}]}`)
	}))
	defer srv.Close()

	c := testClient(t, srv.URL, "k")
	_, err := c.ChatComplete(context.Background(), port.ChatRequest{
		Messages: []port.ChatMessage{
			{Role: port.RoleUser, Content: "кофе 350"},
			{Role: port.RoleAssistant, ToolCalls: []port.ToolCall{{
				ID: "call_1", Name: "add_expense", ArgsJSON: `{"amount":35000}`,
			}}},
			{Role: port.RoleTool, ToolCallID: "call_1", Content: `{"id":"exp-1"}`},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(gotMsgs) != 3 {
		t.Fatalf("messages = %d, want 3", len(gotMsgs))
	}
	assistant := gotMsgs[1].(map[string]any)
	calls, _ := assistant["tool_calls"].([]any)
	if len(calls) != 1 {
		t.Fatalf("assistant tool_calls = %d, want 1", len(calls))
	}
	call := calls[0].(map[string]any)
	if call["id"] != "call_1" {
		t.Errorf("call id = %v", call["id"])
	}
	toolMsg := gotMsgs[2].(map[string]any)
	if toolMsg["role"] != "tool" || toolMsg["tool_call_id"] != "call_1" {
		t.Errorf("tool message = %v", toolMsg)
	}
}

func TestRetryOnRateLimit(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		if hits == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"error":{"message":"rate limited"}}`)
			return
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"ok"}}]}`)
	}))
	defer srv.Close()

	c := testClient(t, srv.URL, "k")
	resp, err := c.ChatComplete(context.Background(), port.ChatRequest{})
	if err != nil {
		t.Fatalf("ChatComplete: %v", err)
	}
	if resp.Content != "ok" || hits != 2 {
		t.Errorf("content = %q, hits = %d, want retry then success", resp.Content, hits)
	}
}

func TestRetryExhausted(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"message":"rate limited"}}`)
	}))
	defer srv.Close()

	c := testClient(t, srv.URL, "k")
	if _, err := c.ChatComplete(context.Background(), port.ChatRequest{}); err == nil {
		t.Fatal("want an error after exhausting retries")
	}
	if hits != 3 { // 1 + maxRetries(2)
		t.Errorf("hits = %d, want 3", hits)
	}
}

func TestPermanentErrorNotRetried(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"message":"model is required"}}`)
	}))
	defer srv.Close()

	c := testClient(t, srv.URL, "k")
	_, err := c.ChatComplete(context.Background(), port.ChatRequest{})
	if err == nil {
		t.Fatal("want an error")
	}
	if hits != 1 {
		t.Errorf("hits = %d, want 1 (4xx is not retried)", hits)
	}
	if !contains(err.Error(), "model is required") {
		t.Errorf("error must carry the provider message, got %q", err.Error())
	}
}

func TestEmptyChoicesIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[]}`)
	}))
	defer srv.Close()

	c := testClient(t, srv.URL, "k")
	if _, err := c.ChatComplete(context.Background(), port.ChatRequest{}); err == nil {
		t.Fatal("want an error for an empty choices list")
	}
}

func TestBrokenJSONIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `<html>oops</html>`)
	}))
	defer srv.Close()

	c := testClient(t, srv.URL, "k")
	if _, err := c.ChatComplete(context.Background(), port.ChatRequest{}); err == nil {
		t.Fatal("want an error for a non-JSON body")
	}
}

func TestContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"late"}}]}`)
	}))
	defer srv.Close()

	c := testClient(t, srv.URL, "k")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := c.ChatComplete(ctx, port.ChatRequest{}); err == nil {
		t.Fatal("want an error on a cancelled context")
	}
}

func TestRetryOnEmptyResponse(t *testing.T) {
	// Free models sometimes answer with a blank message; a silent bot is
	// the worst outcome, so the client must treat it as transient.
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		if hits == 1 {
			_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":""}}]}`)
			return
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"Записал: кофе 350"}}]}`)
	}))
	defer srv.Close()

	c := testClient(t, srv.URL, "k")
	resp, err := c.ChatComplete(context.Background(), port.ChatRequest{})
	if err != nil {
		t.Fatalf("ChatComplete: %v", err)
	}
	if resp.Content != "Записал: кофе 350" || hits != 2 {
		t.Errorf("content = %q, hits = %d, want retry on empty then success", resp.Content, hits)
	}
}

func TestNoRetryWhenBudgetNearlyGone(t *testing.T) {
	// Near the parent deadline a retry cannot finish: it would only burn
	// the remaining seconds and die on the deadline anyway.
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := testClient(t, srv.URL, "k")
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, err := c.ChatComplete(ctx, port.ChatRequest{}); err == nil {
		t.Fatal("want an error")
	}
	if hits != 1 {
		t.Errorf("hits = %d, want 1 (no doomed retries near the deadline)", hits)
	}
}

func TestLargeResponseDecodedFully(t *testing.T) {
	// Provider responses with tool calls and long text exceed the old
	// 4 KiB read cap; a truncated body decoded as "unexpected end of
	// JSON input". The full body must reach the decoder.
	content := strings.Repeat("хранение ", 3000) // ~27 KB
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":`+strconv.Quote(content)+`}}]}`)
	}))
	defer srv.Close()

	c := testClient(t, srv.URL, "k")
	resp, err := c.ChatComplete(context.Background(), port.ChatRequest{})
	if err != nil {
		t.Fatalf("ChatComplete: %v", err)
	}
	if resp.Content != content {
		t.Errorf("content truncated: got %d bytes, want %d", len(resp.Content), len(content))
	}
}

func TestRetryOnTruncatedJSON(t *testing.T) {
	// A body cut off mid-JSON (flaky network, provider hiccup) is
	// transient: the same request succeeds on the next attempt.
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		if hits == 1 {
			_, _ = io.WriteString(w, `{"choices":[{"mess`)
			return
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"ок"}}]}`)
	}))
	defer srv.Close()

	c := testClient(t, srv.URL, "k")
	resp, err := c.ChatComplete(context.Background(), port.ChatRequest{})
	if err != nil {
		t.Fatalf("ChatComplete: %v", err)
	}
	if resp.Content != "ок" || hits != 2 {
		t.Errorf("content = %q, hits = %d, want retry on truncated JSON", resp.Content, hits)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
