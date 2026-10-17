package installer

import (
	"strings"

	"github.com/SynergyFlowLabs/scopra/internal/tui"
)

// minWidth and minHeight are the smallest terminal the installer will draw
// into. Below them it asks for a bigger window instead of drawing a broken
// one.
const (
	minWidth  = 56
	minHeight = 18
	// maxBoxWidth keeps lines readable on very wide terminals.
	maxBoxWidth = 96
)

var (
	styleTitle   = tui.Style{FG: tui.ColorWhite, Bold: true}
	styleAccent  = tui.Style{FG: tui.ColorAccent, Bold: true}
	styleText    = tui.Style{FG: tui.ColorText}
	styleMuted   = tui.Style{FG: tui.ColorMuted}
	styleFaint   = tui.Style{FG: tui.ColorFaint}
	styleSuccess = tui.Style{FG: tui.ColorSuccess}
	styleDanger  = tui.Style{FG: tui.ColorDanger}
	styleWarn    = tui.Style{FG: tui.ColorWarn}
	styleBorder  = tui.Style{FG: tui.ColorBorder}
	styleCursor  = tui.Style{FG: tui.ColorPanel, BG: tui.ColorAccent2}
	styleBadge   = tui.Style{FG: tui.ColorPanel, BG: tui.ColorAccent, Bold: true}
)

// View renders the whole interface into a string. When colors are disabled the
// output is plain text with the same layout.
func (m *Model) View(width, height int, sheet tui.StyleSheet) string {
	frame := tui.NewFrame(width, height)
	profile := sheet.Profile
	if width < minWidth || height < minHeight {
		frame.Put(1, 0, "The terminal is too small.", styleDanger)
		frame.Put(1, 1, "Resize to at least 56x18.", styleMuted)
		return frame.Render(profile)
	}
	m.width, m.height = width, height
	m.drawHeader(frame)
	m.drawRail(frame)
	m.drawBody(frame)
	m.drawFooter(frame)
	return frame.Render(profile)
}

func (m *Model) drawHeader(frame *tui.Frame) {
	width := frame.Width()
	frame.Put(1, 0, "scopra", styleAccent)
	frame.Put(8, 0, "installer", styleMuted)
	version := "v" + strings.TrimPrefix(m.Version, "v")
	if m.Version == "" || m.Version == "dev" {
		version = "dev build"
	}
	frame.Put(width-tui.StringWidth(version)-1, 0, version, styleFaint)
}

// stepIndex maps the screen to the rail position, 0-3.
func (m *Model) stepIndex() int {
	switch m.Screen {
	case ScreenWelcome, ScreenChecks, ScreenSetup:
		return 0
	case ScreenLocation:
		return 1
	case ScreenImage:
		return 2
	default:
		return 3
	}
}

func (m *Model) drawRail(frame *tui.Frame) {
	labels := []string{"Preflight", "Location", "Sandbox", "Install"}
	x := 1
	current := m.stepIndex()
	for index, label := range labels {
		number := string(rune('1' + index))
		if index < current {
			frame.Put(x, 2, "✔", styleSuccess)
			x = frame.Put(x+2, 2, label, styleFaint)
		} else if index == current {
			x = frame.Put(x, 2, " "+number+" "+label+" ", styleBadge)
		} else {
			frame.Put(x, 2, "·", styleFaint)
			x = frame.Put(x+2, 2, label, styleFaint)
		}
		x += 2
		if x > frame.Width()-8 {
			break
		}
	}
}

func (m *Model) drawBody(frame *tui.Frame) {
	width := frame.Width()
	height := frame.Height()
	boxWidth := width - 2
	if boxWidth > maxBoxWidth {
		boxWidth = maxBoxWidth
	}
	boxX := (width - boxWidth) / 2
	boxY := 3
	boxHeight := height - 6
	if boxHeight < 5 {
		boxHeight = 5
	}
	frame.Box(boxX, boxY, boxWidth, boxHeight, styleBorder, m.boxTitle())
	innerX := boxX + 2
	innerWidth := boxWidth - 4
	switch m.Screen {
	case ScreenWelcome:
		// Center the short introduction in the panel so a tall terminal does
		// not leave the text stranded at the top.
		top := boxY + 2
		if offset := (boxHeight - 12) / 2; offset > 0 {
			top = boxY + offset
		}
		m.drawWelcome(frame, innerX, top, innerWidth)
	case ScreenChecks, ScreenRunning:
		m.drawChecks(frame, innerX, boxY+1, innerWidth, boxHeight-2)
	case ScreenSetup:
		m.drawSetup(frame, innerX, boxY+2, innerWidth)
	case ScreenLocation:
		m.drawLocation(frame, innerX, boxY+2, innerWidth)
	case ScreenImage:
		m.drawImage(frame, innerX, boxY+2, innerWidth)
	case ScreenService:
		m.drawService(frame, innerX, boxY+2, innerWidth)
	case ScreenDone:
		m.drawDone(frame, innerX, boxY+2, innerWidth)
	case ScreenFailed:
		m.drawFailed(frame, innerX, boxY+2, innerWidth)
	case ScreenAborted:
		m.drawAborted(frame, innerX, boxY+2, innerWidth)
	}
}

