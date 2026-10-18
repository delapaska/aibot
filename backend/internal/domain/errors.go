package domain

import "errors"

// Sentinel errors shared across layers. Classify with errors.Is.
var (
	// ErrNotFound is returned when an entity does not exist. Scoping must
	// not leak existence: a foreign user's expense looks the same as a
	// missing one.
	ErrNotFound = errors.New("not found")

	ErrInvalidUserID   = errors.New("invalid user id")
	ErrInvalidName     = errors.New("invalid name")
	ErrInvalidAmount   = errors.New("invalid amount")
	ErrInvalidCategory = errors.New("invalid category")
	ErrInvalidDate     = errors.New("invalid date")
	ErrInvalidComment  = errors.New("invalid comment")
)
