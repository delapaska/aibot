// The adapter converts the model's markdown into the HTML subset
// Telegram renders (parse_mode "HTML"). Telegram cannot show GitHub-style
// headers or tables, so they map to the closest supported form: headers
// become bold lines, tables and fences become monospace blocks. Anything
// not understood stays literal text, so a malformed reply can never break
// the parse and kill the whole message.
package telegram

import (
	"html"
	"strings"
	"unicode"
	"unicode/utf8"
)

// toTelegramHTML converts one message of markdown into Telegram HTML.
func toTelegramHTML(src string) string {
	lines := strings.Split(src, "\n")
	out := make([]string, 0, len(lines))
	for i := 0; i < len(lines); {
		line := lines[i]
		switch {
		case isFence(line):
			// A fence renders as a monospace block only when it closes;
			// an unclosed ``` stays literal text.
			close := -1
			for j := i + 1; j < len(lines); j++ {
				if isFence(lines[j]) {
					close = j
					break
				}
			}
			if close < 0 {
				out = append(out, inlineHTML(line))
				i++
				continue
			}
			if close > i+1 {
				body := strings.Join(lines[i+1:close], "\n")
				out = append(out, "<pre>"+html.EscapeString(body)+"</pre>")
			}
			i = close + 1
		case isTableRow(line):
			var rows []string
			j := i
			for ; j < len(lines) && isTableRow(lines[j]); j++ {
				if !isTableSeparator(lines[j]) {
					rows = append(rows, trimTableRow(lines[j]))
				}
			}
			if len(rows) > 0 {
				out = append(out, "<pre>"+html.EscapeString(strings.Join(rows, "\n"))+"</pre>")
			}
			i = j
		default:
			if h, ok := headerText(line); ok {
				out = append(out, "<b>"+inlineHTML(h)+"</b>")
			} else if q, ok := quoteText(line); ok {
				out = append(out, "<blockquote>"+inlineHTML(q)+"</blockquote>")
			} else {
				out = append(out, inlineHTML(line))
			}
			i++
		}
	}
	return strings.Join(out, "\n")
}

// isFence reports whether the line opens or closes a code fence.
func isFence(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "```")
}

// isTableRow reports whether the line looks like a markdown table row:
// it starts and effectively ends with pipes.
func isTableRow(line string) bool {
	row := strings.TrimSpace(line)
	return strings.HasPrefix(row, "|") && strings.HasSuffix(row, "|") &&
		strings.Count(row, "|") >= 2
}

// isTableSeparator reports whether the line is the |---|---| separator
// row of a table; such rows are dropped from the rendered block.
func isTableSeparator(line string) bool {
	row := strings.Trim(strings.TrimSpace(line), "|")
	if row == "" {
		return false
	}
	for _, cell := range strings.Split(row, "|") {
		c := strings.TrimSpace(cell)
		c = strings.Trim(c, ":")
		if c == "" || strings.Trim(c, "-") != "" {
			return false
		}
	}
	return true
}

// trimTableRow strips the leading and trailing pipes of a table row, so
// "| Еда | 350 |" renders as "Еда | 350" inside the monospace block.
func trimTableRow(line string) string {
	row := strings.TrimSpace(line)
	row = strings.TrimPrefix(row, "|")
	row = strings.TrimSuffix(row, "|")
	return strings.TrimSpace(row)
}

// headerText recognizes an ATX header ("## Заголовок") and returns the
// title text.
func headerText(line string) (string, bool) {
	n := 0
	for n < len(line) && line[n] == '#' {
		n++
	}
	if n == 0 || n > 6 {
		return "", false
	}
	rest := line[n:]
	if rest == "" || (rest[0] != ' ' && rest[0] != '\t') {
		return "", false
	}
	return strings.TrimSpace(rest), true
}

// quoteText recognizes a blockquote line ("> текст").
func quoteText(line string) (string, bool) {
	t := strings.TrimLeft(line, " \t")
	if !strings.HasPrefix(t, ">") {
		return "", false
	}
	body := strings.TrimPrefix(strings.TrimPrefix(t, ">"), " ")
	if body == "" {
		return "", false
	}
	return strings.TrimSpace(body), true
}

