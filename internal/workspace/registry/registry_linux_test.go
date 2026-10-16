//go:build linux

package registry

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/SynergyFlowLabs/scopra/internal/workspace"
)

var idPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

func TestOpenStartsEmptyWithoutCreatingFiles(t *testing.T) {
	dir := makeRegistryDir(t)
	registry := mustOpen(t, dir, newValidator(t))

	if got := registry.List(); len(got) != 0 {
		t.Fatalf("List() = %+v, want empty", got)
	}
	requireDirEntries(t, dir)
}

func TestAddRecordsCanonicalWorkspaceAndPersistsIt(t *testing.T) {
	validator := newValidator(t)
	dir := makeRegistryDir(t)
	registry := mustOpen(t, dir, validator)
	project := makeDir(t, t.TempDir(), "project")
	link := filepath.Join(t.TempDir(), "link")
	symlink(t, project, link)

	record, err := registry.Add(link)
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	want, err := validator.Inspect(project)
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if record.Workspace != want || !idPattern.MatchString(record.ID) {
		t.Fatalf("Add() = %+v, want workspace %+v and a 32-hex-digit ID", record, want)
	}
	if got := mustOpen(t, dir, validator).List(); !slices.Equal(got, []Record{record}) {
		t.Fatalf("List() after reopening = %+v, want [%+v]", got, record)
	}
	requireOwnerOnlyRegularFile(t, filepath.Join(dir, "workspaces.json"))
	requireDirEntries(t, dir, "workspaces.json")
}

func TestRegistryFileFormatVersion1(t *testing.T) {
	validator := newValidator(t)
	dir := makeRegistryDir(t)
	project := makeDir(t, t.TempDir(), "project")
	record := mustAdd(t, mustOpen(t, dir, validator), project)

	var written map[string]any
	if err := json.Unmarshal(readFile(t, filepath.Join(dir, "workspaces.json")), &written); err != nil {
		t.Fatalf("Unmarshal(written registry) error = %v", err)
	}
	want := map[string]any{
		"version": float64(1),
		"workspaces": []any{map[string]any{
			"id":   record.ID,
			"path": project,
			"fingerprint": map[string]any{
				"device": float64(record.Fingerprint.Device),
				"inode":  float64(record.Fingerprint.Inode),
			},
		}},
	}
	if fmt.Sprint(written) != fmt.Sprint(want) {
		t.Fatalf("written registry = %v, want %v", written, want)
	}

	handWritten := makeRegistryDir(t)
	writeFile(t, filepath.Join(handWritten, "workspaces.json"), `{"version":1,"workspaces":[`+
		`{"id":"0123456789abcdef0123456789abcdef","path":"/srv/app",`+
		`"fingerprint":{"device":7,"inode":42}}]}`)
	wantRecord := Record{ID: "0123456789abcdef0123456789abcdef", Workspace: workspace.Workspace{
		Path:        "/srv/app",
		Fingerprint: workspace.Fingerprint{Device: 7, Inode: 42},
	}}
	if got := mustOpen(t, handWritten, validator).List(); !slices.Equal(got, []Record{wantRecord}) {
		t.Fatalf("List() of a hand-written version 1 registry = %+v, want [%+v]", got, wantRecord)
	}
}

func TestAddAppliesWorkspaceValidation(t *testing.T) {
	home := makeDir(t, t.TempDir(), "home")
	t.Setenv("HOME", home)
	dir := makeRegistryDir(t)
	registry := mustOpen(t, dir, mustValidator(t))
	file := filepath.Join(t.TempDir(), "file")
	writeFile(t, file, "")

	tests := []struct {
		path string
		want error
	}{
		{path: "relative/project", want: workspace.ErrInvalidPath},
		{path: home, want: workspace.ErrProtectedPath},
		{path: file, want: workspace.ErrNotDirectory},
		{path: filepath.Join(home, "missing"), want: os.ErrNotExist},
	}
	for _, test := range tests {
		if _, err := registry.Add(test.path); !errors.Is(err, test.want) {
			t.Errorf("Add(%q) error = %v, want %v", test.path, err, test.want)
		}
	}
	if got := registry.List(); len(got) != 0 {
		t.Fatalf("List() = %+v, want no registrations", got)
	}
	requireDirEntries(t, dir)
}