func (m *Model) boxTitle() string {
	switch m.Screen {
	case ScreenWelcome:
		return "welcome"
	case ScreenChecks:
		return "system check"
	case ScreenSetup:
		return "set up podman"
	case ScreenLocation:
		return "where to install"
	case ScreenImage:
		return "sandbox image"
	case ScreenService:
		return "background service"
	case ScreenRunning:
		return "installing"
	case ScreenDone:
		return "done"
	case ScreenFailed:
		return "stopped"
	}
	return ""
}

// text writes wrapped paragraphs and returns the next free row.
func (m *Model) text(frame *tui.Frame, x, y, width int, style tui.Style, lines ...string) int {
	for _, line := range lines {
		for _, wrapped := range tui.Wrap(line, width) {
			frame.Put(x, y, wrapped, style)
			y++
		}
	}
	return y
}

func (m *Model) drawWelcome(frame *tui.Frame, x, y, width int) {
	y = m.text(frame, x, y, width, styleText,
		"Scopra runs commands inside a rootless Podman container, one workspace at a time, and keeps an audit-friendly record of every run.",
		"",
		"This installer puts scopra and scoprad in place, never edits your shell profile, and can install a systemd user service so the daemon starts with your session. If a prerequisite is missing it offers to install it for you, showing the exact command first.",
	)
	y++
	frame.Put(x, y, "Nothing is changed until you confirm.", styleFaint)
	y++
	frame.Put(x, y, "Press enter to check your system.", styleAccent)
}

func (m *Model) drawChecks(frame *tui.Frame, x, y, width, height int) {
	if m.Screen == ScreenChecks && (m.checksRunning || len(m.checks) == 0) {
		frame.Put(x, y, m.spinner()+" Checking your system…", styleMuted)
		return
	}
	if m.Screen == ScreenRunning {
		m.drawSteps(frame, x, y, width, height)
		return
	}
	row := y
	for _, check := range m.checks {
		style := styleSuccess
		if check.Status == StatusFailed {
			style = styleDanger
		} else if check.Status == StatusWarn {
			style = styleWarn
		}
		frame.Put(x, row, check.Status.Symbol(), style)
		end := frame.Put(x+2, row, check.Title, styleText)
		if check.Detail != "" {
			if available := width - tui.StringWidth(check.Title) - 3; available > 8 {
				frame.Put(end+2, row, tui.Truncate(check.Detail, available), styleFaint)
			}
		}
		row++
		if check.Status == StatusFailed && check.Fix != "" {
			for _, line := range tui.Wrap(check.Fix, width-2) {
				frame.Put(x+2, row, line, styleWarn)
				row++
			}
		}
		row++
		if row > y+height-2 {
			break
		}
	}
	row++
	if Ready(m.checks) {
		frame.Put(x, row, "Everything Scopra needs is here. Press enter to continue.", styleAccent)
		return
	}
	if m.setup != nil {
		frame.Put(x, row, "Press f and the installer can fix this for you.", styleAccent)
		if m.setupErr != nil && m.setup.NeedsRelogin {
			row++
			for _, line := range tui.Wrap("The change takes effect at your next login: log out, log back in, and run the installer again.", width) {
				frame.Put(x, row, line, styleWarn)
				row++
			}
		}
		return
	}
	frame.Put(x, row, "Fix the item above, then run the installer again.", styleWarn)
}

func (m *Model) drawSteps(frame *tui.Frame, x, y, width, height int) {
	row := y
	frame.Put(x, y, "Installing Scopra", styleTitle)
	row += 2
	for _, step := range m.steps {
		style := styleText
		symbol := step.Status.Symbol()
		switch step.Status {
		case StatusOK:
			style = styleSuccess
		case StatusFailed:
			style = styleDanger
		case StatusWarn:
			style = styleWarn
		case StatusRunning, StatusPending:
			symbol = m.spinner()
			style = styleMuted
		}
		frame.Put(x, row, symbol, style)
		frame.Put(x+2, row, step.Title, styleText)
		row++
		if step.Detail != "" {
			for _, line := range tui.Wrap(step.Detail, width-4) {
				frame.Put(x+4, row, line, styleFaint)
				row++
			}
		}
		row++
		if row > y+height-1 {
			return
		}
	}
}

