package domain

import (
	"errors"
	"math"
	"testing"
)

func TestKopecksFromRubles(t *testing.T) {
	tests := []struct {
		name    string
		rubles  float64
		want    Kopecks
		wantErr error
	}{
		{"integer rubles", 350, 35000, nil},
		{"exact kopecks", 349.99, 34999, nil},
		{"rounding half away from zero", 349.995, 35000, nil},
		{"sub-kopeck rounding", 0.005, 1, nil},
		{"negative is convertible", -10.5, -1050, nil},
		{"NaN rejected", math.NaN(), 0, ErrInvalidAmount},
		{"+Inf rejected", math.Inf(1), 0, ErrInvalidAmount},
		{"overflow rejected", 1e30, 0, ErrInvalidAmount},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := KopecksFromRubles(tt.rubles)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("KopecksFromRubles(%v) error = %v, want %v", tt.rubles, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("KopecksFromRubles(%v) unexpected error: %v", tt.rubles, err)
			}
			if got != tt.want {
				t.Errorf("KopecksFromRubles(%v) = %v, want %v", tt.rubles, got, tt.want)
			}
		})
	}
}

func TestKopecksString(t *testing.T) {
	tests := []struct {
		k    Kopecks
		want string
	}{
		{35000, "350.00"},
		{5, "0.05"},
		{35050, "350.50"},
		{0, "0.00"},
		{-5, "-0.05"},
		{123456789, "1234567.89"},
	}
	for _, tt := range tests {
		if got := tt.k.String(); got != tt.want {
			t.Errorf("Kopecks(%d).String() = %q, want %q", tt.k, got, tt.want)
		}
	}
}

func TestKopecksRubles(t *testing.T) {
	tests := []struct {
		k    Kopecks
		want float64
	}{
		{35000, 350},
		{1, 0.01},
		{-1050, -10.5},
	}
	for _, tt := range tests {
		if got := tt.k.Rubles(); got != tt.want {
			t.Errorf("Kopecks(%d).Rubles() = %v, want %v", tt.k, got, tt.want)
		}
	}
}
