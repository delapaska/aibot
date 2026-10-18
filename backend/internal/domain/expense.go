package domain

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// Expense is a single family expense record.
//
// For a newly created expense ID is empty and is assigned by storage on
// insert. CreatedAt and UpdatedAt are provided by the caller's clock.
type Expense struct {
	ID        string
	UserID    int64
	Amount    Kopecks
	Category  Category
	SpentAt   Date
	Comment   string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// MaxCommentLen caps the free-text comment in runes.
const MaxCommentLen = 500

// minSpentDate rejects obviously mistyped dates ("1999" from a broken parse).
var minSpentDate = mustDate("2000-01-01")

func mustDate(s string) Date {
	d, err := ParseDate(s)
	if err != nil {
		panic("domain: bad built-in date " + s)
	}
	return d
}

// NewExpense validates the input and assembles a new expense.
// now must carry the family time zone (time.Now().In(loc)).
func NewExpense(userID int64, amount Kopecks, cat Category, spentAt Date, comment string, now time.Time) (Expense, error) {
	comment, err := normalizeComment(comment)
	if err != nil {
		return Expense{}, err
	}
	e := Expense{
		UserID:    userID,
		Amount:    amount,
		Category:  cat,
		SpentAt:   spentAt,
		Comment:   comment,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := e.validate(); err != nil {
		return Expense{}, err
	}
	return e, nil
}

// ApplyUpdate applies the non-nil changes to e. Every change is validated
// before anything is mutated, so a rejected update leaves the entity
// untouched. UpdatedAt is set to now (which must carry the family time zone).
func (e *Expense) ApplyUpdate(amount *Kopecks, cat *Category, spentAt *Date, comment *string, now time.Time) error {
	if amount != nil {
		if err := validateAmount(*amount); err != nil {
			return err
		}
	}
	if cat != nil {
		if err := validateCategory(*cat); err != nil {
			return err
		}
	}
	if spentAt != nil {
		if err := validateSpentAt(*spentAt, now); err != nil {
			return err
		}
	}
	var newComment string
	if comment != nil {
		var err error
		if newComment, err = normalizeComment(*comment); err != nil {
			return err
		}
	}

	if amount != nil {
		e.Amount = *amount
	}
	if cat != nil {
		e.Category = *cat
	}
	if spentAt != nil {
		e.SpentAt = *spentAt
	}
	if comment != nil {
		e.Comment = newComment
	}
	e.UpdatedAt = now
	return nil
}

func (e *Expense) validate() error {
	if err := validateUserID(e.UserID); err != nil {
		return err
	}
	if err := validateAmount(e.Amount); err != nil {
		return err
	}
	if err := validateCategory(e.Category); err != nil {
		return err
	}
	return validateSpentAt(e.SpentAt, e.CreatedAt)
}

func validateUserID(id int64) error {
	if id <= 0 {
		return fmt.Errorf("%w: %d", ErrInvalidUserID, id)
	}
	return nil
}

func validateAmount(k Kopecks) error {
	if k < MinAmount || k > MaxAmount {
		return fmt.Errorf("%w: %s is out of [%s, %s]", ErrInvalidAmount, k, MinAmount, MaxAmount)
	}
	return nil
}

func validateCategory(c Category) error {
	if !c.Valid() {
		return fmt.Errorf("%w: %q", ErrInvalidCategory, c)
	}
	return nil
}

// validateSpentAt rejects dates in the future or before minSpentDate.
// now must carry the family time zone.
func validateSpentAt(d Date, now time.Time) error {
	today := DateFromTime(now, now.Location())
	switch {
	case d.After(today):
		return fmt.Errorf("%w: %s is in the future", ErrInvalidDate, d)
	case d.Before(minSpentDate):
		return fmt.Errorf("%w: %s is too far in the past", ErrInvalidDate, d)
	}
	return nil
}

func normalizeComment(s string) (string, error) {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > MaxCommentLen {
		return "", fmt.Errorf("%w: longer than %d runes", ErrInvalidComment, MaxCommentLen)
	}
	return s, nil
}
