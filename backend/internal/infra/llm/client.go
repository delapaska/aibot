// Package llm implements the LLM port against an OpenAI-compatible chat
// completions API (OpenAI, OpenRouter, cloud.ru, Ollama, ...).
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/delapaska/aibot/internal/app/port"
)

const (
	maxRetries  = 2 // extra attempts after the first one
	maxErrBody  = 4 << 10
	chatTimeout = 90 * time.Second
)

// Client talks to an OpenAI-compatible endpoint.
type Client struct {
	baseURL string
	apiKey  string
	model   string
	hc      *http.Client
	logger  *slog.Logger

	// wait pauses before retry attempt n (1-based); replaced in tests.
	wait func(n int) time.Duration
}

// New builds a client for baseURL (e.g. https://openrouter.ai/api/v1).
func New(baseURL, apiKey, model string, logger *slog.Logger) *Client {
	if logger == nil {
		logger = slog.Default()
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		model:   model,
		hc:      &http.Client{Timeout: chatTimeout},
		logger:  logger,
		wait:    func(n int) time.Duration { return time.Duration(n) * time.Second },
	}
}

// ChatComplete performs one chat completion round-trip. Transient
// failures (429, 5xx) are retried with a linear backoff: free-tier
// providers rate-limit aggressively.
func (c *Client) ChatComplete(ctx context.Context, req port.ChatRequest) (port.ChatResponse, error) {
	body, err := json.Marshal(toWireRequest(c.model, req))
	if err != nil {
		return port.ChatResponse{}, fmt.Errorf("llm: encode request: %w", err)
	}

	for attempt := 0; ; attempt++ {
		resp, err := c.do(ctx, body)
		if err == nil {
			return resp, nil
		}
		var retryable *retryableError
		if !errors.As(err, &retryable) || attempt >= maxRetries {
			return port.ChatResponse{}, err
		}
		delay := c.wait(attempt + 1)
		c.logger.Warn("llm retry",
			"attempt", attempt+1,
			"status", retryable.status,
			"wait", delay.String(),
		)
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return port.ChatResponse{}, ctx.Err()
		}
	}
}

type retryableError struct {
	status int
	err    error
}

func (e *retryableError) Error() string { return e.err.Error() }
func (e *retryableError) Unwrap() error { return e.err }

// do performs one attempt.
func (c *Client) do(ctx context.Context, body []byte) (port.ChatResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return port.ChatResponse{}, fmt.Errorf("llm: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	httpResp, err := c.hc.Do(req)
	if err != nil {
		return port.ChatResponse{}, fmt.Errorf("llm: call: %w", err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(httpResp.Body, maxErrBody))
	if err != nil {
		return port.ChatResponse{}, fmt.Errorf("llm: read response: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		msg := excerpt(respBody)
		if apiMsg := apiErrorMessage(respBody); apiMsg != "" {
			msg = apiMsg
		}
		err := fmt.Errorf("llm: status %d: %s", httpResp.StatusCode, msg)
		if httpResp.StatusCode == http.StatusTooManyRequests || httpResp.StatusCode >= 500 {
			return port.ChatResponse{}, &retryableError{status: httpResp.StatusCode, err: err}
		}
		return port.ChatResponse{}, err
	}

	var wire wireResponse
	if err := json.Unmarshal(respBody, &wire); err != nil {
		return port.ChatResponse{}, fmt.Errorf("llm: decode response: %w", err)
	}
	if len(wire.Choices) == 0 {
		return port.ChatResponse{}, errors.New("llm: empty choices in response")
	}
	return fromWire(wire.Choices[0].Message), nil
}

// apiErrorMessage extracts {"error":{"message":...}} if present.
func apiErrorMessage(body []byte) string {
	var payload struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	return payload.Error.Message
}

// excerpt bounds a raw error body so provider noise never floods logs.
func excerpt(body []byte) string {
	s := string(body)
	if len(s) > 200 {
		s = s[:200] + "..."
	}
	return strings.TrimSpace(s)
}

func toWireRequest(model string, req port.ChatRequest) wireRequest {
	wr := wireRequest{Model: model}
	for _, m := range req.Messages {
		wm := wireMessage{Role: string(m.Role), Content: m.Content, ToolCallID: m.ToolCallID}
		for _, tc := range m.ToolCalls {
			wm.ToolCalls = append(wm.ToolCalls, wireToolCall{
				ID:       tc.ID,
				Type:     "function",
				Function: wireFuncCall{Name: tc.Name, Arguments: tc.ArgsJSON},
			})
		}
		wr.Messages = append(wr.Messages, wm)
	}
	for _, t := range req.Tools {
		wr.Tools = append(wr.Tools, wireTool{
			Type: "function",
			Function: wireToolFunc{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  json.RawMessage(t.ParamsJSON),
			},
		})
	}
	return wr
}

func fromWire(m wireMessage) port.ChatResponse {
	resp := port.ChatResponse{Content: m.Content}
	for _, tc := range m.ToolCalls {
		resp.ToolCalls = append(resp.ToolCalls, port.ToolCall{
			ID:       tc.ID,
			Name:     tc.Function.Name,
			ArgsJSON: tc.Function.Arguments,
		})
	}
	return resp
}

type wireRequest struct {
	Model    string        `json:"model"`
	Messages []wireMessage `json:"messages"`
	Tools    []wireTool    `json:"tools,omitempty"`
}

type wireMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content"`
	ToolCalls  []wireToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

type wireToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function wireFuncCall `json:"function"`
}

type wireFuncCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type wireTool struct {
	Type     string       `json:"type"`
	Function wireToolFunc `json:"function"`
}

type wireToolFunc struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type wireResponse struct {
	Choices []struct {
		Message      wireMessage `json:"message"`
		FinishReason string      `json:"finish_reason"`
	} `json:"choices"`
}
