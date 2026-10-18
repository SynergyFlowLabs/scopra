package journal

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/SynergyFlowLabs/scopra/packages/protocol"
)

const testWorkspaceID = "0123456789abcdef0123456789abcdef"

var (
	testStart    = time.Date(2026, 10, 17, 5, 0, 0, 0, time.UTC)
	testRecorded = testStart.Add(2 * time.Second)
)

func testResult(state protocol.ExecutionState) protocol.ExecutionResult {
	result := protocol.ExecutionResult{
		ExecutionID: "local-0123456789abcdef0123456789abcdef",
		ActionHash:  "sha256:" + strings.Repeat("a", 64),
		State:       state,
		StartedAt:   testStart,
		EndedAt:     testStart.Add(1500 * time.Millisecond),
		Output:      protocol.OutputSummary{StdoutBytes: 12, StderrBytes: 3, Chunks: 2},
	}
	switch state {
	case protocol.StateSucceeded:
		result.ExitCode = 0
	case protocol.StateFailed:
		result.ExitCode = 7
	case protocol.StateTimedOut, protocol.StateCancelled, protocol.StateOutOfMemory:
		result.ExitCode = 0
	case protocol.StateErrored, protocol.StateRejected:
		result.StartedAt = time.Time{}
		result.EndedAt = time.Time{}
		result.Error = protocol.ResultError{Code: protocol.ErrorCodeInternal, Message: "fixed text"}
		if state == protocol.StateRejected {
			result.Output = protocol.OutputSummary{}
			result.Error.Code = protocol.ErrorCodeBusy
		}
	}
	return result
}

func mustEntry(t *testing.T, state protocol.ExecutionState) Entry {
	t.Helper()
	entry, err := NewEntry(testWorkspaceID, testResult(state), testRecorded)
	if err != nil {
		t.Fatalf("NewEntry(%s) error = %v", state, err)
	}
	return entry
}

