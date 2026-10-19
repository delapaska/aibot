package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/delapaska/aibot/internal/app/port"
	"github.com/delapaska/aibot/internal/app/usecase"
	"github.com/delapaska/aibot/internal/domain"
)

// debtDTO is the wire format of a debt. The amount travels as a decimal
// string, like expenseDTO, so money never touches floats.
type debtDTO struct {
	ID        string     `json:"id"`
	UserID    int64      `json:"user_id"`
	Debtor    string     `json:"debtor"`
	Direction string     `json:"direction"`
	Amount    string     `json:"amount"`
	Comment   string     `json:"comment"`
	CreatedAt time.Time  `json:"created_at"`
	SettledAt *time.Time `json:"settled_at"`
}

func toDebtDTO(d domain.Debt) debtDTO {
	dto := debtDTO{
		ID:        d.ID,
		UserID:    d.UserID,
		Debtor:    d.Debtor,
		Direction: string(d.Direction),
		Amount:    d.Amount.String(),
		Comment:   d.Comment,
		CreatedAt: d.CreatedAt,
		SettledAt: d.SettledAt,
	}
	return dto
}

type debtsResponse struct {
	Items []debtDTO `json:"items"`
}

// handleCreateDebt registers a debt and auto-registers the owner so they
// appear in the panel.
func (s *Server) handleCreateDebt(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID    int64  `json:"user_id"`
		Debtor    string `json:"debtor"`
		Amount    string `json:"amount"`
		Direction string `json:"direction"`
		Comment   string `json:"comment"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if req.UserID <= 0 {
		fail(w, http.StatusBadRequest, "invalid_user_id", "user_id must be a positive integer")
		return
	}
	kop, err := parseAmount(req.Amount)
	if err != nil {
		fail(w, http.StatusBadRequest, "invalid_amount", "amount must be a decimal number of rubles")
		return
	}
	direction := domain.DebtOwesMe
	if req.Direction != "" {
		d := domain.DebtDirection(req.Direction)
		if d != domain.DebtOwesMe && d != domain.DebtIOwe {
			fail(w, http.StatusBadRequest, "invalid_direction", "direction must be owes_me or i_owe")
			return
		}
		direction = d
	}
	debt, err := s.debts.Create(r.Context(), usecase.DebtInput{
		UserID:    req.UserID,
		Direction: direction,
		Debtor:    req.Debtor,
		Amount:    kop,
		Comment:   req.Comment,
	})
	if err != nil {
		s.failErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toDebtDTO(debt))
}

// handleListDebts returns debts, optionally scoped to a user and to
// open (unsettled) ones only.
func (s *Server) handleListDebts(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var filter port.DebtFilter
	if v := q.Get("user_id"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil || id <= 0 {
			fail(w, http.StatusBadRequest, "invalid_query", "user_id must be a positive integer")
			return
		}
		filter.UserID = &id
	}
	if v := q.Get("open"); v != "" {
		open, err := strconv.ParseBool(v)
		if err != nil {
			fail(w, http.StatusBadRequest, "invalid_query", "open must be true or false")
			return
		}
		filter.Open = &open
	}
	items, err := s.debts.List(r.Context(), filter)
	if err != nil {
		s.failErr(w, err)
		return
	}
	dto := make([]debtDTO, 0, len(items))
	for _, d := range items {
		dto = append(dto, toDebtDTO(d))
	}
	writeJSON(w, http.StatusOK, debtsResponse{Items: dto})
}

// handleSettleDebt closes a debt. The panel's head-of-family view may
// settle anyone's debt, so the call is not scoped to a user.
func (s *Server) handleSettleDebt(w http.ResponseWriter, r *http.Request) {
	debt, err := s.debts.SettleAny(r.Context(), r.PathValue("id"))
	if err != nil {
		s.failErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toDebtDTO(debt))
}
