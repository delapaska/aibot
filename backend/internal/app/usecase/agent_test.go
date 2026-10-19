package usecase

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/delapaska/aibot/internal/app/port"
	"github.com/delapaska/aibot/internal/app/port/fakes"
	"github.com/delapaska/aibot/internal/domain"
)

// newAgent builds an agent on fake ports; returns the scripted LLM and
// the expense fake for assertions.
func newAgent(t *testing.T) (*Agent, *fakes.LLM, *fakes.Expenses) {
	t.Helper()
	expenses := fakes.NewExpenses()
	users := fakes.NewUsers()
	clock := fakes.Clock{T: now}
	svc := NewExpenseService(expenses, users, clock)
	llm := &fakes.LLM{}
	agent := NewAgent(llm, svc, NewDebtService(fakes.NewDebts(), users, clock), NewBudgetService(fakes.NewBudgets(), svc, clock), clock)
	return agent, llm, expenses
}

func mustKop(t *testing.T, rubles float64) domain.Kopecks {
	t.Helper()
	k, err := domain.KopecksFromRubles(rubles)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func seedExpense(t *testing.T, exp *fakes.Expenses, id string, userID int64, kop domain.Kopecks, cat domain.Category, date string) {
	t.Helper()
	e, err := domain.NewExpense(userID, kop, cat, mustDate(t, date), "комментарий", now)
	if err != nil {
		t.Fatal(err)
	}
	e.ID = id
	exp.Put(e)
}

func TestAgentPlainAnswer(t *testing.T) {
	agent, llm, _ := newAgent(t)
	llm.Queue = []port.ChatResponse{{Content: "Привет! Что записать?"}}

	got, err := agent.Handle(ctx, 42, "привет")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if got != "Привет! Что записать?" {
		t.Errorf("answer = %q", got)
	}
	if len(llm.Requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(llm.Requests))
	}
	req := llm.Requests[0]
	if len(req.Messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(req.Messages))
	}
	sys := req.Messages[0]
	if sys.Role != port.RoleSystem || !strings.Contains(sys.Content, "2026-10-18") {
		t.Errorf("system prompt must carry today's date, got %q", sys.Content)
	}
	// The model must always tell the user what happened: confirm the
	// recorded expense with its details, and show expenses from the tool
	// answers when asked — never leave the operation unconfirmed.
	if !strings.Contains(sys.Content, "подтверждай, что записал") {
		t.Errorf("system prompt must require a confirmation with details, got %q", sys.Content)
	}
	if !strings.Contains(sys.Content, "перечисли траты из ответа") {
		t.Errorf("system prompt must require listing expenses from the tool answer, got %q", sys.Content)
	}
	// Record ids are for the agent's own tool chains only: they must never
	// reach the user, who sees them as noise.
	if !strings.Contains(sys.Content, "id не показывай") {
		t.Errorf("system prompt must forbid showing record ids, got %q", sys.Content)
	}
	if req.Messages[1].Content != "привет" || req.Messages[1].Role != port.RoleUser {
		t.Errorf("user message = %+v", req.Messages[1])
	}
	if len(req.Tools) != 11 {
		t.Errorf("tools = %d, want 11", len(req.Tools))
	}
}

func TestAgentAddExpenseFlow(t *testing.T) {
	agent, llm, expenses := newAgent(t)
	llm.Queue = []port.ChatResponse{
		{ToolCalls: []port.ToolCall{{
			ID:       "c1",
			Name:     "add_expense",
			ArgsJSON: `{"amount_rubles":350.13,"category":"food","spent_at":"2026-10-17","comment":"кофе"}`,
		}}},
	}

	got, err := agent.Handle(ctx, 42, "кофе 350.13 17 октября")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	// A plain write is confirmed locally: no second LLM round is spent
	// just to say "записал" — that round doubled the wait on slow models.
	want := "Записал: кофе (Еда) — 350,13 ₽, 17.10.2026."
	if got != want {
		t.Errorf("answer = %q, want %q", got, want)
	}
	if len(llm.Requests) != 1 {
		t.Fatalf("requests = %d, want 1 (no second round for a write)", len(llm.Requests))
	}
	if expenses.Len() != 1 {
		t.Fatalf("stored %d expenses, want 1", expenses.Len())
	}
	stored, _ := expenses.Peek("exp-1")
	if stored.UserID != 42 || stored.Amount != 35013 || stored.Category != domain.CategoryFood {
		t.Errorf("stored = %+v", stored)
	}
	if stored.Comment != "кофе" {
		t.Errorf("comment = %q", stored.Comment)
	}
}

