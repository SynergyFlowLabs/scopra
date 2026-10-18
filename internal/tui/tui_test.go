package tui

import (
	"strings"
	"testing"
)

func TestDecoderReadsPrintableRunes(t *testing.T) {
	decoder := &Decoder{}
	keys := decoder.Feed([]byte("ab "))
	if len(keys) != 3 {
		t.Fatalf("got %d keys, want 3", len(keys))
	}
	if !keys[0].Is('a') || !keys[1].Is('b') || !keys[2].Is(' ') {
		t.Fatalf("unexpected keys: %#v", keys)
	}
}

func TestDecoderReadsUtf8Runes(t *testing.T) {
	decoder := &Decoder{}
	keys := decoder.Feed([]byte("é"))
	if len(keys) != 1 || keys[0].Rune != 'é' {
		t.Fatalf("unexpected keys: %#v", keys)
	}
}

func TestDecoderKeepsAnIncompleteRunePending(t *testing.T) {
	decoder := &Decoder{}
	if keys := decoder.Feed([]byte{0xc3}); len(keys) != 0 {
		t.Fatalf("got %d keys, want 0", len(keys))
	}
	if !decoder.Pending() {
		t.Fatal("expected the partial rune to stay pending")
	}
	keys := decoder.Feed([]byte{0xa9})
	if len(keys) != 1 || keys[0].Rune != 'é' {
		t.Fatalf("unexpected keys: %#v", keys)
	}
}

func TestDecoderReadsControlKeys(t *testing.T) {
	decoder := &Decoder{}
	keys := decoder.Feed([]byte("\r\x7f\t\x03"))
	want := []KeyType{KeyEnter, KeyBackspace, KeyTab, KeyInterrupt}
	if len(keys) != len(want) {
		t.Fatalf("got %d keys, want %d: %#v", len(keys), len(want), keys)
	}
	for index, wantType := range want {
		if keys[index].Type != wantType {
			t.Fatalf("key %d: got %v, want %v", index, keys[index].Type, wantType)
		}
	}
}

func TestDecoderReadsArrowKeys(t *testing.T) {
	decoder := &Decoder{}
	keys := decoder.Feed([]byte("\x1b[A\x1b[B\x1b[C\x1b[D"))
	want := []KeyType{KeyUp, KeyDown, KeyRight, KeyLeft}
	if len(keys) != len(want) {
		t.Fatalf("got %d keys, want %d: %#v", len(keys), len(want), keys)
	}
	for index, wantType := range want {
		if keys[index].Type != wantType {
			t.Fatalf("key %d: got %v, want %v", index, keys[index].Type, wantType)
		}
	}
}

func TestDecoderReadsSS3Keys(t *testing.T) {
	decoder := &Decoder{}
	keys := decoder.Feed([]byte("\x1bOA\x1bOB"))
	if len(keys) != 2 || keys[0].Type != KeyUp || keys[1].Type != KeyDown {
		t.Fatalf("unexpected keys: %#v", keys)
	}
}

func TestDecoderReadsShiftTab(t *testing.T) {
	decoder := &Decoder{}
	keys := decoder.Feed([]byte("\x1b[Z"))
	if len(keys) != 1 || keys[0].Type != KeyShiftTab {
		t.Fatalf("unexpected keys: %#v", keys)
	}
}

func TestDecoderReadsAltModifiedRune(t *testing.T) {
	decoder := &Decoder{}
	keys := decoder.Feed([]byte("\x1bx"))
	if len(keys) != 1 || keys[0].Rune != 'x' || !keys[0].Alt {
		t.Fatalf("unexpected keys: %#v", keys)
	}
	if keys[0].Is('x') {
		t.Fatal("Alt-modified runes must not match a plain rune")
	}
}

func TestDecoderWaitsForAnIncompleteSequence(t *testing.T) {
	decoder := &Decoder{}
	if keys := decoder.Feed([]byte("\x1b[")); len(keys) != 0 {
		t.Fatalf("got %d keys, want 0", len(keys))
	}
	keys := decoder.Feed([]byte("A"))
	if len(keys) != 1 || keys[0].Type != KeyUp {
		t.Fatalf("unexpected keys: %#v", keys)
	}
}

func TestFlushPendingTurnsALoneEscapeIntoAKey(t *testing.T) {
	decoder := &Decoder{}
	if keys := decoder.Feed([]byte("\x1b")); len(keys) != 0 {
		t.Fatalf("got %d keys, want 0", len(keys))
	}
	if !decoder.HasOnlyEscape() {
		t.Fatal("expected the pending input to be a lone escape")
	}
	keys := decoder.FlushPending()
	if len(keys) != 1 || keys[0].Type != KeyEscape {
		t.Fatalf("unexpected keys: %#v", keys)
	}
	if decoder.Pending() {
		t.Fatal("flush must drain the decoder")
	}
}

func TestFlushPendingDiscardsAnUnparseableSequence(t *testing.T) {
	decoder := &Decoder{}
	decoder.Feed([]byte("\x1b[9"))
	keys := decoder.FlushPending()
	if len(keys) != 0 {
		t.Fatalf("got %d keys, want 0", len(keys))
	}
	if decoder.Pending() {
		t.Fatal("flush must drain the decoder")
	}
}

