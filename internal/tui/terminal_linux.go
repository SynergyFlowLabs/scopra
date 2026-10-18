//go:build linux

package tui

import (
	"context"
	"errors"
	"io"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

// ErrNotInteractive reports that no terminal is available for an interactive
// session.
var ErrNotInteractive = errors.New("no interactive terminal is available")

// escapeTimeout is how long the decoder waits for the rest of a sequence
// before treating a lone Escape as a key press.
const escapeTimeout = 60 * time.Millisecond

// idleFlushInterval bounds how often the decoder checks for a pending
// sequence that has gone quiet.
const idleFlushInterval = 20 * time.Millisecond

// Event is what the terminal reports to the application.
type Event struct {
	Key    Key
	Resize bool
}

// Terminal owns a raw-mode terminal session. Input and output may be the
// process's own streams or a terminal opened directly, which is what makes the
// installer work when its standard input is a pipe (`curl … | sh`).
type Terminal struct {
	in      *os.File
	out     *os.File
	opened  *os.File
	saved   syscall.Termios
	raw     bool
	paused  atomic.Bool
	profile Profile
}

// IsTerminal reports whether fd refers to a terminal.
func IsTerminal(fd uintptr) bool {
	var termios syscall.Termios
	_, _, errno := syscall.Syscall6(syscall.SYS_IOCTL, fd, ioctlReadTermios,
		uintptr(unsafe.Pointer(&termios)), 0, 0, 0)
	return errno == 0
}

// Open attaches to a terminal. It prefers the process's streams and falls back
// to /dev/tty so that a piped bootstrap script still gets an interactive UI.
func Open() (*Terminal, error) {
	if IsTerminal(os.Stdin.Fd()) && IsTerminal(os.Stdout.Fd()) {
		return &Terminal{in: os.Stdin, out: os.Stdout, profile: DetectProfile()}, nil
	}
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, ErrNotInteractive
	}
	if !IsTerminal(tty.Fd()) {
		_ = tty.Close()
		return nil, ErrNotInteractive
	}
	return &Terminal{in: tty, out: tty, opened: tty, profile: DetectProfile()}, nil
}

// SetProfile overrides the detected color profile.
func (t *Terminal) SetProfile(profile Profile) { t.profile = profile }

// Profile reports the color profile in use.
func (t *Terminal) Profile() Profile { return t.profile }

// Size reports the terminal size in cells, falling back to 80x24.
func (t *Terminal) Size() (int, int) {
	var size struct {
		rows uint16
		cols uint16
		x    uint16
		y    uint16
	}
	_, _, errno := syscall.Syscall6(syscall.SYS_IOCTL, t.out.Fd(), syscall.TIOCGWINSZ,
		uintptr(unsafe.Pointer(&size)), 0, 0, 0)
	if errno != 0 || size.cols == 0 || size.rows == 0 {
		return 80, 24
	}
	return int(size.cols), int(size.rows)
}

// Enter switches to the alternate screen and raw input.
func (t *Terminal) Enter() error {
	saved, err := getTermios(t.in.Fd())
	if err != nil {
		return err
	}
	raw := *saved
	raw.Iflag &^= syscall.IGNBRK | syscall.BRKINT | syscall.PARMRK | syscall.ISTRIP |
		syscall.INLCR | syscall.IGNCR | syscall.ICRNL | syscall.IXON
	raw.Oflag &^= syscall.OPOST
	raw.Lflag &^= syscall.ECHO | syscall.ECHONL | syscall.ICANON | syscall.ISIG | syscall.IEXTEN
	raw.Cflag &^= syscall.CSIZE | syscall.PARENB
	raw.Cflag |= syscall.CS8
	// Reads return after at most a tenth of a second, with or without input.
	// The timer is what lets the reader stop for a child process: a terminal
	// cannot be woken from a blocking read, so Pause would otherwise have to
	// close a descriptor the child is still using. A zero-byte read is idle,
	// not end of input (see Events).
	raw.Cc[syscall.VMIN] = 0
	raw.Cc[syscall.VTIME] = 1
	if err := setTermios(t.in.Fd(), &raw); err != nil {
		return err
	}
	t.saved = *saved
	t.raw = true
	// Alternate screen, hidden cursor, no line wrapping.
	t.Write("\x1b[?1049h\x1b[?25l\x1b[?7l\x1b[2J\x1b[H")
	return nil
}

// Exit restores the terminal. It is safe to call twice.
func (t *Terminal) Exit() {
	if t.raw {
		t.Write("\x1b[?7h\x1b[?25h\x1b[?1049l")
		_ = setTermios(t.in.Fd(), &t.saved)
		t.raw = false
	}
	if t.opened != nil {
		_ = t.opened.Close()
		t.opened = nil
	}
}

// Suspend restores the terminal so a child process can use it directly: the
// alternate screen is left, the cursor is shown, and the saved terminal
// settings are back. Call Resume afterwards.
func (t *Terminal) Suspend() {
	if !t.raw {
		return
	}
	t.Write("\x1b[?7h\x1b[?25h\x1b[?1049l")
	_ = setTermios(t.in.Fd(), &t.saved)
	t.raw = false
}