func TestAddRejectsDirectoryAlreadyRegistered(t *testing.T) {
	registry := mustOpen(t, makeRegistryDir(t), newValidator(t))
	base := t.TempDir()
	project := makeDir(t, base, "project")
	link := filepath.Join(base, "link")
	symlink(t, project, link)
	mustAdd(t, registry, project)

	for _, path := range []string{project, link} {
		if _, err := registry.Add(path); !errors.Is(err, ErrAlreadyRegistered) {
			t.Errorf("Add(%q) error = %v, want ErrAlreadyRegistered", path, err)
		}
	}
	// A moved directory keeps its fingerprint, so it is still the same workspace.
	moved := filepath.Join(base, "moved")
	rename(t, project, moved)
	if _, err := registry.Add(moved); !errors.Is(err, ErrAlreadyRegistered) {
		t.Fatalf("Add(moved directory) error = %v, want ErrAlreadyRegistered", err)
	}
}

func TestAddRejectsOverlappingWorkspaces(t *testing.T) {
	base := t.TempDir()
	parent := makeDir(t, base, "code")
	child := makeDir(t, parent, "app")
	sharedPrefix := makeDir(t, base, "codex")

	t.Run("inside a registered workspace", func(t *testing.T) {
		registry := mustOpen(t, makeRegistryDir(t), newValidator(t))
		mustAdd(t, registry, parent)
		if _, err := registry.Add(child); !errors.Is(err, ErrOverlap) {
			t.Fatalf("Add(child) error = %v, want ErrOverlap", err)
		}
	})
	t.Run("containing a registered workspace", func(t *testing.T) {
		registry := mustOpen(t, makeRegistryDir(t), newValidator(t))
		mustAdd(t, registry, child)
		if _, err := registry.Add(parent); !errors.Is(err, ErrOverlap) {
			t.Fatalf("Add(parent) error = %v, want ErrOverlap", err)
		}
	})
	t.Run("sibling sharing a name prefix", func(t *testing.T) {
		registry := mustOpen(t, makeRegistryDir(t), newValidator(t))
		mustAdd(t, registry, parent)
		if _, err := registry.Add(sharedPrefix); err != nil {
			t.Fatalf("Add(%q) error = %v, want nil", sharedPrefix, err)
		}
	})
}

func TestSecurity_AddRejectsWorkspaceContainingRegistry(t *testing.T) {
	project := makeDir(t, t.TempDir(), "project")
	dir := filepath.Join(project, "state")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	// The validator was not told about dir; the registry must protect itself.
	registry := mustOpen(t, dir, newValidator(t))

	if _, err := registry.Add(project); !errors.Is(err, workspace.ErrProtectedPath) {
		t.Fatalf("Add(directory containing the registry) error = %v, want ErrProtectedPath", err)
	}
	requireDirEntries(t, dir)
}

func TestSecurity_AddRejectsWorkspaceInsideRegistry(t *testing.T) {
	dir := makeRegistryDir(t)
	inside := makeDir(t, dir, "project")
	// Mio keeps other state beside the registry, so no workspace may live there.
	registry := mustOpen(t, dir, newValidator(t))

	if _, err := registry.Add(inside); !errors.Is(err, workspace.ErrProtectedPath) {
		t.Fatalf("Add(directory inside the registry directory) error = %v, want ErrProtectedPath", err)
	}
	requireDirEntries(t, dir, "project")
}

func TestAddEnforcesMaximumWorkspaces(t *testing.T) {
	registry := mustOpen(t, makeRegistryDir(t), newValidator(t))
	registry.maxWorkspaces = 2
	base := t.TempDir()
	mustAdd(t, registry, makeDir(t, base, "one"))
	mustAdd(t, registry, makeDir(t, base, "two"))

	if _, err := registry.Add(makeDir(t, base, "three")); !errors.Is(err, ErrRegistryFull) {
		t.Fatalf("Add(third workspace) error = %v, want ErrRegistryFull", err)
	}
	if got := len(registry.List()); got != 2 {
		t.Fatalf("List() has %d records, want 2", got)
	}
}

func TestRemoveDeletesOnlyTheRegistration(t *testing.T) {
	validator := newValidator(t)
	dir := makeRegistryDir(t)
	registry := mustOpen(t, dir, validator)
	project := makeDir(t, t.TempDir(), "project")
	source := filepath.Join(project, "main.go")
	writeFile(t, source, "package main\n")
	record := mustAdd(t, registry, project)

	if err := registry.Remove(record.ID); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if got := registry.List(); len(got) != 0 {
		t.Fatalf("List() after Remove() = %+v, want empty", got)
	}
	if got := mustOpen(t, dir, validator).List(); len(got) != 0 {
		t.Fatalf("List() after reopening = %+v, want empty", got)
	}
	if content := string(readFile(t, source)); content != "package main\n" {
		t.Fatalf("workspace file content = %q after Remove(), want it untouched", content)
	}
	if err := registry.Remove(record.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second Remove() error = %v, want ErrNotFound", err)
	}
	requireDirEntries(t, dir, "workspaces.json")
}

