package tui

import (
	"strings"
	"unicode"
)

// RuneWidth reports how many terminal cells a rune occupies. It covers the
// wide and combining ranges that matter for paths and prose; unknown runes are
// treated as one cell.
func RuneWidth(r rune) int {
	switch {
	case r == 0:
		return 0
	case r < 0x20 || (r >= 0x7f && r < 0xa0):
		return 0
	case r == 0x200b || r == 0x200c || r == 0x200d || r == 0xfeff:
		return 0
	case unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || unicode.Is(unicode.Cf, r):
		return 0
	}
	for _, table := range wideRanges {
		if r >= table.first && r <= table.last {
			return 2
		}
	}
	return 1
}

type runeRange struct {
	first rune
	last  rune
}

// wideRanges lists the East Asian wide and fullwidth blocks, plus emoji.
var wideRanges = []runeRange{
	{0x1100, 0x115f}, {0x2329, 0x232a}, {0x2e80, 0x303e}, {0x3041, 0x33ff},
	{0x3400, 0x4dbf}, {0x4e00, 0x9fff}, {0xa000, 0xa4cf}, {0xa960, 0xa97f},
	{0xac00, 0xd7a3}, {0xf900, 0xfaff}, {0xfe10, 0xfe19}, {0xfe30, 0xfe6f},
	{0xff00, 0xff60}, {0xffe0, 0xffe6}, {0x1f300, 0x1f64f}, {0x1f900, 0x1f9ff},
	{0x20000, 0x2fffd}, {0x30000, 0x3fffd},
}

// StringWidth reports the number of cells a string occupies.
func StringWidth(s string) int {
	width := 0
	for _, r := range s {
		width += RuneWidth(r)
	}
	return width
}

// Truncate shortens s to at most width cells, appending an ellipsis when
// something was removed.
func Truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if StringWidth(s) <= width {
		return s
	}
	var out strings.Builder
	used := 0
	for _, r := range s {
		runeWidth := RuneWidth(r)
		if used+runeWidth > width-1 {
			break
		}
		out.WriteRune(r)
		used += runeWidth
	}
	return out.String() + "…"
}

// Sanitize replaces control characters with spaces, so a document or path can
// never move the cursor or clear the screen.
func Sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t':
			return ' '
		case r < 0x20 || r == 0x7f:
			return ' '
		}
		return r
	}, s)
}

// PadRight pads s with spaces to width cells.
func PadRight(s string, width int) string {
	used := StringWidth(s)
	if used >= width {
		return Truncate(s, width)
	}
	return s + strings.Repeat(" ", width-used)
}

// Wrap breaks text into lines of at most width cells on word boundaries.
func Wrap(text string, width int) []string {
	if width <= 0 {
		return nil
	}
	var lines []string
	for _, paragraph := range strings.Split(text, "\n") {
		words := strings.Fields(paragraph)
		if len(words) == 0 {
			lines = append(lines, "")
			continue
		}
		current := ""
		for _, word := range words {
			switch {
			case current == "":
				current = word
			case StringWidth(current)+1+StringWidth(word) <= width:
				current += " " + word
			default:
				lines = append(lines, current)
				current = word
			}
		}
		if current != "" {
			lines = append(lines, current)
		}
	}
	return lines
}
