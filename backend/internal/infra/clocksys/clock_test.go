package clocksys

import (
	"testing"
	"time"
)

func TestNowCarriesLocation(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		t.Fatal(err)
	}
	c := New(loc)

	got := c.Now()
	if got.Location() != loc {
		t.Errorf("location = %v, want Europe/Moscow", got.Location())
	}
	// The wall clock must stay a wall clock: within a minute of real now.
	if diff := time.Since(got); diff < -time.Minute || diff > time.Minute {
		t.Errorf("now = %v, more than a minute away from real time", got)
	}
}
