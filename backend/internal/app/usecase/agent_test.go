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
	return NewAgent(llm, svc, clock), llm, expenses
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
	if req.Messages[1].Content != "привет" || req.Messages[1].Role != port.RoleUser {
		t.Errorf("user message = %+v", req.Messages[1])
	}
	if len(req.Tools) != 5 {
		t.Errorf("tools = %d, want 5", len(req.Tools))
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
		{Content: "Записал: кофе — 350,13 ₽"},
	}

	got, err := agent.Handle(ctx, 42, "кофе 350.13 17 октября")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if got != "Записал: кофе — 350,13 ₽" {
		t.Errorf("answer = %q", got)
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

	// The second LLM round carries the assistant's tool calls and the
	// tool result with the created expense.
	req := llm.Requests[1]
	var sawToolResult bool
	for _, m := range req.Messages {
		if m.Role == port.RoleTool && strings.Contains(m.Content, `"id":"exp-1"`) {
			sawToolResult = true
		}
	}
	if !sawToolResult {
		t.Error("tool result with the created expense id must reach the model")
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
		Total int `json:"total"`
	}
	if err := json.Unmarshal([]byte(toolMsg.Content), &payload); err != nil {
		t.Fatalf("tool result: %v (%q)", err, toolMsg.Content)
	}
	if payload.Total != 2 || len(payload.Items) != 2 {
		t.Fatalf("total = %d, items = %d, want 2/2", payload.Total, len(payload.Items))
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
		{Content: "Итого за октябрь"},
	}

	if _, err := agent.Handle(ctx, 42, "итоги за месяц"); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	toolMsg := lastToolMessage(t, llm.Requests[1])
	var payload struct {
		TotalRubles float64            `json:"total_rubles"`
		Count       int                `json:"count"`
		ByCategory  map[string]float64 `json:"by_category"`
	}
	if err := json.Unmarshal([]byte(toolMsg.Content), &payload); err != nil {
		t.Fatalf("tool result: %v (%q)", err, toolMsg.Content)
	}
	if payload.TotalRubles != 940 || payload.Count != 3 {
		t.Errorf("total = %v, count = %d, want 940/3", payload.TotalRubles, payload.Count)
	}
	if payload.ByCategory["food"] != 820 || payload.ByCategory["transport"] != 120 {
		t.Errorf("by_category = %v", payload.ByCategory)
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