func TestAgentAddWithoutComment(t *testing.T) {
	agent, llm, _ := newAgent(t)
	llm.Queue = []port.ChatResponse{
		{ToolCalls: []port.ToolCall{{
			ID:       "c1",
			Name:     "add_expense",
			ArgsJSON: `{"amount_rubles":200,"category":"food","spent_at":"2026-10-18"}`,
		}}},
	}

	got, err := agent.Handle(ctx, 42, "пельмени 200")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	want := "Записал: Еда — 200 ₽, 18.10.2026."
	if got != want {
		t.Errorf("answer = %q, want %q", got, want)
	}
}

// newAgentWithBudget builds an agent whose user 42 has a monthly limit
// set (0 removes the budget); returns the debts fake too for seeding.
func newAgentWithBudget(t *testing.T, cat domain.Category, limitRubles float64) (*Agent, *fakes.LLM, *fakes.Expenses, *fakes.Debts) {
	t.Helper()
	expenses := fakes.NewExpenses()
	users := fakes.NewUsers()
	clock := fakes.Clock{T: now}
	svc := NewExpenseService(expenses, users, clock)
	budgets := NewBudgetService(fakes.NewBudgets(), svc, clock)
	if limitRubles > 0 {
		if _, err := budgets.Set(ctx, BudgetInput{
			UserID:       42,
			Category:     cat,
			MonthlyLimit: mustKop(t, limitRubles),
		}); err != nil {
			t.Fatal(err)
		}
	}
	debts := fakes.NewDebts()
	llm := &fakes.LLM{}
	agent := NewAgent(llm, svc, NewDebtService(debts, users, clock), budgets, clock)
	return agent, llm, expenses, debts
}

func TestAgentAddBudgetExceededWarning(t *testing.T) {
	agent, llm, expenses, _ := newAgentWithBudget(t, domain.CategoryFood, 1000)
	seedExpense(t, expenses, "exp-9", 42, mustKop(t, 900), domain.CategoryFood, "2026-10-05")
	llm.Queue = []port.ChatResponse{
		{ToolCalls: []port.ToolCall{{
			ID:       "c1",
			Name:     "add_expense",
			ArgsJSON: `{"amount_rubles":200,"category":"food","spent_at":"2026-10-18"}`,
		}}},
	}

	got, err := agent.Handle(ctx, 42, "пельмени 200")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	want := "Записал: Еда — 200 ₽, 18.10.2026. ⚠️ Месячный лимит на Еду превышен: потрачено 1 100 ₽ из 1 000 ₽."
	if got != want {
		t.Errorf("answer = %q, want %q", got, want)
	}
}

func TestAgentAddBudgetLowWarning(t *testing.T) {
	agent, llm, expenses, _ := newAgentWithBudget(t, domain.CategoryFood, 1000)
	seedExpense(t, expenses, "exp-9", 42, mustKop(t, 750), domain.CategoryFood, "2026-10-05")
	llm.Queue = []port.ChatResponse{
		{ToolCalls: []port.ToolCall{{
			ID:       "c1",
			Name:     "add_expense",
			ArgsJSON: `{"amount_rubles":200,"category":"food","spent_at":"2026-10-18"}`,
		}}},
	}

	got, err := agent.Handle(ctx, 42, "пельмени 200")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	want := "Записал: Еда — 200 ₽, 18.10.2026. ⚠️ На Еду до конца месяца осталось 50 ₽ из 1 000 ₽."
	if got != want {
		t.Errorf("answer = %q, want %q", got, want)
	}
}

func TestAgentUpdateRepliesLocally(t *testing.T) {
	agent, llm, expenses := newAgent(t)
	seedExpense(t, expenses, "exp-1", 42, mustKop(t, 350), domain.CategoryFood, "2026-10-17")
	llm.Queue = []port.ChatResponse{
		{ToolCalls: []port.ToolCall{{
			ID:       "c1",
			Name:     "update_expense",
			ArgsJSON: `{"expense_id":"exp-1","amount_rubles":250}`,
		}}},
	}

	got, err := agent.Handle(ctx, 42, "исправь кофе на 250")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	want := "Изменил: комментарий (Еда) — 250 ₽, 17.10.2026."
	if got != want {
		t.Errorf("answer = %q, want %q", got, want)
	}
	if len(llm.Requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(llm.Requests))
	}
}

