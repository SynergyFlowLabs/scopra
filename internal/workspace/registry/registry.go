// Package registry stores Scopra Mio's workspace registrations (ADR-005).
// Workspaces are validated with the workspace package (ADR-004) and persisted
// atomically as versioned JSON in a private directory that the caller owns
// exclusively. Removing a registration never touches the workspace itself.
package registry

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/SynergyFlowLabs/scopra/internal/workspace"
)

const (
	fileName      = "workspaces.json"
	formatVersion = 1
	// maxWorkspaces bounds the number of registrations.
	maxWorkspaces = 256
	// maxFileBytes bounds the size of the registry file that is read or written.
	maxFileBytes = 1 << 20
)

const (
	// MaxPathBytes is the longest workspace path the registry stores: Linux
	// PATH_MAX without its terminating NUL. Longer paths cannot be resolved,
	// so Add never produces them.
	MaxPathBytes = 4095
	// MaxWorkspaces is the largest number of registered workspaces.
	MaxWorkspaces = maxWorkspaces
)

var (
	// ErrUnsupportedPlatform reports that the registry is unavailable on this platform.
	ErrUnsupportedPlatform = errors.New("registry: unsupported on this platform")
	// ErrInsecureRegistry reports a registry file or directory with unsafe type, owner, or mode.
	ErrInsecureRegistry = errors.New("registry: insecure registry file or directory")
	// ErrCorruptRegistry reports registry content that is malformed or violates the rules.
	ErrCorruptRegistry = errors.New("registry: registry file is invalid")
	// ErrAlreadyRegistered reports a directory that is already registered.
	ErrAlreadyRegistered = errors.New("registry: directory is already registered")
	// ErrOverlap reports a workspace inside, or containing, a registered workspace.
	ErrOverlap = errors.New("registry: workspace overlaps a registered workspace")
	// ErrRegistryFull reports that no more workspaces can be registered.
	ErrRegistryFull = errors.New("registry: too many registered workspaces")
	// ErrNotFound reports an unknown workspace ID.
	ErrNotFound = errors.New("registry: workspace is not registered")
)

// Record is a registered workspace.
type Record struct {
	// ID is a random 128-bit identifier in lowercase hexadecimal.
	ID string
	workspace.Workspace
}

// Registry holds the workspace registrations stored in one directory. Its
// methods are safe for concurrent use; only one process may open a directory.
type Registry struct {
	dir           string // canonical
	validator     *workspace.Validator
	maxWorkspaces int

	mu      sync.Mutex
	records []Record // sorted by Path
}

// storedRegistry is the version 1 file format.
type storedRegistry struct {
	Version    int             `json:"version"`
	Workspaces *[]storedRecord `json:"workspaces"`
}

type storedRecord struct {
	ID          string            `json:"id"`
	Path        string            `json:"path"`
	Fingerprint storedFingerprint `json:"fingerprint"`
}

type storedFingerprint struct {
	Device uint64 `json:"device"`
	Inode  uint64 `json:"inode"`
}

// Open loads the registry stored in dir: an absolute path to an existing
// directory owned by the effective user with no group or other access. A
// missing registry file means no registrations, and Open never writes. Content
// that Add could not have written in dir, including a workspace that contains
// or lies inside dir, is rejected with ErrCorruptRegistry. The caller must own
// dir exclusively; Scopra Mio passes its state directory while it holds the
// state-directory lock.
func Open(dir string, validator *workspace.Validator) (*Registry, error) {
	if validator == nil {
		return nil, errors.New("registry: a workspace validator is required")
	}
	if !filepath.IsAbs(dir) {
		return nil, fmt.Errorf("%w: registry directory %q", workspace.ErrInvalidPath, dir)
	}
	dir = filepath.Clean(dir)
	if err := checkDirectory(dir); err != nil {
		return nil, err
	}
	canonical, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, fmt.Errorf("resolve registry directory: %w", err)
	}

	data, found, err := readRegistryFile(filepath.Join(canonical, fileName))
	if err != nil {
		return nil, err
	}
	var records []Record
	if found {
		if records, err = decode(data); err != nil {
			return nil, err
		}
	}
	for _, record := range records {
		if overlaps(record.Path, canonical) {
			return nil, fmt.Errorf("%w: workspace %q overlaps the registry directory",
				ErrCorruptRegistry, record.Path)
		}
	}
	return &Registry{
		dir:           canonical,
		validator:     validator,
		maxWorkspaces: maxWorkspaces,
		records:       records,
	}, nil
}

