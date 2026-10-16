//go:build linux

package workspace

import (
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"syscall"
	"testing"
)

func TestInspectReturnsCanonicalPathAndFingerprint(t *testing.T) {
	validator := newTestValidator(t)
	project := makeDir(t, t.TempDir(), "project")

	got, err := validator.Inspect(project)
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	want := Workspace{Path: project, Fingerprint: fingerprintOf(t, project)}
	if got != want {
		t.Fatalf("Inspect() = %+v, want %+v", got, want)
	}
}

func TestInspectResolvesSymlinksBeforeDotDot(t *testing.T) {
	validator := newTestValidator(t)
	base := t.TempDir()
	makeDir(t, base, "x")
	target := makeDir(t, base, "y", "z")
	link := filepath.Join(base, "x", "link")
	symlink(t, target, link)

	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "symlink to directory", path: link, want: target},
		// Lexical cleaning would give base/x; the kernel resolves ".." in base/y/z.
		{name: "dot-dot after symlink", path: link + "/..", want: filepath.Join(base, "y")},
		{name: "redundant separators", path: base + "//y/./z/", want: target},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := validator.Inspect(test.path)
			if err != nil {
				t.Fatalf("Inspect(%q) error = %v", test.path, err)
			}
			if got.Path != test.want || got.Fingerprint != fingerprintOf(t, test.want) {
				t.Fatalf("Inspect(%q) = %+v, want path %q and its fingerprint",
					test.path, got, test.want)
			}
		})
	}
}

func TestInspectRejectsInvalidPaths(t *testing.T) {
	validator := newTestValidator(t)
	base := t.TempDir()
	invalidName := makeDir(t, base, "bad\xffname")
	linkToInvalid := filepath.Join(base, "valid-link")
	symlink(t, invalidName, linkToInvalid)

	invalid := []string{"", "relative/project", "./project", base + "/\xff", linkToInvalid}
	for _, path := range invalid {
		if _, err := validator.Inspect(path); !errors.Is(err, ErrInvalidPath) {
			t.Errorf("Inspect(%q) error = %v, want ErrInvalidPath", path, err)
		}
	}
}

func TestInspectRejectsMissingPaths(t *testing.T) {
	validator := newTestValidator(t)
	base := t.TempDir()
	dangling := filepath.Join(base, "dangling")
	symlink(t, filepath.Join(base, "missing-target"), dangling)

	for _, path := range []string{filepath.Join(base, "missing"), dangling} {
		if _, err := validator.Inspect(path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("Inspect(%q) error = %v, want os.ErrNotExist", path, err)
		}
	}
}