func TestAgentDeleteRepliesLocally(t *testing.T) {
	agent, llm, expenses := newAgent(t)
	seedExpense(t, expenses, "exp-1", 42, mustKop(t, 350), domain.CategoryFood, "2026-10-17")
	llm.Queue = []port.ChatResponse{
		{ToolCalls: []port.ToolCall{{
			ID:       "c1",
			Name:     "delete_expense",
			ArgsJSON: `{"expense_id":"exp-1"}`,
		}}},
	}

	got, err := agent.Handle(ctx, 42, "удали кофе")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if got != "Удалил расход." {
		t.Errorf("answer = %q", got)
	}
	if expenses.Len() != 0 {
		t.Errorf("expense was not deleted")
	}
}

func TestAgentDebtRepliesLocally(t *testing.T) {
	agent, llm, _, _ := newAgentWithBudget(t, domain.CategoryFood, 0)
	llm.Queue = []port.ChatResponse{
		{ToolCalls: []port.ToolCall{{
			ID:       "c1",
			Name:     "add_debt",
			ArgsJSON: `{"debtor":"Егор","amount_rubles":500,"direction":"owes_me"}`,
		}}},
	}

	got, err := agent.Handle(ctx, 42, "Егор взял у меня 500")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	want := "Записал долг: Егор → тебе, 500 ₽."
	if got != want {
		t.Errorf("answer = %q, want %q", got, want)
	}

	llm.Queue = []port.ChatResponse{
		{ToolCalls: []port.ToolCall{{
			ID:       "c2",
			Name:     "add_debt",
			ArgsJSON: `{"debtor":"Егор","amount_rubles":300,"direction":"i_owe"}`,
		}}},
	}
	got, err = agent.Handle(ctx, 42, "я должен Егору 300")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	want = "Записал долг: ты → Егор, 300 ₽."
	if got != want {
		t.Errorf("answer = %q, want %q", got, want)
	}
}

func TestAgentRepayRepliesLocally(t *testing.T) {
	agent, llm, _, debts := newAgentWithBudget(t, domain.CategoryFood, 0)
	users := fakes.NewUsers()
	svc := NewDebtService(debts, users, fakes.Clock{T: now})
	if _, err := svc.Create(ctx, DebtInput{
		UserID: 42, Direction: domain.DebtOwesMe, Debtor: "Егор", Amount: mustKop(t, 500),
	}); err != nil {
		t.Fatal(err)
	}
	llm.Queue = []port.ChatResponse{
		{ToolCalls: []port.ToolCall{{
			ID:       "c1",
			Name:     "repay_debt",
			ArgsJSON: `{"debtor":"Егор"}`,
		}}},
	}

	got, err := agent.Handle(ctx, 42, "Егор вернул долг")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	want := "Долг закрыт: Егор — 500 ₽."
	if got != want {
		t.Errorf("answer = %q, want %q", got, want)
	}
}

func TestAgentMixedRoundUsesModel(t *testing.T) {
	// A round that mixes a write with list_expenses still needs the
	// model: the expense list is presented by the model, so no local
	// shortcut (unlike list_debts/get_summary/get_budgets, which render
	// locally).
	agent, llm, expenses := newAgent(t)
	llm.Queue = []port.ChatResponse{
		{ToolCalls: []port.ToolCall{
			{ID: "c1", Name: "add_expense", ArgsJSON: `{"amount_rubles":200,"category":"food","spent_at":"2026-10-18"}`},
			{ID: "c2", Name: "list_expenses", ArgsJSON: `{}`},
		}},
		{Content: "Записал. Вот список: ..."},
	}

	got, err := agent.Handle(ctx, 42, "запиши пельмени 200 и покажи расходы")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if got != "Записал. Вот список: ..." {
		t.Errorf("answer = %q", got)
	}
	if len(llm.Requests) != 2 {
		t.Fatalf("requests = %d, want 2 (mixed round goes back to the model)", len(llm.Requests))
	}
	if expenses.Len() != 1 {
		t.Fatalf("stored %d expenses, want 1", expenses.Len())
	}
}

