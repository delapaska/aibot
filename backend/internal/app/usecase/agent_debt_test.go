package usecase

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/delapaska/aibot/internal/app/port"
	"github.com/delapaska/aibot/internal/app/port/fakes"
	"github.com/delapaska/aibot/internal/domain"
)

// newFullAgent builds an agent wired to debt and budget fakes too.
func newFullAgent(t *testing.T) (*Agent, *fakes.LLM, *fakes.Expenses, *fakes.Debts, *fakes.Budgets) {
	t.Helper()
	expenses := fakes.NewExpenses()
	debts := fakes.NewDebts()
	budgets := fakes.NewBudgets()
	users := fakes.NewUsers()
	clock := fakes.Clock{T: now}
	llm := &fakes.LLM{}
	expenseSvc := NewExpenseService(expenses, users, clock)
	agent := NewAgent(
		llm,
		expenseSvc,
		NewDebtService(debts, users, clock),
		NewBudgetService(budgets, expenseSvc, clock),
		clock,
	)
	return agent, llm, expenses, debts, budgets
}

func TestAgentAddDebtFlow(t *testing.T) {
	agent, llm, _, debts, _ := newFullAgent(t)
	llm.Queue = []port.ChatResponse{
		{ToolCalls: []port.ToolCall{{
			ID:       "c1",
			Name:     "add_debt",
			ArgsJSON: `{"debtor":"Егор","amount_rubles":500,"comment":"до пятницы"}`,
		}}},
		{Content: "Записала: Егор должен тебе 500 ₽"},
	}

	if _, err := agent.Handle(ctx, 42, "Егор взял у меня в долг 500, запиши на него"); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	stored, ok := debts.Peek("debt-1")
	if !ok {
		t.Fatal("debt was not stored")
	}
	// The record belongs to the authenticated user; the model never picks
	// the owner, so "запиши на него" cannot point the debt at Егор.
	if stored.UserID != 42 || stored.Direction != domain.DebtOwesMe || stored.Debtor != "Егор" || stored.Amount != 50000 {
		t.Fatalf("stored = %+v", stored)
	}
}

func TestAgentAddDebtIOwe(t *testing.T) {
	agent, llm, _, debts, _ := newFullAgent(t)
	llm.Queue = []port.ChatResponse{
		{ToolCalls: []port.ToolCall{{
			ID:       "c1",
			Name:     "add_debt",
			ArgsJSON: `{"debtor":"Егор","amount_rubles":300,"direction":"i_owe"}`,
		}}},
		{Content: "Записала: ты должна Егору 300 ₽"},
	}

	if _, err := agent.Handle(ctx, 42, "я взяла у Егора 300 в долг"); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	stored, _ := debts.Peek("debt-1")
	if stored.Direction != domain.DebtIOwe {
		t.Fatalf("Direction = %q, want i_owe", stored.Direction)
	}
}

func TestAgentAddDebtValidation(t *testing.T) {
	agent, llm, _, debts, _ := newFullAgent(t)
	llm.Queue = []port.ChatResponse{
		{ToolCalls: []port.ToolCall{{
			ID:       "c1",
			Name:     "add_debt",
			ArgsJSON: `{"debtor":"  ","amount_rubles":500}`,
		}}},
		{Content: "Нужно имя того, кто взял в долг"},
	}

	if _, err := agent.Handle(ctx, 42, "запиши долг"); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if debts.Len() != 0 {
		t.Fatalf("debts.Len() = %d, want 0 (invalid input must not be stored)", debts.Len())
	}
	toolMsg := lastToolMessage(t, llm.Requests[1])
	if !containsJSONError(toolMsg.Content) {
		t.Errorf("tool error must reach the model, got %q", toolMsg.Content)
	}
}

