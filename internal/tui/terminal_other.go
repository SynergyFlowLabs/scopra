//go:build !linux

package tui

import (
	"context"
	"errors"
	"os"
)

// ErrNotInteractive reports that no terminal is available for an interactive
// session.
var ErrNotInteractive = errors.New("no interactive terminal is available")

// ErrUnsupported reports that this build cannot run the interactive installer.
var ErrUnsupported = errors.New("the interactive installer supports Linux terminals only")

// Event is what the terminal reports to the application.
type Event struct {
	Key    Key
	Resize bool
}

// Terminal is a placeholder on platforms without a raw-mode implementation.
// The installer itself refuses to run there, so it never reaches this code.
type Terminal struct {
	profile Profile
}

// IsTerminal reports false on unsupported platforms.
func IsTerminal(uintptr) bool { return false }

// Open always fails on unsupported platforms.
func Open() (*Terminal, error) { return nil, ErrUnsupported }

// SetProfile records the profile; it has no effect.
func (t *Terminal) SetProfile(profile Profile) { t.profile = profile }

// Profile reports the configured profile.
func (t *Terminal) Profile() Profile { return t.profile }

// Size reports a conventional size.
func (t *Terminal) Size() (int, int) { return 80, 24 }

// Enter fails: there is no raw-mode support.
func (t *Terminal) Enter() error { return ErrUnsupported }

// Exit does nothing.
func (t *Terminal) Exit() {}

// Write sends text to standard output.
func (t *Terminal) Write(text string) { _, _ = os.Stdout.WriteString(text) }

// HideCursor does nothing.
func (t *Terminal) HideCursor() {}

// Events returns a closed channel.
func (t *Terminal) Events(ctx context.Context) <-chan Event {
	events := make(chan Event)
	close(events)
	return events
}
