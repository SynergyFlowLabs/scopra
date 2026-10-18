package tui

import (
	"os"
	"strconv"
	"strings"
)

// Color is a 256-color palette index. Its zero value means "terminal
// default", so a Style that was never configured renders no color at all.
type Color int

// The palette the installer uses. Values are picked to match the published
// documentation site: a teal accent on a dark, quiet base.
const (
	ColorDefault Color = 0
	ColorAccent  Color = 79  // teal
	ColorAccent2 Color = 86  // pale teal
	ColorMuted   Color = 245 // grey
	ColorFaint   Color = 240 // dim grey
	ColorText    Color = 252
	ColorWhite   Color = 255
	ColorDanger  Color = 203 // red
	ColorWarn    Color = 221 // amber
	ColorSuccess Color = 79
	ColorBorder  Color = 238
	ColorPanel   Color = 236
)

// Style is a renderable text style.
type Style struct {
	FG      Color
	BG      Color
	Bold    bool
	Dim     bool
	Italic  bool
	Inverse bool
}

// With returns the style with selected attributes replaced.
func (s Style) With(change func(*Style)) Style {
	change(&s)
	return s
}

// Foreground returns the style with a new foreground color.
func (s Style) Foreground(color Color) Style {
	s.FG = color
	return s
}

// Bolded returns the style in bold.
func (s Style) Bolded() Style {
	s.Bold = true
	return s
}

// Faint returns the style dimmed.
func (s Style) Faint() Style {
	s.Dim = true
	return s
}

// Profile decides how much color to emit.
type Profile int

const (
	// ProfileNone emits no escape sequences at all.
	ProfileNone Profile = iota
	// Profile256 emits 256-color sequences.
	Profile256
)

// DetectProfile chooses a color profile from the environment.
func DetectProfile() Profile {
	if os.Getenv("NO_COLOR") != "" {
		return ProfileNone
	}
	term := os.Getenv("TERM")
	if term == "" || term == "dumb" {
		return ProfileNone
	}
	return Profile256
}

// Render applies the style to text. It never adds a reset when the profile is
// none, so plain output stays plain.
func (s Style) Render(text string, profile Profile) string {
	if profile == ProfileNone || text == "" {
		return text
	}
	sequence := s.Sequence(profile)
	if sequence == "" {
		return text
	}
	return sequence + text + "\x1b[0m"
}

// Sequence returns the escape sequence that turns the style on, or an empty
// string when the style is invisible.
func (s Style) Sequence(profile Profile) string {
	if profile == ProfileNone {
		return ""
	}
	var codes []string
	if s.Bold {
		codes = append(codes, "1")
	}
	if s.Dim {
		codes = append(codes, "2")
	}
	if s.Italic {
		codes = append(codes, "3")
	}
	if s.Inverse {
		codes = append(codes, "7")
	}
	if s.FG > 0 {
		codes = append(codes, "38;5;"+strconv.Itoa(int(s.FG)))
	}
	if s.BG > 0 {
		codes = append(codes, "48;5;"+strconv.Itoa(int(s.BG)))
	}
	if len(codes) == 0 {
		return ""
	}
	return "\x1b[" + strings.Join(codes, ";") + "m"
}

// StyleSheet is the installer's shared styling.
type StyleSheet struct {
	Profile Profile
}

// Paint applies a style with the sheet's profile.
func (s StyleSheet) Paint(style Style, text string) string {
	return style.Render(text, s.Profile)
}

// Bold renders bold text.
func (s StyleSheet) Bold(text string) string { return s.Paint(Style{Bold: true}, text) }

// Muted renders dimmed text.
func (s StyleSheet) Muted(text string) string {
	return s.Paint(Style{FG: ColorMuted}, text)
}

// Accent renders accented text.
func (s StyleSheet) Accent(text string) string {
	return s.Paint(Style{FG: ColorAccent}, text)
}
