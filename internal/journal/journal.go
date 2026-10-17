// Package journal records metadata about Mio's local runs (ADR-013). It stores
// one JSON object per line in a bounded, owner-only file inside Mio's state
// directory, and reads it back newest first. It never stores a command line or
// any output.
package journal

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/SynergyFlowLabs/scopra/packages/protocol"
)

const (
	// FileName is the journal's name inside Mio's state directory.
	FileName = "runs.jsonl"
	// SchemaVersion identifies the entry layout. An incompatible change
	// needs a new version.
	SchemaVersion = 1
	// MaxEntries bounds the journal's length by count.
	MaxEntries = 1000
	// MaxBytes bounds the journal file's size.
	MaxBytes = 1 << 20
	// DefaultLimit is how many entries `scopra logs` asks for by default.
	DefaultLimit = 20
	// MaxLimit is the most entries one request may ask for.
	MaxLimit = 100

	// maxEntryBytes bounds one encoded line, far above any valid entry.
	maxEntryBytes = 8 << 10
	// maxOutputCount and maxOutputChunkBytes mirror ADR-009's output summary.
	maxOutputCount      = 1 << 30
	maxOutputChunkBytes = 64 << 10
	maxExecutionIDBytes = 128
	maxErrorCodeBytes   = 32

	// timestampLayout is the one accepted timestamp form: UTC, milliseconds.
	timestampLayout = "2006-01-02T15:04:05.000Z"
)

var (
	// ErrInvalidEntry reports an entry that breaks ADR-013's rules.
	ErrInvalidEntry = errors.New("journal: invalid entry")
	// ErrBroken reports a journal that can no longer record anything.
	ErrBroken = errors.New("journal: the journal is unusable")
	// ErrInvalidLimit reports a limit outside 1 to MaxLimit.
	ErrInvalidLimit = errors.New("journal: invalid limit")
	// ErrUnsupportedPlatform reports that the journal is unavailable here.
	ErrUnsupportedPlatform = errors.New("journal: the run journal is unsupported on this platform")
)

// Entry is one run's metadata as stored. It holds no command, arguments,
// reason, path, output, or identity beyond the execution and workspace IDs.
type Entry struct {
	SchemaVersion int
	ExecutionID   string
	WorkspaceID   string
	State         protocol.ExecutionState
	RecordedAt    time.Time
	// StartedAt and EndedAt are zero for rejected and errored results, which
	// ADR-009 gives no times.
	StartedAt time.Time
	EndedAt   time.Time
	// Duration is endedAt minus startedAt, in whole milliseconds.
	Duration    time.Duration
	ExitCode    int
	StdoutBytes uint64
	StderrBytes uint64
	Truncated   bool
	Chunks      uint64
	// ErrorCode is set for rejected and errored results only.
	ErrorCode string
}

// NewEntry builds the entry for one result. It copies only metadata, so the
// command and its output cannot reach the journal.
func NewEntry(workspaceID string, result protocol.ExecutionResult, recordedAt time.Time) (Entry, error) {
	entry := Entry{
		SchemaVersion: SchemaVersion,
		ExecutionID:   result.ExecutionID,
		WorkspaceID:   workspaceID,
		State:         result.State,
		RecordedAt:    recordedAt.UTC().Truncate(time.Millisecond),
		StartedAt:     result.StartedAt,
		EndedAt:       result.EndedAt,
		ExitCode:      result.ExitCode,
		StdoutBytes:   result.Output.StdoutBytes,
		StderrBytes:   result.Output.StderrBytes,
		Truncated:     result.Output.Truncated,
		Chunks:        result.Output.Chunks,
		ErrorCode:     result.Error.Code,
	}
	if !entry.StartedAt.IsZero() && !entry.EndedAt.IsZero() {
		entry.Duration = entry.EndedAt.Sub(entry.StartedAt).Truncate(time.Millisecond)
	}
	return entry, entry.Validate()
}

