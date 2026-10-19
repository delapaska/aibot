package usecase

import (
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

	got, err := agent.Handle(ctx, 42, "какие долги открыты")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	// The debts list is rendered locally: no ids, no English labels, and
	// precomputed totals the model cannot swap.
	want := "Долги:\n" +
		"Мне должны:\n" +
		"1. Егор — 500 ₽\n" +
		"Я должен:\n" +
		"1. Папа — 1 000 ₽\n" +
		"Итого: мне должны 500 ₽, я должен 1 000 ₽."
	if got != want {
		t.Fatalf("answer = %q, want %q", got, want)
	}
	if len(llm.Requests) != 1 {
		t.Fatalf("requests = %d, want 1 (local render, no second round)", len(llm.Requests))
	}
}

func TestAgentListDebtsEmpty(t *testing.T) {
	agent, llm, _, _, _ := newFullAgent(t)
	llm.Queue = []port.ChatResponse{
		{ToolCalls: []port.ToolCall{{ID: "c1", Name: "list_debts", ArgsJSON: `{}`}}},
	}

	got, err := agent.Handle(ctx, 42, "какие долги открыты")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if got != "Открытых долгов нет." {
		t.Fatalf("answer = %q, want the empty-debts line", got)
	}
	if len(llm.Requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(llm.Requests))
	}
}

func TestAgentRepayDebtFlow(t *testing.T) {
	agent, llm, _, debts, _ := newFullAgent(t)
	seedDebt(t, debts, "debt-1", 42, domain.DebtOwesMe, "Егор", 50000, false)

	llm.Queue = []port.ChatResponse{
		{ToolCalls: []port.ToolCall{{ID: "c1", Name: "repay_debt", ArgsJSON: `{"debtor":"Егор"}`}}},
	}

	got, err := agent.Handle(ctx, 42, "Егор вернул долг")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	stored, _ := debts.Peek("debt-1")
	if stored.SettledAt == nil {
		t.Fatal("SettledAt = nil, want set")
	}
	// Repay works by name: one round, local confirmation.
	if got != "Долг закрыт: Егор — 500 ₽." {
		t.Errorf("answer = %q", got)
	}
	if len(llm.Requests) != 1 {
		t.Fatalf("requests = %d, want 1 (no id lookup round)", len(llm.Requests))
	}

	// A foreign debtor is not found: the error goes back to the model,
	// so this round still spends a second LLM call.
	llm.Queue = []port.ChatResponse{
		{ToolCalls: []port.ToolCall{{ID: "c2", Name: "repay_debt", ArgsJSON: `{"debtor":"Чужой"}`}}},
		{Content: "Не нашла такой долг"},
	}
	if _, err := agent.Handle(ctx, 42, "закрой долг Чужого"); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	toolMsg := lastToolMessage(t, llm.Requests[2])
	if !containsJSONError(toolMsg.Content) {
		t.Errorf("repay of a missing debtor must surface an error, got %q", toolMsg.Content)
	}
}

