// Package workspace validates directories that Scopra Mio may expose as
// workspaces. It canonicalizes paths, rejects locations that must never be
// exposed, and fingerprints workspace roots so callers can revalidate them
// before use. It reads only filesystem metadata: it never reads repository
// contents, runs commands, or changes the filesystem.
package workspace

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"
)

var (
	// ErrUnsupportedPlatform reports that workspace validation is unavailable.
	ErrUnsupportedPlatform = errors.New("workspace: validation is unsupported on this platform")
	// ErrHomeUnknown reports that no absolute home directory could be determined.
	ErrHomeUnknown = errors.New("workspace: cannot determine the current user's home directory")
	// ErrInvalidPath reports a path that is not absolute or not valid UTF-8.
	ErrInvalidPath = errors.New("workspace: path must be absolute and valid UTF-8")
	// ErrUnresolvable reports a workspace path that cannot be resolved.
	// Skeleton for the Step 9 tests: not returned yet.
	ErrUnresolvable = errors.New("workspace: path cannot be resolved")
	// ErrNotDirectory reports a workspace path that does not resolve to a directory.
	ErrNotDirectory = errors.New("workspace: path is not a directory")
	// ErrProtectedPath reports a directory that is, or contains, a protected location.
	ErrProtectedPath = errors.New("workspace: directory is or contains a protected location")
	// ErrNotOwned reports a workspace root not owned by the effective user.
	ErrNotOwned = errors.New("workspace: directory is not owned by the current user")
	// ErrChanged reports a workspace that no longer matches its recorded identity.
	ErrChanged = errors.New("workspace: workspace changed since it was recorded")
)

// Workspace is a validated workspace root.
type Workspace struct {
	// Path is canonical: absolute, cleaned, and free of symlinks.
	Path string
	// Fingerprint identifies the root directory as it was when validated.
	Fingerprint Fingerprint
}

// Fingerprint identifies a workspace root directory on its filesystem.
// Version 1 is the directory's device and inode numbers. It changes when the
// directory is replaced or moved to another filesystem, but not when the
// repository's contents change. A directory that is deleted and recreated may
// reuse an inode number, and some filesystems renumber devices on remount.
type Fingerprint struct {
	Device uint64
	Inode  uint64
}

// Validator checks workspace roots against protected locations: the
// filesystem root, the current user's home directories, and any additional
// directories given to NewValidator.
type Validator struct {
	// protected holds absolute paths with symlinks resolved in the prefix that
	// existed when the Validator was created.
	protected []string
}

// NewValidator returns a Validator that rejects the filesystem root, the
// current user's home directories (from both $HOME and the account database),
// each additional protected directory, and every directory containing one of
// them. Protected directories need not exist yet. NewValidator fails closed
// when no absolute home directory is known.
func NewValidator(protected ...string) (*Validator, error) {
	if !platformSupported() {
		return nil, ErrUnsupportedPlatform
	}
	return newValidator(homeDirectories(), protected...)
}

func newValidator(homes []string, protected ...string) (*Validator, error) {
	if len(homes) == 0 {
		return nil, ErrHomeUnknown
	}
	for _, path := range protected {
		if !filepath.IsAbs(path) {
			return nil, fmt.Errorf("%w: protected path %q", ErrInvalidPath, path)
		}
	}

	validator := &Validator{}
	for _, path := range append(slices.Clone(homes), protected...) {
		resolved, err := resolveExisting(path)
		if err != nil {
			return nil, fmt.Errorf("resolve protected path %q: %w", path, err)
		}
		validator.protected = append(validator.protected, resolved)
	}
	return validator, nil
}

// homeDirectories returns the absolute home directories named by $HOME and by
// the account database, so that overriding $HOME cannot unprotect the other.
func homeDirectories() []string {
	var homes []string
	if home := os.Getenv("HOME"); filepath.IsAbs(home) {
		homes = append(homes, home)
	}
	if account, err := user.Current(); err == nil && filepath.IsAbs(account.HomeDir) {
		homes = append(homes, account.HomeDir)
	}
	return homes
}

// Inspect validates path as a workspace root and returns its canonical path
// and fingerprint. The path must be absolute: a daemon must never interpret
// paths relative to its own working directory.
func (v *Validator) Inspect(path string) (Workspace, error) {
	if !filepath.IsAbs(path) || !utf8.ValidString(path) {
		return Workspace{}, fmt.Errorf("%w: %q", ErrInvalidPath, path)
	}
	// EvalSymlinks resolves ".." after the symlinks before it, as the kernel
	// does; cleaning the path first would resolve ".." lexically instead.
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return Workspace{}, fmt.Errorf("resolve workspace path: %w", err)
	}
	if !utf8.ValidString(canonical) {
		return Workspace{}, fmt.Errorf("%w: %q resolves to %q", ErrInvalidPath, path, canonical)
	}
	if err := v.checkProtected(canonical); err != nil {
		return Workspace{}, err
	}
	fingerprint, err := inspectRoot(canonical)
	if err != nil {
		return Workspace{}, err
	}
	return Workspace{Path: canonical, Fingerprint: fingerprint}, nil
}

// Revalidate re-applies every rule to recorded.Path. It reports ErrChanged if
// the path no longer resolves to itself, for example because an ancestor is
// now a symlink, or if it names a different directory than when recorded.
func (v *Validator) Revalidate(recorded Workspace) error {
	current, err := v.Inspect(recorded.Path)
	if err != nil {
		return err
	}
	if current.Path != recorded.Path {
		return fmt.Errorf("%w: %q now resolves to %q", ErrChanged, recorded.Path, current.Path)
	}
	if current.Fingerprint != recorded.Fingerprint {
		return fmt.Errorf("%w: the directory at %q was replaced", ErrChanged, recorded.Path)
	}
	return nil
}

func (v *Validator) checkProtected(canonical string) error {
	if canonical == string(filepath.Separator) {
		return fmt.Errorf("%w: the filesystem root", ErrProtectedPath)
	}
	for _, protected := range v.protected {
		if contains(canonical, protected) {
			return fmt.Errorf("%w: %q", ErrProtectedPath, protected)
		}
	}
	return nil
}

// contains reports whether dir is path or one of its ancestors. Both must be
// clean absolute paths.
func contains(dir, path string) bool {
	return path == dir || strings.HasPrefix(path, dir+string(filepath.Separator))
}

// resolveExisting resolves symlinks in the longest existing prefix of path, an
// absolute path, and appends the components that do not exist yet.
func resolveExisting(path string) (string, error) {
	existing := filepath.Clean(path)
	var missing []string
	for {
		resolved, err := filepath.EvalSymlinks(existing)
		if err == nil {
			return filepath.Join(append([]string{resolved}, missing...)...), nil
		}
		parent := filepath.Dir(existing)
		if !errors.Is(err, fs.ErrNotExist) || parent == existing {
			return "", err
		}
		missing = append([]string{filepath.Base(existing)}, missing...)
		existing = parent
	}
}
