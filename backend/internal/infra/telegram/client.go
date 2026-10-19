// Package telegram implements the port.TelegramAPI boundary over the
// Telegram Bot API: long polling for updates and outgoing messages.
// No third-party SDK — stdlib HTTP only, like the LLM client.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/delapaska/aibot/internal/app/port"
	"github.com/delapaska/aibot/internal/infra/httpclient"
)

const (
	defaultBase        = "https://api.telegram.org"
	defaultPollTimeout = 50 // seconds per long poll
	// The HTTP timeout must exceed the long-poll window: the server holds
	// the request open until an update arrives or the window expires.
	requestTimeout = defaultPollTimeout*time.Second + 15*time.Second
	maxBodyBytes   = 4 << 20 // updates can carry media metadata; cap the body
	// parseModeHTML makes Telegram render the markup the adapter produces
	// (see markdown.go): replies arrive as markdown and go out as HTML.
	parseModeHTML = "HTML"
)

// Client talks to the Telegram Bot API.
type Client struct {
	token  string
	base   string
	hc     *http.Client
	logger *slog.Logger
}

// New returns a client for the given bot token.
func New(token string, logger *slog.Logger) *Client {
	return &Client{
		token:  token,
		base:   defaultBase,
		hc:     httpclient.New(requestTimeout),
		logger: logger,
	}
}

// GetUpdates long-polls for updates after the offset.
func (c *Client) GetUpdates(ctx context.Context, offset int64, timeoutSec int) ([]port.TelegramUpdate, error) {
	if timeoutSec <= 0 {
		timeoutSec = defaultPollTimeout
	}
	q := url.Values{}
	q.Set("offset", strconv.FormatInt(offset, 10))
	q.Set("timeout", strconv.Itoa(timeoutSec))
	// Only plain messages are consumed; edits, callbacks etc. are dropped
	// server-side and never reach the bot.
	q.Set("allowed_updates", `["message"]`)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.base+"/bot"+c.token+"/getUpdates?"+q.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("telegram: build request: %w", err)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("telegram: getUpdates: %w", err)
	}
	defer resp.Body.Close()

	body, err := readBody(resp)
	if err != nil {
		return nil, err
	}
	var out struct {
		OK          bool         `json:"ok"`
		Result      []wireUpdate `json:"result"`
		Description string       `json:"description"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("telegram: decode getUpdates: %w", err)
	}
	if !out.OK {
		return nil, fmt.Errorf("telegram: getUpdates: %s", out.Description)
	}
	updates := make([]port.TelegramUpdate, 0, len(out.Result))
	for _, w := range out.Result {
		updates = append(updates, w.toPort())
	}
	return updates, nil
}

// SendMessage delivers one text message and returns the created message
// id, so the caller can edit or delete it later. The text is markdown;
// it is rendered into the HTML subset Telegram understands.
func (c *Client) SendMessage(ctx context.Context, chatID int64, text string) (int64, error) {
	body, err := json.Marshal(struct {
		ChatID    int64  `json:"chat_id"`
		Text      string `json:"text"`
		ParseMode string `json:"parse_mode"`
	}{ChatID: chatID, Text: toTelegramHTML(text), ParseMode: parseModeHTML})
	if err != nil {
		return 0, fmt.Errorf("telegram: encode message: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.base+"/bot"+c.token+"/sendMessage", bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("telegram: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return 0, fmt.Errorf("telegram: sendMessage: %w", err)
	}
	defer resp.Body.Close()

	raw, err := readBody(resp)
	if err != nil {
		return 0, err
	}
	var out struct {
		OK     bool `json:"ok"`
		Result struct {
			MessageID int64 `json:"message_id"`
		} `json:"result"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return 0, fmt.Errorf("telegram: decode sendMessage: %w", err)
	}
	if !out.OK {
		return 0, fmt.Errorf("telegram: sendMessage: %s", out.Description)
	}
	return out.Result.MessageID, nil
}

