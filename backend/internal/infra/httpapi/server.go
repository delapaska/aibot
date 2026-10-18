// Package httpapi serves the REST API of the web panel.
//
// The API works with the whole family (the head-of-family view): the
// user_id of an expense is chosen in the request, while the bot adapters
// always scope by their authenticated user. Authentication is optional
// (PANEL_TOKEN): the task requires the panel to work without it.
package httpapi

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/delapaska/aibot/internal/app/usecase"
)

// Server holds the dependencies of the REST API.
type Server struct {
	expenses   *usecase.ExpenseService
	users      *usecase.UserService
	logger     *slog.Logger
	panelToken string
}

// NewServer assembles the API. panelToken empty = open API.
func NewServer(expenses *usecase.ExpenseService, users *usecase.UserService, logger *slog.Logger, panelToken string) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{
		expenses:   expenses,
		users:      users,
		logger:     logger,
		panelToken: panelToken,
	}
}

// Handler builds the routing table wrapped in middleware. Middleware
// order (outermost first): security headers, panic recovery, access log.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// healthz is open even with a token: it backs the container
	// healthcheck and reveals nothing.
	mux.HandleFunc("GET /api/v1/healthz", s.handleHealth)

	mux.Handle("GET /api/v1/users", s.auth(http.HandlerFunc(s.handleListUsers)))

	mux.Handle("GET /api/v1/expenses", s.auth(http.HandlerFunc(s.handleListExpenses)))
	mux.Handle("POST /api/v1/expenses", s.auth(http.HandlerFunc(s.handleCreateExpense)))
	mux.Handle("GET /api/v1/expenses/{id}", s.auth(http.HandlerFunc(s.handleGetExpense)))
	mux.Handle("PATCH /api/v1/expenses/{id}", s.auth(http.HandlerFunc(s.handleUpdateExpense)))
	mux.Handle("DELETE /api/v1/expenses/{id}", s.auth(http.HandlerFunc(s.handleDeleteExpense)))

	// Everything unmatched lands here: unknown paths get a JSON 404, and
	// known paths hit with the wrong method get a proper 405 + Allow (the
	// catch-all would otherwise swallow the mux's automatic 405).
	mux.HandleFunc("/", s.catchAll)

	return s.securityHeaders(s.recoverer(s.accessLog(mux)))
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// catchAll answers requests that matched no method-specific pattern:
// unknown paths as 404, known paths with a wrong method as 405 + Allow.
func (s *Server) catchAll(w http.ResponseWriter, r *http.Request) {
	if allow, ok := allowedMethods(r.URL.Path); ok {
		w.Header().Set("Allow", strings.Join(allow, ", "))
		fail(w, http.StatusMethodNotAllowed, "method_not_allowed",
			"method "+r.Method+" is not allowed for this endpoint")
		return
	}
	fail(w, http.StatusNotFound, "not_found", "unknown endpoint")
}

// allowedMethods reports which methods a known API path accepts. It
// mirrors the routes registered in Handler; keep them in sync.
func allowedMethods(path string) ([]string, bool) {
	switch path {
	case "/api/v1/users":
		return []string{http.MethodGet}, true
	case "/api/v1/expenses":
		return []string{http.MethodGet, http.MethodPost}, true
	}
	if rest, ok := strings.CutPrefix(path, "/api/v1/expenses/"); ok &&
		rest != "" && !strings.Contains(rest, "/") {
		return []string{http.MethodGet, http.MethodPatch, http.MethodDelete}, true
	}
	return nil, false
}
