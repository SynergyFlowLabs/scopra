package installer

import (
	"strings"

	"github.com/SynergyFlowLabs/scopra/internal/tui"
)

// Input is a single-line text field. It keeps a rune slice and a cursor so
// that editing works the same way in every locale.
type Input struct {
	Label       string
	Placeholder string
	Value       []rune
	Cursor      int
	Hint        string
	Error       string
}

// NewInput returns an input pre-filled with value, with the cursor at the end.
func NewInput(label, value, placeholder string) *Input {
	return &Input{
		Label:       label,
		Placeholder: placeholder,
		Value:       []rune(value),
		Cursor:      len([]rune(value)),
	}
}

// Text returns the current value.
func (i *Input) Text() string { return string(i.Value) }

// SetValue replaces the value and puts the cursor at the end.
func (i *Input) SetValue(value string) {
	i.Value = []rune(value)
	i.Cursor = len(i.Value)
}

// Handle applies an editing key, reporting whether it consumed the key.
func (i *Input) Handle(key tui.Key) bool {
	switch key.Type {
	case tui.KeyRune:
		if key.Alt {
			return false
		}
		i.insert(key.Rune)
		return true
	case tui.KeyBackspace:
		i.backspace()
		return true
	case tui.KeyDelete:
		i.delete()
		return true
	case tui.KeyLeft:
		if i.Cursor > 0 {
			i.Cursor--
		}
		return true
	case tui.KeyRight:
		if i.Cursor < len(i.Value) {
			i.Cursor++
		}
		return true
	case tui.KeyHome:
		i.Cursor = 0
		return true
	case tui.KeyEnd:
		i.Cursor = len(i.Value)
		return true
	}
	return false
}

func (i *Input) insert(r rune) {
	// Printable characters only: a pasted control byte must never be stored.
	if r < 0x20 || r == 0x7f {
		return
	}
	i.Value = append(i.Value, 0)
	copy(i.Value[i.Cursor+1:], i.Value[i.Cursor:])
	i.Value[i.Cursor] = r
	i.Cursor++
}

func (i *Input) backspace() {
	if i.Cursor == 0 {
		return
	}
	i.Value = append(i.Value[:i.Cursor-1], i.Value[i.Cursor:]...)
	i.Cursor--
}

func (i *Input) delete() {
	if i.Cursor >= len(i.Value) {
		return
	}
	i.Value = append(i.Value[:i.Cursor], i.Value[i.Cursor+1:]...)
}

// Visible returns the text to draw, with a cursor marker. When width is too
// small the value scrolls so the cursor stays visible.
func (i *Input) Visible(width int, focused bool) (text string, cursorColumn int) {
	if len(i.Value) == 0 && i.Placeholder != "" && !focused {
		return i.Placeholder, 0
	}
	if width <= 0 {
		return "", 0
	}
	start := 0
	if i.Cursor > width-1 {
		start = i.Cursor - (width - 1)
	}
	end := len(i.Value)
	if end > start+width {
		end = start + width
	}
	text = string(i.Value[start:end])
	cursorColumn = i.Cursor - start
	return text, cursorColumn
}

// Trimmed returns the value without surrounding whitespace.
func (i *Input) Trimmed() string { return strings.TrimSpace(i.Text()) }