// Validate checks every rule that ties an entry's fields together.
func (e Entry) Validate() error {
	switch {
	case e.SchemaVersion != SchemaVersion:
		return fmt.Errorf("%w: unsupported schema version %d", ErrInvalidEntry, e.SchemaVersion)
	case !validExecutionID(e.ExecutionID):
		return fmt.Errorf("%w: invalid execution ID", ErrInvalidEntry)
	case !validWorkspaceID(e.WorkspaceID):
		return fmt.Errorf("%w: invalid workspace ID", ErrInvalidEntry)
	case e.RecordedAt.IsZero():
		return fmt.Errorf("%w: a recorded time is required", ErrInvalidEntry)
	}
	keys, ok := stateRules[e.State]
	if !ok {
		return fmt.Errorf("%w: unknown state %q", ErrInvalidEntry, e.State)
	}
	if keys.times {
		if e.StartedAt.IsZero() || e.EndedAt.IsZero() {
			return fmt.Errorf("%w: %s needs both times", ErrInvalidEntry, e.State)
		}
		if e.EndedAt.Before(e.StartedAt) {
			return fmt.Errorf("%w: endedAt precedes startedAt", ErrInvalidEntry)
		}
		if e.Duration != e.EndedAt.Sub(e.StartedAt).Truncate(time.Millisecond) {
			return fmt.Errorf("%w: duration does not match the times", ErrInvalidEntry)
		}
	} else if !e.StartedAt.IsZero() || !e.EndedAt.IsZero() || e.Duration != 0 {
		return fmt.Errorf("%w: %s has no times or duration", ErrInvalidEntry, e.State)
	}
	if keys.exitCode {
		if e.ExitCode < 0 || e.ExitCode > 255 || (e.State == protocol.StateSucceeded && e.ExitCode != 0) ||
			(e.State == protocol.StateFailed && e.ExitCode == 0) {
			return fmt.Errorf("%w: invalid exit code %d for %s", ErrInvalidEntry, e.ExitCode, e.State)
		}
	} else if e.ExitCode != 0 {
		return fmt.Errorf("%w: %s has no exit code", ErrInvalidEntry, e.State)
	}
	if keys.errorCode {
		if !validErrorCode(e.ErrorCode) {
			return fmt.Errorf("%w: invalid error code", ErrInvalidEntry)
		}
	} else if e.ErrorCode != "" {
		return fmt.Errorf("%w: %s has no error code", ErrInvalidEntry, e.State)
	}
	if e.StdoutBytes > maxOutputCount || e.StderrBytes > maxOutputCount || e.Chunks > maxOutputCount {
		return fmt.Errorf("%w: output counts exceed the bound", ErrInvalidEntry)
	}
	total := e.StdoutBytes + e.StderrBytes
	if total < e.Chunks || total > e.Chunks*maxOutputChunkBytes {
		return fmt.Errorf("%w: byte counts must fit the chunk count", ErrInvalidEntry)
	}
	if e.State == protocol.StateRejected && (total != 0 || e.Chunks != 0 || e.Truncated) {
		return fmt.Errorf("%w: a rejected run has no output", ErrInvalidEntry)
	}
	return nil
}

// stateRules says which optional fields a state carries.
var stateRules = map[protocol.ExecutionState]struct {
	times, exitCode, errorCode bool
}{
	protocol.StateSucceeded:   {times: true, exitCode: true},
	protocol.StateFailed:      {times: true, exitCode: true},
	protocol.StateTimedOut:    {times: true},
	protocol.StateCancelled:   {times: true},
	protocol.StateOutOfMemory: {times: true},
	protocol.StateErrored:     {errorCode: true},
	protocol.StateRejected:    {errorCode: true},
}

// entryKeys lists the exact keys of one journal line.
var entryKeys = []string{
	"schemaVersion", "executionId", "workspaceId", "state", "recordedAt",
	"startedAt", "endedAt", "durationMs", "exitCode", "stdoutBytes",
	"stderrBytes", "truncated", "chunks", "errorCode",
}

// entryJSON is an entry's wire form. Times are empty when the state has none.
type entryJSON struct {
	SchemaVersion int    `json:"schemaVersion"`
	ExecutionID   string `json:"executionId"`
	WorkspaceID   string `json:"workspaceId"`
	State         string `json:"state"`
	RecordedAt    string `json:"recordedAt"`
	StartedAt     string `json:"startedAt"`
	EndedAt       string `json:"endedAt"`
	DurationMs    int64  `json:"durationMs"`
	ExitCode      int    `json:"exitCode"`
	StdoutBytes   uint64 `json:"stdoutBytes"`
	StderrBytes   uint64 `json:"stderrBytes"`
	Truncated     bool   `json:"truncated"`
	Chunks        uint64 `json:"chunks"`
	ErrorCode     string `json:"errorCode"`
}

