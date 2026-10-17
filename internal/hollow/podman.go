// Package hollow implements Scopra Hollow, the execution sandbox. It builds
// the rootless Podman invocation for one execution (ADR-010) and starts no
// process itself.
package hollow

import (
	"errors"

	"github.com/SynergyFlowLabs/scopra/packages/protocol"
)

var (
	// ErrUnmountableWorkspace reports a workspace path that cannot be bind
	// mounted safely. Mio reports it as workspace_unavailable (ADR-009).
	ErrUnmountableWorkspace = errors.New("hollow: the workspace path cannot be mounted")
	// ErrInvalidImage reports an image reference that is not pinned by digest.
	ErrInvalidImage = errors.New("hollow: the image must be a reference pinned by digest")
)

// RunSpec is everything one execution needs.
type RunSpec struct {
	// Request is the validated execution request.
	Request protocol.ExecutionRequest
	// WorkspacePath is the workspace's canonical path, revalidated (ADR-004)
	// by the caller immediately before the run.
	WorkspacePath string
	// Image is the configured container image, pinned by digest.
	Image string
}

// PodmanRunArgs returns the arguments that follow "podman" for spec.
func PodmanRunArgs(spec RunSpec) ([]string, error) {
	return nil, ErrInvalidImage
}
