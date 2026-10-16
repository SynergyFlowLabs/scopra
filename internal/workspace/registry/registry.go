// Package registry stores Scopra Mio's workspace registrations.
package registry

import (
	"errors"

	"github.com/SynergyFlowLabs/scopra/internal/workspace"
)

// maxWorkspaces bounds the number of registrations.
const maxWorkspaces = 256

var (
	// ErrUnsupportedPlatform reports that the registry is unavailable on this platform.
	ErrUnsupportedPlatform = errors.New("registry: workspace registry is unsupported on this platform")
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

var errNotImplemented = errors.New("registry: not implemented")

// Record is a registered workspace.
type Record struct {
	ID string
	workspace.Workspace
}

// Registry holds the workspace registrations stored in one directory.
type Registry struct {
	maxWorkspaces int
}

// Open loads the registry stored in dir.
func Open(_ string, _ *workspace.Validator) (*Registry, error) { return nil, errNotImplemented }

// Add registers the workspace at path.
func (r *Registry) Add(_ string) (Record, error) { return Record{}, errNotImplemented }

// Remove deletes the registration with the given ID.
func (r *Registry) Remove(_ string) error { return errNotImplemented }

// Get returns the registration with the given ID.
func (r *Registry) Get(_ string) (Record, error) { return Record{}, errNotImplemented }

// List returns every registration.
func (r *Registry) List() []Record { return nil }
