package httpclient

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewSetsTimeoutAndTransport(t *testing.T) {
	c := New(5 * time.Second)
	if c.Timeout != 5*time.Second {
		t.Errorf("timeout = %v, want 5s", c.Timeout)
	}
	if _, ok := c.Transport.(*http.Transport); !ok {
		t.Errorf("transport = %T, want *http.Transport", c.Transport)
	}
}

func TestDialForcesIPv4(t *testing.T) {
	// The container has no IPv6 route: an AAAA record makes the dial fail
	// with "network is unreachable". Whatever network family the caller
	// asks for, the transport must dial IPv4 (tcp4) — an IPv4-only test
	// server is reachable even when "tcp6" is requested.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()

	c := New(time.Second)
	tr := c.Transport.(*http.Transport)
	conn, err := tr.DialContext(context.Background(), "tcp6", strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatalf("dial tcp6 to an IPv4-only server: %v (want forced tcp4)", err)
	}
	_ = conn.Close()
}

func TestClientEndToEnd(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "pong")
	}))
	defer srv.Close()

	c := New(2 * time.Second)
	resp, err := c.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "pong" {
		t.Errorf("body = %q, want pong", body)
	}
}
