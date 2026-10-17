package hollow

import (
	"context"
	"errors"
	"time"

	"github.com/SynergyFlowLabs/scopra/packages/protocol"
)

// ErrInvalidRunner reports a Runner that cannot run anything, such as one
// without an absolute path to podman.
var ErrInvalidRunner = errors.New("hollow: invalid runner configuration")

// Runner runs executions in rootless Podman containers (ADR-010).
type Runner struct {
	// Podman is the absolute path of the podman binary, resolved once at
	// startup and never searched for again.
	Podman string
	// Image is the configured container image, pinned by digest.
	Image string
	// pollInterval is how often the runner asks podman inspect whether the
	// container has started. Zero means the default.
	pollInterval time.Duration
}

// Events receives the progress messages of one execution, in order: at most
// one start notice, then output chunks. A nil callback ignores its messages.
type Events struct {
	Started func(protocol.ExecutionStarted)
	Output  func(protocol.ExecutionOutput)
}

// Run runs request with workspacePath mounted and returns its terminal
// result.
func (r Runner) Run(ctx context.Context, request protocol.ExecutionRequest, workspacePath string, events Events) (protocol.ExecutionResult, error) {
	return protocol.ExecutionResult{}, ErrInvalidRunner
}