func TestAgentListDebtsTotals(t *testing.T) {
	agent, llm, _, debts, _ := newFullAgent(t)
	seedDebt(t, debts, "debt-1", 42, domain.DebtOwesMe, "Егор", 50000, false)
	seedDebt(t, debts, "debt-2", 42, domain.DebtOwesMe, "Маша", 30000, true)
	seedDebt(t, debts, "debt-3", 42, domain.DebtIOwe, "Папа", 100000, false)
	seedDebt(t, debts, "debt-4", 7, domain.DebtOwesMe, "Чужой", 99900, false)

	llm.Queue = []port.ChatResponse{
		{ToolCalls: []port.ToolCall{{ID: "c1", Name: "list_debts", ArgsJSON: `{}`}}},
		{Content: "Вот твои долги"},
	}

	if _, err := agent.Handle(ctx, 42, "какие долги открыты"); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	toolMsg := lastToolMessage(t, llm.Requests[1])
	var payload struct {
		Items []struct {
			ID        string  `json:"id"`
			Direction string  `json:"direction"`
			Debtor    string  `json:"debtor"`
			Amount    float64 `json:"amount_rubles"`
			Settled   bool    `json:"settled"`
		} `json:"items"`
		TotalOwesMe float64 `json:"total_owes_me_rubles"`
		TotalIOwe   float64 `json:"total_i_owe_rubles"`
	}
	if err := json.Unmarshal([]byte(toolMsg.Content), &payload); err != nil {
		t.Fatalf("tool result: %v (%q)", err, toolMsg.Content)
	}
	// Only this user's debts; totals are precomputed, open ones only.
	if payload.TotalOwesMe != 500 || payload.TotalIOwe != 1000 {
		t.Fatalf("totals = %v/%v, want 500/1000 (open only)", payload.TotalOwesMe, payload.TotalIOwe)
	}
	if len(payload.Items) != 2 {
		t.Fatalf("items = %d, want 2 (open only)", len(payload.Items))
	}
	for _, it := range payload.Items {
		if it.ID == "debt-4" {
			t.Error("another member's debt leaked into the tool result")
		}
		if it.Settled {
			t.Error("settled debts must not appear in the open list")
		}
	}
}

func TestAgentRepayDebtFlow(t *testing.T) {
	agent, llm, _, debts, _ := newFullAgent(t)
	seedDebt(t, debts, "debt-1", 42, domain.DebtOwesMe, "Егор", 50000, false)

	llm.Queue = []port.ChatResponse{
		{ToolCalls: []port.ToolCall{{ID: "c1", Name: "repay_debt", ArgsJSON: `{"debt_id":"debt-1"}`}}},
		{Content: "Отметила: долг Егора закрыт"},
	}

	if _, err := agent.Handle(ctx, 42, "Егор вернул долг"); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	stored, _ := debts.Peek("debt-1")
	if stored.SettledAt == nil {
		t.Fatal("SettledAt = nil, want set")
	}

	// A foreign debt is not found (no existence leak).
	llm.Queue = []port.ChatResponse{
		{ToolCalls: []port.ToolCall{{ID: "c2", Name: "repay_debt", ArgsJSON: `{"debt_id":"debt-9"}`}}},
		{Content: "Не нашла такой долг"},
	}
	if _, err := agent.Handle(ctx, 42, "закрой долг debt-9"); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	toolMsg := lastToolMessage(t, llm.Requests[3])
	if !containsJSONError(toolMsg.Content) {
		t.Errorf("repay of a missing debt must surface an error, got %q", toolMsg.Content)
	}
}

func TestAgentSetBudgetFlow(t *testing.T) {
	agent, llm, _, _, budgets := newFullAgent(t)
	llm.Queue = []port.ChatResponse{
		{ToolCalls: []port.ToolCall{{
			ID:       "c1",
			Name:     "set_budget",
			ArgsJSON: `{"category":"food","monthly_limit_rubles":20000}`,
		}}},
		{Content: "Лимит на еду — 20000 ₽ в месяц"},
	}

	if _, err := agent.Handle(ctx, 42, "поставь лимит на еду 20000 в месяц"); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	got, err := budgets.List(ctx, 42)
	if err != nil || len(got) != 1 {
		t.Fatalf("budgets = %+v err=%v", got, err)
	}
	if got[0].Category != domain.CategoryFood || got[0].MonthlyLimit != 2000000 {
		t.Fatalf("budget = %+v", got[0])
	}
}

func TestAgentRemoveBudgetFlow(t *testing.T) {
	agent, llm, _, _, budgets := newFullAgent(t)
	seedBudget(t, budgets, 42, domain.CategoryFood, 100000)

	llm.Queue = []port.ChatResponse{
		{ToolCalls: []port.ToolCall{{ID: "c1", Name: "remove_budget", ArgsJSON: `{"category":"food"}`}}},
		{Content: "Убрала лимит"},
	}

	if _, err := agent.Handle(ctx, 42, "убери лимит на еду"); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	got, _ := budgets.List(ctx, 42)
	if len(got) != 0 {
		t.Fatalf("budgets = %+v, want none", got)
	}
}

