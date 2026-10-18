// Agent part of the application layer: turns a natural-language message
// into tool calls over the expense service. The model never chooses the
// data owner — every tool call is executed for the authenticated user.
package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/delapaska/aibot/internal/app/port"
	"github.com/delapaska/aibot/internal/domain"
)

// ErrAgentLimit is returned when the model keeps requesting tools and
// never produces a final answer.
var ErrAgentLimit = errors.New("agent: too many tool rounds")

// maxToolRounds bounds one Handle call: enough for
// find-expense-then-change-it chains, small enough to cap cost.
const maxToolRounds = 6

// listPage is the List page size the agent uses; summaries page through
// up to maxSummaryPages pages.
const (
	listPage        = 100
	maxSummaryPages = 10
)

// Agent runs the conversation loop for one user message.
type Agent struct {
	llm      port.LLM
	expenses *ExpenseService
	debts    *DebtService
	budgets  *BudgetService
	clock    port.Clock
}

func NewAgent(llm port.LLM, expenses *ExpenseService, debts *DebtService, budgets *BudgetService, clock port.Clock) *Agent {
	return &Agent{llm: llm, expenses: expenses, debts: debts, budgets: budgets, clock: clock}
}

// Handle resolves one user message into a reply text.
func (a *Agent) Handle(ctx context.Context, userID int64, userMsg string) (string, error) {
	msgs := []port.ChatMessage{
		{Role: port.RoleSystem, Content: a.systemPrompt()},
		{Role: port.RoleUser, Content: userMsg},
	}
	for round := 0; round < maxToolRounds; round++ {
		resp, err := a.llm.ChatComplete(ctx, port.ChatRequest{Messages: msgs, Tools: agentTools})
		if err != nil {
			return "", fmt.Errorf("agent: %w", err)
		}
		if len(resp.ToolCalls) == 0 {
			return resp.Content, nil
		}
		msgs = append(msgs, port.ChatMessage{Role: port.RoleAssistant, ToolCalls: resp.ToolCalls})
		for _, call := range resp.ToolCalls {
			msgs = append(msgs, port.ChatMessage{
				Role:       port.RoleTool,
				ToolCallID: call.ID,
				Content:    a.runTool(ctx, userID, call),
			})
		}
	}
	return "", ErrAgentLimit
}

