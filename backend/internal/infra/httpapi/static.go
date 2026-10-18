// Static serving of the web panel. The built frontend lands in dist/app
// (see make panel / the Docker build) and is embedded into the binary;
// until the first build a placeholder page from dist/index.html is
// served, so the binary always compiles and runs.
package httpapi

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var distFS embed.FS

// panelRoot returns the filesystem the panel is served from. Production
// leaves Server.panelFS nil and the embedded dist is used; tests inject
// a fake tree through it.
func (s *Server) panelRoot() fs.FS {
	if s.panelFS != nil {
		return s.panelFS
	}
	// Prefer the built SPA; fall back to the placeholder page.
	if _, err := fs.Stat(distFS, "dist/app/index.html"); err == nil {
		if sub, err := fs.Sub(distFS, "dist/app"); err == nil {
			return sub
		}
	}
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		// all:dist guarantees dist exists; unreachable in practice.
		return distFS
	}
	return sub
}

// mountRoot registers the root handler: unmatched /api/* paths keep the
// JSON error envelopes, everything else is the panel static.
func (s *Server) mountRoot(mux *http.ServeMux) {
	panel := s.panelRoot()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			s.catchAll(w, r)
			return
		}
		s.servePanel(panel, w, r)
	})
}

// servePanel answers GET/HEAD with panel files: exact file first, then
// the SPA fallback for client routes, 404 for asset-like misses.
func (s *Server) servePanel(panel fs.FS, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		fail(w, http.StatusMethodNotAllowed, "method_not_allowed",
			"method "+r.Method+" is not allowed here")
		return
	}
	clean := path.Clean("/" + r.URL.Path) // leading slash, no ".."
	name := strings.TrimPrefix(clean, "/")
	if name == "" {
		name = "index.html"
	}
	if data, err := fs.ReadFile(panel, name); err == nil {
		writeStatic(w, name, data)
		return
	}
	// A missing path that looks like a file (asset hash, favicon) must
	// stay a 404: falling back to index would hide broken references.
	if path.Ext(name) != "" {
		fail(w, http.StatusNotFound, "not_found", "no such file")
		return
	}
	// Client-side route: hand the SPA its shell.
	data, err := fs.ReadFile(panel, "index.html")
	if err != nil {
		fail(w, http.StatusNotFound, "not_found", "panel is not built")
		return
	}
	writeStatic(w, "index.html", data)
}

func writeStatic(w http.ResponseWriter, name string, data []byte) {
	w.Header().Set("Content-Type", mimeByExt(name))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(data)
}

func mimeByExt(name string) string {
	switch path.Ext(name) {
	case ".html":
		return "text/html; charset=utf-8"
	case ".js", ".mjs":
		return "text/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".png":
		return "image/png"
	case ".ico":
		return "image/x-icon"
	case ".woff2":
		return "font/woff2"
	default:
		return "application/octet-stream"
	}
}
