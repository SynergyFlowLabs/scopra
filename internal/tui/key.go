// Package tui provides the small terminal toolkit that Scopra's installer
// uses: raw-mode input on Linux, key decoding for ANSI terminals, and a
// cell-grid frame renderer. It has no dependencies outside the standard
// library, in keeping with the rest of the project.
package tui

import (
	"strings"
	"unicode/utf8"
)

// KeyType identifies a decoded key.
type KeyType int

const (
	// KeyRune is a printable character, including space.
	KeyRune KeyType = iota
	// KeyEnter is carriage return or line feed.
	KeyEnter
	// KeyBackspace is DEL or BS.
	KeyBackspace
	// KeyDelete is the Delete key.
	KeyDelete
	// KeyTab is the Tab key.
	KeyTab
	// KeyShiftTab is Shift+Tab.
	KeyShiftTab
	// KeyEscape is a standalone Escape.
	KeyEscape
	// KeyUp, KeyDown, KeyLeft, and KeyRight are the arrow keys.
	KeyUp
	KeyDown
	KeyLeft
	KeyRight
	// KeyHome and KeyEnd are the Home and End keys.
	KeyHome
	KeyEnd
	// KeyPageUp and KeyPageDown are the page keys.
	KeyPageUp
	KeyPageDown
	// KeyInterrupt is Ctrl-C.
	KeyInterrupt
)

// Key is one decoded keyboard event.
type Key struct {
	Type KeyType
	Rune rune
	// Alt reports that Escape preceded the key. The installer does not use
	// it, but decoding it keeps the parser honest about its input.
	Alt bool
}

// Is reports whether the key is a plain rune matching r.
func (k Key) Is(r rune) bool {
	return k.Type == KeyRune && k.Rune == r && !k.Alt
}

// Decoder turns terminal bytes into key events. The caller feeds bytes as they
// arrive and flushes pending input after a short timeout, which is how a
// standalone Escape is told apart from the start of a sequence.
type Decoder struct {
	pending []byte
}

// Feed consumes bytes and returns every complete key. An incomplete sequence
// stays pending until more bytes arrive or FlushPending is called.
func (d *Decoder) Feed(data []byte) []Key {
	d.pending = append(d.pending, data...)
	var keys []Key
	for len(d.pending) > 0 {
		key, consumed, complete := decodeOne(d.pending)
		if !complete {
			break
		}
		d.pending = d.pending[consumed:]
		if consumed > 0 && key != nil {
			keys = append(keys, *key)
		}
	}
	if len(d.pending) > 64 {
		// A bounded parser never grows without limit on garbage input.
		d.pending = d.pending[len(d.pending)-64:]
	}
	return keys
}

// FlushPending resolves input that has been idle. A lone Escape becomes
// KeyEscape; a truncated escape sequence is discarded.
func (d *Decoder) FlushPending() []Key {
	if len(d.pending) == 0 {
		return nil
	}
	pending := d.pending
	d.pending = nil
	// Only a lone Escape is a key. A truncated sequence such as ESC [ 9 is
	// ambiguous, and guessing would let a stray bracket drive the interface,
	// so it is dropped instead.
	if len(pending) == 1 && pending[0] == 0x1b {
		return []Key{{Type: KeyEscape}}
	}
	return nil
}

// Pending reports whether the decoder holds bytes that may still complete.
func (d *Decoder) Pending() bool { return len(d.pending) > 0 }

// HasOnlyEscape reports whether the pending bytes are exactly one Escape.
func (d *Decoder) HasOnlyEscape() bool {
	return len(d.pending) == 1 && d.pending[0] == 0x1b
}

