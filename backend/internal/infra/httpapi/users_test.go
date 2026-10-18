package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/delapaska/aibot/internal/domain"
)

func TestListUsers(t *testing.T) {
	srv, _, users := newTestServer(t)
	h := srv.Handler()

	// Members are registered through the repository, like the bot would.
	profile := func(id int64) domain.User {
		u, err := domain.NewUser(id, "Аня", "anya", testNow)
		if err != nil {
			t.Fatal(err)
		}
		return u
	}
	for _, id := range []int64{42, 7} {
		if _, err := users.Upsert(context.Background(), profile(id)); err != nil {
			t.Fatal(err)
		}
	}

	rec := do(t, h, "GET", "/api/v1/users", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var got struct {
		Items []struct {
			ID          int64  `json:"id"`
			FirstName   string `json:"first_name"`
			DisplayName string `json:"display_name"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 2 {
		t.Fatalf("users = %d, want 2", len(got.Items))
	}
	if got.Items[0].ID != 7 || got.Items[1].ID != 42 {
		t.Errorf("order = %v/%v, want 7 then 42 (oldest first)", got.Items[0].ID, got.Items[1].ID)
	}
	if got.Items[0].DisplayName == "" {
		t.Error("display_name must always be set")
	}
}