func TestAgentRepayAmbiguousDebtAsksForAmount(t *testing.T) {
	agent, llm, _, debts, _ := newFullAgent(t)
	seedDebt(t, debts, "debt-1", 42, domain.DebtOwesMe, "Егор", 20000, false)
	seedDebt(t, debts, "debt-2", 42, domain.DebtOwesMe, "Егор", 25000, false)

	llm.Queue = []port.ChatResponse{
		{ToolCalls: []port.ToolCall{{ID: "c1", Name: "repay_debt", ArgsJSON: `{"debtor":"Егор"}`}}},
		{Content: "У Егора два долга — уточни, какой закрыть"},
	}

	if _, err := agent.Handle(ctx, 42, "закрой долг Егора"); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	// Nothing settled yet: two open debts of the same person is
	// ambiguous without an amount.
	for _, id := range []string{"debt-1", "debt-2"} {
		if d, _ := debts.Peek(id); d.SettledAt != nil {
			t.Errorf("%s settled without disambiguation", id)
		}
	}
	toolMsg := lastToolMessage(t, llm.Requests[1])
	if !containsJSONError(toolMsg.Content) {
		t.Errorf("ambiguity must reach the model as an error, got %q", toolMsg.Content)
	}
	if !strings.Contains(toolMsg.Content, "200") || !strings.Contains(toolMsg.Content, "250") {
		t.Errorf("the error must list the amounts to pick from, got %q", toolMsg.Content)
	}

	// With an amount the same request settles the matching debt.
	llm.Queue = []port.ChatResponse{
		{ToolCalls: []port.ToolCall{{ID: "c2", Name: "repay_debt", ArgsJSON: `{"debtor":"Егор","amount_rubles":250}`}}},
	}
	got, err := agent.Handle(ctx, 42, "закрой долг Егора 250")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if got != "Долг закрыт: Егор — 250 ₽." {
		t.Errorf("answer = %q", got)
	}
	if d, _ := debts.Peek("debt-2"); d.SettledAt == nil {
		t.Error("the 250 debt must be settled")
	}
	if d, _ := debts.Peek("debt-1"); d.SettledAt != nil {
		t.Error("the 200 debt must stay open")
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
	}

	got, err := agent.Handle(ctx, 42, "мои лимиты")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	// Budgets are rendered locally: another member's spending must not
	// leak, and the status line carries no English or ids.
	want := "Лимиты на месяц:\nЕда — 1 000 ₽: потрачено 300 ₽, осталось 700 ₽"
	if got != want {
		t.Fatalf("answer = %q, want %q", got, want)
	}
	if len(llm.Requests) != 1 {
		t.Fatalf("requests = %d, want 1 (local render)", len(llm.Requests))
	}
}

func TestAgentGetBudgetsExceeded(t *testing.T) {
	agent, llm, expenses, _, budgets := newFullAgent(t)
	seedBudget(t, budgets, 42, domain.CategoryFood, 100000)
	seedExpense(t, expenses, "exp-1", 42, 150000, domain.CategoryFood, "2026-10-18")

	llm.Queue = []port.ChatResponse{
		{ToolCalls: []port.ToolCall{{ID: "c1", Name: "get_budgets", ArgsJSON: `{}`}}},
	}

	got, err := agent.Handle(ctx, 42, "мои лимиты")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	want := "Лимиты на месяц:\nЕда — 1 000 ₽: потрачено 1 500 ₽, лимит превышен"
	if got != want {
		t.Fatalf("answer = %q, want %q", got, want)
	}
	if len(llm.Requests) != 1 {
		t.Fatalf("requests = %d, want 1 (local render)", len(llm.Requests))
	}
}

func TestAgentAddExpenseCarriesBudgetWarning(t *testing.T) {
	agent, llm, _, _, budgets := newFullAgent(t)
	seedBudget(t, budgets, 42, domain.CategoryFood, 100000)

	llm.Queue = []port.ChatResponse{
		{ToolCalls: []port.ToolCall{{
			ID:       "c1",
			Name:     "add_expense",
			ArgsJSON: `{"amount_rubles":800,"category":"food","spent_at":"2026-10-18"}`,
		}}},
	}

	got, err := agent.Handle(ctx, 42, "обед 800")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	// The budget status is attached to the add payload and surfaces in
	// the local confirmation — no second round needed to warn the user.
	want := "Записал: Еда — 800 ₽, 18.10.2026. ⚠️ На Еду до конца месяца осталось 200 ₽ из 1 000 ₽."
	if got != want {
		t.Fatalf("answer = %q, want %q", got, want)
	}
	if len(llm.Requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(llm.Requests))
	}
}

func TestAgentAddExpenseNoBudgetBlockWhenUnset(t *testing.T) {
	agent, llm, _, _, _ := newFullAgent(t)
	llm.Queue = []port.ChatResponse{
		{ToolCalls: []port.ToolCall{{
			ID:       "c1",
			Name:     "add_expense",
			ArgsJSON: `{"amount_rubles":800,"category":"food","spent_at":"2026-10-18"}`,
		}}},
	}

	got, err := agent.Handle(ctx, 42, "обед 800")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	want := "Записал: Еда — 800 ₽, 18.10.2026."
	if got != want {
		t.Fatalf("answer = %q, want %q", got, want)
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
