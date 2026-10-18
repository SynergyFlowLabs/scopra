package main

import (
	"html"
	"strings"
	"unicode"
)

// inline renders one run of text. It never emits raw HTML from the source:
// anything that is not recognized syntax is escaped.
func (r *renderer) inline(text string) string {
	var out strings.Builder
	for index := 0; index < len(text); {
		char := text[index]
		switch {
		case char == '\\' && index+1 < len(text) && isPunctuation(text[index+1]):
			out.WriteString(html.EscapeString(string(text[index+1])))
			index += 2
		case char == '`':
			index = r.codeSpan(&out, text, index)
		case char == '!' && index+1 < len(text) && text[index+1] == '[':
			if next, ok := r.link(&out, text, index, true); ok {
				index = next
			} else {
				out.WriteString("!")
				index++
			}
		case char == '[':
			if next, ok := r.link(&out, text, index, false); ok {
				index = next
			} else {
				out.WriteString("[")
				index++
			}
		case char == '<':
			if next, ok := r.autolink(&out, text, index); ok {
				index = next
			} else {
				out.WriteString("&lt;")
				index++
			}
		case char == '*' || char == '_' || char == '~':
			if next, ok := r.emphasis(&out, text, index); ok {
				index = next
			} else {
				out.WriteString(html.EscapeString(string(char)))
				index++
			}
		default:
			start := index
			for index < len(text) && !strings.ContainsRune("\\`![<*_~", rune(text[index])) {
				index++
			}
			out.WriteString(html.EscapeString(text[start:index]))
		}
	}
	return out.String()
}

func isPunctuation(char byte) bool {
	return strings.ContainsRune("!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~", rune(char))
}

func (r *renderer) codeSpan(out *strings.Builder, text string, start int) int {
	ticks := 0
	for start+ticks < len(text) && text[start+ticks] == '`' {
		ticks++
	}
	closing := strings.Index(text[start+ticks:], strings.Repeat("`", ticks))
	if closing < 0 {
		out.WriteString(strings.Repeat("`", ticks))
		return start + ticks
	}
	code := text[start+ticks : start+ticks+closing]
	out.WriteString("<code>" + html.EscapeString(code) + "</code>")
	return start + ticks + closing + ticks
}

func (r *renderer) link(out *strings.Builder, text string, start int, image bool) (int, bool) {
	open := start
	if image {
		open++
	}
	closeBracket, ok := matchingBracket(text, open)
	if !ok {
		return 0, false
	}
	if closeBracket+1 >= len(text) || text[closeBracket+1] != '(' {
		return 0, false
	}
	depth := 0
	end := -1
	for index := closeBracket + 1; index < len(text); index++ {
		switch text[index] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				end = index
			}
		}
		if end >= 0 {
			break
		}
	}
	if end < 0 {
		return 0, false
	}
	label := text[open+1 : closeBracket]
	destination, title := splitDestination(text[closeBracket+2 : end])
	external := strings.HasPrefix(destination, "http://") || strings.HasPrefix(destination, "https://")
	if r.resolve != nil {
		destination = r.resolve(destination)
	}
	titleAttribute := ""
	if title != "" {
		titleAttribute = ` title="` + html.EscapeString(title) + `"`
	}
	rel := ""
	if external {
		rel = ` rel="noopener noreferrer"`
	}
	if image {
		out.WriteString(`<img src="` + html.EscapeString(destination) + `" alt="` +
			html.EscapeString(plainText(label)) + `"` + titleAttribute + ` loading="lazy">`)
	} else {
		out.WriteString(`<a href="` + html.EscapeString(destination) + `"` + titleAttribute + rel + `>` +
			r.inline(label) + `</a>`)
	}
	return end + 1, true
}

// splitDestination separates a link destination from an optional quoted title.
func splitDestination(raw string) (string, string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}
	if strings.HasPrefix(raw, "<") {
		if close := strings.IndexByte(raw, '>'); close > 0 {
			return raw[1:close], ""
		}
	}
	fields := strings.Fields(raw)
	if len(fields) == 1 {
		return raw, ""
	}
	destination := fields[0]
	title := strings.TrimSpace(strings.TrimPrefix(raw, destination))
	title = strings.Trim(title, `"'()`)
	return destination, title
}

func (r *renderer) autolink(out *strings.Builder, text string, start int) (int, bool) {
	close := strings.IndexByte(text[start:], '>')
	if close < 0 {
		return 0, false
	}
	target := text[start+1 : start+close]
	if !strings.HasPrefix(target, "http://") && !strings.HasPrefix(target, "https://") {
		return 0, false
	}
	out.WriteString(`<a href="` + html.EscapeString(target) + `" rel="noopener noreferrer">` +
		html.EscapeString(target) + `</a>`)
	return start + close + 1, true
}

func (r *renderer) emphasis(out *strings.Builder, text string, start int) (int, bool) {
	char := text[start]
	run := 1
	for start+run < len(text) && text[start+run] == char {
		run++
	}
	marker := string(char)
	switch {
	case char == '~':
		if run < 2 {
			return 0, false
		}
		marker = "~~"
	case run >= 2:
		marker = strings.Repeat(marker, 2)
	}
	// Underscore emphasis never happens inside a word: file_name.md keeps its
	// underscores.
	if char == '_' && start > 0 {
		previous := rune(text[start-1])
		if unicode.IsLetter(previous) || unicode.IsDigit(previous) {
			return 0, false
		}
	}
	rest := text[start+len(marker):]
	close := strings.Index(rest, marker)
	if close <= 0 {
		return 0, false
	}
	inner := rest[:close]
	if char == '_' && len(rest) > close+len(marker) {
		next := rune(rest[close+len(marker)])
		if unicode.IsLetter(next) || unicode.IsDigit(next) {
			return 0, false
		}
	}
	tag := "em"
	switch marker {
	case "**", "__":
		tag = "strong"
	case "~~":
		tag = "del"
	}
	out.WriteString("<" + tag + ">" + r.inline(inner) + "</" + tag + ">")
	return start + len(marker) + close + len(marker), true
}

// matchingBracket finds the "]" that closes the "[" at open, skipping code
// spans, escapes, and nested brackets (a badge is an image inside a link).
func matchingBracket(text string, open int) (int, bool) {
	depth := 0
	for index := open; index < len(text); index++ {
		switch text[index] {
		case '\\':
			index++
		case '`':
			ticks := 1
			for index+ticks < len(text) && text[index+ticks] == '`' {
				ticks++
			}
			if closing := strings.Index(text[index+ticks:], strings.Repeat("`", ticks)); closing >= 0 {
				index += ticks + closing + ticks - 1
			}
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return index, true
			}
		}
	}
	return 0, false
}
