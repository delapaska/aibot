package httpapi

import (
	"net/http"
	"testing"
)

func TestPanelTokenRequired(t *testing.T) {
	srv, _, _ := newTestServerToken(t, "secret-token")
	h := srv.Handler()

	cases := []struct{ name, auth string }{
		{"no header", ""},
		{"wrong token", "Bearer wrong"},
		{"not bearer", "Basic secret-token"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := http.NewRequest("GET", "/api/v1/expenses", nil)
			if tc.auth != "" {
				req.Header.Set("Authorization", tc.auth)
			}
			rec := httptestDo(t, h, req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", rec.Code)
			}
			if env := decodeErr(t, rec); env.Error.Code != "unauthorized" {
				t.Errorf("code = %q, want unauthorized", env.Error.Code)
			}
		})
	}
}

func TestPanelTokenAccepted(t *testing.T) {
	srv, _, _ := newTestServerToken(t, "secret-token")
	h := srv.Handler()

	req, _ := http.NewRequest("GET", "/api/v1/expenses", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	rec := httptestDo(t, h, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestHealthzStaysOpenWithToken(t *testing.T) {
	srv, _, _ := newTestServerToken(t, "secret-token")
	rec := do(t, srv.Handler(), "GET", "/api/v1/healthz", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz status = %d, want 200", rec.Code)
	}
}

func TestTokenDisabledWhenEmpty(t *testing.T) {
	// Empty token = open API, as the task requires ("no authorization").
	srv, _, _ := newTestServer(t)
	rec := do(t, srv.Handler(), "GET", "/api/v1/expenses", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}