func TestNewEntryCopiesOnlyMetadata(t *testing.T) {
	for _, state := range []protocol.ExecutionState{
		protocol.StateSucceeded, protocol.StateFailed, protocol.StateTimedOut,
		protocol.StateCancelled, protocol.StateOutOfMemory, protocol.StateErrored, protocol.StateRejected,
	} {
		t.Run(string(state), func(t *testing.T) {
			result := testResult(state)
			entry, err := NewEntry(testWorkspaceID, result, testRecorded)
			if err != nil {
				t.Fatalf("NewEntry() error = %v", err)
			}
			if entry.SchemaVersion != SchemaVersion || entry.ExecutionID != result.ExecutionID ||
				entry.WorkspaceID != testWorkspaceID || entry.State != result.State {
				t.Fatalf("entry identity = %+v, want the result's metadata", entry)
			}
			if entry.RecordedAt != testRecorded {
				t.Fatalf("recordedAt = %v, want %v", entry.RecordedAt, testRecorded)
			}
			if entry.StartedAt != result.StartedAt || entry.EndedAt != result.EndedAt {
				t.Fatalf("times = %v..%v, want %v..%v", entry.StartedAt, entry.EndedAt, result.StartedAt, result.EndedAt)
			}
			if result.State == protocol.StateSucceeded || result.State == protocol.StateFailed ||
				result.State == protocol.StateTimedOut || result.State == protocol.StateCancelled ||
				result.State == protocol.StateOutOfMemory {
				if want := result.EndedAt.Sub(result.StartedAt); entry.Duration != want {
					t.Fatalf("duration = %v, want %v", entry.Duration, want)
				}
			} else if entry.Duration != 0 {
				t.Fatalf("duration = %v, want zero for %s", entry.Duration, result.State)
			}
			if entry.ExitCode != result.ExitCode {
				t.Fatalf("exit code = %d, want %d", entry.ExitCode, result.ExitCode)
			}
			if entry.StdoutBytes != result.Output.StdoutBytes || entry.StderrBytes != result.Output.StderrBytes ||
				entry.Truncated != result.Output.Truncated || entry.Chunks != result.Output.Chunks {
				t.Fatalf("output summary = %+v, want %+v", entry, result.Output)
			}
			if entry.ErrorCode != result.Error.Code {
				t.Fatalf("error code = %q, want %q", entry.ErrorCode, result.Error.Code)
			}
			if err := entry.Validate(); err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}

func TestNewEntryRejectsInconsistentResults(t *testing.T) {
	bad := map[string]protocol.ExecutionResult{
		"unknown state": {
			ExecutionID: "local-0123456789abcdef0123456789abcdef",
			State:       protocol.ExecutionState("exploded"),
		},
		"rejected with output": func() protocol.ExecutionResult {
			result := testResult(protocol.StateRejected)
			result.Output = protocol.OutputSummary{StdoutBytes: 1, Chunks: 1}
			return result
		}(),
		"errored without a code": func() protocol.ExecutionResult {
			result := testResult(protocol.StateErrored)
			result.Error.Code = ""
			return result
		}(),
		"failed without a failing code": func() protocol.ExecutionResult {
			result := testResult(protocol.StateFailed)
			result.ExitCode = 0
			return result
		}(),
		"succeeded with a nonzero code": func() protocol.ExecutionResult {
			result := testResult(protocol.StateSucceeded)
			result.ExitCode = 3
			return result
		}(),
		"timed out with a nonzero code": func() protocol.ExecutionResult {
			result := testResult(protocol.StateTimedOut)
			result.ExitCode = 3
			return result
		}(),
		"rejected with times": func() protocol.ExecutionResult {
			result := testResult(protocol.StateRejected)
			result.StartedAt, result.EndedAt = testStart, testStart.Add(time.Second)
			return result
		}(),
		"ended before start": func() protocol.ExecutionResult {
			result := testResult(protocol.StateSucceeded)
			result.StartedAt, result.EndedAt = testStart.Add(time.Second), testStart
			return result
		}(),
		"failed above 255": func() protocol.ExecutionResult {
			result := testResult(protocol.StateFailed)
			result.ExitCode = 256
			return result
		}(),
		"zero execution ID": {State: protocol.StateSucceeded},
	}
	for name, result := range bad {
		t.Run(name, func(t *testing.T) {
			if _, err := NewEntry(testWorkspaceID, result, testRecorded); err == nil {
				t.Fatalf("NewEntry(%+v) error = nil, want a refusal", result)
			}
		})
	}
	if _, err := NewEntry("not-a-workspace-id", testResult(protocol.StateSucceeded), testRecorded); err == nil {
		t.Fatal("NewEntry() accepted an invalid workspace ID")
	}
	if _, err := NewEntry(testWorkspaceID, testResult(protocol.StateSucceeded), time.Time{}); err == nil {
		t.Fatal("NewEntry() accepted a zero recorded time")
	}
}

func TestEntryJSONRoundTrip(t *testing.T) {
	for _, state := range []protocol.ExecutionState{
		protocol.StateSucceeded, protocol.StateFailed, protocol.StateTimedOut,
		protocol.StateCancelled, protocol.StateOutOfMemory, protocol.StateErrored, protocol.StateRejected,
	} {
		t.Run(string(state), func(t *testing.T) {
			entry := mustEntry(t, state)
			encoded, err := entry.MarshalJSON()
			if err != nil {
				t.Fatalf("MarshalJSON() error = %v", err)
			}
			if strings.ContainsAny(string(encoded), "\n\r") {
				t.Fatalf("entry JSON contains a newline: %q", encoded)
			}
			if !strings.Contains(string(encoded), `"schemaVersion":1`) {
				t.Fatalf("entry JSON lacks the schema version: %s", encoded)
			}
			decoded, err := DecodeEntry(encoded)
			if err != nil {
				t.Fatalf("DecodeEntry() error = %v; JSON = %s", err, encoded)
			}
			if !decoded.RecordedAt.Equal(entry.RecordedAt) || !decoded.StartedAt.Equal(entry.StartedAt) ||
				!decoded.EndedAt.Equal(entry.EndedAt) || decoded.Duration != entry.Duration {
				t.Fatalf("decoded times = %+v, want %+v", decoded, entry)
			}
			decoded.RecordedAt, entry.RecordedAt = testRecorded, testRecorded
			if !reflect.DeepEqual(decoded, entry) {
				t.Fatalf("decoded entry = %+v, want %+v", decoded, entry)
			}
		})
	}
}

func TestDecodeEntryRejectsInvalidJSON(t *testing.T) {
	valid, err := mustEntry(t, protocol.StateSucceeded).MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON() error = %v", err)
	}
	replace := func(old, new string) []byte {
		return []byte(strings.Replace(string(valid), old, new, 1))
	}
	tests := map[string][]byte{
		"empty":                nil,
		"not an object":        []byte(`[]`),
		"unknown key":          replace(`"schemaVersion":1`, `"extra":true,"schemaVersion":1`),
		"mis-cased key":        replace(`"truncated":false`, `"Truncated":false`),
		"missing key":          replace(`,"errorCode":""`, ``),
		"duplicate key":        replace(`"schemaVersion":1`, `"schemaVersion":1,"schemaVersion":1`),
		"trailing data":        append(append([]byte{}, valid...), []byte(`{}`)...),
		"bad schema":           replace(`"schemaVersion":1`, `"schemaVersion":2`),
		"bad execution ID":     replace(`"local-0123456789abcdef0123456789abcdef"`, `"has space"`),
		"bad workspace ID":     replace(`"workspaceId":"`+testWorkspaceID+`"`, `"workspaceId":"`+strings.Repeat("A", 32)+`"`),
		"bad state":            replace(`"succeeded"`, `"exploded"`),
		"bare timestamp":       replace(`2026-10-17T05:00:02.000Z`, `2026-10-17T05:00:02Z`),
		"offset timestamp":     replace(`2026-10-17T05:00:02.000Z`, `2026-10-17T05:00:02.000+01:00`),
		"negative exit":        replace(`"exitCode":0`, `"exitCode":-1`),
		"exit too high":        replace(`"exitCode":0`, `"exitCode":256`),
		"duration mismatch":    replace(`"durationMs":1500`, `"durationMs":1`),
		"uppercase code":       replace(`"errorCode":""`, `"errorCode":"INTERNAL"`),
		"code with digit":      replace(`"errorCode":""`, `"errorCode":"internal1"`),
		"bytes without chunks": replace(`"chunks":2`, `"chunks":0`),
		"limit as string":      replace(`"schemaVersion":1`, `"schemaVersion":"1"`),
		"invalid UTF-8":        []byte{'{', '"', 0xff, '"', ':', '1', '}'},
	}
	for name, encoded := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeEntry(encoded); err == nil {
				t.Fatalf("DecodeEntry(%q) error = nil, want a refusal", encoded)
			}
		})
	}
}