func TestGetReturnsRegisteredWorkspace(t *testing.T) {
	registry := mustOpen(t, makeRegistryDir(t), newValidator(t))
	record := mustAdd(t, registry, makeDir(t, t.TempDir(), "project"))

	got, err := registry.Get(record.ID)
	if err != nil || got != record {
		t.Fatalf("Get(%q) = %+v, %v; want %+v, nil", record.ID, got, err, record)
	}
	for _, id := range []string{"", "not-an-id", "0123456789abcdef0123456789abcdef"} {
		if _, err := registry.Get(id); !errors.Is(err, ErrNotFound) {
			t.Errorf("Get(%q) error = %v, want ErrNotFound", id, err)
		}
	}
}

func TestListIsSortedByPathAndDetached(t *testing.T) {
	registry := mustOpen(t, makeRegistryDir(t), newValidator(t))
	base := t.TempDir()
	second := mustAdd(t, registry, makeDir(t, base, "b"))
	first := mustAdd(t, registry, makeDir(t, base, "a"))

	got := registry.List()
	if !slices.Equal(got, []Record{first, second}) {
		t.Fatalf("List() = %+v, want [%+v %+v]", got, first, second)
	}
	got[0].Path = "/tampered"
	if registry.List()[0] != first {
		t.Fatal("modifying the slice returned by List() changed the registry")
	}
}

func TestSecurity_OpenRejectsUnsafeRegistryFile(t *testing.T) {
	for _, occupant := range unsafeRegistryFiles {
		t.Run(occupant.name, func(t *testing.T) {
			dir := makeRegistryDir(t)
			verifyUnchanged := occupant.plant(t, filepath.Join(dir, "workspaces.json"))

			_, err := openWithin(t, dir, newValidator(t), 5*time.Second)
			if !errors.Is(err, ErrInsecureRegistry) {
				t.Fatalf("Open() error = %v, want ErrInsecureRegistry", err)
			}
			verifyUnchanged(t)
		})
	}
}

func TestSecurity_OpenRejectsUnsafeDirectory(t *testing.T) {
	validator := newValidator(t)
	shared := makeDir(t, t.TempDir(), "shared")
	if err := os.Chmod(shared, 0o750); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}
	link := filepath.Join(t.TempDir(), "link")
	symlink(t, makeRegistryDir(t), link)

	for _, dir := range []string{shared, link} {
		if _, err := Open(dir, validator); !errors.Is(err, ErrInsecureRegistry) {
			t.Errorf("Open(%q) error = %v, want ErrInsecureRegistry", dir, err)
		}
	}
	if _, err := Open("relative/state", validator); !errors.Is(err, workspace.ErrInvalidPath) {
		t.Errorf("Open(relative) error = %v, want ErrInvalidPath", err)
	}
	if _, err := Open(filepath.Join(t.TempDir(), "missing"), validator); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Open(missing) error = %v, want os.ErrNotExist", err)
	}
	if _, err := Open(makeRegistryDir(t), nil); err == nil {
		t.Error("Open() with a nil validator succeeded, want an error")
	}
}

