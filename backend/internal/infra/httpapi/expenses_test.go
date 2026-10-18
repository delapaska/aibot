package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/delapaska/aibot/internal/app/port"
	"github.com/delapaska/aibot/internal/app/port/fakes"
	"github.com/delapaska/aibot/internal/domain"
)

// seedExpense creates one expense through the public API (so the tests
// exercise the whole path) and returns its id.
func seedExpense(t *testing.T, h http.Handler, userID int64, amount, cat, date string) string {
	t.Helper()
	body := fmt.Sprintf(`{"user_id":%d,"amount":%q,"category":%q,"spent_at":%q,"comment":"кофе"}`,
		userID, amount, cat, date)
	rec := do(t, h, "POST", "/api/v1/expenses", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("seed: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("seed body: %v", err)
	}
	return got.ID
}

func TestCreateExpense(t *testing.T) {
	srv, expenses, users := newTestServer(t)
	h := srv.Handler()

	rec := do(t, h, "POST", "/api/v1/expenses",
		`{"user_id":42,"amount":"350.13","category":"food","spent_at":"2026-10-17","comment":"  кофе  "}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("content-type = %q", ct)
	}

	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body: %v", err)
	}
	if got["user_id"].(float64) != 42 || got["amount"] != "350.13" ||
		got["category"] != "food" || got["spent_at"] != "2026-10-17" || got["comment"] != "кофе" {
		t.Errorf("body = %v", got)
	}
	id, _ := got["id"].(string)
	if id == "" {
		t.Error("id must be assigned")
	}
	// The owner is registered automatically.
	if users.Len() != 1 {
		t.Errorf("users = %d, want 1", users.Len())
	}
	if expenses.Len() != 1 {
		t.Errorf("expenses = %d, want 1", expenses.Len())
	}
}

func TestCreateValidation(t *testing.T) {
	cases := []struct {
		name, body, wantCode string
	}{
		{"bad amount", `{"user_id":1,"amount":"abc","category":"food","spent_at":"2026-10-17"}`, "invalid_amount"},
		{"zero amount", `{"user_id":1,"amount":"0","category":"food","spent_at":"2026-10-17"}`, "invalid_amount"},
		{"bad category", `{"user_id":1,"amount":"10.00","category":"cafe","spent_at":"2026-10-17"}`, "invalid_category"},
		{"bad date", `{"user_id":1,"amount":"10.00","category":"food","spent_at":"2026-13-40"}`, "invalid_date"},
		{"future date", `{"user_id":1,"amount":"10.00","category":"food","spent_at":"2027-01-01"}`, "invalid_date"},
		{"zero user", `{"user_id":0,"amount":"10.00","category":"food","spent_at":"2026-10-17"}`, "invalid_user_id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, expenses, _ := newTestServer(t)
			rec := do(t, srv.Handler(), "POST", "/api/v1/expenses", tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
			}
			if env := decodeErr(t, rec); env.Error.Code != tc.wantCode {
				t.Errorf("code = %q, want %q", env.Error.Code, tc.wantCode)
			}
			if expenses.Len() != 0 {
				t.Error("rejected input must not be stored")
			}
		})
	}
}

func TestCreateInvalidJSON(t *testing.T) {
	srv, _, _ := newTestServer(t)
	rec := do(t, srv.Handler(), "POST", "/api/v1/expenses", `{"user_id":1,`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
	if env := decodeErr(t, rec); env.Error.Code != "invalid_json" {
		t.Errorf("code = %q, want invalid_json", env.Error.Code)
	}
}

func TestCreateUnknownFieldRejected(t *testing.T) {
	srv, _, _ := newTestServer(t)
	rec := do(t, srv.Handler(), "POST", "/api/v1/expenses",
		`{"user_id":1,"amount":"10.00","category":"food","spent_at":"2026-10-17","extra":true}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestListExpensesFilters(t *testing.T) {
	srv, _, _ := newTestServer(t)
	h := srv.Handler()

	seedExpense(t, h, 42, "350.00", "food", "2026-10-10")
	seedExpense(t, h, 42, "120.00", "transport", "2026-10-17")
	seedExpense(t, h, 7, "990.00", "food", "2026-10-16")

	cases := []struct {
		name, query   string
		wantTotal     int
		wantLen       int
		wantFirstUser int64
	}{
		{"no filter", "", 3, 3, 42},
		{"by user", "?user_id=42", 2, 2, 42},
		{"by category", "?category=food", 2, 2, 7},
		{"by user+category", "?user_id=42&category=transport", 1, 1, 42},
		{"by period", "?from=2026-10-16&to=2026-10-17", 2, 2, 42},
		{"paging", "?limit=1&offset=1", 3, 1, 7},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := do(t, h, "GET", "/api/v1/expenses"+tc.query, "")
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
			}
			var got struct {
				Items []struct {
					ID     string `json:"id"`
					UserID int64  `json:"user_id"`
				} `json:"items"`
				Total int `json:"total"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("body: %v", err)
			}
			if got.Total != tc.wantTotal || len(got.Items) != tc.wantLen {
				t.Fatalf("total = %d, items = %d, want %d/%d", got.Total, len(got.Items), tc.wantTotal, tc.wantLen)
			}
			if len(got.Items) > 0 && got.Items[0].UserID != tc.wantFirstUser {
				t.Errorf("first item user = %d, want %d (newest first)", got.Items[0].UserID, tc.wantFirstUser)
			}
		})
	}
}

func TestListInvalidQuery(t *testing.T) {
	cases := []struct{ name, query string }{
		{"bad user", "?user_id=abc"},
		{"bad date", "?from=17.10.2026"},
		{"bad category", "?category=cafe"},
		{"bad limit", "?limit=many"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, _, _ := newTestServer(t)
			rec := do(t, srv.Handler(), "GET", "/api/v1/expenses"+tc.query, "")
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d", rec.Code)
			}
			if env := decodeErr(t, rec); env.Error.Code != "invalid_query" {
				t.Errorf("code = %q, want invalid_query", env.Error.Code)
			}
		})
	}
}

func TestGetExpense(t *testing.T) {
	srv, _, _ := newTestServer(t)
	h := srv.Handler()
	id := seedExpense(t, h, 42, "350.00", "food", "2026-10-17")

	rec := do(t, h, "GET", "/api/v1/expenses/"+id, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["id"] != id || got["user_id"].(float64) != 42 || got["amount"] != "350.00" {
		t.Errorf("body = %v", got)
	}

	rec = do(t, h, "GET", "/api/v1/expenses/00000000-0000-0000-0000-000000000000", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing status = %d, want 404", rec.Code)
	}
	if env := decodeErr(t, rec); env.Error.Code != "not_found" {
		t.Errorf("code = %q, want not_found", env.Error.Code)
	}
}

func TestUpdateExpense(t *testing.T) {
	srv, _, _ := newTestServer(t)
	h := srv.Handler()
	id := seedExpense(t, h, 42, "350.00", "food", "2026-10-17")

	// Partial update: only amount changes, comment is cleared.
	rec := do(t, h, "PATCH", "/api/v1/expenses/"+id, `{"amount":"400.00","comment":""}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["amount"] != "400.00" || got["comment"] != "" || got["category"] != "food" {
		t.Errorf("body = %v", got)
	}
	if got["spent_at"] != "2026-10-17" {
		t.Error("unspecified fields must stay unchanged")
	}

	// Invalid value is rejected and the entity is untouched.
	rec = do(t, h, "PATCH", "/api/v1/expenses/"+id, `{"amount":"-5"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid status = %d", rec.Code)
	}
	if env := decodeErr(t, rec); env.Error.Code != "invalid_amount" {
		t.Errorf("code = %q", env.Error.Code)
	}
	rec = do(t, h, "GET", "/api/v1/expenses/"+id, "")
	var after map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &after); err != nil {
		t.Fatal(err)
	}
	if after["amount"] != "400.00" {
		t.Errorf("amount = %v, want 400.00 kept", after["amount"])
	}

	// Unknown id → 404.
	rec = do(t, h, "PATCH", "/api/v1/expenses/00000000-0000-0000-0000-000000000000", `{"amount":"1.00"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing status = %d, want 404", rec.Code)
	}
}

func TestDeleteExpense(t *testing.T) {
	srv, _, _ := newTestServer(t)
	h := srv.Handler()
	id := seedExpense(t, h, 42, "350.00", "food", "2026-10-17")

	rec := do(t, h, "DELETE", "/api/v1/expenses/"+id, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	// Second delete → 404.
	rec = do(t, h, "DELETE", "/api/v1/expenses/"+id, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("second delete status = %d, want 404", rec.Code)
	}
}

func TestListOnEmptyDB(t *testing.T) {
	srv, _, _ := newTestServer(t)
	rec := do(t, srv.Handler(), "GET", "/api/v1/expenses", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var got struct {
		Items []any `json:"items"`
		Total int   `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Total != 0 || got.Items == nil || len(got.Items) != 0 {
		t.Errorf("got %+v, want empty non-null items", got)
	}
}

// panickingRepo is a repository whose List always panics; it is used to
// check that the recovery middleware converts a panic into a 500.
type panickingRepo struct {
	*fakes.Expenses
}

func (panickingRepo) List(_ context.Context, _ port.ExpenseFilter) ([]domain.Expense, int, error) {
	panic("boom")
}