func decodeOne(data []byte) (*Key, int, bool) {
	switch data[0] {
	case 0x03:
		return &Key{Type: KeyInterrupt}, 1, true
	case '\r', '\n':
		return &Key{Type: KeyEnter}, 1, true
	case 0x7f, 0x08:
		return &Key{Type: KeyBackspace}, 1, true
	case '\t':
		return &Key{Type: KeyTab}, 1, true
	case 0x1b:
		return decodeEscape(data)
	}
	r, size := utf8.DecodeRune(data)
	if r == utf8.RuneError && size <= 1 {
		if len(data) < utf8.UTFMax && !utf8.FullRune(data) {
			return nil, 0, false
		}
		return nil, 1, true
	}
	if r < 0x20 {
		return nil, size, true
	}
	return &Key{Type: KeyRune, Rune: r}, size, true
}

func decodeEscape(data []byte) (*Key, int, bool) {
	if len(data) == 1 {
		return nil, 0, false
	}
	switch data[1] {
	case '[':
		return decodeCSI(data)
	case 'O':
		if len(data) < 3 {
			return nil, 0, false
		}
		if key := finalEscapeKey(data[2]); key != nil {
			return key, 3, true
		}
		return nil, 3, true
	}
	// Alt+key: report the key with Alt set, and Escape on its own before it.
	inner, consumed, complete := decodeOne(data[1:])
	if !complete || inner == nil {
		return &Key{Type: KeyEscape}, 1, true
	}
	inner.Alt = true
	return inner, 1 + consumed, true
}

func decodeCSI(data []byte) (*Key, int, bool) {
	// CSI sequences end with a byte in 0x40..0x7e. Parameters are ignored
	// beyond the handful the installer actually understands.
	for index := 2; index < len(data); index++ {
		b := data[index]
		if b >= 0x40 && b <= 0x7e {
			params := string(data[2:index])
			if key := csiKey(b, params); key != nil {
				return key, index + 1, true
			}
			return nil, index + 1, true
		}
		if index > 32 {
			return nil, index, true
		}
	}
	return nil, 0, false
}

func csiKey(final byte, params string) *Key {
	switch final {
	case 'A':
		return &Key{Type: KeyUp}
	case 'B':
		return &Key{Type: KeyDown}
	case 'C':
		return &Key{Type: KeyRight}
	case 'D':
		return &Key{Type: KeyLeft}
	case 'H':
		return &Key{Type: KeyHome}
	case 'F':
		return &Key{Type: KeyEnd}
	case 'Z':
		return &Key{Type: KeyShiftTab}
	case '~':
		switch params {
		case "1", "7":
			return &Key{Type: KeyHome}
		case "3":
			return &Key{Type: KeyDelete}
		case "4", "8":
			return &Key{Type: KeyEnd}
		case "5":
			return &Key{Type: KeyPageUp}
		case "6":
			return &Key{Type: KeyPageDown}
		}
	}
	return nil
}

func finalEscapeKey(final byte) *Key {
	switch final {
	case 'A':
		return &Key{Type: KeyUp}
	case 'B':
		return &Key{Type: KeyDown}
	case 'C':
		return &Key{Type: KeyRight}
	case 'D':
		return &Key{Type: KeyLeft}
	case 'H':
		return &Key{Type: KeyHome}
	case 'F':
		return &Key{Type: KeyEnd}
	}
	return nil
}

// KeyLabel renders a key for the hint line.
func KeyLabel(key Key) string {
	switch key.Type {
	case KeyEnter:
		return "enter"
	case KeyUp:
		return "↑"
	case KeyDown:
		return "↓"
	case KeyLeft:
		return "←"
	case KeyRight:
		return "→"
	case KeyEscape:
		return "esc"
	case KeyBackspace:
		return "backspace"
	case KeyTab:
		return "tab"
	case KeyInterrupt:
		return "ctrl-c"
	case KeyDelete:
		return "delete"
	case KeyPageUp:
		return "page-up"
	case KeyPageDown:
		return "page-down"
	case KeyHome:
		return "home"
	case KeyEnd:
		return "end"
	case KeyShiftTab:
		return "shift-tab"
	}
	if key.Rune == ' ' {
		return "space"
	}
	return strings.ToLower(string(key.Rune))
}