func TestAgentIgnoresModelSuppliedUserID(t *testing.T) {
	// Security invariant: the owner comes from authentication, never
	// from the model — even if the model puts user_id into arguments.
	agent, llm, expenses := newAgent(t)
	llm.Queue = []port.ChatResponse{
		{ToolCalls: []port.ToolCall{{
			ID:       "c1",
			Name:     "add_expense",
			ArgsJSON: `{"amount_rubles":100,"category":"food","user_id":999}`,
		}}},
		{Content: "ok"},
	}

	if _, err := agent.Handle(ctx, 42, "обед 100"); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	stored, ok := expenses.Peek("exp-1")
	if !ok {
		t.Fatal("expense was not stored")
	}
	if stored.UserID != 42 {
		t.Errorf("owner = %d, want 42 (authenticated user)", stored.UserID)
	}
}

func TestAgentToolErrorReachesModel(t *testing.T) {
	agent, llm, expenses := newAgent(t)
	llm.Queue = []port.ChatResponse{
		{ToolCalls: []port.ToolCall{{
			ID:       "c1",
			Name:     "add_expense",
			ArgsJSON: `{"amount_rubles":50,"category":"cafe"}`,
		}}},
		{Content: "Такой категории нет, вот список: ..."},
	}

	got, err := agent.Handle(ctx, 42, "кафе 50")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if got != "Такой категории нет, вот список: ..." {
		t.Errorf("answer = %q", got)
	}
	if expenses.Len() != 0 {
		t.Errorf("invalid input must not be stored")
	}
	toolMsg := llm.Requests[1].Messages[len(llm.Requests[1].Messages)-1]
	if toolMsg.Role != port.RoleTool {
		t.Fatalf("last message role = %v", toolMsg.Role)
	}
	if !strings.Contains(toolMsg.Content, `"error"`) || !strings.Contains(toolMsg.Content, "категория") {
		t.Errorf("tool error must mention the problem, got %q", toolMsg.Content)
	}
}

func TestAgentListIsScoped(t *testing.T) {
	agent, llm, expenses := newAgent(t)
	seedExpense(t, expenses, "exp-1", 42, 35000, domain.CategoryFood, "2026-10-10")
	seedExpense(t, expenses, "exp-2", 42, 12000, domain.CategoryTransport, "2026-10-17")
	seedExpense(t, expenses, "exp-3", 7, 99000, domain.CategoryFood, "2026-10-16")

	llm.Queue = []port.ChatResponse{
		{ToolCalls: []port.ToolCall{{ID: "c1", Name: "list_expenses", ArgsJSON: `{}`}}},
		{Content: "Вот твои расходы"},
	}

	if _, err := agent.Handle(ctx, 42, "покажи расходы"); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	toolMsg := lastToolMessage(t, llm.Requests[1])
	var payload struct {
		Items []struct {
			ID     string  `json:"id"`
			Amount float64 `json:"amount_rubles"`
		} `json:"items"`
		TotalCount int     `json:"total_count"`
		SumRubles  float64 `json:"sum_rubles"`
	}
	if err := json.Unmarshal([]byte(toolMsg.Content), &payload); err != nil {
		t.Fatalf("tool result: %v (%q)", err, toolMsg.Content)
	}
	if payload.TotalCount != 2 || len(payload.Items) != 2 {
		t.Fatalf("total_count = %d, items = %d, want 2/2", payload.TotalCount, len(payload.Items))
	}
	if payload.SumRubles != 470 {
		t.Errorf("sum_rubles = %v, want 470", payload.SumRubles)
	}
	for _, it := range payload.Items {
		if it.ID == "exp-3" {
			t.Error("another member's expense leaked into the tool result")
		}
	}
}

func TestAgentUpdateFlow(t *testing.T) {
	agent, llm, expenses := newAgent(t)
	seedExpense(t, expenses, "exp-1", 42, 35000, domain.CategoryFood, "2026-10-17")

	llm.Queue = []port.ChatResponse{
		{ToolCalls: []port.ToolCall{{
			ID:       "c1",
			Name:     "update_expense",
			ArgsJSON: `{"expense_id":"exp-1","amount_rubles":400}`,
		}}},
		{Content: "Обновил"},
	}

	if _, err := agent.Handle(ctx, 42, "измени расход на 400"); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	stored, _ := expenses.Peek("exp-1")
	if stored.Amount != 40000 {
		t.Errorf("amount = %d, want 40000", stored.Amount)
	}
}

