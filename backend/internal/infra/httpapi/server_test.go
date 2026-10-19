package httpapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/delapaska/aibot/internal/app/port/fakes"
	"github.com/delapaska/aibot/internal/app/usecase"
)

// testNow is the fixed family time used across the tests (Moscow noon).
var testNow = time.Date(2026, 10, 18, 12, 0, 0, 0, mustLoc("Europe/Moscow"))

func mustLoc(name string) *time.Location {
	tz, err := time.LoadLocation(name)
	if err != nil {
		panic("bad test location " + name)
	}
	return tz
}

// newTestServer builds a Server on fake ports and returns the fakes for
// assertions. No panel token: the API is open, as the task requires.
func newTestServer(t *testing.T) (*Server, *fakes.Expenses, *fakes.Users) {
	t.Helper()
	return newTestServerToken(t, "")
}

// newTestServerAll builds a Server with every service (expenses, users,
// debts) on fake ports and returns the fakes for assertions.
func newTestServerAll(t *testing.T, token string) (*Server, *fakes.Expenses, *fakes.Users, *fakes.Debts) {
	t.Helper()
	expenses := fakes.NewExpenses()
	users := fakes.NewUsers()
	debts := fakes.NewDebts()
	svc := usecase.NewExpenseService(expenses, users, fakes.Clock{T: testNow})
	usvc := usecase.NewUserService(users, fakes.Clock{T: testNow})
	dsvc := usecase.NewDebtService(debts, users, fakes.Clock{T: testNow})
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewServer(svc, usvc, dsvc, logger, token), expenses, users, debts
}

// newTestServerToken builds a Server with an optional bearer token.
func newTestServerToken(t *testing.T, token string) (*Server, *fakes.Expenses, *fakes.Users) {
	t.Helper()
	srv, expenses, users, _ := newTestServerAll(t, token)
	return srv, expenses, users
}

// do runs a request against the server handler and returns the response.
func do(t *testing.T, h http.Handler, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, rdr)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// httptestDo is like do but accepts a ready request (for custom headers).
func httptestDo(t *testing.T, h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// errEnvelope mirrors the error response shape.
type errEnvelope struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func decodeErr(t *testing.T, rec *httptest.ResponseRecorder) errEnvelope {
	t.Helper()
	var env errEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("error body is not JSON: %v; body: %s", err, rec.Body.String())
	}
	return env
}

func TestHealthz(t *testing.T) {
	srv, _, _ := newTestServer(t)
	rec := do(t, srv.Handler(), "GET", "/api/v1/healthz", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body: %v", err)
	}
	if body.Status != "ok" {
		t.Errorf("status = %q, want ok", body.Status)
	}
}

func TestUnknownPathIsJSON404(t *testing.T) {
	srv, _, _ := newTestServer(t)
	rec := do(t, srv.Handler(), "GET", "/api/v1/nope", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if env := decodeErr(t, rec); env.Error.Code != "not_found" {
		t.Errorf("code = %q, want not_found", env.Error.Code)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	srv, _, _ := newTestServer(t)
	rec := do(t, srv.Handler(), "POST", "/api/v1/users", "")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestRecoverTurnsPanicInto500(t *testing.T) {
	// A repository that panics: the middleware must turn it into a 500
	// envelope instead of killing the connection.
	srv, _, _ := newTestServer(t)
	expenses := fakes.NewExpenses()
	users := fakes.NewUsers()
	svc := usecase.NewExpenseService(panickingRepo{Expenses: expenses}, users, fakes.Clock{T: testNow})
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv = NewServer(svc, usecase.NewUserService(users, fakes.Clock{T: testNow}), nil, logger, "")

	rec := do(t, srv.Handler(), "GET", "/api/v1/expenses", "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if env := decodeErr(t, rec); env.Error.Code != "internal" {
		t.Errorf("code = %q, want internal", env.Error.Code)
	}
}