// EditMessageText rewrites the text of an earlier bot message. The text
// is rendered like SendMessage's.
func (c *Client) EditMessageText(ctx context.Context, chatID int64, messageID int64, text string) error {
	return c.mutateMessage(ctx, "editMessageText", struct {
		ChatID    int64  `json:"chat_id"`
		MessageID int64  `json:"message_id"`
		Text      string `json:"text"`
		ParseMode string `json:"parse_mode"`
	}{ChatID: chatID, MessageID: messageID, Text: toTelegramHTML(text), ParseMode: parseModeHTML})
}

// DeleteMessage removes an earlier bot message.
func (c *Client) DeleteMessage(ctx context.Context, chatID int64, messageID int64) error {
	return c.mutateMessage(ctx, "deleteMessage", struct {
		ChatID    int64 `json:"chat_id"`
		MessageID int64 `json:"message_id"`
	}{ChatID: chatID, MessageID: messageID})
}

// mutateMessage posts a message-mutating call and checks the ok-envelope.
func (c *Client) mutateMessage(ctx context.Context, method string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("telegram: encode %s: %w", method, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.base+"/bot"+c.token+"/"+method, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("telegram: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("telegram: %s: %w", method, err)
	}
	defer resp.Body.Close()

	raw, err := readBody(resp)
	if err != nil {
		return err
	}
	var out struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return fmt.Errorf("telegram: decode %s: %w", method, err)
	}
	if !out.OK {
		return fmt.Errorf("telegram: %s: %s", method, out.Description)
	}
	return nil
}

// SendChatAction shows a chat status (e.g. "typing") in the chat.
func (c *Client) SendChatAction(ctx context.Context, chatID int64, action string) error {
	body, err := json.Marshal(struct {
		ChatID int64  `json:"chat_id"`
		Action string `json:"action"`
	}{ChatID: chatID, Action: action})
	if err != nil {
		return fmt.Errorf("telegram: encode chat action: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.base+"/bot"+c.token+"/sendChatAction", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("telegram: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("telegram: sendChatAction: %w", err)
	}
	defer resp.Body.Close()

	raw, err := readBody(resp)
	if err != nil {
		return err
	}
	var out struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return fmt.Errorf("telegram: decode sendChatAction: %w", err)
	}
	if !out.OK {
		return fmt.Errorf("telegram: sendChatAction: %s", out.Description)
	}
	return nil
}

// readBody caps the response body and turns non-200 answers into errors
// carrying a short excerpt of the provider message.
func readBody(resp *http.Response) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("telegram: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("telegram: status %d: %s", resp.StatusCode, excerpt(raw, 200))
	}
	return raw, nil
}

func excerpt(b []byte, max int) string {
	s := string(b)
	if len(s) > max {
		s = s[:max] + "…"
	}
	return s
}

// Wire types mirror the Bot API JSON.

type wireUpdates struct {
	OK          bool         `json:"ok"`
	Result      []wireUpdate `json:"result"`
	Description string       `json:"description"`
}

type wireUpdate struct {
	UpdateID int64        `json:"update_id"`
	Message  *wireMessage `json:"message"`
}

func (w wireUpdate) toPort() port.TelegramUpdate {
	u := port.TelegramUpdate{UpdateID: w.UpdateID}
	if w.Message != nil {
		u.Message = w.Message.toPort()
	}
	return u
}

type wireMessage struct {
	MessageID int64     `json:"message_id"`
	From      *wireUser `json:"from"`
	Chat      wireChat  `json:"chat"`
	Text      string    `json:"text"`
	Date      int64     `json:"date"`
}

func (w wireMessage) toPort() *port.TelegramMessage {
	m := &port.TelegramMessage{
		MessageID: w.MessageID,
		Chat:      port.TelegramChat{ID: w.Chat.ID, Type: w.Chat.Type},
		Text:      w.Text,
	}
	if w.From != nil {
		m.From = port.TelegramUser{ID: w.From.ID, Username: w.From.UserName, FirstName: w.From.FirstName}
	}
	return m
}

type wireUser struct {
	ID        int64  `json:"id"`
	UserName  string `json:"username"`
	FirstName string `json:"first_name"`
}

type wireChat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}