func TestSecurity_OpenRejectsCorruptRegistry(t *testing.T) {
	const id = "0123456789abcdef0123456789abcdef"
	tooMany := make([]string, 0, maxWorkspaces+1)
	for index := range maxWorkspaces + 1 {
		id, path := fmt.Sprintf("%032x", index), fmt.Sprintf("/srv/w%03d", index)
		tooMany = append(tooMany, recordJSON(id, path))
	}
	tests := []struct {
		name    string
		content string
	}{
		{name: "not JSON", content: `{`},
		{name: "invalid UTF-8", content: registryJSON(recordJSON(id, "/srv/\xff"))},
		{name: "trailing data", content: registryJSON() + ` {}`},
		{name: "unknown top-level field", content: `{"version":1,"workspaces":[],"mode":"rw"}`},
		{name: "unknown record field", content: registryJSON(
			strings.TrimSuffix(recordJSON(id, "/srv/a"), "}") + `,"mode":"rw"}`)},
		{name: "missing version", content: `{"workspaces":[]}`},
		{name: "missing workspace list", content: `{"version":1}`},
		{name: "null workspace list", content: `{"version":1,"workspaces":null}`},
		{name: "unsupported version", content: `{"version":2,"workspaces":[]}`},
		{name: "malformed ID", content: registryJSON(recordJSON("abc", "/srv/a"))},
		{name: "uppercase ID", content: registryJSON(recordJSON(strings.ToUpper(id), "/srv/a"))},
		{name: "duplicate IDs", content: registryJSON(recordJSON(id, "/srv/a"), recordJSON(id, "/srv/b"))},
		{name: "relative path", content: registryJSON(recordJSON(id, "srv/a"))},
		{name: "unclean path", content: registryJSON(recordJSON(id, "/srv/a/../b"))},
		{name: "trailing slash", content: registryJSON(recordJSON(id, "/srv/a/"))},
		{name: "filesystem root", content: registryJSON(recordJSON(id, "/"))},
		{name: "path longer than 4095 bytes", content: registryJSON(recordJSON(id, "/"+strings.Repeat("p", MaxPathBytes)))},
		{name: "duplicate paths", content: registryJSON(recordJSON(id, "/srv/a"),
			recordJSON(strings.Repeat("f", 32), "/srv/a"))},
		{name: "overlapping paths", content: registryJSON(recordJSON(id, "/srv/a"),
			recordJSON(strings.Repeat("f", 32), "/srv/a/b"))},
		// recordJSON gives every record the same fingerprint.
		{name: "duplicate fingerprints", content: registryJSON(recordJSON(id, "/srv/a"),
			recordJSON(strings.Repeat("f", 32), "/srv/b"))},
		{name: "too many records", content: registryJSON(tooMany...)},
		{name: "larger than 1 MiB", content: registryJSON() + strings.Repeat(" ", 1<<20)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := makeRegistryDir(t)
			file := filepath.Join(dir, "workspaces.json")
			writeFile(t, file, test.content)

			if _, err := Open(dir, newValidator(t)); !errors.Is(err, ErrCorruptRegistry) {
				t.Fatalf("Open() error = %v, want ErrCorruptRegistry", err)
			}
			if got := string(readFile(t, file)); got != test.content {
				t.Fatal("Open() modified a corrupt registry file")
			}
		})
	}
}

func TestSecurity_OpenRejectsRecordOverlappingRegistry(t *testing.T) {
	const id = "0123456789abcdef0123456789abcdef"
	tests := []struct {
		name string
		path func(dir string) string
	}{
		// For example, a state directory moved into a registered workspace.
		{name: "workspace contains the registry", path: filepath.Dir},
		{name: "workspace is the registry directory", path: func(dir string) string { return dir }},
		{name: "workspace inside the registry directory", path: func(dir string) string {
			return filepath.Join(dir, "project")
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			canonical, err := filepath.EvalSymlinks(makeRegistryDir(t))
			if err != nil {
				t.Fatalf("EvalSymlinks(registry dir) error = %v", err)
			}
			// Records hold canonical paths. Opening through a symlinked
			// ancestor makes the check compare them with the canonical directory.
			link := filepath.Join(t.TempDir(), "link")
			symlink(t, filepath.Dir(canonical), link)
			dir := filepath.Join(link, filepath.Base(canonical))
			file := filepath.Join(canonical, "workspaces.json")
			content := registryJSON(recordJSON(id, test.path(canonical)))
			writeFile(t, file, content)

			if _, err := Open(dir, newValidator(t)); !errors.Is(err, ErrCorruptRegistry) {
				t.Fatalf("Open() error = %v, want ErrCorruptRegistry", err)
			}
			if got := string(readFile(t, file)); got != content {
				t.Fatal("Open() modified the registry file")
			}
		})
	}
}