// MarshalJSON encodes the entry as one JSON object after checking it. The
// result never contains a newline.
func (e Entry) MarshalJSON() ([]byte, error) {
	if err := e.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(entryJSON{
		SchemaVersion: e.SchemaVersion,
		ExecutionID:   e.ExecutionID,
		WorkspaceID:   e.WorkspaceID,
		State:         string(e.State),
		RecordedAt:    formatTimestamp(e.RecordedAt),
		StartedAt:     formatTimestamp(e.StartedAt),
		EndedAt:       formatTimestamp(e.EndedAt),
		DurationMs:    e.Duration.Milliseconds(),
		ExitCode:      e.ExitCode,
		StdoutBytes:   e.StdoutBytes,
		StderrBytes:   e.StderrBytes,
		Truncated:     e.Truncated,
		Chunks:        e.Chunks,
		ErrorCode:     e.ErrorCode,
	})
}

// UnmarshalJSON decodes strictly, exactly as DecodeEntry does.
func (e *Entry) UnmarshalJSON(data []byte) error {
	entry, err := DecodeEntry(data)
	if err != nil {
		return err
	}
	*e = entry
	return nil
}

// DecodeEntry strictly decodes one journal entry: valid UTF-8, exactly the
// fourteen keys, no duplicates, no trailing data, and every Validate rule.
func DecodeEntry(encoded []byte) (Entry, error) {
	if len(encoded) == 0 || len(encoded) > maxEntryBytes {
		return Entry{}, fmt.Errorf("%w: entry size must be 1 to %d bytes", ErrInvalidEntry, maxEntryBytes)
	}
	if !utf8.Valid(encoded) {
		return Entry{}, fmt.Errorf("%w: entry is not valid UTF-8", ErrInvalidEntry)
	}
	if err := checkEntryKeys(encoded); err != nil {
		return Entry{}, err
	}
	var raw entryJSON
	if err := json.Unmarshal(encoded, &raw); err != nil {
		return Entry{}, fmt.Errorf("%w: decode entry: %v", ErrInvalidEntry, err)
	}
	if raw.DurationMs < 0 {
		return Entry{}, fmt.Errorf("%w: negative duration", ErrInvalidEntry)
	}
	entry := Entry{
		SchemaVersion: raw.SchemaVersion,
		ExecutionID:   raw.ExecutionID,
		WorkspaceID:   raw.WorkspaceID,
		State:         protocol.ExecutionState(raw.State),
		ExitCode:      raw.ExitCode,
		StdoutBytes:   raw.StdoutBytes,
		StderrBytes:   raw.StderrBytes,
		Truncated:     raw.Truncated,
		Chunks:        raw.Chunks,
		ErrorCode:     raw.ErrorCode,
		Duration:      time.Duration(raw.DurationMs) * time.Millisecond,
	}
	var err error
	if entry.RecordedAt, err = parseTimestamp(raw.RecordedAt); err != nil {
		return Entry{}, fmt.Errorf("%w: recordedAt: %v", ErrInvalidEntry, err)
	}
	if entry.StartedAt, err = parseTimestamp(raw.StartedAt); err != nil {
		return Entry{}, fmt.Errorf("%w: startedAt: %v", ErrInvalidEntry, err)
	}
	if entry.EndedAt, err = parseTimestamp(raw.EndedAt); err != nil {
		return Entry{}, fmt.Errorf("%w: endedAt: %v", ErrInvalidEntry, err)
	}
	return entry, entry.Validate()
}