func TestDecoderDoesNotLoseInputAfterAnUnknownSequence(t *testing.T) {
	decoder := &Decoder{}
	keys := decoder.Feed([]byte("\x1b[99~x"))
	if len(keys) != 1 || keys[0].Rune != 'x' {
		t.Fatalf("unexpected keys: %#v", keys)
	}
}

func TestKeyLabelNamesEveryKey(t *testing.T) {
	cases := map[KeyType]string{
		KeyEnter:     "enter",
		KeyBackspace: "backspace",
		KeyTab:       "tab",
		KeyShiftTab:  "shift-tab",
		KeyEscape:    "esc",
		KeyUp:        "↑",
		KeyDown:      "↓",
		KeyPageUp:    "page-up",
		KeyPageDown:  "page-down",
		KeyHome:      "home",
		KeyEnd:       "end",
	}
	for keyType, want := range cases {
		if got := KeyLabel(Key{Type: keyType}); got != want {
			t.Fatalf("%v: got %q, want %q", keyType, got, want)
		}
	}
	if got := KeyLabel(Key{Type: KeyRune, Rune: 'q'}); got != "q" {
		t.Fatalf("rune label: got %q", got)
	}
}

func TestStringWidthCountsWideRunes(t *testing.T) {
	cases := map[string]int{
		"":        0,
		"abc":     3,
		"café":    4,
		"日本語":     6,
		"a界":      3,
		"e\u0301": 1,
	}
	for text, want := range cases {
		if got := StringWidth(text); got != want {
			t.Fatalf("%q: got %d, want %d", text, got, want)
		}
	}
}

func TestTruncateRespectsWidth(t *testing.T) {
	if got := Truncate("hello", 10); got != "hello" {
		t.Fatalf("got %q", got)
	}
	got := Truncate("hello world", 8)
	if StringWidth(got) > 8 {
		t.Fatalf("%q is %d cells wide", got, StringWidth(got))
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("expected an ellipsis, got %q", got)
	}
	if got := Truncate("hello", 0); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

func TestWrapBreaksOnWords(t *testing.T) {
	lines := Wrap("the quick brown fox jumps", 10)
	for _, line := range lines {
		if StringWidth(line) > 10 {
			t.Fatalf("%q exceeds the width", line)
		}
	}
	if strings.Join(lines, " ") != "the quick brown fox jumps" {
		t.Fatalf("wrap changed the text: %#v", lines)
	}
}

func TestSanitizeRemovesControlCharacters(t *testing.T) {
	got := Sanitize("a\x1b[2Jb\tc\nd")
	if strings.ContainsRune(got, 0x1b) {
		t.Fatalf("escape survived: %q", got)
	}
	if strings.ContainsRune(got, '\n') {
		t.Fatalf("newline survived: %q", got)
	}
}

func TestFrameRendersTextAndClips(t *testing.T) {
	frame := NewFrame(10, 3)
	frame.Put(0, 0, "hello", Style{Bold: true})
	if line := frame.Line(0); line != "hello" {
		t.Fatalf("got %q", line)
	}
	frame.Put(8, 0, "overflow", Style{})
	if line := frame.Line(0); line != "hello   ov" {
		t.Fatalf("clipping failed: %q", line)
	}
	// Out-of-bounds writes are ignored rather than panicking.
	frame.Put(-5, 0, "x", Style{})
	frame.Put(0, 99, "x", Style{})
}

func TestFrameBoxAndRules(t *testing.T) {
	frame := NewFrame(12, 4)
	frame.Box(0, 0, 12, 4, Style{}, "hi")
	text := frame.Text()
	if !strings.HasPrefix(text, "╭─ hi ─") {
		t.Fatalf("unexpected box: %q", text)
	}
	if !strings.HasSuffix(text, "╰──────────╯") {
		t.Fatalf("unexpected box: %q", text)
	}
}

func TestFrameRenderUsesPlainTextWithoutColor(t *testing.T) {
	frame := NewFrame(5, 1)
	frame.Put(0, 0, "ok", Style{FG: ColorAccent, Bold: true})
	rendered := frame.Render(ProfileNone)
	if strings.ContainsRune(rendered, 0x1b) {
		t.Fatalf("plain profile emitted escapes: %q", rendered)
	}
	if !strings.Contains(rendered, "ok") {
		t.Fatalf("text missing: %q", rendered)
	}
}

func TestFrameRenderEmitsColorAndResets(t *testing.T) {
	frame := NewFrame(5, 1)
	frame.Put(0, 0, "ok", Style{FG: ColorAccent})
	rendered := frame.Render(Profile256)
	if !strings.Contains(rendered, "\x1b[38;5;79m") {
		t.Fatalf("accent color missing: %q", rendered)
	}
	if !strings.Contains(rendered, "\x1b[0m") {
		t.Fatalf("reset missing: %q", rendered)
	}
}

func TestStyleRenderWithoutCodesReturnsPlainText(t *testing.T) {
	if got := (Style{}).Render("plain", Profile256); got != "plain" {
		t.Fatalf("got %q", got)
	}
	if got := (Style{FG: ColorAccent}).Render("plain", ProfileNone); got != "plain" {
		t.Fatalf("got %q", got)
	}
}
