package tui

import (
	"strings"
)

// Cell is one character cell of a frame.
type Cell struct {
	Rune  rune
	Style Style
}

// Frame is an in-memory screen. Panels are drawn into it and the whole frame is
// written in one go, which keeps the terminal from flickering.
type Frame struct {
	width  int
	height int
	cells  []Cell
}

// NewFrame returns a blank frame. Sizes below one cell are clamped.
func NewFrame(width, height int) *Frame {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	frame := &Frame{width: width, height: height, cells: make([]Cell, width*height)}
	for index := range frame.cells {
		frame.cells[index] = Cell{Rune: ' '}
	}
	return frame
}

// Width reports the frame width in cells.
func (f *Frame) Width() int { return f.width }

// Height reports the frame height in rows.
func (f *Frame) Height() int { return f.height }

// Clear blanks the frame.
func (f *Frame) Clear() {
	for index := range f.cells {
		f.cells[index] = Cell{Rune: ' '}
	}
}

// Put writes text at (x, y) with the given style, clipped to the frame.
func (f *Frame) Put(x, y int, text string, style Style) int {
	if y < 0 || y >= f.height {
		return x
	}
	for _, r := range Sanitize(text) {
		width := RuneWidth(r)
		if width == 0 {
			// Combining marks attach to the previous cell.
			if x-1 >= 0 && x-1 < f.width {
				index := y*f.width + (x - 1)
				f.cells[index].Rune = r
			}
			continue
		}
		if x >= 0 && x < f.width {
			f.cells[y*f.width+x] = Cell{Rune: r, Style: style}
			if width == 2 && x+1 < f.width {
				f.cells[y*f.width+x+1] = Cell{Rune: 0, Style: style}
			}
		}
		x += width
		if x >= f.width {
			break
		}
	}
	return x
}

// Cell returns the cell at (x, y), or a blank cell outside the frame.
func (f *Frame) Cell(x, y int) Cell {
	if x < 0 || x >= f.width || y < 0 || y >= f.height {
		return Cell{Rune: ' '}
	}
	return f.cells[y*f.width+x]
}

// Fill paints a rectangle with a style.
func (f *Frame) Fill(x, y, width, height int, style Style) {
	for row := y; row < y+height; row++ {
		for column := x; column < x+width; column++ {
			if column < 0 || column >= f.width || row < 0 || row >= f.height {
				continue
			}
			f.cells[row*f.width+column] = Cell{Rune: ' ', Style: style}
		}
	}
}

// HorizontalRule draws a line of text repeated across width.
func (f *Frame) HorizontalRule(x, y, width int, style Style) {
	for column := 0; column < width; column++ {
		f.Put(x+column, y, "─", style)
	}
}

// Box draws a rounded border with an optional title on the top edge.
func (f *Frame) Box(x, y, width, height int, style Style, title string) {
	if width < 2 || height < 2 {
		return
	}
	f.Put(x, y, "╭"+strings.Repeat("─", width-2)+"╮", style)
	f.Put(x, y+height-1, "╰"+strings.Repeat("─", width-2)+"╯", style)
	for row := 1; row < height-1; row++ {
		f.Put(x, y+row, "│", style)
		f.Put(x+width-1, y+row, "│", style)
	}
	if title != "" && StringWidth(title) <= width-6 {
		f.Put(x+2, y, " "+title+" ", style)
	}
}

// Line returns the visible text of one row, without styling.
func (f *Frame) Line(y int) string {
	if y < 0 || y >= f.height {
		return ""
	}
	var out strings.Builder
	for column := 0; column < f.width; column++ {
		cell := f.cells[y*f.width+column]
		if cell.Rune == 0 {
			continue
		}
		out.WriteRune(cell.Rune)
	}
	return strings.TrimRight(out.String(), " ")
}

// Text returns the visible text of the whole frame.
func (f *Frame) Text() string {
	lines := make([]string, f.height)
	for row := 0; row < f.height; row++ {
		lines[row] = f.Line(row)
	}
	return strings.Join(lines, "\n")
}

// Render serializes the frame for the terminal: the cursor is homed, every row
// is written with its styles, and the rest of the screen is cleared. It uses
// explicit colors rather than a full reset per cell so a frame stays small.
func (f *Frame) Render(profile Profile) string {
	if profile == ProfileNone {
		// No escapes at all: a plain dump stays usable in logs and tests.
		return f.Text()
	}
	var out strings.Builder
	out.Grow(f.width * f.height * 2)
	// Home the cursor; Enter() already hid it and disabled line wrapping.
	out.WriteString("\x1b[H")
	for row := 0; row < f.height; row++ {
		var current Style
		styled := false
		if row > 0 {
			out.WriteString("\r\n")
		}
		for column := 0; column < f.width; column++ {
			cell := f.cells[row*f.width+column]
			if cell.Rune == 0 {
				continue
			}
			if cell.Style != current {
				if styled {
					out.WriteString("\x1b[0m")
				}
				out.WriteString(cell.Style.Sequence(profile))
				current = cell.Style
				styled = cell.Style != (Style{})
			}
			out.WriteRune(cell.Rune)
		}
		if styled {
			out.WriteString("\x1b[0m")
		}
	}
	out.WriteString("\x1b[0J")
	return out.String()
}