func TestSecurity_FailedWriteLeavesRegistryUnchanged(t *testing.T) {
	t.Run("temporary file cannot be created", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root ignores directory permissions")
		}
		dir := makeRegistryDir(t)
		registry := mustOpen(t, dir, newValidator(t))
		base := t.TempDir()
		kept := mustAdd(t, registry, makeDir(t, base, "kept"))
		file := filepath.Join(dir, "workspaces.json")
		before := readFile(t, file)
		if err := os.Chmod(dir, 0o500); err != nil {
			t.Fatalf("Chmod() error = %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

		if _, err := registry.Add(makeDir(t, base, "rejected")); err == nil {
			t.Fatal("Add() succeeded although the registry directory is read-only")
		}
		if got := registry.List(); !slices.Equal(got, []Record{kept}) {
			t.Fatalf("List() after a failed Add() = %+v, want [%+v]", got, kept)
		}
		if after := readFile(t, file); string(after) != string(before) {
			t.Fatal("a failed Add() changed the registry file")
		}
	})
	t.Run("rename fails after the temporary file is written", func(t *testing.T) {
		dir := makeRegistryDir(t)
		registry := mustOpen(t, dir, newValidator(t))
		base := t.TempDir()
		kept := mustAdd(t, registry, makeDir(t, base, "kept"))
		// A non-empty directory at the registry path makes the final rename fail.
		file := filepath.Join(dir, "workspaces.json")
		if err := os.Remove(file); err != nil {
			t.Fatalf("Remove() error = %v", err)
		}
		makeDir(t, file, "blocker")

		if _, err := registry.Add(makeDir(t, base, "rejected")); err == nil {
			t.Fatal("Add() succeeded although the registry file could not be replaced")
		}
		if err := registry.Remove(kept.ID); err == nil {
			t.Fatal("Remove() succeeded although the registry file could not be replaced")
		}
		if got := registry.List(); !slices.Equal(got, []Record{kept}) {
			t.Fatalf("List() after failed writes = %+v, want [%+v]", got, kept)
		}
		requireDirEntries(t, dir, "workspaces.json")
	})
}

func TestEncodeNeverWritesWhatOpenWouldReject(t *testing.T) {
	// The longest storable paths make even a legal number of records exceed
	// the read limit.
	records := make([]Record, 0, maxWorkspaces)
	for index := range maxWorkspaces {
		records = append(records, Record{
			ID:        fmt.Sprintf("%032x", index),
			Workspace: workspace.Workspace{Path: fmt.Sprintf("/%04d/%s", index, strings.Repeat("p", MaxPathBytes-6))},
		})
	}

	if _, err := encode(records); !errors.Is(err, ErrRegistryFull) {
		t.Fatalf("encode(%d records of about 4 KiB each) error = %v, want ErrRegistryFull", len(records), err)
	}
	data, err := encode(records[:1])
	if err != nil {
		t.Fatalf("encode(one record) error = %v", err)
	}
	if _, err := decode(data); err != nil {
		t.Fatalf("decode(encode(one record)) error = %v, want a round trip", err)
	}
}

func TestConcurrentAddsAreAllPersisted(t *testing.T) {
	validator := newValidator(t)
	dir := makeRegistryDir(t)
	registry := mustOpen(t, dir, validator)
	base := t.TempDir()
	const workers = 8
	errs := make([]error, workers)
	var wg sync.WaitGroup
	for index := range workers {
		path := makeDir(t, base, fmt.Sprintf("w%d", index))
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[index] = registry.Add(path)
		}()
	}
	wg.Wait()

	for index, err := range errs {
		if err != nil {
			t.Errorf("Add(w%d) error = %v", index, err)
		}
	}
	if got := len(mustOpen(t, dir, validator).List()); got != workers {
		t.Fatalf("reopened registry has %d records, want %d", got, workers)
	}
}

// unsafeRegistryFiles plants occupants that Open must refuse to read. Each
// plant returns a check that the occupant was left unchanged.
var unsafeRegistryFiles = []struct {
	name  string
	plant func(t *testing.T, path string) (verifyUnchanged func(t *testing.T))
}{
	{
		name: "dangling symlink",
		plant: func(t *testing.T, path string) func(t *testing.T) {
			target := filepath.Join(t.TempDir(), "target")
			symlink(t, target, path)
			return func(t *testing.T) {
				if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("symlink target was created (Lstat error = %v)", err)
				}
			}
		},
	},
	{
		name: "symlink to a valid registry",
		plant: func(t *testing.T, path string) func(t *testing.T) {
			target := filepath.Join(makeRegistryDir(t), "workspaces.json")
			writeFile(t, target, registryJSON())
			symlink(t, target, path)
			return func(t *testing.T) {
				if got := string(readFile(t, target)); got != registryJSON() {
					t.Fatalf("symlink target content = %q, want it unchanged", got)
				}
			}
		},
	},
	{
		name: "group-readable file",
		plant: func(t *testing.T, path string) func(t *testing.T) {
			writeFile(t, path, registryJSON())
			if err := os.Chmod(path, 0o640); err != nil {
				t.Fatalf("Chmod() error = %v", err)
			}
			return func(t *testing.T) {
				info, err := os.Lstat(path)
				if err != nil || info.Mode().Perm() != 0o640 {
					t.Fatalf("registry file was modified (info = %v, error = %v)", info, err)
				}
			}
		},
	},
	{
		name: "directory",
		plant: func(t *testing.T, path string) func(t *testing.T) {
			makeDir(t, path)
			return func(t *testing.T) {
				if info, err := os.Lstat(path); err != nil || !info.IsDir() {
					t.Fatalf("registry directory was modified (error = %v)", err)
				}
			}
		},
	},
	{
		name: "FIFO",
		plant: func(t *testing.T, path string) func(t *testing.T) {
			if err := syscall.Mkfifo(path, 0o600); err != nil {
				t.Fatalf("Mkfifo() error = %v", err)
			}
			return func(t *testing.T) {
				if info, err := os.Lstat(path); err != nil || info.Mode()&os.ModeNamedPipe == 0 {
					t.Fatalf("registry FIFO was modified (error = %v)", err)
				}
			}
		},
	},
}

