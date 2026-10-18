// Package main renders Scopra's Markdown documentation as a static site
// (ADR-015). It uses only the standard library: the project keeps no external
// Go dependencies, and a docs generator should not be the exception.
package main

import (
	"fmt"
	"html"
	"regexp"
	"strings"
	"unicode"
)

// Heading is one rendered section heading, used to build a table of contents.
type Heading struct {
	Level int
	Text  string
	ID    string
}

// LinkResolver rewrites a link destination. A nil resolver leaves links alone.
type LinkResolver func(destination string) string

type renderer struct {
	resolve   LinkResolver
	anchors   map[string]int
	headings  []Heading
	inlineIDs map[string]string
}

// RenderMarkdown converts the Markdown subset used by this repository to HTML.
//
// It deliberately does not accept raw HTML: every character outside the
// supported syntax is escaped, so a document can never inject markup. The
// supported syntax is the one the docs actually use — ATX headings, fenced
// code, GFM tables, ordered and unordered lists (nested, with task boxes),
// blockquotes, thematic breaks, and inline code, emphasis, strikethrough,
// links, images, and autolinks.
func RenderMarkdown(source []byte, resolve LinkResolver) (string, []Heading, error) {
	lines := strings.Split(strings.ReplaceAll(string(source), "\r\n", "\n"), "\n")
	r := &renderer{resolve: resolve, anchors: map[string]int{}, inlineIDs: map[string]string{}}
	body := r.blocks(lines)
	return body, r.headings, nil
}