func checkEntryKeys(encoded []byte) error {
	expected := make(map[string]struct{}, len(entryKeys))
	for _, key := range entryKeys {
		expected[key] = struct{}{}
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("%w: malformed object", ErrInvalidEntry)
	}
	if delimiter, ok := token.(json.Delim); !ok || delimiter != '{' {
		return fmt.Errorf("%w: an entry must be a JSON object", ErrInvalidEntry)
	}
	seen := make(map[string]struct{}, len(entryKeys))
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return fmt.Errorf("%w: malformed key", ErrInvalidEntry)
		}
		key, ok := keyToken.(string)
		if !ok {
			return fmt.Errorf("%w: malformed key", ErrInvalidEntry)
		}
		if _, ok := expected[key]; !ok {
			return fmt.Errorf("%w: unexpected or incorrectly-cased key %q", ErrInvalidEntry, key)
		}
		if _, duplicate := seen[key]; duplicate {
			return fmt.Errorf("%w: duplicate key %q", ErrInvalidEntry, key)
		}
		seen[key] = struct{}{}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return fmt.Errorf("%w: malformed value", ErrInvalidEntry)
		}
	}
	if _, err := decoder.Token(); err != nil {
		return fmt.Errorf("%w: malformed object", ErrInvalidEntry)
	}
	if len(seen) != len(expected) {
		return fmt.Errorf("%w: missing required key", ErrInvalidEntry)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("%w: trailing data", ErrInvalidEntry)
	}
	return nil
}

func formatTimestamp(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(timestampLayout)
}

func parseTimestamp(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	if len(value) != len(timestampLayout) {
		return time.Time{}, errors.New("must be UTC with millisecond precision")
	}
	parsed, err := time.Parse(timestampLayout, value)
	if err != nil {
		return time.Time{}, errors.New("must be UTC with millisecond precision")
	}
	return parsed, nil
}

func validExecutionID(value string) bool {
	if value == "" || len(value) > maxExecutionIDBytes || strings.TrimSpace(value) != value {
		return false
	}
	for index := 0; index < len(value); index++ {
		char := value[index]
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') || char == '-' || char == '_' || char == '.' || char == ':' {
			continue
		}
		return false
	}
	return true
}

func validWorkspaceID(value string) bool {
	if len(value) != 32 {
		return false
	}
	for index := 0; index < len(value); index++ {
		char := value[index]
		if (char >= '0' && char <= '9') || (char >= 'a' && char <= 'f') {
			continue
		}
		return false
	}
	return true
}

func validErrorCode(value string) bool {
	if value == "" || len(value) > maxErrorCodeBytes || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for index := 1; index < len(value); index++ {
		char := value[index]
		if (char < 'a' || char > 'z') && char != '_' {
			return false
		}
	}
	return true
}

// Journal is one Mio's run journal. It is safe for concurrent use.
type Journal struct {
	path string
	mu   sync.Mutex
	file *os.File
	// broken holds the first write failure. A broken journal accepts no more
	// appends, but its earlier entries remain readable.
	broken     error
	count      int
	maxEntries int
	maxBytes   int
}

// Open opens or creates stateDir's journal, checking that it is a private
// regular file, discarding an unterminated final line, and validating every
// committed line. Any other problem fails closed.
func Open(stateDir string) (*Journal, error) {
	if !platformSupported() {
		return nil, ErrUnsupportedPlatform
	}
	if stateDir == "" || !filepath.IsAbs(stateDir) {
		return nil, fmt.Errorf("%w: the state directory must be absolute", ErrInvalidEntry)
	}
	dirInfo, err := os.Lstat(stateDir)
	if err != nil {
		return nil, fmt.Errorf("open the run journal: %w", err)
	}
	if !dirInfo.IsDir() || dirInfo.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("open the run journal: %s is not a real directory", stateDir)
	}
	path := filepath.Join(stateDir, FileName)
	file, err := openJournalFile(path)
	if err != nil {
		return nil, err
	}
	j := &Journal{path: path, file: file, maxEntries: MaxEntries, maxBytes: MaxBytes}
	if err := j.prepare(); err != nil {
		_ = file.Close()
		return nil, err
	}
	return j, nil
}