func registryJSON(records ...string) string {
	return `{"version":1,"workspaces":[` + strings.Join(records, ",") + `]}`
}

func recordJSON(id, path string) string {
	return `{"id":"` + id + `","path":"` + path + `","fingerprint":{"device":1,"inode":2}}`
}

// newValidator returns a validator whose $HOME is a fresh temporary directory.
func newValidator(t *testing.T) *workspace.Validator {
	t.Helper()
	t.Setenv("HOME", makeDir(t, t.TempDir(), "home"))
	return mustValidator(t)
}

func mustValidator(t *testing.T) *workspace.Validator {
	t.Helper()
	validator, err := workspace.NewValidator()
	if err != nil {
		t.Fatalf("NewValidator() error = %v", err)
	}
	return validator
}

func mustOpen(t *testing.T, dir string, validator *workspace.Validator) *Registry {
	t.Helper()
	registry, err := Open(dir, validator)
	if err != nil {
		t.Fatalf("Open(%q) error = %v", dir, err)
	}
	return registry
}

func mustAdd(t *testing.T, registry *Registry, path string) Record {
	t.Helper()
	record, err := registry.Add(path)
	if err != nil {
		t.Fatalf("Add(%q) error = %v", path, err)
	}
	return record
}

// openWithin calls Open but fails the test instead of hanging if Open blocks,
// for example on a FIFO planted at the registry path.
func openWithin(
	t *testing.T, dir string, validator *workspace.Validator, timeout time.Duration,
) (*Registry, error) {
	t.Helper()
	type result struct {
		registry *Registry
		err      error
	}
	done := make(chan result, 1)
	go func() {
		registry, err := Open(dir, validator)
		done <- result{registry: registry, err: err}
	}()
	select {
	case got := <-done:
		return got.registry, got.err
	case <-time.After(timeout):
		t.Fatalf("Open() did not return within %v", timeout)
		return nil, nil
	}
}

func makeRegistryDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatalf("Mkdir(registry dir) error = %v", err)
	}
	return dir
}

// requireDirEntries checks that dir contains exactly the named entries, which
// also proves that no temporary file was left behind.
func requireDirEntries(t *testing.T, dir string, want ...string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%q) error = %v", dir, err)
	}
	got := make([]string, 0, len(entries))
	for _, entry := range entries {
		got = append(got, entry.Name())
	}
	if !slices.Equal(got, want) {
		t.Fatalf("registry directory entries = %q, want %q", got, want)
	}
}

func requireOwnerOnlyRegularFile(t *testing.T, path string) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("Lstat(%q) error = %v", path, err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || !ok || int(stat.Uid) != os.Geteuid() {
		t.Fatalf("%s mode = %v, want an owner-only regular file owned by the current user",
			filepath.Base(path), info.Mode())
	}
}

func makeDir(t *testing.T, parent string, names ...string) string {
	t.Helper()
	path := filepath.Join(append([]string{parent}, names...)...)
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", path, err)
	}
	return path
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	return content
}

func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("Symlink(%q, %q) error = %v", target, link, err)
	}
}

func rename(t *testing.T, from, to string) {
	t.Helper()
	if err := os.Rename(from, to); err != nil {
		t.Fatalf("Rename(%q, %q) error = %v", from, to, err)
	}
}