func TestAgentGetBudgetsStatus(t *testing.T) {
	agent, llm, expenses, _, budgets := newFullAgent(t)
	seedBudget(t, budgets, 42, domain.CategoryFood, 100000)
	seedExpense(t, expenses, "exp-1", 42, 30000, domain.CategoryFood, "2026-10-18")
	seedExpense(t, expenses, "exp-2", 7, 50000, domain.CategoryFood, "2026-10-18")

	llm.Queue = []port.ChatResponse{
		{ToolCalls: []port.ToolCall{{ID: "c1", Name: "get_budgets", ArgsJSON: `{}`}}},
		{Content: "Лимиты и траты"},
	}

	if _, err := agent.Handle(ctx, 42, "мои лимиты"); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	toolMsg := lastToolMessage(t, llm.Requests[1])
	var payload struct {
		Items []struct {
			Category       string  `json:"category"`
			MonthlyLimit   float64 `json:"monthly_limit_rubles"`
			SpentThisMonth float64 `json:"spent_this_month_rubles"`
			Remaining      float64 `json:"remaining_rubles"`
			Exceeded       bool    `json:"exceeded"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(toolMsg.Content), &payload); err != nil {
		t.Fatalf("tool result: %v (%q)", err, toolMsg.Content)
	}
	if len(payload.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(payload.Items))
	}
	st := payload.Items[0]
	if st.MonthlyLimit != 1000 || st.SpentThisMonth != 300 || st.Remaining != 700 || st.Exceeded {
		t.Fatalf("status = %+v", st)
	}
}

func TestAgentAddExpenseCarriesBudgetWarning(t *testing.T) {
	agent, llm, _, _, budgets := newFullAgent(t)
	seedBudget(t, budgets, 42, domain.CategoryFood, 100000)

	llm.Queue = []port.ChatResponse{
		{ToolCalls: []port.ToolCall{{
			ID:       "c1",
			Name:     "add_expense",
			ArgsJSON: `{"amount_rubles":800,"category":"food"}`,
		}}},
		{Content: "Записала, но лимит почти исчерпан"},
	}

	if _, err := agent.Handle(ctx, 42, "обед 800"); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	toolMsg := lastToolMessage(t, llm.Requests[1])
	var payload struct {
		Budget *struct {
			MonthlyLimit   float64 `json:"monthly_limit_rubles"`
			SpentThisMonth float64 `json:"spent_this_month_rubles"`
			Remaining      float64 `json:"remaining_rubles"`
			Exceeded       bool    `json:"exceeded"`
		} `json:"budget"`
	}
	if err := json.Unmarshal([]byte(toolMsg.Content), &payload); err != nil {
		t.Fatalf("tool result: %v (%q)", err, toolMsg.Content)
	}
	if payload.Budget == nil {
		t.Fatal("budget block missing from add_expense payload")
	}
	if payload.Budget.SpentThisMonth != 800 || payload.Budget.Remaining != 200 || payload.Budget.Exceeded {
		t.Fatalf("budget = %+v", payload.Budget)
	}
}

func TestAgentAddExpenseNoBudgetBlockWhenUnset(t *testing.T) {
	agent, llm, _, _, _ := newFullAgent(t)
	llm.Queue = []port.ChatResponse{
		{ToolCalls: []port.ToolCall{{
			ID:       "c1",
			Name:     "add_expense",
			ArgsJSON: `{"amount_rubles":800,"category":"food"}`,
		}}},
		{Content: "Записала"},
	}

	if _, err := agent.Handle(ctx, 42, "обед 800"); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	toolMsg := lastToolMessage(t, llm.Requests[1])
	if strings.Contains(toolMsg.Content, `"budget"`) {
		t.Errorf("no budget block expected without a budget, got %q", toolMsg.Content)
	}
}

// helpers

func seedDebt(t *testing.T, debts *fakes.Debts, id string, userID int64, dir domain.DebtDirection, debtor string, kop domain.Kopecks, settled bool) {
	t.Helper()
	d, err := domain.NewDebt(userID, dir, debtor, kop, "", now)
	if err != nil {
		t.Fatal(err)
	}
	d.ID = id
	if settled {
		at := now.Add(-time.Hour)
		d.SettledAt = &at
	}
	// Put into the fake directly through Create-like seeding.
	debts.Put(d)
}

func seedBudget(t *testing.T, budgets *fakes.Budgets, userID int64, cat domain.Category, limit domain.Kopecks) {
	t.Helper()
	b, err := domain.NewBudget(userID, cat, limit, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := budgets.Set(ctx, b); err != nil {
		t.Fatal(err)
	}
}

func containsJSONError(s string) bool { return strings.Contains(s, `"error"`) }