// openJournalFile opens or creates path and checks its type, owner, and mode.
func openJournalFile(path string) (*os.File, error) {
	info, err := os.Lstat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return nil, fmt.Errorf("create the run journal: %w", err)
		}
		// The mode is masked by umask, so set it explicitly.
		if err := file.Chmod(0o600); err != nil {
			_ = file.Close()
			return nil, fmt.Errorf("set the run journal's mode: %w", err)
		}
		opened, statErr := file.Stat()
		if statErr != nil || !validJournalInfo(opened) {
			_ = file.Close()
			return nil, errors.New("create the run journal: unsafe file")
		}
		return file, nil
	case err != nil:
		return nil, fmt.Errorf("inspect the run journal: %w", err)
	}
	if !validJournalInfo(info) {
		return nil, fmt.Errorf("refuse the run journal at %s: it must be a regular file owned by the user with mode 0600", path)
	}
	file, err := os.OpenFile(path, os.O_RDWR|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open the run journal: %w", err)
	}
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) || !validJournalInfo(opened) {
		_ = file.Close()
		return nil, errors.New("open the run journal: the file changed while opening it")
	}
	return file, nil
}

func validJournalInfo(info os.FileInfo) bool {
	return info != nil && info.Mode().IsRegular() && info.Mode().Perm() == 0o600 && fileOwnedBy(info)
}

// prepare discards an unterminated final line and validates every committed
// line. Only newline-terminated lines count as committed, so a crash cannot
// leave a half-written entry behind.
func (j *Journal) prepare() error {
	info, err := j.file.Stat()
	if err != nil {
		return fmt.Errorf("inspect the run journal: %w", err)
	}
	if info.Size() > int64(j.maxBytes) {
		return fmt.Errorf("the run journal is larger than its %d-byte bound", j.maxBytes)
	}
	data := make([]byte, info.Size())
	if _, err := j.file.ReadAt(data, 0); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("read the run journal: %w", err)
	}
	if len(data) > 0 && data[len(data)-1] != '\n' {
		cut := bytes.LastIndexByte(data, '\n') + 1
		if err := j.file.Truncate(int64(cut)); err != nil {
			return fmt.Errorf("discard the run journal's torn line: %w", err)
		}
		if err := j.file.Sync(); err != nil {
			return fmt.Errorf("sync the run journal: %w", err)
		}
		data = data[:cut]
	}
	entries, err := parseEntries(data)
	if err != nil {
		return err
	}
	j.count = len(entries)
	return nil
}

// Append records result for workspaceID. A failed write marks the journal
// unusable and every later Append returns ErrBroken.
func (j *Journal) Append(workspaceID string, result protocol.ExecutionResult) error {
	entry, err := NewEntry(workspaceID, result, time.Now())
	if err != nil {
		return err
	}
	line, err := entry.MarshalJSON()
	if err != nil {
		return err
	}
	return j.appendLine(line)
}

func (j *Journal) appendLine(line []byte) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.broken != nil {
		return fmt.Errorf("%w: %v", ErrBroken, j.broken)
	}
	info, err := j.file.Stat()
	if err != nil {
		return j.failLocked(err)
	}
	if j.count >= j.maxEntries || info.Size()+int64(len(line))+1 > int64(j.maxBytes) {
		if err := j.compactLocked(line); err != nil {
			return j.failLocked(err)
		}
	}
	if _, err := j.file.Write(append(line, '\n')); err != nil {
		return j.failLocked(err)
	}
	if err := j.file.Sync(); err != nil {
		return j.failLocked(err)
	}
	j.count++
	return nil
}

// compactLocked rewrites the journal with its newest entries and the room the
// new line needs, keeping at most maxEntries-1 old entries. It replaces the
// file by rename and reopens it.
func (j *Journal) compactLocked(newLine []byte) error {
	data, err := j.readLocked()
	if err != nil {
		return err
	}
	entries, err := parseEntries(data)
	if err != nil {
		return err
	}
	// Measure from the newest entry, leaving room for the new line.
	total := len(newLine) + 1
	lines := make([][]byte, 0, min(len(entries), j.maxEntries))
	for index := len(entries) - 1; index >= 0 && len(lines) < j.maxEntries-1; index-- {
		encoded, err := entries[index].MarshalJSON()
		if err != nil {
			return err
		}
		if total+len(encoded)+1 > j.maxBytes {
			break
		}
		total += len(encoded) + 1
		lines = append(lines, encoded)
	}
	// lines is newest first; write it oldest first.
	for left, right := 0, len(lines)-1; left < right; left, right = left+1, right-1 {
		lines[left], lines[right] = lines[right], lines[left]
	}
	return j.rewriteLocked(lines)
}