// drawSetup shows exactly what will run, and asks for one deliberate key. It
// is the only screen that can lead to a privileged command.
func (m *Model) drawSetup(frame *tui.Frame, x, y, width int) {
	summary := "Scopra needs rootless Podman 4.0+, and this machine does not have it yet."
	note := "They install from your distribution's repositories. sudo asks for your password; Scopra never stores it, and nothing else on the system changes."
	if m.setup != nil && m.setup.Summary != "" {
		summary = m.setup.Summary
	}
	if m.setup != nil && m.setup.Note != "" {
		note = m.setup.Note
	}
	y = m.text(frame, x, y, width, styleText, summary, "", "The installer can run these commands for you:", "")
	if m.setup != nil {
		for _, line := range m.setup.CommandLines() {
			for _, wrapped := range tui.Wrap(line, width-2) {
				frame.Put(x+2, y, wrapped, styleAccent)
				y++
			}
		}
	}
	y++
	y = m.text(frame, x, y, width, styleMuted, note)
	if m.setupErr != nil {
		y++
		for _, line := range tui.Wrap("The last attempt failed: "+m.setupErr.Error(), width) {
			frame.Put(x, y, line, styleDanger)
			y++
		}
	}
	y++
	frame.Put(x, y, "y install now", styleAccent)
	frame.Put(x+16, y, "enter or n cancel", styleFaint)
}

func (m *Model) drawLocation(frame *tui.Frame, x, y, width int) {
	y = m.text(frame, x, y, width, styleText,
		"The two executables go in one directory. The default is on your PATH for most shells; if it is not, the summary will tell you what to add.",
		"",
	)
	m.drawField(frame, x, y, width, m.Dir, true)
	y += 3
	if m.Dir.Error != "" {
		frame.Put(x, y, tui.Truncate(m.Dir.Error, width), styleDanger)
	}
}

func (m *Model) drawImage(frame *tui.Frame, x, y, width int) {
	y = m.text(frame, x, y, width, styleText,
		"Optional. Give a container image pinned by digest, for example debian@sha256:…, and Mio runs every command inside it.",
		"Leave this empty to install only the binaries: Mio will then refuse to run anything until an image is configured.",
		"",
	)
	m.drawField(frame, x, y, width, m.Image, true)
	y += 3
	if m.Image.Error != "" {
		frame.Put(x, y, tui.Truncate(m.Image.Error, width), styleDanger)
		return
	}
	frame.Put(x, y, tui.Truncate("Mio never pulls images: the digest must already be in local storage.", width), styleFaint)
}

func (m *Model) drawService(frame *tui.Frame, x, y, width int) {
	frame.Put(x, y, "How should scoprad start?", styleText)
	y += 2
	options := []struct {
		title  string
		detail string
	}{
		{"Install a systemd user service (recommended)", "starts with your session and restarts after a crash"},
		{"Only the command line", "you start scoprad yourself when you need it"},
	}
	for index, option := range options {
		selected := index == m.serviceChoice
		marker := "( )"
		style := styleMuted
		titleStyle := styleText
		if selected {
			marker = "(•)"
			style = styleAccent
			titleStyle = styleTitle
		}
		if !m.HasDaemon {
			style = styleFaint
			titleStyle = styleFaint
		}
		frame.Put(x, y, marker, style)
		frame.Put(x+4, y, tui.Truncate(option.title, width-4), titleStyle)
		y++
		frame.Put(x+4, y, tui.Truncate(option.detail, width-4), styleFaint)
		y += 2
	}
	if !m.HasDaemon {
		for _, line := range tui.Wrap("scoprad was not found beside this installer, so the service cannot be installed. Extract the whole release archive and run the installer again, or continue with the command line only.", width) {
			frame.Put(x, y, line, styleWarn)
			y++
		}
	}
	frame.Put(x, y+1, "↑/↓ choose   enter install   esc back", styleFaint)
}

