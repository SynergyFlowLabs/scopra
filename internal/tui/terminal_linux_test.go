//go:build linux

package tui

import (
	"context"
	"os"
	"testing"
	"time"
)

// pipeTerminal builds a Terminal whose input is a pipe, so tests can control
// exactly when bytes arrive, when they stop, and when the input ends. Reading
// a pipe blocks just like reading a quiet terminal does, which is the property
// these tests care about.
func pipeTerminal(t *testing.T) (*Terminal, *os.File) {
	t.Helper()
	in, out, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	t.Cleanup(func() {
		_ = in.Close()
		_ = out.Close()
	})
	return &Terminal{in: in, out: out, profile: ProfileNone}, out
}

func waitForKey(t *testing.T, events <-chan Event) (Key, bool) {
	t.Helper()
	select {
	case event, open := <-events:
		return event.Key, open
	case <-time.After(2 * time.Second):
		t.Fatal("no event within two seconds")
		return Key{}, false
	}
}

// TestTerminalEventsKeepWaitingWhileInputIsQuiet is the regression test for the
// bug that silently swallowed every key press: a quiet terminal must not look
// like the end of input. The old loop abandoned reading on the first
// zero-length read, so no key ever reached the installer.
func TestTerminalEventsKeepWaitingWhileInputIsQuiet(t *testing.T) {
	terminal, input := pipeTerminal(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := terminal.Events(ctx)

	if _, err := input.WriteString("a"); err != nil {
		t.Fatalf("write: %v", err)
	}
	key, open := waitForKey(t, events)
	if !open || !key.Is('a') {
		t.Fatalf("first key = %+v (open %v), want a", key, open)
	}

	// Stay quiet for several flush intervals. Nothing should arrive, and the
	// channel must stay open: silence is not the end of the session.
	select {
	case event, open := <-events:
		t.Fatalf("quiet terminal produced %+v (open %v), want nothing", event, open)
	case <-time.After(4 * escapeTimeout):
	}

	// A key typed after that silence still has to come through.
	if _, err := input.WriteString("b"); err != nil {
		t.Fatalf("write: %v", err)
	}
	key, open = waitForKey(t, events)
	if !open || !key.Is('b') {
		t.Fatalf("key after silence = %+v (open %v), want b", key, open)
	}
}

// TestTerminalEventsFlushALoneEscape covers the other job of the idle timer:
// a bare Escape has to become a key press even though it is also the first
// byte of every arrow-key sequence.
func TestTerminalEventsFlushALoneEscape(t *testing.T) {
	terminal, input := pipeTerminal(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := terminal.Events(ctx)

	if _, err := input.WriteString("\x1b"); err != nil {
		t.Fatalf("write: %v", err)
	}
	key, open := waitForKey(t, events)
	if !open || key.Type != KeyEscape {
		t.Fatalf("key = %+v (open %v), want escape", key, open)
	}

	// The escape key is the whole event. The bytes of the sequence are not
	// replayed as separate presses.
	select {
	case event, open := <-events:
		t.Fatalf("escape produced an extra %+v (open %v)", event, open)
	case <-time.After(4 * escapeTimeout):
	}
}

// TestTerminalEventsJoinSplitSequences checks the other half of the same
// timer: bytes that arrive in separate reads still decode as one key.
func TestTerminalEventsJoinSplitSequences(t *testing.T) {
	terminal, input := pipeTerminal(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := terminal.Events(ctx)

	if _, err := input.WriteString("\x1b"); err != nil {
		t.Fatalf("write: %v", err)
	}
	time.Sleep(escapeTimeout / 6)
	if _, err := input.WriteString("[B"); err != nil {
		t.Fatalf("write: %v", err)
	}

	key, open := waitForKey(t, events)
	if !open || key.Type != KeyDown {
		t.Fatalf("key = %+v (open %v), want down", key, open)
	}
}

// TestTerminalEventsCloseWhenInputEnds checks that a real end of input still
// terminates the stream, so the installer cannot hang after the terminal goes
// away.
func TestTerminalEventsCloseWhenInputEnds(t *testing.T) {
	terminal, input := pipeTerminal(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := terminal.Events(ctx)

	if err := input.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	select {
	case _, open := <-events:
		if open {
			t.Fatal("event after the input ended, want a closed channel")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("events did not close after the input ended")
	}
}

// TestTerminalEventsStopOnCancel checks that cancelling the context retires
// the event stream instead of leaking it for the life of the process.
func TestTerminalEventsStopOnCancel(t *testing.T) {
	terminal, _ := pipeTerminal(t)
	ctx, cancel := context.WithCancel(context.Background())
	events := terminal.Events(ctx)
	cancel()
	select {
	case _, open := <-events:
		if open {
			t.Fatal("event after cancel, want a closed channel")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("events did not close after cancel")
	}
}

// TestTerminalPauseWakesABlockedRead checks the mechanism that lets the
// installer hand the terminal to sudo: a read deadline unblocks the reader,
// and clearing it lets the same terminal stream events again.
func TestTerminalPauseWakesABlockedRead(t *testing.T) {
	terminal, input := pipeTerminal(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := terminal.Events(ctx)

	terminal.Pause()
	time.Sleep(10 * time.Millisecond)
	terminal.Unpause()
	if _, err := input.WriteString("x"); err != nil {
		t.Fatalf("write: %v", err)
	}
	key, open := waitForKey(t, events)
	if !open || !key.Is('x') {
		t.Fatalf("key after unpause = %+v (open %v), want x", key, open)
	}
}

// TestTerminalPauseThenCancelStopsTheStream is the teardown half: a paused
// reader must still notice cancellation instead of blocking the session.
func TestTerminalPauseThenCancelStopsTheStream(t *testing.T) {
	terminal, _ := pipeTerminal(t)
	ctx, cancel := context.WithCancel(context.Background())
	events := terminal.Events(ctx)
	terminal.Pause()
	time.Sleep(10 * time.Millisecond)
	cancel()
	select {
	case _, open := <-events:
		if open {
			t.Fatal("event after cancel, want a closed channel")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a paused stream did not close after cancel")
	}
}