func TestInspectRejectsNonDirectories(t *testing.T) {
	validator := newTestValidator(t)
	base := t.TempDir()
	file := filepath.Join(base, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	linkToFile := filepath.Join(base, "file-link")
	symlink(t, file, linkToFile)

	for _, path := range []string{file, linkToFile} {
		if _, err := validator.Inspect(path); !errors.Is(err, ErrNotDirectory) {
			t.Errorf("Inspect(%q) error = %v, want ErrNotDirectory", path, err)
		}
	}
}

func TestSecurity_InspectRejectsFilesystemRoot(t *testing.T) {
	validator := newTestValidator(t)
	rootLink := filepath.Join(t.TempDir(), "root-link")
	symlink(t, "/", rootLink)

	for _, path := range []string{"/", "/tmp/..", rootLink} {
		if _, err := validator.Inspect(path); !errors.Is(err, ErrProtectedPath) {
			t.Errorf("Inspect(%q) error = %v, want ErrProtectedPath", path, err)
		}
	}
}

func TestSecurity_InspectRejectsHomeDirectoryAndItsAncestors(t *testing.T) {
	base := t.TempDir()
	home := makeDir(t, base, "users", "alice")
	t.Setenv("HOME", home)
	validator := mustValidator(t)
	homeLink := filepath.Join(t.TempDir(), "home-link")
	symlink(t, home, homeLink)

	for _, path := range []string{home, filepath.Dir(home), base, homeLink, home + "/."} {
		if _, err := validator.Inspect(path); !errors.Is(err, ErrProtectedPath) {
			t.Errorf("Inspect(%q) error = %v, want ErrProtectedPath", path, err)
		}
	}
	project := makeDir(t, home, "src", "project")
	if _, err := validator.Inspect(project); err != nil {
		t.Fatalf("Inspect(project inside home) error = %v, want nil", err)
	}
}

func TestSecurity_InspectProtectsAccountHomeWhenHOMEIsOverridden(t *testing.T) {
	account, err := user.Current()
	if err != nil || !filepath.IsAbs(account.HomeDir) {
		t.Skipf("no account home directory available (error = %v)", err)
	}
	if _, err := os.Stat(account.HomeDir); err != nil {
		t.Skipf("account home directory is unavailable: %v", err)
	}
	t.Setenv("HOME", makeDir(t, t.TempDir(), "elsewhere"))
	validator := mustValidator(t)

	if _, err := validator.Inspect(account.HomeDir); !errors.Is(err, ErrProtectedPath) {
		t.Fatalf("Inspect(account home %q) error = %v, want ErrProtectedPath", account.HomeDir, err)
	}
}

func TestSecurity_InspectRejectsDirectoriesContainingProtectedPaths(t *testing.T) {
	base := t.TempDir()
	cache := makeDir(t, base, "cache")
	notYetCreated := filepath.Join(cache, "scopra", "run")
	realState := makeDir(t, base, "real", "state")
	stateLink := filepath.Join(base, "link")
	symlink(t, filepath.Join(base, "real"), stateLink)
	validator := mustValidator(t, notYetCreated, filepath.Join(stateLink, "state"))

	for _, path := range []string{cache, base, realState, filepath.Join(base, "real")} {
		if _, err := validator.Inspect(path); !errors.Is(err, ErrProtectedPath) {
			t.Errorf("Inspect(%q) error = %v, want ErrProtectedPath", path, err)
		}
	}
	sibling := makeDir(t, cache, "other")
	if _, err := validator.Inspect(sibling); err != nil {
		t.Fatalf("Inspect(sibling of protected path) error = %v, want nil", err)
	}
	// A shared name prefix is not containment: base/cach does not contain base/cache.
	sharedPrefix := makeDir(t, base, "cach")
	if _, err := validator.Inspect(sharedPrefix); err != nil {
		t.Fatalf("Inspect(%q) error = %v, want nil", sharedPrefix, err)
	}
}

func TestSecurity_InspectRejectsDirectoryOwnedByAnotherUser(t *testing.T) {
	info, err := os.Stat("/usr")
	if err != nil {
		t.Skipf("/usr is unavailable: %v", err)
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); !ok || int(stat.Uid) == os.Geteuid() {
		t.Skip("/usr is owned by the current user")
	}
	validator := newTestValidator(t)

	if _, err := validator.Inspect("/usr"); !errors.Is(err, ErrNotOwned) {
		t.Fatalf("Inspect(/usr) error = %v, want ErrNotOwned", err)
	}
}

func TestRevalidateAcceptsUnchangedWorkspace(t *testing.T) {
	validator := newTestValidator(t)
	recorded := mustInspect(t, validator, makeDir(t, t.TempDir(), "project"))

	if err := validator.Revalidate(recorded); err != nil {
		t.Fatalf("Revalidate(unchanged) error = %v", err)
	}
}

func TestSecurity_RevalidateDetectsChangedWorkspace(t *testing.T) {
	tests := []struct {
		name   string
		change func(t *testing.T, workspace string)
		want   error
	}{
		{
			name: "directory replaced",
			change: func(t *testing.T, workspace string) {
				rename(t, workspace, workspace+".old")
				makeDir(t, workspace)
			},
			want: ErrChanged,
		},
		{
			name: "root replaced by symlink to another directory",
			change: func(t *testing.T, workspace string) {
				other := makeDir(t, t.TempDir(), "other")
				rename(t, workspace, workspace+".old")
				symlink(t, other, workspace)
			},
			want: ErrChanged,
		},
		{
			name: "ancestor replaced by symlink to the same directory",
			change: func(t *testing.T, workspace string) {
				parent := filepath.Dir(workspace)
				rename(t, parent, parent+".moved")
				symlink(t, parent+".moved", parent)
			},
			want: ErrChanged,
		},
		{
			name: "directory deleted",
			change: func(t *testing.T, workspace string) {
				if err := os.Remove(workspace); err != nil {
					t.Fatalf("Remove() error = %v", err)
				}
			},
			want: os.ErrNotExist,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			validator := newTestValidator(t)
			workspace := makeDir(t, t.TempDir(), "parent", "project")
			recorded := mustInspect(t, validator, workspace)
			test.change(t, workspace)

			if err := validator.Revalidate(recorded); !errors.Is(err, test.want) {
				t.Fatalf("Revalidate() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestSecurity_RevalidateAppliesCurrentRules(t *testing.T) {
	workspace := makeDir(t, t.TempDir(), "project")
	recorded := mustInspect(t, newTestValidator(t), workspace)
	stricter := mustValidator(t, filepath.Join(workspace, "state"))

	if err := stricter.Revalidate(recorded); !errors.Is(err, ErrProtectedPath) {
		t.Fatalf("Revalidate() error = %v, want ErrProtectedPath", err)
	}
}

func TestNewValidatorRejectsRelativeProtectedPath(t *testing.T) {
	if _, err := NewValidator("relative/state"); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("NewValidator(relative) error = %v, want ErrInvalidPath", err)
	}
}

func TestNewValidatorFailsClosedWithoutHomeDirectory(t *testing.T) {
	if _, err := newValidator(nil); !errors.Is(err, ErrHomeUnknown) {
		t.Fatalf("newValidator(no homes) error = %v, want ErrHomeUnknown", err)
	}
}

func TestHomeDirectoriesIgnoresRelativeHOME(t *testing.T) {
	t.Setenv("HOME", "relative/home")

	for _, home := range homeDirectories() {
		if !filepath.IsAbs(home) {
			t.Fatalf("homeDirectories() = %q, includes relative %q", homeDirectories(), home)
		}
	}
}

// newTestValidator returns a validator whose $HOME is a fresh temporary
// directory, so tests do not depend on the real home directory's location.
func newTestValidator(t *testing.T) *Validator {
	t.Helper()
	t.Setenv("HOME", makeDir(t, t.TempDir(), "home"))
	return mustValidator(t)
}

func mustValidator(t *testing.T, protected ...string) *Validator {
	t.Helper()
	validator, err := NewValidator(protected...)
	if err != nil {
		t.Fatalf("NewValidator(%q) error = %v", protected, err)
	}
	return validator
}

func mustInspect(t *testing.T, validator *Validator, path string) Workspace {
	t.Helper()
	workspace, err := validator.Inspect(path)
	if err != nil {
		t.Fatalf("Inspect(%q) error = %v", path, err)
	}
	return workspace
}

func fingerprintOf(t *testing.T, path string) Fingerprint {
	t.Helper()
	var stat syscall.Stat_t
	if err := syscall.Lstat(path, &stat); err != nil {
		t.Fatalf("Lstat(%q) error = %v", path, err)
	}
	return Fingerprint{Device: uint64(stat.Dev), Inode: stat.Ino}
}

func makeDir(t *testing.T, parent string, names ...string) string {
	t.Helper()
	path := filepath.Join(append([]string{parent}, names...)...)
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", path, err)
	}
	return path
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