func TestDecodeEntryRequiresTimesForTimedStates(t *testing.T) {
	encoded := mustValidLine(t)
	encoded = strings.Replace(encoded,
		`"startedAt":"2026-10-17T05:00:00.000Z","endedAt":"2026-10-17T05:00:01.500Z","durationMs":1500`,
		`"startedAt":"","endedAt":"","durationMs":0`, 1)
	if _, err := DecodeEntry([]byte(encoded)); err == nil {
		t.Fatal("DecodeEntry() accepted a timed state without times")
	}
}

func TestDecodeEntryEnforcesTheOutputCountBound(t *testing.T) {
	encoded := mustValidLine(t)
	encoded = strings.Replace(encoded, `"stdoutBytes":12,"stderrBytes":3`,
		`"stdoutBytes":1073741825,"stderrBytes":0`, 1)
	encoded = strings.Replace(encoded, `"chunks":2`, `"chunks":1073741824`, 1)
	if _, err := DecodeEntry([]byte(encoded)); err == nil {
		t.Fatal("DecodeEntry() accepted output counts above the bound")
	}
}

// mustValidLine returns the canonical JSON line of a succeeded entry.
func mustValidLine(t *testing.T) string {
	t.Helper()
	encoded, err := mustEntry(t, protocol.StateSucceeded).MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON() error = %v", err)
	}
	return string(encoded)
}