// agentTools are offered to the model. None of them accepts a user id:
// the owner is injected server-side from authentication.
var agentTools = []port.Tool{
	{
		Name:        "add_expense",
		Description: "Добавить расход текущего пользователя",
		ParamsJSON: `{"type":"object","properties":{` +
			`"amount_rubles":{"type":"number","description":"сумма в рублях"},` +
			`"category":{"type":"string","enum":["food","transport","housing","entertainment","health","shopping","other"]},` +
			`"spent_at":{"type":"string","format":"date","description":"дата YYYY-MM-DD, по умолчанию сегодня"},` +
			`"comment":{"type":"string","description":"короткий комментарий"}},` +
			`"required":["amount_rubles","category"]}`,
	},
	{
		Name:        "list_expenses",
		Description: "Список расходов текущего пользователя, новые раньше",
		ParamsJSON: `{"type":"object","properties":{` +
			`"category":{"type":"string","enum":["food","transport","housing","entertainment","health","shopping","other"]},` +
			`"from":{"type":"string","description":"дата YYYY-MM-DD, включительно"},` +
			`"to":{"type":"string","description":"дата YYYY-MM-DD, включительно"},` +
			`"limit":{"type":"integer","description":"сколько вернуть, по умолчанию 10"}}}`,
	},
	{
		Name:        "update_expense",
		Description: "Изменить расход по id; id берётся из list_expenses",
		ParamsJSON: `{"type":"object","properties":{` +
			`"expense_id":{"type":"string"},` +
			`"amount_rubles":{"type":"number"},` +
			`"category":{"type":"string","enum":["food","transport","housing","entertainment","health","shopping","other"]},` +
			`"spent_at":{"type":"string","format":"date"},` +
			`"comment":{"type":"string"}},` +
			`"required":["expense_id"]}`,
	},
	{
		Name:        "delete_expense",
		Description: "Удалить расход по id; id берётся из list_expenses",
		ParamsJSON: `{"type":"object","properties":{` +
			`"expense_id":{"type":"string"}},"required":["expense_id"]}`,
	},
	{
		Name:        "get_summary",
		Description: "Суммы расходов текущего пользователя по категориям за период",
		ParamsJSON: `{"type":"object","properties":{` +
			`"from":{"type":"string","description":"дата YYYY-MM-DD, включительно"},` +
			`"to":{"type":"string","description":"дата YYYY-MM-DD, включительно"},` +
			`"category":{"type":"string","enum":["food","transport","housing","entertainment","health","shopping","other"]}}}`,
	},
	{
		Name:        "add_debt",
		Description: "Записать долг между текущим пользователем и другим человеком",
		ParamsJSON: `{"type":"object","properties":{` +
			`"debtor":{"type":"string","description":"имя другого человека, например Егор"},` +
			`"amount_rubles":{"type":"number","description":"сумма долга в рублях"},` +
			`"direction":{"type":"string","enum":["owes_me","i_owe"],"description":"owes_me — мне должны; i_owe — я должен; по умолчанию owes_me"},` +
			`"comment":{"type":"string","description":"короткий комментарий"}},` +
			`"required":["debtor","amount_rubles"]}`,
	},
	{
		Name:        "list_debts",
		Description: "Открытые (невозвращённые) долги текущего пользователя и их суммы",
		ParamsJSON:  `{"type":"object","properties":{}}`,
	},
	{
		Name:        "repay_debt",
		Description: "Отметить долг возвращённым; id берётся из list_debts",
		ParamsJSON: `{"type":"object","properties":{` +
			`"debt_id":{"type":"string"}},` +
			`"required":["debt_id"]}`,
	},
	{
		Name:        "set_budget",
		Description: "Поставить месячный лимит трат по категории",
		ParamsJSON: `{"type":"object","properties":{` +
			`"category":{"type":"string","enum":["food","transport","housing","entertainment","health","shopping","other"]},` +
			`"monthly_limit_rubles":{"type":"number","description":"лимит в рублях за месяц"}},` +
			`"required":["category","monthly_limit_rubles"]}`,
	},
	{
		Name:        "remove_budget",
		Description: "Убрать месячный лимит по категории",
		ParamsJSON: `{"type":"object","properties":{` +
			`"category":{"type":"string","enum":["food","transport","housing","entertainment","health","shopping","other"]}},` +
			`"required":["category"]}`,
	},
	{
		Name:        "get_budgets",
		Description: "Лимиты по категориям и сколько потрачено в этом месяце",
		ParamsJSON:  `{"type":"object","properties":{}}`,
	},
}

const promptTemplate = `Ты — финансовый помощник семьи. Через инструменты ты записываешь, показываете, ` +
	`изменяешь и удаляешь расходы участника, который с тобой разговаривает, а также ведёшь его долги и лимиты.

Сегодня: %s (%s).

Правила:
- Суммы в рублях, числом.
- Категории — только эти: food (еда), transport (транспорт), housing (жильё), ` +
	`entertainment (развлечения), health (здоровье), shopping (покупки), other (другое).
- Относительные даты («вчера», «в пятницу») переводи сам в YYYY-MM-DD, опираясь на сегодняшнюю дату.
- Чтобы показать, изменить или удалить конкретный расход, сначала найди его через list_expenses ` +
	`и используй его id из ответа.
- Когда просят показать расходы — вызови list_expenses и перечисли траты из ответа: сумму, категорию, ` +
	`дату и комментарий каждой. Никогда не отвечай про траты без вызова инструмента.
- После успешной записи подтверждай, что записал, и повторяй, что именно: сумму, категорию, дату ` +
	`(и комментарий, если был). Если инструмент вернул ошибку — скажи, что записать не получилось.
- В ответе list_expenses: total_count — сколько всего найдено операций, sum_rubles — их сумма в рублях. ` +
	`Суммы и числа бери только из ответов инструментов, не считай и не придумывай сам.
- Долги: owes_me — «мне должны» (Егор взял у меня в долг), i_owe — «я должен». ` +
	`В ответе list_debts: total_owes_me_rubles и total_i_owe_rubles — готовые суммы открытых долгов.
- Если в ответе add_expense есть блок budget: remaining_rubles — сколько осталось до лимита за месяц. ` +
	`Если exceeded true — предупреди, что месячный лимит превышен; если осталось меньше 20%% лимита — мягко напомни.
- Не выдумывай данные: если инструмент вернул ошибку, сообщи о ней пользователю своими словами.
- Отвечай кратко и по-русски.`

