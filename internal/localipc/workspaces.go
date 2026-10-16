package localipc

import (
	"context"
	"errors"

	"github.com/SynergyFlowLabs/scopra/internal/workspace/registry"
)

// Skeleton for the Step 9 tests: declarations only, nothing is served yet.

const (
	addWorkspaceCommand    = "workspace.add"
	listWorkspacesCommand  = "workspace.list"
	removeWorkspaceCommand = "workspace.remove"
	maxErrorMessageBytes   = 1024
	listPageBudget         = MaxFrameBytes - 512
)

// Error codes that Mio reports for failed workspace requests.
const (
	CodeInvalidPath       = "invalid_path"
	CodeUnresolvablePath  = "unresolvable_path"
	CodeNotDirectory      = "not_directory"
	CodeProtectedPath     = "protected_path"
	CodeNotOwned          = "not_owned"
	CodeAlreadyRegistered = "already_registered"
	CodeOverlap           = "overlap"
	CodeRegistryFull      = "registry_full"
	CodeNotFound          = "not_found"
	CodeInternal          = "internal"
)

// Workspace is a registered workspace as reported by Mio.
type Workspace struct {
	ID   string `json:"id"`
	Path string `json:"path"`
}

// WorkspaceError is Mio's reply to a well-formed workspace request that failed.
type WorkspaceError struct {
	Code    string
	Message string
}

func (e *WorkspaceError) Error() string { return "localipc: Mio reported " + e.Code + ": " + e.Message }

// WorkspaceRegistry is the registry that Mio serves workspace commands from.
type WorkspaceRegistry interface {
	Add(path string) (registry.Record, error)
	Get(id string) (registry.Record, error)
	Remove(id string) error
	List() []registry.Record
}

var errNotImplemented = errors.New("localipc: workspace commands are not implemented yet")

// EnableWorkspaces is not implemented yet.
func (d *Daemon) EnableWorkspaces(workspaces WorkspaceRegistry) error { return errNotImplemented }

// AddWorkspace is not implemented yet.
func AddWorkspace(ctx context.Context, stateDir, path string) (Workspace, error) {
	return Workspace{}, errNotImplemented
}

// ListWorkspaces is not implemented yet.
func ListWorkspaces(ctx context.Context, stateDir string) ([]Workspace, error) {
	return nil, errNotImplemented
}

// RemoveWorkspace is not implemented yet.
func RemoveWorkspace(ctx context.Context, stateDir, id string) (Workspace, error) {
	return Workspace{}, errNotImplemented
}

func listWorkspacesPage(ctx context.Context, stateDir, cursor string) ([]Workspace, string, error) {
	return nil, "", errNotImplemented
}

func workspacePage(records []Workspace, cursor string, budget int) ([]Workspace, string) {
	return nil, ""
}

func encodeListReply(requestID string, page []Workspace, next string) ([]byte, error) {
	return nil, errNotImplemented
}

func encodeErrorReply(requestID string, err error) ([]byte, error) { return nil, errNotImplemented }

func workspaceErrorCode(err error) string { return "" }

func decodeWorkspaceReply(encoded []byte, requestID string) (Workspace, error) {
	return Workspace{}, errNotImplemented
}

func decodeListReply(encoded []byte, requestID, cursor string) ([]Workspace, string, error) {
	return nil, "", errNotImplemented
}

func validWorkspaceID(id string) bool { return false }
