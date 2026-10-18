package port

import "time"

// Clock supplies the current time to the use cases. The returned time must
// carry the family time zone, so relative dates ("yesterday") resolve
// consistently. Tests replace it with a fixed clock.
type Clock interface {
	Now() time.Time
}
