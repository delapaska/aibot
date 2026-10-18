package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestNewUserValid(t *testing.T) {
	u, err := NewUser(42, " Аня ", "anya", testNow)
	if err != nil {
		t.Fatalf("NewUser() unexpected error: %v", err)
	}
	if u.ID != 42 || u.FirstName != "Аня" || u.Username != "anya" || !u.CreatedAt.Equal(testNow) {
		t.Errorf("NewUser() = %+v, want id=42 name=%q username=%q", u, "Аня", "anya")
	}
}

func TestNewUserEmptyNamesAllowed(t *testing.T) {
	u, err := NewUser(42, "", "", testNow)
	if err != nil {
		t.Fatalf("NewUser() unexpected error: %v", err)
	}
	if u.FirstName != "" || u.Username != "" {
		t.Errorf("names must stay empty, got %+v", u)
	}
}

func TestNewUserValidation(t *testing.T) {
	tests := []struct {
		name      string
		id        int64
		firstName string
		username  string
		wantErr   error
	}{
		{"zero id", 0, "Аня", "anya", ErrInvalidUserID},
		{"negative id", -1, "Аня", "anya", ErrInvalidUserID},
		{"first name too long", 42, strings.Repeat("А", MaxNameLen+1), "anya", ErrInvalidName},
		{"username too long", 42, "Аня", strings.Repeat("b", MaxNameLen+1), ErrInvalidName},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewUser(tt.id, tt.firstName, tt.username, testNow)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("NewUser() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}
