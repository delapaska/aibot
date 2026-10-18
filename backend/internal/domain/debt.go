package domain

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// DebtDirection says which side of the debt the record owner is on:
// DebtOwesMe — the other person owes the owner, DebtIOwe — the owner owes.
type DebtDirection string

const (
	DebtOwesMe DebtDirection = "owes_me"
	DebtIOwe   DebtDirection = "i_owe"
)

// Valid reports whether d is one of the known directions.
func (d DebtDirection) Valid() bool {
	return d == DebtOwesMe || d == DebtIOwe
}

// MaxDebtorLen caps the free-text counterparty name in runes.
const MaxDebtorLen = 100

// Debt is one loan record between the record owner and another person.
// The counterparty is free text (the family may lend to people who never
// talked to the bot), so debts never reference another user's id.
type Debt struct {
	ID        string
	UserID    int64 // the record owner (creditor for owes_me, debtor for i_owe)
	Direction DebtDirection
	Debtor    string // the other party's name
	Amount    Kopecks
	Comment   string
	CreatedAt time.Time
	SettledAt *time.Time // nil while the debt is open
}

// NewDebt validates the input and assembles a new open debt.
// now must carry the family time zone (time.Now().In(loc)).
func NewDebt(userID int64, dir DebtDirection, debtor string, amount Kopecks, comment string, now time.Time) (Debt, error) {
	debtor = strings.TrimSpace(debtor)
	comment, err := normalizeComment(comment)
	switch {
	case err != nil:
		return Debt{}, err
	case userID <= 0:
		return Debt{}, fmt.Errorf("%w: %d", ErrInvalidUserID, userID)
	case !dir.Valid():
		return Debt{}, fmt.Errorf("%w: %q", ErrInvalidDirection, dir)
	case debtor == "" || utf8.RuneCountInString(debtor) > MaxDebtorLen:
		return Debt{}, fmt.Errorf("%w: %q", ErrInvalidDebtor, debtor)
	case amount < MinAmount || amount > MaxAmount:
		return Debt{}, fmt.Errorf("%w: %s is out of [%s, %s]", ErrInvalidAmount, amount, MinAmount, MaxAmount)
	}
	return Debt{
		UserID:    userID,
		Direction: dir,
		Debtor:    debtor,
		Amount:    amount,
		Comment:   comment,
		CreatedAt: now,
	}, nil
}

// Settle marks the debt repaid at now. A debt settles once: a second call
// is ErrAlreadySettled.
func (d *Debt) Settle(now time.Time) error {
	if d.SettledAt != nil {
		return ErrAlreadySettled
	}
	d.SettledAt = &now
	return nil
}