func (a *Agent) systemPrompt() string {
	t := a.clock.Now()
	return fmt.Sprintf(promptTemplate, t.Format("2006-01-02"), russianWeekday(t.Weekday()))
}

var weekdaysRu = map[time.Weekday]string{
	time.Sunday:    "воскресенье",
	time.Monday:    "понедельник",
	time.Tuesday:   "вторник",
	time.Wednesday: "среда",
	time.Thursday:  "четверг",
	time.Friday:    "пятница",
	time.Saturday:  "суббота",
}

func russianWeekday(w time.Weekday) string { return weekdaysRu[w] }

// runTool executes one tool call and returns the JSON payload for the
// model. Errors are carried inside the payload: the model should see
// them and explain the problem to the user.
func (a *Agent) runTool(ctx context.Context, userID int64, call port.ToolCall) string {
	payload := a.execTool(ctx, userID, call)
	b, err := json.Marshal(payload)
	if err != nil {
		return `{"error":"не удалось сформировать ответ инструмента"}`
	}
	return string(b)
}

func (a *Agent) execTool(ctx context.Context, userID int64, call port.ToolCall) any {
	switch call.Name {
	case "add_expense":
		return a.toolAdd(ctx, userID, call.ArgsJSON)
	case "list_expenses":
		return a.toolList(ctx, userID, call.ArgsJSON)
	case "update_expense":
		return a.toolUpdate(ctx, userID, call.ArgsJSON)
	case "delete_expense":
		return a.toolDelete(ctx, userID, call.ArgsJSON)
	case "get_summary":
		return a.toolSummary(ctx, userID, call.ArgsJSON)
	case "add_debt":
		return a.toolAddDebt(ctx, userID, call.ArgsJSON)
	case "list_debts":
		return a.toolListDebts(ctx, userID)
	case "repay_debt":
		return a.toolRepayDebt(ctx, userID, call.ArgsJSON)
	case "set_budget":
		return a.toolSetBudget(ctx, userID, call.ArgsJSON)
	case "remove_budget":
		return a.toolRemoveBudget(ctx, userID, call.ArgsJSON)
	case "get_budgets":
		return a.toolGetBudgets(ctx, userID)
	default:
		return map[string]string{"error": fmt.Sprintf("неизвестный инструмент %q", call.Name)}
	}
}

func toolError(err error) map[string]string {
	return map[string]string{"error": err.Error()}
}

type addArgs struct {
	Amount   float64 `json:"amount_rubles"`
	Category string  `json:"category"`
	SpentAt  string  `json:"spent_at"`
	Comment  string  `json:"comment"`
}

func (a *Agent) toolAdd(ctx context.Context, userID int64, args string) any {
	var in addArgs
	if err := json.Unmarshal([]byte(args), &in); err != nil {
		return toolError(errors.New("не удалось разобрать аргументы"))
	}
	kop, err := domain.KopecksFromRubles(in.Amount)
	if err != nil {
		return toolError(errors.New("некорректная сумма"))
	}
	cat, err := domain.ParseCategory(in.Category)
	if err != nil {
		return toolError(fmt.Errorf("категория %q недопустима, доступно: %s", in.Category, categoryList()))
	}
	spentAt, err := parseToolDate(in.SpentAt, a.today())
	if err != nil {
		return toolError(err)
	}
	e, err := a.expenses.Create(ctx, CreateInput{
		UserID:   userID,
		Amount:   kop,
		Category: cat,
		SpentAt:  spentAt,
		Comment:  in.Comment,
	})
	if err != nil {
		return toolError(err)
	}
	payload := expensePayload(e)
	// If the category has a monthly limit, attach its status so the model
	// can warn the user in the same reply — no extra tool round needed.
	if st, ok, err := a.budgets.StatusOf(ctx, userID, cat); err == nil && ok {
		payload["budget"] = budgetStatusPayload(st)
	}
	return payload
}