var (
	fencePattern     = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})\\s*([^\\s`]*).*$")
	headingPattern   = regexp.MustCompile(`^ {0,3}(#{1,6})\s+(.*?)\s*#*\s*$`)
	rulePattern      = regexp.MustCompile(`^ {0,3}((\*\s*){3,}|(-\s*){3,}|(_\s*){3,})$`)
	listPattern      = regexp.MustCompile(`^(\s*)([-*+]|\d{1,9}[.)])\s+(.*)$`)
	tableRulePattern = regexp.MustCompile(`^\s*\|?\s*:?-{1,}:?\s*(\|\s*:?-{1,}:?\s*)*\|?\s*$`)
	taskPattern      = regexp.MustCompile(`^\[([ xX])\]\s+(.*)$`)
	quotePattern     = regexp.MustCompile(`^ {0,3}>\s?(.*)$`)
)

// hardBreakToken marks an explicit line break; it cannot occur in a document.
const hardBreakToken = "\x00"

func isBlank(line string) bool { return strings.TrimSpace(line) == "" }

func (r *renderer) blocks(lines []string) string {
	var out strings.Builder
	for index := 0; index < len(lines); {
		line := lines[index]
		switch {
		case isBlank(line):
			index++
		case fencePattern.MatchString(line):
			index = r.fencedCode(&out, lines, index)
		case headingPattern.MatchString(line):
			r.headingBlock(&out, line)
			index++
		case rulePattern.MatchString(line):
			out.WriteString("<hr>\n")
			index++
		case quotePattern.MatchString(line):
			index = r.blockquote(&out, lines, index)
		case startsTable(lines, index):
			index = r.table(&out, lines, index)
		case listPattern.MatchString(line):
			index = r.list(&out, lines, index)
		default:
			index = r.paragraph(&out, lines, index)
		}
	}
	return out.String()
}

func (r *renderer) fencedCode(out *strings.Builder, lines []string, start int) int {
	opening := fencePattern.FindStringSubmatch(lines[start])
	fence := opening[1]
	language := strings.ToLower(opening[2])
	start++
	var code []string
	for start < len(lines) {
		trimmed := strings.TrimSpace(lines[start])
		if strings.HasPrefix(trimmed, strings.Repeat(string(fence[0]), len(fence))) &&
			strings.Trim(trimmed, string(fence[0])) == "" && len(lines[start])-len(strings.TrimLeft(lines[start], " ")) <= 3 {
			start++
			break
		}
		code = append(code, lines[start])
		start++
	}
	class := ""
	if language != "" {
		class = ` class="language-` + html.EscapeString(language) + `"`
	}
	out.WriteString("<pre><code" + class + ">")
	out.WriteString(html.EscapeString(strings.Join(code, "\n")))
	if len(code) > 0 {
		out.WriteString("\n")
	}
	out.WriteString("</code></pre>\n")
	return start
}

func (r *renderer) headingBlock(out *strings.Builder, line string) {
	match := headingPattern.FindStringSubmatch(line)
	level := len(match[1])
	text := match[2]
	plain := plainText(text)
	id := r.uniqueAnchor(slug(plain))
	r.headings = append(r.headings, Heading{Level: level, Text: plain, ID: id})
	fmt.Fprintf(out, `<h%d id="%s">%s</h%d>`+"\n", level, id, r.inline(text), level)
}

func (r *renderer) uniqueAnchor(base string) string {
	if base == "" {
		base = "section"
	}
	if count, seen := r.anchors[base]; seen {
		count++
		r.anchors[base] = count
		return fmt.Sprintf("%s-%d", base, count)
	}
	r.anchors[base] = 0
	return base
}

func (r *renderer) blockquote(out *strings.Builder, lines []string, start int) int {
	var quoted []string
	for start < len(lines) && quotePattern.MatchString(lines[start]) {
		quoted = append(quoted, quotePattern.FindStringSubmatch(lines[start])[1])
		start++
	}
	out.WriteString("<blockquote>\n")
	out.WriteString(r.blocks(quoted))
	out.WriteString("</blockquote>\n")
	return start
}

func startsTable(lines []string, index int) bool {
	if index+1 >= len(lines) || !strings.Contains(lines[index], "|") {
		return false
	}
	return tableRulePattern.MatchString(lines[index+1]) && strings.Contains(lines[index+1], "-")
}

func (r *renderer) table(out *strings.Builder, lines []string, start int) int {
	header := splitRow(lines[start])
	alignments := make([]string, len(header))
	for index, cell := range splitRow(lines[start+1]) {
		if index < len(alignments) {
			alignments[index] = alignment(cell)
		}
	}
	start += 2
	var rows [][]string
	for start < len(lines) && strings.Contains(lines[start], "|") && !isBlank(lines[start]) {
		rows = append(rows, splitRow(lines[start]))
		start++
	}
	out.WriteString("<div class=\"table-wrap\"><table>\n<thead><tr>")
	for index, cell := range header {
		fmt.Fprintf(out, `<th%s>%s</th>`, alignAttribute(alignments, index), r.inline(cell))
	}
	out.WriteString("</tr></thead>\n<tbody>\n")
	for _, row := range rows {
		out.WriteString("<tr>")
		for index := range header {
			cell := ""
			if index < len(row) {
				cell = row[index]
			}
			fmt.Fprintf(out, `<td%s>%s</td>`, alignAttribute(alignments, index), r.inline(cell))
		}
		out.WriteString("</tr>\n")
	}
	out.WriteString("</tbody></table></div>\n")
	return start
}

func alignAttribute(alignments []string, index int) string {
	if index < len(alignments) && alignments[index] != "" {
		return ` class="align-` + alignments[index] + `"`
	}
	return ""
}

func alignment(cell string) string {
	cell = strings.TrimSpace(cell)
	left := strings.HasPrefix(cell, ":")
	right := strings.HasSuffix(cell, ":")
	switch {
	case left && right:
		return "center"
	case right:
		return "right"
	case left:
		return "left"
	}
	return ""
}

// splitRow splits a table row on unescaped pipes that are outside code spans.
func splitRow(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	var cells []string
	var cell strings.Builder
	inCode := false
	for index := 0; index < len(line); index++ {
		char := line[index]
		switch {
		case char == '\\' && index+1 < len(line) && line[index+1] == '|':
			cell.WriteByte('|')
			index++
		case char == '`':
			inCode = !inCode
			cell.WriteByte(char)
		case char == '|' && !inCode:
			cells = append(cells, strings.TrimSpace(cell.String()))
			cell.Reset()
		default:
			cell.WriteByte(char)
		}
	}
	cells = append(cells, strings.TrimSpace(cell.String()))
	return cells
}

func (r *renderer) list(out *strings.Builder, lines []string, start int) int {
	match := listPattern.FindStringSubmatch(lines[start])
	baseIndent := len(match[1])
	ordered := match[2][0] >= '0' && match[2][0] <= '9'
	tag := "ul"
	if ordered {
		tag = "ol"
	}
	startAttribute := ""
	if ordered {
		number := strings.TrimRight(match[2], ".)")
		if number != "1" {
			startAttribute = ` start="` + number + `"`
		}
	}
	fmt.Fprintf(out, "<%s%s>\n", tag, startAttribute)

	for start < len(lines) {
		current := listPattern.FindStringSubmatch(lines[start])
		if current == nil || len(current[1]) != baseIndent || isOrdered(current[2]) != ordered {
			break
		}
		contentIndent := baseIndent + len(current[2]) + 1
		itemLines := []string{current[3]}
		start++
		sawBlank := false
		for start < len(lines) {
			line := lines[start]
			if isBlank(line) {
				// A blank line belongs to the item only if the next line is
				// indented under it; otherwise the list ends.
				if start+1 < len(lines) && indentOf(lines[start+1]) > baseIndent && !isBlank(lines[start+1]) {
					itemLines = append(itemLines, "")
					sawBlank = true
					start++
					continue
				}
				break
			}
			if indentOf(line) <= baseIndent {
				break
			}
			itemLines = append(itemLines, dedent(line, contentIndent))
			start++
		}
		r.listItem(out, itemLines, sawBlank)
	}

	fmt.Fprintf(out, "</%s>\n", tag)
	return start
}

func (r *renderer) listItem(out *strings.Builder, lines []string, loose bool) {
	task := ""
	body := lines
	if len(lines) > 0 {
		if match := taskPattern.FindStringSubmatch(lines[0]); match != nil {
			if match[1] == " " {
				task = `<input type="checkbox" disabled> `
			} else {
				task = `<input type="checkbox" checked disabled> `
			}
			body = append([]string{match[2]}, lines[1:]...)
		}
	}
	if !loose {
		lead, rest := splitLeadParagraph(body)
		out.WriteString("<li>" + task + r.inline(strings.Join(trimEach(lead), " ")))
		out.WriteString(r.blocks(rest))
		out.WriteString("</li>\n")
		return
	}
	out.WriteString("<li>" + task)
	out.WriteString(r.blocks(body))
	out.WriteString("</li>\n")
}

func containsBlank(lines []string) bool {
	for _, line := range lines {
		if isBlank(line) {
			return true
		}
	}
	return false
}

// splitLeadParagraph separates a list item's opening text from any blocks
// that follow it (a nested list, a fence, and so on).
func splitLeadParagraph(lines []string) ([]string, []string) {
	for index, line := range lines {
		if index > 0 && (fencePattern.MatchString(line) || listPattern.MatchString(line) ||
			headingPattern.MatchString(line) || isBlank(line)) {
			return lines[:index], lines[index:]
		}
	}
	return lines, nil
}

func trimEach(lines []string) []string {
	trimmed := make([]string, len(lines))
	for index, line := range lines {
		trimmed[index] = strings.TrimSpace(line)
	}
	return trimmed
}

func isOrdered(marker string) bool { return marker[0] >= '0' && marker[0] <= '9' }

func indentOf(line string) int {
	return len(line) - len(strings.TrimLeft(line, " \t"))
}

func dedent(line string, width int) string {
	spaces := 0
	for spaces < len(line) && spaces < width && line[spaces] == ' ' {
		spaces++
	}
	return line[spaces:]
}

func (r *renderer) paragraph(out *strings.Builder, lines []string, start int) int {
	var collected []string
	for start < len(lines) {
		line := lines[start]
		if isBlank(line) || fencePattern.MatchString(line) || headingPattern.MatchString(line) ||
			rulePattern.MatchString(line) || listPattern.MatchString(line) || quotePattern.MatchString(line) ||
			startsTable(lines, start) {
			break
		}
		collected = append(collected, strings.TrimLeft(line, " \t"))
		start++
	}
	if len(collected) == 0 {
		return start + 1
	}
	out.WriteString("<p>" + r.paragraphText(collected) + "</p>\n")
	return start
}

// paragraphText renders one paragraph, keeping a hard line break (two trailing
// spaces or a trailing backslash) and turning every other break into a space.
func (r *renderer) paragraphText(lines []string) string {
	// Join first, then render: emphasis and links may span a soft line break,
	// and a hard break must survive as a marker that inline escaping ignores.
	var joined strings.Builder
	for index, line := range lines {
		if index > 0 {
			if lastHard(lines[index-1]) {
				joined.WriteString(hardBreakToken)
			} else {
				joined.WriteString(" ")
			}
		}
		joined.WriteString(strings.TrimSpace(line))
	}
	return strings.ReplaceAll(r.inline(joined.String()), hardBreakToken, "<br>")
}

func lastHard(line string) bool {
	return strings.HasSuffix(line, "  ") || strings.HasSuffix(line, "\\")
}

func slug(text string) string {
	var out strings.Builder
	lastDash := false
	for _, char := range strings.ToLower(text) {
		switch {
		case unicode.IsLetter(char) || unicode.IsDigit(char):
			out.WriteRune(char)
			lastDash = false
		case char == ' ' || char == '-' || char == '_' || char == '.':
			if !lastDash && out.Len() > 0 {
				out.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(out.String(), "-")
}

// plainText strips inline markers so a heading can be indexed and slugged.
func plainText(text string) string {
	replaced := text
	for _, pattern := range []*regexp.Regexp{
		regexp.MustCompile(`!\[([^\]]*)\]\([^)]*\)`),
		regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`),
		regexp.MustCompile("`([^`]*)`"),
		regexp.MustCompile(`\*\*([^*]+)\*\*`),
		regexp.MustCompile(`__([^_]+)__`),
		regexp.MustCompile(`~~([^~]+)~~`),
	} {
		replaced = pattern.ReplaceAllString(replaced, "$1")
	}
	return strings.TrimSpace(replaced)
}