func (m *Model) drawDone(frame *tui.Frame, x, y, width int) {
	frame.Put(x, y, "✔ Scopra is installed.", styleSuccess)
	y += 2
	if m.result != nil {
		frame.Put(x, y, "Directory", styleMuted)
		frame.Put(x+12, y, tui.Truncate(m.result.Dir, width-12), styleText)
		y++
		frame.Put(x, y, "Version", styleMuted)
		frame.Put(x+12, y, tui.Truncate(m.Version, width-12), styleText)
		y++
		service := "not installed"
		if m.result.Service {
			service = "scoprad.service is active"
			if !m.result.ServiceActive {
				service = "installed, but not active"
			}
		}
		frame.Put(x, y, "Service", styleMuted)
		frame.Put(x+12, y, service, styleText)
		y += 2
		for _, warning := range m.result.Warnings {
			for _, line := range tui.Wrap(warning, width) {
				frame.Put(x, y, line, styleWarn)
				y++
			}
			y++
		}
	}
	if m.PathNotice != "" {
		for _, line := range tui.Wrap(m.PathNotice, width) {
			frame.Put(x, y, line, styleWarn)
			y++
		}
		y++
	}
	frame.Put(x, y, "Next:", styleMuted)
	y++
	frame.Put(x+2, y, "scopra workspace add ~/code/my-project", styleText)
	y++
	frame.Put(x+2, y, "scopra run --workspace <id> -- make test", styleText)
}

func (m *Model) drawFailed(frame *tui.Frame, x, y, width int) {
	frame.Put(x, y, "✖ The install stopped.", styleDanger)
	y += 2
	message := "unknown error"
	if m.err != nil {
		message = m.err.Error()
	}
	for _, line := range tui.Wrap(message, width) {
		frame.Put(x, y, line, styleText)
		y++
	}
	y++
	frame.Put(x, y, "Nothing else was written. Fix the cause and run the installer again; it is safe to repeat.", styleFaint)
}

func (m *Model) drawAborted(frame *tui.Frame, x, y, width int) {
	frame.Put(x, y, "Install cancelled.", styleWarn)
	y += 2
	frame.Put(x, y, "Nothing was changed. Press enter to close.", styleMuted)
}

// drawField draws a labelled text input with a visible cursor.
func (m *Model) drawField(frame *tui.Frame, x, y, width int, input *Input, focused bool) {
	frame.Put(x, y, input.Label, styleMuted)
	fieldWidth := width - 2
	text, cursor := input.Visible(fieldWidth, focused)
	row := y + 1
	frame.Put(x, row, "▏", styleBorder)
	start := x + 2
	display := text
	if display == "" {
		display = input.Placeholder
		if display != "" && !focused {
			frame.Put(start, row, tui.Truncate(display, fieldWidth-1), styleFaint)
			frame.Put(x+fieldWidth, row, "▏", styleBorder)
			return
		}
	}
	frame.Put(start, row, tui.Truncate(display, fieldWidth-1), styleText)
	if focused && cursor < fieldWidth-1 {
		runeAt := []rune(display)
		cell := " "
		if cursor < len(runeAt) {
			cell = string(runeAt[cursor])
		}
		frame.Put(start+cursor, row, cell, styleCursor)
	}
	frame.Put(x+fieldWidth, row, "▏", styleBorder)
}

func (m *Model) drawFooter(frame *tui.Frame) {
	y := frame.Height() - 2
	hints := m.hints()
	x := 1
	for index, hint := range hints {
		if index > 0 {
			frame.Put(x, y, "·", styleFaint)
			x += 2
		}
		x = frame.Put(x, y, hint, styleMuted)
		x++
		if x > frame.Width()-4 {
			return
		}
	}
}

func (m *Model) hints() []string {
	switch m.Screen {
	case ScreenWelcome:
		return []string{"enter start", "q quit"}
	case ScreenChecks:
		if m.checksRunning {
			return []string{"checking…", "ctrl-c quit"}
		}
		if Ready(m.checks) {
			return []string{"enter continue", "q quit"}
		}
		if m.setup != nil {
			return []string{"f fix this", "q quit"}
		}
		return []string{"q quit"}
	case ScreenSetup:
		return []string{"y install now", "n cancel", "q quit"}
	case ScreenLocation:
		return []string{"enter continue", "esc back", "q quit"}
	case ScreenImage:
		return []string{"enter continue", "esc back"}
	case ScreenService:
		if !m.HasDaemon {
			return []string{"enter install", "esc back"}
		}
		return []string{"↑/↓ choose", "enter install", "esc back"}
	case ScreenRunning:
		return []string{"installing…"}
	case ScreenDone:
		return []string{"enter exit"}
	case ScreenFailed:
		return []string{"enter exit with status 1"}
	}
	return []string{"enter close"}
}

// spinner cycles through the frames of the progress indicator.
func (m *Model) spinner() string {
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	return frames[m.tick%len(frames)]
}