// budgetStatusPayload speaks rubles to the model, fully precomputed
// server-side so the model never does arithmetic.
func budgetStatusPayload(st BudgetStatus) map[string]any {
	return map[string]any{
		"category":                string(st.Category),
		"monthly_limit_rubles":    st.MonthlyLimit,
		"spent_this_month_rubles": st.SpentThisMonth,
		"remaining_rubles":        st.Remaining,
		"exceeded":                st.Exceeded,
	}
}

type listArgs struct {
	Category string `json:"category"`
	From     string `json:"from"`
	To       string `json:"to"`
	Limit    int    `json:"limit"`
}

func (a *Agent) toolList(ctx context.Context, userID int64, args string) any {
	var in listArgs
	if err := json.Unmarshal([]byte(args), &in); err != nil {
		return toolError(errors.New("не удалось разобрать аргументы"))
	}
	filter, err := a.filterFromArgs(userID, in.From, in.To, in.Category)
	if err != nil {
		return toolError(err)
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 10
	}
	if limit > 20 {
		limit = 20
	}
	filter.Limit = limit

	items, total, err := a.expenses.List(ctx, filter)
	if err != nil {
		return toolError(err)
	}
	return listPayload(items, total)
}

type updateArgs struct {
	ExpenseID string   `json:"expense_id"`
	Amount    *float64 `json:"amount_rubles"`
	Category  *string  `json:"category"`
	SpentAt   *string  `json:"spent_at"`
	Comment   *string  `json:"comment"`
}

func (a *Agent) toolUpdate(ctx context.Context, userID int64, args string) any {
	var in updateArgs
	if err := json.Unmarshal([]byte(args), &in); err != nil {
		return toolError(errors.New("не удалось разобрать аргументы"))
	}
	if in.ExpenseID == "" {
		return toolError(errors.New("укажите expense_id"))
	}
	inp := UpdateInput{}
	if in.Amount != nil {
		kop, err := domain.KopecksFromRubles(*in.Amount)
		if err != nil {
			return toolError(errors.New("некорректная сумма"))
		}
		inp.Amount = &kop
	}
	if in.Category != nil {
		cat, err := domain.ParseCategory(*in.Category)
		if err != nil {
			return toolError(fmt.Errorf("категория %q недопустима, доступно: %s", *in.Category, categoryList()))
		}
		inp.Category = &cat
	}
	if in.SpentAt != nil {
		d, err := parseToolDate(*in.SpentAt, a.today())
		if err != nil {
			return toolError(err)
		}
		inp.SpentAt = &d
	}
	e, err := a.expenses.Update(ctx, &userID, in.ExpenseID, inp)
	if err != nil {
		return toolError(err)
	}
	return expensePayload(e)
}

func (a *Agent) toolDelete(ctx context.Context, userID int64, args string) any {
	var in struct {
		ExpenseID string `json:"expense_id"`
	}
	if err := json.Unmarshal([]byte(args), &in); err != nil {
		return toolError(errors.New("не удалось разобрать аргументы"))
	}
	if in.ExpenseID == "" {
		return toolError(errors.New("укажите expense_id"))
	}
	if err := a.expenses.Delete(ctx, &userID, in.ExpenseID); err != nil {
		return toolError(err)
	}
	return map[string]bool{"ok": true}
}

