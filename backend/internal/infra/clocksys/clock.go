// Package clocksys provides the system clock adapter for the Clock port.
package clocksys

import "time"

// Clock returns the wall clock time in the family time zone. Domain code
// requires "now" to carry the family location, so relative-date logic
// (today, yesterday) resolves against the configured time zone, not the
// server's.
type Clock struct {
	loc *time.Location
}

// New builds a wall clock bound to loc.
func New(loc *time.Location) Clock { return Clock{loc: loc} }

// Now implements port.Clock.
func (c Clock) Now() time.Time { return time.Now().In(c.loc) }