// inlineHTML converts one line's inline markdown (bold, italics, code,
// links), escaping HTML on the way. Works on a single line: the block
// pass splits the source beforehand.
func inlineHTML(s string) string {
	var b strings.Builder
	start := 0
	flush := func(end int) {
		if end > start {
			b.WriteString(html.EscapeString(s[start:end]))
		}
		start = end
	}
	for i := 0; i < len(s); {
		switch c := s[i]; c {
		case '`':
			if end := strings.IndexByte(s[i+1:], '`'); end > 0 {
				if content := s[i+1 : i+1+end]; edgeOK(content) {
					flush(i)
					b.WriteString("<code>")
					b.WriteString(html.EscapeString(content))
					b.WriteString("</code>")
					i += end + 2
					start = i
					continue
				}
			}
		case '[':
			if text, url, n, ok := parseLink(s[i:]); ok {
				flush(i)
				b.WriteString(`<a href="`)
				b.WriteString(html.EscapeString(url))
				b.WriteString(`">`)
				b.WriteString(inlineHTML(text))
				b.WriteString("</a>")
				i += n
				start = i
				continue
			}
		case '*', '_':
			if m, n, ok := emphasisHTML(s[i:], prevIsWord(s, i)); ok {
				flush(i)
				b.WriteString(m)
				i += n
				start = i
				continue
			}
		}
		i++
	}
	flush(len(s))
	return b.String()
}

// prevIsWord reports whether the rune before s[i] is a letter, digit or
// underscore — i.e. the marker sits inside a word.
func prevIsWord(s string, i int) bool {
	if i == 0 {
		return false
	}
	r, _ := utf8.DecodeLastRuneInString(s[:i])
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// emphasisHTML converts markdown emphasis opening at s[0] ('*' or '_')
// into <b>/<i>. It returns the HTML, the consumed byte count, and
// whether a conversion happened; without a well-formed pair the markers
// stay literal text.
func emphasisHTML(s string, prevWord bool) (string, int, bool) {
	mark := s[0]
	// Markers open only outside a word: this keeps snake_case
	// identifiers and "2 * 3" arithmetic intact.
	if prevWord || len(s) < 2 {
		return "", 0, false
	}
	if s[1] == mark { // doubled marker: bold
		if end := closer(s, 2, mark, 2); end > 0 {
			content := s[2 : end-2]
			if edgeOK(content) {
				return "<b>" + inlineHTML(content) + "</b>", end, true
			}
		}
		return "", 0, false
	}
	if end := closer(s, 1, mark, 1); end > 0 {
		content := s[1 : end-1]
		if edgeOK(content) {
			return "<i>" + inlineHTML(content) + "</i>", end, true
		}
	}
	return "", 0, false
}

// closer finds the index just past the closing marker (n copies of mark)
// for emphasis opened at s[0]. The content between must be non-empty and
// must not be preceded by a space; otherwise there is no closer.
func closer(s string, from int, mark byte, n int) int {
	for j := from; j+n <= len(s); j++ {
		if s[j] != mark {
			continue
		}
		match := true
		for k := 1; k < n; k++ {
			if s[j+k] != mark {
				match = false
				break
			}
		}
		if !match {
			continue
		}
		if j == from || s[j-1] == ' ' || s[j-1] == '\t' {
			return 0
		}
		return j + n
	}
	return 0
}

// edgeOK rejects content whose first or last byte is a space: "** a **"
// is not emphasis.
func edgeOK(content string) bool {
	first, last := content[0], content[len(content)-1]
	return first != ' ' && first != '\t' && last != ' ' && last != '\t'
}

// parseLink parses "[text](url)" at the start of s. Only http(s), tg and
// mailto links convert: Telegram rejects other schemes, and a rejected
// parse_mode kills the whole message.
func parseLink(s string) (text, url string, consumed int, ok bool) {
	if len(s) == 0 || s[0] != '[' {
		return "", "", 0, false
	}
	end := strings.IndexByte(s, ']')
	if end <= 1 {
		return "", "", 0, false
	}
	if end+1 >= len(s) || s[end+1] != '(' {
		return "", "", 0, false
	}
	close := strings.IndexByte(s[end+2:], ')')
	if close < 0 {
		return "", "", 0, false
	}
	u := s[end+2 : end+2+close]
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") &&
		!strings.HasPrefix(u, "tg://") && !strings.HasPrefix(u, "mailto:") {
		return "", "", 0, false
	}
	return s[1:end], u, end + 2 + close + 1, true
}
