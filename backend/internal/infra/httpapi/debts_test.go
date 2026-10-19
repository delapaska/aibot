package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

// seedDebt creates one debt through the public API and returns its id.
func seedDebt(t *testing.T, h http.Handler, userID int64, debtor, amount, direction string) string {
	t.Helper()
	body := fmt.Sprintf(
		`{"user_id":%d,"debtor":%q,"amount":%q,"direction":%q,"comment":"заём"}`,
		userID, debtor, amount, direction,
	)
	rec := do(t, h, "POST", "/api/v1/debts", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("seed debt: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var created debtDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("seed debt: decode response: %v", err)
	}
	return created.ID
}

func TestCreateDebt(t *testing.T) {
	srv, _, users, debts := newTestServerAll(t, "")
	h := srv.Handler()

	rec := do(t, h, "POST", "/api/v1/debts",
		`{"user_id":42,"debtor":"Егор","amount":"250.00","direction":"i_owe","comment":"заём"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got debtDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.ID == "" {
		t.Error("id is empty")
	}
	if got.UserID != 42 || got.Debtor != "Егор" || got.Direction != "i_owe" ||
		got.Amount != "250.00" || got.Comment != "заём" {
		t.Errorf("dto = %+v", got)
	}
	if got.SettledAt != nil {
		t.Errorf("settled_at = %v, want nil", got.SettledAt)
	}
	// The owner is auto-registered so the user appears in the panel.
	if users.Len() != 1 {
		t.Errorf("users.Len() = %d, want 1", users.Len())
	}
	if debts.Len() != 1 {
		t.Errorf("debts.Len() = %d, want 1", debts.Len())
	}
}

func TestCreateDebtDefaultsToOwesMe(t *testing.T) {
	srv, _, _, _ := newTestServerAll(t, "")
	h := srv.Handler()

	rec := do(t, h, "POST", "/api/v1/debts", `{"user_id":42,"debtor":"Егор","amount":"100"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got debtDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Direction != "owes_me" {
		t.Errorf("direction = %q, want owes_me", got.Direction)
	}
}

func TestCreateDebtValidation(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"no user", `{"debtor":"Егор","amount":"100"}`, "invalid_user_id"},
		{"bad user", `{"user_id":-1,"debtor":"Егор","amount":"100"}`, "invalid_user_id"},
		{"no debtor", `{"user_id":42,"amount":"100"}`, "invalid_debtor"},
		{"bad amount", `{"user_id":42,"debtor":"Егор","amount":"abc"}`, "invalid_amount"},
		{"negative amount", `{"user_id":42,"debtor":"Егор","amount":"-5"}`, "invalid_amount"},
		{"bad direction", `{"user_id":42,"debtor":"Егор","amount":"100","direction":"lent"}`, "invalid_direction"},
	}
	srv, _, _, _ := newTestServerAll(t, "")
	h := srv.Handler()

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := do(t, h, "POST", "/api/v1/debts", tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
			}
			if env := decodeErr(t, rec); env.Error.Code != tc.want {
				t.Errorf("code = %q, want %q", env.Error.Code, tc.want)
			}
		})
	}
}

func TestListDebts(t *testing.T) {
	srv, _, _, _ := newTestServerAll(t, "")
	h := srv.Handler()

	seedDebt(t, h, 42, "Егор", "250.00", "owes_me")
	seedDebt(t, h, 42, "Маша", "300.00", "i_owe")
	seedDebt(t, h, 43, "Егор", "10.00", "owes_me")

	// Without filters: every debt.
	rec := do(t, h, "GET", "/api/v1/debts", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var all debtsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &all); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(all.Items) != 3 {
		t.Fatalf("all items = %d, want 3", len(all.Items))
	}

	// Scoped to one user.
	rec = do(t, h, "GET", "/api/v1/debts?user_id=42", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var page debtsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("user 42 items = %d, want 2", len(page.Items))
	}
	for _, d := range page.Items {
		if d.UserID != 42 {
			t.Errorf("item user_id = %d, want 42", d.UserID)
		}
	}
}

func TestListDebtsOpenFilter(t *testing.T) {
	srv, _, _, _ := newTestServerAll(t, "")
	h := srv.Handler()

	id := seedDebt(t, h, 42, "Егор", "250.00", "owes_me")
	seedDebt(t, h, 42, "Маша", "300.00", "i_owe")

	if rec := do(t, h, "POST", "/api/v1/debts/"+id+"/settle", ""); rec.Code != http.StatusOK {
		t.Fatalf("settle: status = %d, body = %s", rec.Code, rec.Body.String())
	}

	rec := do(t, h, "GET", "/api/v1/debts?user_id=42&open=true", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var page debtsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("open items = %d, want 1", len(page.Items))
	}
	if page.Items[0].Debtor != "Маша" {
		t.Errorf("debtor = %q, want Маша", page.Items[0].Debtor)
	}
}

func TestListDebtsBadQuery(t *testing.T) {
	srv, _, _, _ := newTestServerAll(t, "")
	h := srv.Handler()

	for _, target := range []string{"/api/v1/debts?user_id=abc", "/api/v1/debts?open=maybe"} {
		rec := do(t, h, "GET", target, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, body = %s", target, rec.Code, rec.Body.String())
		}
		if env := decodeErr(t, rec); env.Error.Code != "invalid_query" {
			t.Errorf("code = %q, want invalid_query", env.Error.Code)
		}
	}
}

func TestSettleDebt(t *testing.T) {
	srv, _, _, _ := newTestServerAll(t, "")
	h := srv.Handler()

	id := seedDebt(t, h, 42, "Егор", "250.00", "owes_me")

	rec := do(t, h, "POST", "/api/v1/debts/"+id+"/settle", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got debtDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.SettledAt == nil {
		t.Error("settled_at is nil after settle")
	}

	// A second settle conflicts.
	rec = do(t, h, "POST", "/api/v1/debts/"+id+"/settle", "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("second settle: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if env := decodeErr(t, rec); env.Error.Code != "already_settled" {
		t.Errorf("code = %q, want already_settled", env.Error.Code)
	}
}

func TestSettleDebtNotFound(t *testing.T) {
	srv, _, _, _ := newTestServerAll(t, "")
	h := srv.Handler()

	rec := do(t, h, "POST", "/api/v1/debts/no-such-debt/settle", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if env := decodeErr(t, rec); env.Error.Code != "not_found" {
		t.Errorf("code = %q, want not_found", env.Error.Code)
	}
}

func TestDebtsMethodNotAllowed(t *testing.T) {
	srv, _, _, _ := newTestServerAll(t, "")
	h := srv.Handler()

	rec := do(t, h, "PATCH", "/api/v1/debts", "")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
	if allow := rec.Header().Get("Allow"); allow == "" {
		t.Error("Allow header is empty")
	}

	rec = do(t, h, "DELETE", "/api/v1/debts/debt-1/settle", "")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
	if allow := rec.Header().Get("Allow"); allow == "" {
		t.Error("Allow header is empty")
	}
}