// Check will validate the registry in dir without writing.
// Skeleton for the Step 11 tests: not implemented yet.
func Check(dir string) (int, error) { return 0, errors.New("registry: Check is not implemented yet") }

// Add validates the workspace at path and registers it under a new ID. It
// rejects a directory that is already registered, even under another path; a
// workspace overlapping a registered one; and a workspace that contains or lies
// inside the registry directory, where Mio keeps its other state. If the
// registry file was replaced but its directory could not be synced, Add
// returns the error and the registration stays in effect.
func (r *Registry) Add(path string) (Record, error) {
	inspected, err := r.validator.Inspect(path)
	if err != nil {
		return Record{}, err
	}
	if overlaps(inspected.Path, r.dir) {
		return Record{}, fmt.Errorf("%w: %q overlaps the workspace registry directory",
			workspace.ErrProtectedPath, inspected.Path)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.records {
		if existing.Path == inspected.Path || existing.Fingerprint == inspected.Fingerprint {
			return Record{}, fmt.Errorf("%w as %s at %q",
				ErrAlreadyRegistered, existing.ID, existing.Path)
		}
		if overlaps(existing.Path, inspected.Path) {
			return Record{}, fmt.Errorf("%w: %q and %q", ErrOverlap, existing.Path, inspected.Path)
		}
	}
	if len(r.records) >= r.maxWorkspaces {
		return Record{}, fmt.Errorf("%w (limit %d)", ErrRegistryFull, r.maxWorkspaces)
	}

	record := Record{ID: newID(), Workspace: inspected}
	updated := append(slices.Clone(r.records), record)
	sortRecords(updated)
	if err := r.commit(updated); err != nil {
		return Record{}, err
	}
	return record, nil
}

// Remove deletes the registration with the given ID. It never deletes or
// modifies the workspace directory or its contents.
func (r *Registry) Remove(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	index := slices.IndexFunc(r.records, func(record Record) bool { return record.ID == id })
	if index < 0 {
		return fmt.Errorf("%w: %q", ErrNotFound, id)
	}
	return r.commit(slices.Delete(slices.Clone(r.records), index, index+1))
}

// Get returns the registration with the given ID.
func (r *Registry) Get(id string) (Record, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, record := range r.records {
		if record.ID == id {
			return record, nil
		}
	}
	return Record{}, fmt.Errorf("%w: %q", ErrNotFound, id)
}

// List returns a copy of every registration, sorted by path.
func (r *Registry) List() []Record {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.records)
}

// commit persists records and adopts them once the registry file has been
// replaced. The caller must hold r.mu.
func (r *Registry) commit(records []Record) error {
	replaced, err := r.save(records)
	if replaced {
		r.records = records
	}
	return err
}