func TestAgentUpdateForeignExpenseDenied(t *testing.T) {
	agent, llm, expenses := newAgent(t)
	seedExpense(t, expenses, "exp-1", 7, 35000, domain.CategoryFood, "2026-10-17")

	llm.Queue = []port.ChatResponse{
		{ToolCalls: []port.ToolCall{{
			ID:       "c1",
			Name:     "update_expense",
			ArgsJSON: `{"expense_id":"exp-1","amount_rubles":400}`,
		}}},
		{Content: "Не нашёл такой расход"},
	}

	if _, err := agent.Handle(ctx, 42, "измени расход"); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if stored, _ := expenses.Peek("exp-1"); stored.Amount != 35000 {
		t.Error("a foreign expense must stay untouched")
	}
	toolMsg := lastToolMessage(t, llm.Requests[1])
	if !strings.Contains(toolMsg.Content, `"error"`) {
		t.Errorf("foreign update must surface an error to the model, got %q", toolMsg.Content)
	}
}

func TestAgentDeleteFlow(t *testing.T) {
	agent, llm, expenses := newAgent(t)
	seedExpense(t, expenses, "exp-1", 42, 35000, domain.CategoryFood, "2026-10-17")

	llm.Queue = []port.ChatResponse{
		{ToolCalls: []port.ToolCall{{ID: "c1", Name: "delete_expense", ArgsJSON: `{"expense_id":"exp-1"}`}}},
		{Content: "Удалил"},
	}

	if _, err := agent.Handle(ctx, 42, "удали расход"); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if expenses.Len() != 0 {
		t.Error("expense must be removed")
	}
}

func TestAgentSummary(t *testing.T) {
	agent, llm, expenses := newAgent(t)
	seedExpense(t, expenses, "exp-1", 42, 35000, domain.CategoryFood, "2026-10-10")
	seedExpense(t, expenses, "exp-2", 42, 12000, domain.CategoryTransport, "2026-10-17")
	seedExpense(t, expenses, "exp-3", 42, 47000, domain.CategoryFood, "2026-10-16")
	seedExpense(t, expenses, "exp-4", 7, 99000, domain.CategoryFood, "2026-10-16")

	llm.Queue = []port.ChatResponse{
		{ToolCalls: []port.ToolCall{{ID: "c1", Name: "get_summary", ArgsJSON: `{"from":"2026-10-01","to":"2026-10-31"}`}}},
	}

	got, err := agent.Handle(ctx, 42, "итоги за месяц")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	// The summary is rendered locally: English category keys never
	// surface and the model cannot add up amounts wrongly.
	want := "Расходы по категориям:\n" +
		"Еда — 820 ₽\n" +
		"Транспорт — 120 ₽\n" +
		"Итого: 940 ₽ за 3 операции."
	if got != want {
		t.Fatalf("answer = %q, want %q", got, want)
	}
	if len(llm.Requests) != 1 {
		t.Fatalf("requests = %d, want 1 (local render)", len(llm.Requests))
	}
}

func TestAgentRoundsGuard(t *testing.T) {
	agent, llm, _ := newAgent(t)
	// A model stuck in a tool loop: every round asks for tools again.
	for i := 0; i < 10; i++ {
		llm.Queue = append(llm.Queue, port.ChatResponse{
			ToolCalls: []port.ToolCall{{ID: "c", Name: "list_expenses", ArgsJSON: `{}`}},
		})
	}

	if _, err := agent.Handle(ctx, 42, "покажи расходы"); !errors.Is(err, ErrAgentLimit) {
		t.Fatalf("error = %v, want ErrAgentLimit", err)
	}
}

func TestAgentLLMError(t *testing.T) {
	agent, llm, _ := newAgent(t)
	llm.Err = errors.New("provider down")

	if _, err := agent.Handle(ctx, 42, "привет"); err == nil || !strings.Contains(err.Error(), "provider down") {
		t.Fatalf("error = %v, want provider error", err)
	}
}

func TestStripIDNoise(t *testing.T) {
	in := "Егор — 500 ₽ (id 01a1505f-2b3c-4d5e-8f90-112233445566), кофе 200 ₽"
	want := "Егор — 500 ₽ (), кофе 200 ₽"
	if got := stripIDNoise(in); got != want {
		t.Errorf("stripIDNoise = %q, want %q", got, want)
	}
}

func lastToolMessage(t *testing.T, req port.ChatRequest) port.ChatMessage {
	t.Helper()
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == port.RoleTool {
			return req.Messages[i]
		}
	}
	t.Fatal("no tool message found")
	return port.ChatMessage{}
}