// rewriteLocked replaces the journal with lines, atomically, and reopens it.
func (j *Journal) rewriteLocked(lines [][]byte) error {
	dir := filepath.Dir(j.path)
	temp, err := os.CreateTemp(dir, ".runs-*.tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer func() { _ = os.Remove(tempPath) }()
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return err
	}
	for _, line := range lines {
		if _, err := temp.Write(append(line, '\n')); err != nil {
			_ = temp.Close()
			return err
		}
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, j.path); err != nil {
		return err
	}
	if err := syncDir(dir); err != nil {
		return err
	}
	opened, err := os.OpenFile(j.path, os.O_RDWR|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	info, err := opened.Stat()
	if err != nil || !validJournalInfo(info) {
		_ = opened.Close()
		return errors.New("the rewritten run journal is unsafe")
	}
	old := j.file
	j.file = opened
	j.count = len(lines)
	if err := old.Close(); err != nil {
		return err
	}
	return nil
}

// Entries returns up to limit entries, newest first, optionally filtered by
// workspace ID. A read error does not mark the journal broken.
func (j *Journal) Entries(workspaceID string, limit int) ([]Entry, error) {
	if limit < 1 || limit > MaxLimit {
		return nil, fmt.Errorf("%w: limit must be 1 to %d", ErrInvalidLimit, MaxLimit)
	}
	if workspaceID != "" && !validWorkspaceID(workspaceID) {
		return nil, fmt.Errorf("%w: invalid workspace ID", ErrInvalidEntry)
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	data, err := j.readLocked()
	if err != nil {
		return nil, err
	}
	if len(data) > 0 && data[len(data)-1] != '\n' {
		data = data[:bytes.LastIndexByte(data, '\n')+1]
	}
	entries, err := parseEntries(data)
	if err != nil {
		return nil, err
	}
	result := make([]Entry, 0, min(limit, len(entries)))
	for index := len(entries) - 1; index >= 0 && len(result) < limit; index-- {
		if workspaceID != "" && entries[index].WorkspaceID != workspaceID {
			continue
		}
		result = append(result, entries[index])
	}
	return result, nil
}

// readLocked reads the whole journal through the open handle.
func (j *Journal) readLocked() ([]byte, error) {
	info, err := j.file.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect the run journal: %w", err)
	}
	if info.Size() > int64(j.maxBytes) {
		return nil, fmt.Errorf("the run journal is larger than its %d-byte bound", j.maxBytes)
	}
	data := make([]byte, info.Size())
	if _, err := j.file.ReadAt(data, 0); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("read the run journal: %w", err)
	}
	return data, nil
}

// parseEntries parses newline-separated entries. data must end at a line
// boundary or hold a torn final segment, which is discarded.
func parseEntries(data []byte) ([]Entry, error) {
	if len(data) == 0 {
		return []Entry{}, nil
	}
	if data[len(data)-1] != '\n' {
		cut := bytes.LastIndexByte(data, '\n') + 1
		data = data[:cut]
		if len(data) == 0 {
			return []Entry{}, nil
		}
	}
	segments := bytes.Split(data[:len(data)-1], []byte{'\n'})
	entries := make([]Entry, 0, len(segments))
	for index, segment := range segments {
		if len(segment) == 0 {
			return nil, fmt.Errorf("the run journal has an empty line at %d", index+1)
		}
		entry, err := DecodeEntry(segment)
		if err != nil {
			return nil, fmt.Errorf("the run journal is invalid at line %d: %w", index+1, err)
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// Broken reports whether a write failure made the journal unusable.
func (j *Journal) Broken() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.broken != nil
}

// Close releases the journal's file. It is safe to call more than once.
func (j *Journal) Close() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.file == nil {
		return nil
	}
	err := j.file.Close()
	j.file = nil
	return err
}

func (j *Journal) failLocked(err error) error {
	if j.broken == nil {
		j.broken = err
	}
	return fmt.Errorf("%w: %v", ErrBroken, err)
}

func syncDir(dir string) error {
	handle, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer handle.Close()
	return handle.Sync()
}