func (a *Agent) toolSummary(ctx context.Context, userID int64, args string) any {
	var in struct {
		From     string `json:"from"`
		To       string `json:"to"`
		Category string `json:"category"`
	}
	if err := json.Unmarshal([]byte(args), &in); err != nil {
		return toolError(errors.New("не удалось разобрать аргументы"))
	}
	filter, err := a.filterFromArgs(userID, in.From, in.To, in.Category)
	if err != nil {
		return toolError(err)
	}

	// The repository pages at listPage; summaries need every matching
	// row, so page through with a hard bound (a family's history fits
	// comfortably; anything beyond is reported as truncated).
	var (
		byCat     = map[domain.Category]domain.Kopecks{}
		sum       domain.Kopecks
		collected int
	)
	filter.Limit = listPage
	for page := 0; page < maxSummaryPages; page++ {
		filter.Offset = page * listPage
		items, _, err := a.expenses.List(ctx, filter)
		if err != nil {
			return toolError(err)
		}
		for _, e := range items {
			byCat[e.Category] += e.Amount
			sum += e.Amount
		}
		collected += len(items)
		if len(items) < listPage {
			break
		}
	}
	cats := map[string]float64{}
	for c, k := range byCat {
		cats[string(c)] = k.Rubles()
	}
	payload := map[string]any{
		"total_rubles": sum.Rubles(),
		"count":        collected,
		"by_category":  cats,
	}
	if collected == maxSummaryPages*listPage {
		payload["note"] = "показана часть данных"
	}
	return payload
}

type addDebtArgs struct {
	Debtor    string  `json:"debtor"`
	Amount    float64 `json:"amount_rubles"`
	Direction string  `json:"direction"`
	Comment   string  `json:"comment"`
}

func (a *Agent) toolAddDebt(ctx context.Context, userID int64, args string) any {
	var in addDebtArgs
	if err := json.Unmarshal([]byte(args), &in); err != nil {
		return toolError(errors.New("не удалось разобрать аргументы"))
	}
	kop, err := domain.KopecksFromRubles(in.Amount)
	if err != nil {
		return toolError(errors.New("некорректная сумма"))
	}
	dir := domain.DebtOwesMe
	if in.Direction != "" {
		if !domain.DebtDirection(in.Direction).Valid() {
			return toolError(fmt.Errorf("направление %q недопустимо: owes_me или i_owe", in.Direction))
		}
		dir = domain.DebtDirection(in.Direction)
	}
	d, err := a.debts.Create(ctx, DebtInput{
		UserID:    userID,
		Direction: dir,
		Debtor:    in.Debtor,
		Amount:    kop,
		Comment:   in.Comment,
	})
	if err != nil {
		return toolError(err)
	}
	return debtPayload(d)
}

func (a *Agent) toolListDebts(ctx context.Context, userID int64) any {
	// Only open debts: the model shows what is still outstanding; the
	// totals are precomputed so no arithmetic lands on the model.
	open := true
	items, err := a.debts.List(ctx, port.DebtFilter{UserID: &userID, Open: &open})
	if err != nil {
		return toolError(err)
	}
	var owesMe, iOwe domain.Kopecks
	out := make([]map[string]any, len(items))
	for i, d := range items {
		out[i] = debtPayload(d)
		if d.Direction == domain.DebtOwesMe {
			owesMe += d.Amount
		} else {
			iOwe += d.Amount
		}
	}
	return map[string]any{
		"items":                out,
		"total_owes_me_rubles": owesMe.Rubles(),
		"total_i_owe_rubles":   iOwe.Rubles(),
	}
}

func (a *Agent) toolRepayDebt(ctx context.Context, userID int64, args string) any {
	var in struct {
		DebtID string `json:"debt_id"`
	}
	if err := json.Unmarshal([]byte(args), &in); err != nil {
		return toolError(errors.New("не удалось разобрать аргументы"))
	}
	if in.DebtID == "" {
		return toolError(errors.New("укажите debt_id"))
	}
	d, err := a.debts.Settle(ctx, userID, in.DebtID, a.clock.Now())
	if err != nil {
		return toolError(err)
	}
	return debtPayload(d)
}

func (a *Agent) toolSetBudget(ctx context.Context, userID int64, args string) any {
	var in struct {
		Category string  `json:"category"`
		Limit    float64 `json:"monthly_limit_rubles"`
	}
	if err := json.Unmarshal([]byte(args), &in); err != nil {
		return toolError(errors.New("не удалось разобрать аргументы"))
	}
	kop, err := domain.KopecksFromRubles(in.Limit)
	if err != nil {
		return toolError(errors.New("некорректная сумма лимита"))
	}
	b, err := a.budgets.Set(ctx, BudgetInput{
		UserID:       userID,
		Category:     domain.Category(in.Category),
		MonthlyLimit: kop,
	})
	if err != nil {
		return toolError(err)
	}
	return budgetPayload(b)
}