func TestJournalAppendsAndReadsNewestFirst(t *testing.T) {
	dir := t.TempDir()
	j, err := Open(dir)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer j.Close()

	states := []protocol.ExecutionState{
		protocol.StateSucceeded, protocol.StateFailed, protocol.StateRejected,
	}
	otherWorkspace := "fedcba9876543210fedcba9876543210"
	for index, state := range states {
		workspace := testWorkspaceID
		if index == 1 {
			workspace = otherWorkspace
		}
		if err := j.Append(workspace, testResult(state)); err != nil {
			t.Fatalf("Append(%s) error = %v", state, err)
		}
	}

	if err := j.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	reopened, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen error = %v", err)
	}
	defer reopened.Close()

	all, err := reopened.Entries("", 10)
	if err != nil {
		t.Fatalf("Entries() error = %v", err)
	}
	if len(all) != len(states) {
		t.Fatalf("Entries() returned %d entries, want %d", len(all), len(states))
	}
	for index, entry := range all {
		want := states[len(states)-1-index]
		if entry.State != want {
			t.Fatalf("entry %d state = %s, want %s (newest first)", index, entry.State, want)
		}
	}
	filtered, err := reopened.Entries(otherWorkspace, 10)
	if err != nil || len(filtered) != 1 || filtered[0].WorkspaceID != otherWorkspace {
		t.Fatalf("Entries(%q) = %+v, %v; want that workspace's one entry", otherWorkspace, filtered, err)
	}
	limited, err := reopened.Entries("", 1)
	if err != nil || len(limited) != 1 || limited[0].State != protocol.StateRejected {
		t.Fatalf("Entries(limit 1) = %+v, %v; want the newest entry", limited, err)
	}
	for _, test := range []struct {
		workspace string
		limit     int
	}{
		{"", 0}, {"", MaxLimit + 1}, {"not-hex", 1},
	} {
		if _, err := reopened.Entries(test.workspace, test.limit); err == nil {
			t.Fatalf("Entries(%q, %d) error = nil, want a refusal", test.workspace, test.limit)
		}
	}
}

func TestJournalFileIsPrivateAndRegular(t *testing.T) {
	dir := t.TempDir()
	j, err := Open(dir)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer j.Close()
	info, err := os.Lstat(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatalf("Lstat() error = %v", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("journal mode = %v, want a regular file with 0600", info.Mode())
	}
}

func TestSecurity_JournalRefusesASymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, nil, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.Symlink(target, filepath.Join(dir, FileName)); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}
	if _, err := Open(dir); err == nil {
		t.Fatal("Open() accepted a symlinked journal")
	}
}

func TestSecurity_JournalRefusesUnsafeMode(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FileName), nil, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.Chmod(filepath.Join(dir, FileName), 0o644); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}
	if _, err := Open(dir); err == nil {
		t.Fatal("Open() accepted a journal with mode 0644")
	}
}

func TestJournalDiscardsATornTail(t *testing.T) {
	dir := t.TempDir()
	entry := mustEntry(t, protocol.StateSucceeded)
	line, err := entry.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON() error = %v", err)
	}
	path := filepath.Join(dir, FileName)
	content := append(append(append([]byte{}, line...), '\n'), []byte(`{"schemaVersion":1,"executionId":`)...)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	j, err := Open(dir)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer j.Close()
	entries, err := j.Entries("", 10)
	if err != nil || len(entries) != 1 {
		t.Fatalf("Entries() = %d entries, %v; want the committed line only", len(entries), err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(after) != string(line)+"\n" {
		t.Fatalf("Open() left a torn tail: %q", after)
	}
}

func TestJournalRefusesCorruption(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	if err := os.WriteFile(path, []byte("not json\n"+`{"schemaVersion":1}`+"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if _, err := Open(dir); err == nil {
		t.Fatal("Open() accepted a corrupted committed line")
	}
}

func TestJournalRefusesAnOversizedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	if err := os.WriteFile(path, []byte(strings.Repeat("a", MaxBytes)+"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if _, err := Open(dir); err == nil {
		t.Fatal("Open() accepted a journal larger than its bound")
	}
}

func TestJournalRefusesAnOversizedFileOfValidEntries(t *testing.T) {
	line, err := mustEntry(t, protocol.StateSucceeded).MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON() error = %v", err)
	}
	repeats := MaxBytes/(len(line)+1) + 1
	content := bytes.Repeat(append(append([]byte{}, line...), '\n'), repeats)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FileName), content, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if _, err := Open(dir); err == nil {
		t.Fatal("Open() accepted a journal larger than its byte bound")
	}
}

