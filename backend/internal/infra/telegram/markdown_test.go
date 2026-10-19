package telegram

import "testing"

func TestToTelegramHTML(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "plain text unchanged",
			in:   "Записал: Еда — 350,13 ₽, 17.10.2026.",
			want: "Записал: Еда — 350,13 ₽, 17.10.2026.",
		},
		{
			name: "bold",
			in:   "**Итого: 25 500 ₽**",
			want: "<b>Итого: 25 500 ₽</b>",
		},
		{
			name: "italics",
			in:   "_небольшая ремарка_",
			want: "<i>небольшая ремарка</i>",
		},
		{
			name: "inline code",
			in:   "команда `add_expense` сделана",
			want: "команда <code>add_expense</code> сделана",
		},
		{
			name: "header becomes bold",
			in:   "## Траты за неделю",
			want: "<b>Траты за неделю</b>",
		},
		{
			name: "header with inline bold",
			in:   "## **Итоги** дня",
			want: "<b><b>Итоги</b> дня</b>",
		},
		{
			name: "header without space stays literal",
			in:   "#hashtag",
			want: "#hashtag",
		},
		{
			name: "too many hashes stays literal",
			in:   "####### семь",
			want: "####### семь",
		},
		{
			name: "table becomes monospace block",
			in:   "| Категория | Сумма |\n|---|---|\n| Еда | 350 |\n| Такси | 500 |",
			want: "<pre>Категория | Сумма\nЕда | 350\nТакси | 500</pre>",
		},
		{
			name: "table cells escaped",
			in:   "| a<b | c |",
			want: "<pre>a&lt;b | c</pre>",
		},
		{
			name: "fenced code block",
			in:   "```go\nx := 1 & 2\n```",
			want: "<pre>x := 1 &amp; 2</pre>",
		},
		{
			name: "unclosed fence stays literal",
			in:   "начало ```код\nи ещё",
			want: "начало ```код\nи ещё",
		},
		{
			name: "html is escaped",
			in:   "5 < 10 & 4 > 3",
			want: "5 &lt; 10 &amp; 4 &gt; 3",
		},
		{
			name: "link becomes anchor",
			in:   "[чек](https://example.com/x.png)",
			want: `<a href="https://example.com/x.png">чек</a>`,
		},
		{
			name: "javascript link stays literal",
			in:   "[клик](javascript:alert(1))",
			want: "[клик](javascript:alert(1))",
		},
		{
			name: "snake_case untouched",
			in:   "total_owes_me_rubles = 1",
			want: "total_owes_me_rubles = 1",
		},
		{
			name: "arithmetic asterisks untouched",
			in:   "2 * 3 * 4 = 24",
			want: "2 * 3 * 4 = 24",
		},
		{
			name: "single star at word start converts",
			in:   "*важно*",
			want: "<i>важно</i>",
		},
		{
			name: "unmatched bold stays literal",
			in:   "**незакрытый",
			want: "**незакрытый",
		},
		{
			name: "spaced bold is not emphasis",
			in:   "** пробелы **",
			want: "** пробелы **",
		},
		{
			name: "empty code span stays literal",
			in:   "пустые `` кавычки",
			want: "пустые `` кавычки",
		},
		{
			name: "blockquote",
			in:   "> цитата дня",
			want: "<blockquote>цитата дня</blockquote>",
		},
		{
			name: "mixed message",
			in:   "## Траты\n**Еда** — 350 ₽, ` Cafe ` не парсим\nВсего: 5 < 6",
			want: "<b>Траты</b>\n<b>Еда</b> — 350 ₽, ` Cafe ` не парсим\nВсего: 5 &lt; 6",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := toTelegramHTML(tc.in); got != tc.want {
				t.Errorf("toTelegramHTML(%q)\n  got  %q\n  want %q", tc.in, got, tc.want)
			}
		})
	}
}