func (a *Agent) toolRemoveBudget(ctx context.Context, userID int64, args string) any {
	var in struct {
		Category string `json:"category"`
	}
	if err := json.Unmarshal([]byte(args), &in); err != nil {
		return toolError(errors.New("не удалось разобрать аргументы"))
	}
	if err := a.budgets.Remove(ctx, userID, domain.Category(in.Category)); err != nil {
		return toolError(err)
	}
	return map[string]bool{"ok": true}
}

func (a *Agent) toolGetBudgets(ctx context.Context, userID int64) any {
	status, err := a.budgets.Status(ctx, userID)
	if err != nil {
		return toolError(err)
	}
	items := make([]map[string]any, len(status))
	for i, st := range status {
		items[i] = budgetStatusPayload(st)
	}
	return map[string]any{"items": items}
}

// debtPayload speaks rubles to the model.
func debtPayload(d domain.Debt) map[string]any {
	return map[string]any{
		"id":            d.ID,
		"direction":     string(d.Direction),
		"debtor":        d.Debtor,
		"amount_rubles": d.Amount.Rubles(),
		"comment":       d.Comment,
		"created_at":    d.CreatedAt.Format("2006-01-02"),
		"settled":       d.SettledAt != nil,
	}
}

func budgetPayload(b domain.Budget) map[string]any {
	return map[string]any{
		"category":             string(b.Category),
		"monthly_limit_rubles": b.MonthlyLimit.Rubles(),
	}
}

// filterFromArgs builds a scoped filter; empty period bounds are nil.
func (a *Agent) filterFromArgs(userID int64, from, to, category string) (port.ExpenseFilter, error) {
	f := port.ExpenseFilter{UserID: &userID}
	if from != "" {
		d, err := parseToolDate(from, a.today())
		if err != nil {
			return f, err
		}
		f.From = &d
	}
	if to != "" {
		d, err := parseToolDate(to, a.today())
		if err != nil {
			return f, err
		}
		f.To = &d
	}
	if category != "" {
		cat, err := domain.ParseCategory(category)
		if err != nil {
			return f, fmt.Errorf("категория %q недопустима, доступно: %s", category, categoryList())
		}
		f.Category = &cat
	}
	return f, nil
}

func (a *Agent) today() domain.Date {
	t := a.clock.Now()
	return domain.DateFromTime(t, t.Location())
}

// parseToolDate accepts an empty string as "default" (today).
func parseToolDate(s string, def domain.Date) (domain.Date, error) {
	if s == "" {
		return def, nil
	}
	d, err := domain.ParseDate(s)
	if err != nil {
		return domain.Date{}, errors.New("дата должна быть в формате YYYY-MM-DD")
	}
	return d, nil
}

// expensePayload speaks rubles to the model and includes the id so the
// model can reference the expense in later tool calls.
func expensePayload(e domain.Expense) map[string]any {
	return map[string]any{
		"id":            e.ID,
		"amount_rubles": e.Amount.Rubles(),
		"category":      string(e.Category),
		"spent_at":      e.SpentAt.String(),
		"comment":       e.Comment,
	}
}

func listPayload(items []domain.Expense, total int) map[string]any {
	out := make([]map[string]any, len(items))
	sum := domain.Kopecks(0)
	for i, e := range items {
		out[i] = expensePayload(e)
		sum += e.Amount
	}
	// total_count is the number of matching expenses (pagination hint);
	// sum_rubles is precomputed so the model never adds amounts itself.
	return map[string]any{
		"items":       out,
		"total_count": total,
		"sum_rubles":  sum.Rubles(),
	}
}

// categoryList is the human-readable category list for error messages.
func categoryList() string {
	cats := domain.Categories()
	names := make([]string, len(cats))
	for i, c := range cats {
		names[i] = string(c)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}
