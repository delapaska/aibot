package httpapi

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// The panel is served from an embedded dist directory. The handler takes
// the filesystem as an argument, so tests drive it with a fake tree; the
// real embed only chooses what gets mounted in production.
func TestStaticServesIndexAtRoot(t *testing.T) {
	h := newStaticHandler(t, map[string]string{
		"index.html": "<html><body>panel</body></html>",
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("content-type = %q, want text/html", ct)
	}
	if body := rec.Body.String(); !strings.Contains(body, "panel") {
		t.Fatalf("body = %q, want embedded index", body)
	}
}

func TestStaticServesAssets(t *testing.T) {
	h := newStaticHandler(t, map[string]string{
		"index.html":         "<html></html>",
		"assets/app-123.js":  "console.log(1)",
		"assets/app-abc.css": "body{}",
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/assets/app-123.js", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Fatalf("content-type = %q, want javascript", ct)
	}
	if body := rec.Body.String(); body != "console.log(1)" {
		t.Fatalf("body = %q", body)
	}
}

// SPA fallback: client-side routes must return index.html, not 404.
func TestStaticFallsBackToIndex(t *testing.T) {
	h := newStaticHandler(t, map[string]string{
		"index.html": "<html><body>panel</body></html>",
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/some/client/route", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "panel") {
		t.Fatalf("body = %q, want index.html", body)
	}
}

// Missing files that look like assets (have an extension) must 404
// instead of serving index: a broken asset link should be visible.
func TestStaticMissingAssetIs404(t *testing.T) {
	h := newStaticHandler(t, map[string]string{
		"index.html": "<html></html>",
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/assets/nope.js", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// Non-GET methods are not a panel concern.
func TestStaticRejectsNonGet(t *testing.T) {
	h := newStaticHandler(t, map[string]string{
		"index.html": "<html></html>",
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/", strings.NewReader("x")))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

// API paths must never be swallowed by the static handler: unknown API
// paths keep the JSON 404 envelope even when the panel is mounted.
func TestStaticDoesNotTouchAPI(t *testing.T) {
	h := newStaticHandler(t, map[string]string{
		"index.html": "<html><body>panel</body></html>",
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/unknown", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if body := rec.Body.String(); strings.Contains(body, "<html") {
		t.Fatalf("api path answered with html: %q", body)
	}
}

// newStaticHandler wires the given files as the panel filesystem behind
// the real routing table, the same way production mounts the embed.
func newStaticHandler(t *testing.T, files map[string]string) http.Handler {
	t.Helper()
	fsys := fstest.MapFS{}
	for name, content := range files {
		fsys[name] = &fstest.MapFile{Data: []byte(content)}
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := NewServer(nil, nil, nil, logger, "")
	s.panelFS = fsys
	return s.Handler()
}
