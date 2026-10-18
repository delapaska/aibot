package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/delapaska/aibot/internal/app/port"
	"github.com/delapaska/aibot/internal/app/usecase"
	"github.com/delapaska/aibot/internal/domain"
)

// maxBodyBytes bounds request bodies: JSON payloads here are tiny, and
// an explicit cap turns oversized bodies into a clean 400 instead of a
// memory problem.
const maxBodyBytes = 1 << 20 // 1 MiB

// expenseDTO is the wire format of an expense. The amount travels as a
// decimal string ("350.13"), never as a float: JSON numbers lose
// precision on money.
type expenseDTO struct {
	ID        string    `json:"id"`
	UserID    int64     `json:"user_id"`
	Amount    string    `json:"amount"`
	Category  string    `json:"category"`
	SpentAt   string    `json:"spent_at"`
	Comment   string    `json:"comment"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func toExpenseDTO(e domain.Expense) expenseDTO {
	return expenseDTO{
		ID:        e.ID,
		UserID:    e.UserID,
		Amount:    e.Amount.String(),
		Category:  string(e.Category),
		SpentAt:   e.SpentAt.String(),
		Comment:   e.Comment,
		CreatedAt: e.CreatedAt,
		UpdatedAt: e.UpdatedAt,
	}
}

func (s *Server) handleCreateExpense(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID   int64  `json:"user_id"`
		Amount   string `json:"amount"`
		Category string `json:"category"`
		SpentAt  string `json:"spent_at"`
		Comment  string `json:"comment"`
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
		fail(w, http.StatusBadRequest, "invalid_amount", err.Error())
		return
	}
	cat, err := domain.ParseCategory(req.Category)
	if err != nil {
		fail(w, http.StatusBadRequest, "invalid_category", err.Error())
		return
	}
	spentAt, err := domain.ParseDate(req.SpentAt)
	if err != nil {
		fail(w, http.StatusBadRequest, "invalid_date", err.Error())
		return
	}

	e, err := s.expenses.Create(r.Context(), usecase.CreateInput{
		UserID:   req.UserID,
		Amount:   kop,
		Category: cat,
		SpentAt:  spentAt,
		Comment:  req.Comment,
	})
	if err != nil {
		s.failErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toExpenseDTO(e))
}

func (s *Server) handleListExpenses(w http.ResponseWriter, r *http.Request) {
	filter, err := parseListQuery(r)
	if err != nil {
		fail(w, http.StatusBadRequest, "invalid_query", err.Error())
		return
	}
	items, total, err := s.expenses.List(r.Context(), filter)
	if err != nil {
		s.failErr(w, err)
		return
	}
	dto := make([]expenseDTO, len(items))
	for i, e := range items {
		dto[i] = toExpenseDTO(e)
	}
	writeJSON(w, http.StatusOK, listExpensesResponse{Items: dto, Total: total})
}

type listExpensesResponse struct {
	Items []expenseDTO `json:"items"`
	Total int          `json:"total"`
}

// parseListQuery validates the filters. Bad values are rejected with an
// error rather than silently ignored: a typo in a filter must not look
// like "no expenses found".
func parseListQuery(r *http.Request) (port.ExpenseFilter, error) {
	q := r.URL.Query()
	var f port.ExpenseFilter

	if v := q.Get("user_id"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil || id <= 0 {
			return f, errors.New("user_id must be a positive integer")
		}
		f.UserID = &id
	}
	for name, dst := range map[string]**domain.Date{
		"from": &f.From,
		"to":   &f.To,
	} {
		if v := q.Get(name); v != "" {
			d, err := domain.ParseDate(v)
			if err != nil {
				return f, errors.New(name + " must be YYYY-MM-DD")
			}
			*dst = &d
		}
	}
	if v := q.Get("category"); v != "" {
		cat, err := domain.ParseCategory(v)
		if err != nil {
			return f, errors.New("unknown category")
		}
		f.Category = &cat
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return f, errors.New("limit must be a non-negative integer")
		}
		f.Limit = n
	}
	if v := q.Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return f, errors.New("offset must be a non-negative integer")
		}
		f.Offset = n
	}
	return f, nil
}

func (s *Server) handleGetExpense(w http.ResponseWriter, r *http.Request) {
	e, err := s.expenses.Get(r.Context(), nil, r.PathValue("id"))
	if err != nil {
		s.failErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toExpenseDTO(e))
}

func (s *Server) handleUpdateExpense(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Amount   *string `json:"amount"`
		Category *string `json:"category"`
		SpentAt  *string `json:"spent_at"`
		Comment  *string `json:"comment"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}

	in := usecase.UpdateInput{}
	if req.Amount != nil {
		kop, err := parseAmount(*req.Amount)
		if err != nil {
			fail(w, http.StatusBadRequest, "invalid_amount", err.Error())
			return
		}
		in.Amount = &kop
	}
	if req.Category != nil {
		cat, err := domain.ParseCategory(*req.Category)
		if err != nil {
			fail(w, http.StatusBadRequest, "invalid_category", err.Error())
			return
		}
		in.Category = &cat
	}
	if req.SpentAt != nil {
		d, err := domain.ParseDate(*req.SpentAt)
		if err != nil {
			fail(w, http.StatusBadRequest, "invalid_date", err.Error())
			return
		}
		in.SpentAt = &d
	}

	// An empty comment is a legitimate value: it clears the field. A
	// missing one (nil) leaves it untouched.
	in.Comment = req.Comment

	e, err := s.expenses.Update(r.Context(), nil, r.PathValue("id"), in)
	if err != nil {
		s.failErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toExpenseDTO(e))
}

func (s *Server) handleDeleteExpense(w http.ResponseWriter, r *http.Request) {
	if err := s.expenses.Delete(r.Context(), nil, r.PathValue("id")); err != nil {
		s.failErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// decodeJSON decodes the request body strictly: unknown fields are
// rejected (typos fail loudly instead of being ignored) and the body is
// size-capped. It writes the error response itself and reports whether
// the caller may continue.
func (s *Server) decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			fail(w, http.StatusRequestEntityTooLarge, "body_too_large", "request body exceeds 1 MiB")
			return false
		}
		fail(w, http.StatusBadRequest, "invalid_json", err.Error())
		return false
	}
	return true
}

// parseAmount converts a decimal ruble string ("350.13") to kopecks.
func parseAmount(s string) (domain.Kopecks, error) {
	rub, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0, errors.New("amount must be a decimal number like 350.13")
	}
	return domain.KopecksFromRubles(rub)
}
