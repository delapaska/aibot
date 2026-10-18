package domain

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// User is a family member known to the system (a Telegram user).
type User struct {
	ID        int64 // Telegram user ID
	FirstName string
	Username  string
	CreatedAt time.Time
}

// MaxNameLen caps Telegram names, which can be arbitrarily long.
const MaxNameLen = 128

// NewUser validates the input and assembles a user. Telegram always sends
// a first name, but the domain tolerates empty names: the panel falls back
// to the username or the raw ID when displaying.
func NewUser(id int64, firstName, username string, now time.Time) (User, error) {
	u := User{
		ID:        id,
		FirstName: strings.TrimSpace(firstName),
		Username:  strings.TrimSpace(username),
		CreatedAt: now,
	}
	if err := validateUserID(u.ID); err != nil {
		return User{}, err
	}
	if utf8.RuneCountInString(u.FirstName) > MaxNameLen {
		return User{}, fmt.Errorf("%w: first name longer than %d runes", ErrInvalidName, MaxNameLen)
	}
	if utf8.RuneCountInString(u.Username) > MaxNameLen {
		return User{}, fmt.Errorf("%w: username longer than %d runes", ErrInvalidName, MaxNameLen)
	}
	return u, nil
}

// DisplayName picks the best available label for the panel.
func (u User) DisplayName() string {
	switch {
	case u.FirstName != "":
		return u.FirstName
	case u.Username != "":
		return u.Username
	default:
		return fmt.Sprintf("id%d", u.ID)
	}
}