// save atomically replaces the registry file: it writes a private temporary
// file in the same directory, syncs it, renames it over the registry file, and
// then syncs the directory so the rename survives a crash. replaced reports
// whether the rename happened; any earlier failure leaves the file unchanged.
func (r *Registry) save(records []Record) (replaced bool, err error) {
	data, err := encode(records)
	if err != nil {
		return false, err
	}
	temporary, err := os.CreateTemp(r.dir, ".workspaces-*.tmp")
	if err != nil {
		return false, fmt.Errorf("create temporary registry file: %w", err)
	}
	defer func() {
		if !replaced {
			_ = temporary.Close()
			_ = os.Remove(temporary.Name())
		}
	}()
	if _, err := temporary.Write(data); err != nil {
		return false, fmt.Errorf("write temporary registry file: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return false, fmt.Errorf("sync temporary registry file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return false, fmt.Errorf("close temporary registry file: %w", err)
	}
	if err := os.Rename(temporary.Name(), filepath.Join(r.dir, fileName)); err != nil {
		return false, fmt.Errorf("replace registry file: %w", err)
	}
	return true, syncDirectory(r.dir)
}

func encode(records []Record) ([]byte, error) {
	stored := make([]storedRecord, 0, len(records))
	for _, record := range records {
		stored = append(stored, storedRecord{
			ID:   record.ID,
			Path: record.Path,
			Fingerprint: storedFingerprint{
				Device: record.Fingerprint.Device,
				Inode:  record.Fingerprint.Inode,
			},
		})
	}
	data, err := json.Marshal(storedRegistry{Version: formatVersion, Workspaces: &stored})
	if err != nil {
		return nil, fmt.Errorf("encode registry: %w", err)
	}
	data = append(data, '\n')
	if len(data) > maxFileBytes {
		return nil, fmt.Errorf("%w: the registry file would exceed %d bytes",
			ErrRegistryFull, maxFileBytes)
	}
	return data, nil
}

// decode parses version 1 content strictly. Records are checked structurally
// only; workspaces are revalidated against the filesystem before use.
func decode(data []byte) ([]Record, error) {
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("%w: content is not valid UTF-8", ErrCorruptRegistry)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var stored storedRegistry
	if err := decoder.Decode(&stored); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCorruptRegistry, err)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%w: unexpected data after the registry object", ErrCorruptRegistry)
	}
	if stored.Version != formatVersion {
		return nil, fmt.Errorf("%w: unsupported version %d", ErrCorruptRegistry, stored.Version)
	}
	if stored.Workspaces == nil {
		return nil, fmt.Errorf("%w: missing workspace list", ErrCorruptRegistry)
	}
	if len(*stored.Workspaces) > maxWorkspaces {
		return nil, fmt.Errorf("%w: more than %d workspaces", ErrCorruptRegistry, maxWorkspaces)
	}

	records := make([]Record, 0, len(*stored.Workspaces))
	for _, entry := range *stored.Workspaces {
		record := Record{ID: entry.ID, Workspace: workspace.Workspace{
			Path: entry.Path,
			Fingerprint: workspace.Fingerprint{
				Device: entry.Fingerprint.Device,
				Inode:  entry.Fingerprint.Inode,
			},
		}}
		if err := checkRecord(record, records); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrCorruptRegistry, err)
		}
		records = append(records, record)
	}
	sortRecords(records)
	return records, nil
}

// checkRecord applies the registration rules that Add enforces, so that Open
// accepts only content that Add could have written.
func checkRecord(record Record, accepted []Record) error {
	if !validID(record.ID) {
		return fmt.Errorf("malformed ID %q", record.ID)
	}
	path := record.Path
	canonical := filepath.IsAbs(path) && filepath.Clean(path) == path
	if !canonical || path == string(filepath.Separator) {
		return fmt.Errorf("%q is not a canonical workspace path", path)
	}
	if len(path) > MaxPathBytes {
		return fmt.Errorf("a workspace path is longer than %d bytes", MaxPathBytes)
	}
	for _, other := range accepted {
		switch {
		case other.ID == record.ID:
			return fmt.Errorf("duplicate ID %q", record.ID)
		case other.Fingerprint == record.Fingerprint:
			return fmt.Errorf("%q and %q have the same fingerprint", other.Path, path)
		case overlaps(other.Path, path):
			return fmt.Errorf("%q and %q overlap", other.Path, path)
		}
	}
	return nil
}

func validID(id string) bool {
	if len(id) != 32 {
		return false
	}
	for _, character := range id {
		if !strings.ContainsRune("0123456789abcdef", character) {
			return false
		}
	}
	return true
}

func newID() string {
	var raw [16]byte
	// crypto/rand.Read never returns an error; it crashes the program instead.
	_, _ = rand.Read(raw[:])
	return hex.EncodeToString(raw[:])
}

func sortRecords(records []Record) {
	slices.SortFunc(records, func(a, b Record) int { return strings.Compare(a.Path, b.Path) })
}

// overlaps reports whether either clean absolute path is, or contains, the other.
func overlaps(a, b string) bool {
	return contains(a, b) || contains(b, a)
}

// contains reports whether dir is path or one of its ancestors.
func contains(dir, path string) bool {
	return path == dir || strings.HasPrefix(path, dir+string(filepath.Separator))
}
