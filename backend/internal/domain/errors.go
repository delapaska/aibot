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
	ErrInvalidDebtor   = errors.New("invalid debtor name")

	// ErrInvalidDirection is returned for an unknown debt direction.
	ErrInvalidDirection = errors.New("invalid debt direction")
	// ErrAlreadySettled is returned when a repaid debt is settled again.
	ErrAlreadySettled = errors.New("debt is already settled")
)