func TestJournalRotationByEntryCount(t *testing.T) {
	dir := t.TempDir()
	j := openWithLimits(t, dir, 3, MaxBytes)
	defer j.Close()

	for index := 0; index < 5; index++ {
		result := testResult(protocol.StateSucceeded)
		result.ExecutionID = "local-" + strings.Repeat("0", 27) + string(rune('a'+index))
		if err := j.Append(testWorkspaceID, result); err != nil {
			t.Fatalf("Append() error = %v", err)
		}
	}
	entries, err := j.Entries("", MaxLimit)
	if err != nil {
		t.Fatalf("Entries() error = %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("rotated journal holds %d entries, want 3", len(entries))
	}
	for index, entry := range entries {
		want := "local-" + strings.Repeat("0", 27) + string(rune('a'+4-index))
		if entry.ExecutionID != want {
			t.Fatalf("entry %d = %q, want %q", index, entry.ExecutionID, want)
		}
	}
}

func TestJournalRotationBySize(t *testing.T) {
	dir := t.TempDir()
	probe := mustEntry(t, protocol.StateSucceeded)
	line, err := probe.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON() error = %v", err)
	}
	j := openWithLimits(t, dir, MaxEntries, 2*len(line)+1)
	defer j.Close()
	for index := 0; index < 4; index++ {
		result := testResult(protocol.StateSucceeded)
		result.ExecutionID = "local-" + strings.Repeat("0", 27) + string(rune('a'+index))
		if err := j.Append(testWorkspaceID, result); err != nil {
			t.Fatalf("Append() error = %v", err)
		}
	}
	entries, err := j.Entries("", MaxLimit)
	if err != nil || len(entries) < 1 || len(entries) > 2 {
		t.Fatalf("Entries() = %d entries, %v; want the newest one or two", len(entries), err)
	}
	if entries[0].ExecutionID != "local-"+strings.Repeat("0", 27)+"d" {
		t.Fatalf("newest entry = %q, want the last append", entries[0].ExecutionID)
	}
}

func TestJournalBrokenRefusesAppends(t *testing.T) {
	dir := t.TempDir()
	j, err := Open(dir)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer j.Close()
	j.broken = errors.New("disk went away")
	if !j.Broken() {
		t.Fatal("Broken() = false, want true")
	}
	if err := j.Append(testWorkspaceID, testResult(protocol.StateSucceeded)); !errors.Is(err, ErrBroken) {
		t.Fatalf("Append() error = %v, want ErrBroken", err)
	}
}

func TestJournalCloseIsIdempotent(t *testing.T) {
	j, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := j.Close(); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}
	if err := j.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}

// openWithLimits opens dir's journal and shrinks its bounds for the test.
func openWithLimits(t *testing.T, dir string, maxEntries, maxBytes int) *Journal {
	t.Helper()
	j, err := Open(dir)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	j.maxEntries, j.maxBytes = maxEntries, maxBytes
	return j
}

func TestJournalMarksItselfBrokenAfterAWriteFailure(t *testing.T) {
	j, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	// Closing the handle behind the journal's back makes every operation
	// fail exactly as an I/O error would.
	if err := j.file.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := j.Append(testWorkspaceID, testResult(protocol.StateSucceeded)); !errors.Is(err, ErrBroken) {
		t.Fatalf("Append() after an I/O failure error = %v, want ErrBroken", err)
	}
	if !j.Broken() {
		t.Fatal("Broken() = false after a write failure")
	}
	if err := j.Append(testWorkspaceID, testResult(protocol.StateSucceeded)); !errors.Is(err, ErrBroken) {
		t.Fatalf("a second Append() error = %v, want ErrBroken", err)
	}
}

func TestJournalRefusesAnEmptyLine(t *testing.T) {
	dir := t.TempDir()
	entry := mustEntry(t, protocol.StateSucceeded)
	line, err := entry.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, FileName), append(append([]byte{}, line...), []byte("\n\n")...), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if _, err := Open(dir); err == nil {
		t.Fatal("Open() accepted an empty journal line")
	}
}
