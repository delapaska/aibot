package domain

import (
	"fmt"
	"time"
)

const dateLayout = "2006-01-02"

// Date is a calendar date without a time or zone part. Internally it is
// the UTC midnight of that day, which keeps comparisons and storage simple;
// which day an instant falls on is always decided by the family time zone
// (see DateFromTime).
type Date struct{ t time.Time }

// DateFromTime converts an instant to the calendar date it falls on in loc.
func DateFromTime(t time.Time, loc *time.Location) Date {
	y, m, d := t.In(loc).Date()
	return Date{t: time.Date(y, m, d, 0, 0, 0, 0, time.UTC)}
}

// ParseDate parses a date in "YYYY-MM-DD" form.
func ParseDate(s string) (Date, error) {
	t, err := time.Parse(dateLayout, s)
	if err != nil {
		return Date{}, fmt.Errorf("%w: %q: %v", ErrInvalidDate, s, err)
	}
	return Date{t: t}, nil
}

// String renders the date as "YYYY-MM-DD".
func (d Date) String() string { return d.t.Format(dateLayout) }

// Time returns the underlying UTC midnight instant.
func (d Date) Time() time.Time { return d.t }

// Before reports whether d is strictly before other.
func (d Date) Before(other Date) bool { return d.t.Before(other.t) }

// After reports whether d is strictly after other.
func (d Date) After(other Date) bool { return d.t.After(other.t) }
