package telegram

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// testServer starts an httptest server standing in for api.telegram.org.
func testServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func TestGetUpdatesMapsWireToUpdate(t *testing.T) {
	var gotPath string
	var gotQuery map[string]string
	srv := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		q := r.URL.Query()
		gotQuery = map[string]string{
			"offset":          q.Get("offset"),
			"timeout":         q.Get("timeout"),
			"allowed_updates": q.Get("allowed_updates"),
		}
		w.Write([]byte(`{"ok":true,"result":[` +
			`{"update_id":7,"message":{"message_id":11,"from":{"id":42,"is_bot":false,"first_name":"Аня","username":"anya"},"chat":{"id":42,"type":"private"},"text":"кофе 350","date":1700000000}},` +
			`{"update_id":8}` +
			`]}`))
	})
	c := New("TOK", testLogger(t))
	c.base = srv.URL

	ups, err := c.GetUpdates(context.Background(), 5, 10)
	if err != nil {
		t.Fatalf("GetUpdates: %v", err)
	}
	if gotPath != "/botTOK/getUpdates" {
		t.Errorf("path = %q", gotPath)
	}
	if gotQuery["offset"] != "5" || gotQuery["timeout"] != "10" || gotQuery["allowed_updates"] != `["message"]` {
		t.Errorf("query = %v", gotQuery)
	}
	if len(ups) != 2 {
		t.Fatalf("updates = %d, want 2", len(ups))
	}
	u := ups[0]
	if u.UpdateID != 7 || u.Message == nil {
		t.Fatalf("update = %+v", u)
	}
	m := u.Message
	if m.MessageID != 11 || m.From.ID != 42 || m.From.FirstName != "Аня" || m.From.Username != "anya" ||
		m.Chat.ID != 42 || m.Chat.Type != "private" || m.Text != "кофе 350" {
		t.Fatalf("message = %+v", m)
	}
	if ups[1].UpdateID != 8 || ups[1].Message != nil {
		t.Fatalf("second update = %+v", ups[1])
	}
}

func TestGetUpdatesAPIError(t *testing.T) {
	srv := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":false,"error_code":401,"description":"Unauthorized"}`))
	})
	c := New("TOK", testLogger(t))
	c.base = srv.URL

	if _, err := c.GetUpdates(context.Background(), 0, 1); err == nil || !strings.Contains(err.Error(), "Unauthorized") {
		t.Fatalf("err = %v, want description", err)
	}
}

func TestGetUpdatesHTTPError(t *testing.T) {
	srv := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	c := New("TOK", testLogger(t))
	c.base = srv.URL

	if _, err := c.GetUpdates(context.Background(), 0, 1); err == nil {
		t.Fatal("want error on 500")
	}
}

func TestSendMessagePostsJSON(t *testing.T) {
	var gotPath string
	var gotBody struct {
		ChatID int64  `json:"chat_id"`
		Text   string `json:"text"`
	}
	srv := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.Write([]byte(`{"ok":true,"result":{"message_id":1}}`))
	})
	c := New("TOK", testLogger(t))
	c.base = srv.URL

	if err := c.SendMessage(context.Background(), 42, "привет"); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if gotPath != "/botTOK/sendMessage" {
		t.Errorf("path = %q", gotPath)
	}
	if gotBody.ChatID != 42 || gotBody.Text != "привет" {
		t.Errorf("body = %+v", gotBody)
	}
}

func TestSendMessageError(t *testing.T) {
	srv := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"ok":false,"description":"chat not found"}`))
	})
	c := New("TOK", testLogger(t))
	c.base = srv.URL

	if err := c.SendMessage(context.Background(), 42, "привет"); err == nil || !strings.Contains(err.Error(), "chat not found") {
		t.Fatalf("err = %v, want description", err)
	}
}
