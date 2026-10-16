// Package workspace validates directories that Scopra Mio may expose as
// workspaces.
package workspace

import "errors"

var (
	// ErrUnsupportedPlatform reports that workspace validation is unavailable.
	ErrUnsupportedPlatform = errors.New("workspace: validation is unsupported on this platform")
	// ErrHomeUnknown reports that no absolute home directory could be determined.
	ErrHomeUnknown = errors.New("workspace: cannot determine the current user's home directory")
	// ErrInvalidPath reports a path that is not absolute or not valid UTF-8.
	ErrInvalidPath = errors.New("workspace: path must be absolute and valid UTF-8")
	// ErrNotDirectory reports a workspace path that does not resolve to a directory.
	ErrNotDirectory = errors.New("workspace: path is not a directory")
	// ErrProtectedPath reports a directory that is, or contains, a protected location.
	ErrProtectedPath = errors.New("workspace: directory is or contains a protected location")
	// ErrNotOwned reports a workspace root not owned by the effective user.
	ErrNotOwned = errors.New("workspace: directory is not owned by the current user")
	// ErrChanged reports a workspace that no longer matches its recorded identity.
	ErrChanged = errors.New("workspace: workspace changed since it was recorded")
)

var errNotImplemented = errors.New("workspace: not implemented")

// Workspace is a validated workspace root.
type Workspace struct {
	Path        string
	Fingerprint Fingerprint
}

// Fingerprint identifies a workspace root directory on its filesystem.
type Fingerprint struct {
	Device uint64
	Inode  uint64
}

// Validator checks workspace roots against protected locations.
type Validator struct {
	protected []string
}

// NewValidator returns a Validator.
func NewValidator(protected ...string) (*Validator, error) {
	return newValidator(homeDirectories(), protected...)
}

func newValidator(_ []string, _ ...string) (*Validator, error) { return &Validator{}, nil }

func homeDirectories() []string { return nil }

// Inspect validates path as a workspace root.
func (v *Validator) Inspect(_ string) (Workspace, error) { return Workspace{}, errNotImplemented }

// Revalidate checks that recorded still names the same valid workspace.
func (v *Validator) Revalidate(_ Workspace) error { return errNotImplemented }
