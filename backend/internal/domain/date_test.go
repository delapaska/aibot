package domain

import (
	"errors"
	"testing"
	"time"
)

var moscow = mustLocation("Europe/Moscow")

func mustLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

func TestParseDate(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr error
	}{
		{"iso date", "2026-10-18", "2026-10-18", nil},
		{"feb 30 rejected", "2026-02-30", "", ErrInvalidDate},
		{"wrong format", "18.10.2026", "", ErrInvalidDate},
		{"empty", "", "", ErrInvalidDate},
		{"garbage", "yesterday", "", ErrInvalidDate},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, err := ParseDate(tt.in)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("ParseDate(%q) error = %v, want %v", tt.in, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseDate(%q) unexpected error: %v", tt.in, err)
			}
			if d.String() != tt.want {
				t.Errorf("ParseDate(%q) = %s, want %s", tt.in, d.String(), tt.want)
			}
		})
	}
}

func TestDateFromTimeRespectsLocation(t *testing.T) {
	// 02:30 in Moscow on Oct 18 is 23:30 UTC on Oct 17 — the calendar date
	// must follow the family time zone, not UTC.
	msk := time.Date(2026, 10, 18, 2, 30, 0, 0, moscow)

	if got := DateFromTime(msk, moscow).String(); got != "2026-10-18" {
		t.Errorf("DateFromTime(msk, Moscow) = %s, want 2026-10-18", got)
	}
	if got := DateFromTime(msk, time.UTC).String(); got != "2026-10-17" {
		t.Errorf("DateFromTime(msk, UTC) = %s, want 2026-10-17", got)
	}
}

func TestDateRoundTrip(t *testing.T) {
	d, err := ParseDate("2026-10-18")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Converting through a location must not shift the calendar date.
	back := DateFromTime(d.Time().In(moscow), moscow)
	if back.String() != "2026-10-18" {
		t.Errorf("round trip = %s, want 2026-10-18", back.String())
	}
}

func TestDateComparisons(t *testing.T) {
	a, _ := ParseDate("2026-10-17")
	b, _ := ParseDate("2026-10-18")

	if !a.Before(b) {
		t.Error("2026-10-17 should be before 2026-10-18")
	}
	if !b.After(a) {
		t.Error("2026-10-18 should be after 2026-10-17")
	}
	if a.After(a) || b.Before(b) {
		t.Error("equal dates must not be before/after each other")
	}
}
