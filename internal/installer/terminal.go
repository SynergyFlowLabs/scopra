package installer

import "github.com/SynergyFlowLabs/scopra/internal/tui"

// OpenTerminal returns a terminal for the interactive installer. It prefers
// the process's own streams and falls back to /dev/tty, which is what lets
// `curl … | sh` hand the script's standard input to the shell and still give
// the installer a keyboard.
func OpenTerminal() (*tui.Terminal, error) {
	return tui.Open()
}