// Resume takes the terminal back after Suspend.
func (t *Terminal) Resume() error {
	if t.raw {
		return nil
	}
	return t.Enter()
}

// Pause stops the reader before the terminal is handed to a child process.
// Without it the reader and the child would compete for the same keystrokes,
// and a password typed at sudo's prompt could be swallowed by the interface.
// A read already in flight returns within the terminal's timer; anything typed
// after that waits in the terminal's queue for Unpause.
func (t *Terminal) Pause() { t.paused.Store(true) }

// Unpause lets the reader consume input again, after a child process has
// finished with the terminal.
func (t *Terminal) Unpause() { t.paused.Store(false) }

// Devices returns the files the terminal reads from and writes to, so a child
// process can inherit them while the interface is suspended.
func (t *Terminal) Devices() (in, out *os.File) { return t.in, t.out }

// Write sends raw bytes to the terminal.
func (t *Terminal) Write(text string) {
	_, _ = t.out.WriteString(text)
}

// HideCursor hides the cursor; Exit restores it.
func (t *Terminal) HideCursor() { t.Write("\x1b[?25l") }

// Events streams key and resize events until ctx is cancelled. The channel is
// closed when the terminal stops.
func (t *Terminal) Events(ctx context.Context) <-chan Event {
	events := make(chan Event, 16)
	resized := make(chan os.Signal, 1)
	signal.Notify(resized, syscall.SIGWINCH)
	go func() {
		defer close(events)
		defer signal.Stop(resized)

		incoming := make(chan []byte, 8)
		readDone := make(chan struct{})
		go func() {
			defer close(readDone)
			buffer := make([]byte, 256)
			for {
				if ctx.Err() != nil {
					return
				}
				if t.paused.Load() {
					// A child owns the terminal. Do not read: the child's
					// prompt has to receive what is typed.
					time.Sleep(5 * time.Millisecond)
					continue
				}
				count, err := t.in.Read(buffer)
				if err != nil {
					if errors.Is(err, syscall.EINTR) || errors.Is(err, syscall.EAGAIN) {
						continue
					}
					// In raw mode the terminal is read with a timer, and a
					// quiet tenth of a second arrives as a zero-byte read
					// that Go reports as io.EOF. That is idle, not the end of
					// the session; treating it as EOF made the interface exit
					// on the first quiet moment.
					if t.raw && errors.Is(err, io.EOF) {
						continue
					}
					// Anything else means the terminal is gone.
					return
				}
				if count == 0 {
					continue
				}
				data := make([]byte, count)
				copy(data, buffer[:count])
				select {
				case incoming <- data:
				case <-ctx.Done():
					return
				}
			}
		}()

		decoder := &Decoder{}
		// idle decides when a pending escape sequence is final. A lone Escape
		// has to become a key, but ESC [ has to wait for its last byte, so the
		// decoder is flushed only after a quiet moment.
		idle := time.NewTicker(idleFlushInterval)
		defer idle.Stop()
		lastInput := time.Now()
		for {
			select {
			case <-ctx.Done():
				return
			case <-idle.C:
				if !decoder.Pending() || time.Since(lastInput) < escapeTimeout {
					continue
				}
				for _, key := range decoder.FlushPending() {
					if !sendEvent(ctx, events, Event{Key: key}) {
						return
					}
				}
			case <-readDone:
				for _, key := range decoder.FlushPending() {
					if !sendEvent(ctx, events, Event{Key: key}) {
						return
					}
				}
				return
			case <-resized:
				if !sendEvent(ctx, events, Event{Resize: true}) {
					return
				}
			case data := <-incoming:
				lastInput = time.Now()
				var keys []Key
				if len(data) == 0 {
					keys = decoder.FlushPending()
				} else {
					keys = decoder.Feed(data)
				}
				for _, key := range keys {
					if !sendEvent(ctx, events, Event{Key: key}) {
						return
					}
				}
			}
		}
	}()
	return events
}

func sendEvent(ctx context.Context, events chan<- Event, event Event) bool {
	select {
	case events <- event:
		return true
	case <-ctx.Done():
		return false
	}
}

const ioctlReadTermios = syscall.TCGETS
const ioctlWriteTermios = syscall.TCSETS

func getTermios(fd uintptr) (*syscall.Termios, error) {
	var termios syscall.Termios
	_, _, errno := syscall.Syscall6(syscall.SYS_IOCTL, fd, ioctlReadTermios,
		uintptr(unsafe.Pointer(&termios)), 0, 0, 0)
	if errno != 0 {
		return nil, errno
	}
	return &termios, nil
}

func setTermios(fd uintptr, termios *syscall.Termios) error {
	_, _, errno := syscall.Syscall6(syscall.SYS_IOCTL, fd, ioctlWriteTermios,
		uintptr(unsafe.Pointer(termios)), 0, 0, 0)
	if errno != 0 {
		return errno
	}
	return nil
}
