package main

import (
	"context"
	"os/exec"

	"github.com/SynergyFlowLabs/scopra/internal/execution"
	"github.com/SynergyFlowLabs/scopra/internal/hollow"
	"github.com/SynergyFlowLabs/scopra/internal/localipc"
	"github.com/SynergyFlowLabs/scopra/packages/protocol"
)

// Execution refusals. They are fixed text, so they satisfy ADR-009's message
// rules; the client sees them in a rejected result.
const (
	imageNotConfiguredReason = "no sandbox image is configured; start Mio with --sandbox-image"
	podmanNotFoundReason     = "the podman command was not found"
	cleanupFailedReason      = "Mio could not remove leftover containers at startup"
	podmanNotFoundWarning    = "podman was not found; executions are refused until Mio is restarted"
	cleanupFailedWarning     = "leftover container cleanup failed; executions are refused until Mio restarts"
)

// newExecutor builds Mio's local executor (ADR-011, increment 2). It never
// fails: when containment cannot be offered, every run is refused with
// containment_unavailable and the reason, so a client gets a typed answer
// instead of a closed connection. The second return value is a startup
// warning to print after the listening line, or "".
//
// Mio resolves podman once, with exec.LookPath, and keeps the absolute path
// for every run. When an image is configured and podman exists, it removes
// containers that carry this Mio's action-hash label from an earlier run; a
// failure disables execution (fail closed) rather than leaving unknown
// containers behind.
func newExecutor(workspaces execution.WorkspaceRegistry, validator execution.WorkspaceValidator, image string) (localipc.Executor, string) {
	service := &execution.Service{Workspaces: workspaces, Validator: validator}
	if image == "" {
		service.Unavailable = imageNotConfiguredReason
		return localExecutor{service: service}, ""
	}
	// exec.LookPath fails closed for a relative result (ErrDot), so the path
	// kept here is always absolute, which Hollow requires.
	podman, err := exec.LookPath("podman")
	if err != nil {
		service.Unavailable = podmanNotFoundReason
		return localExecutor{service: service}, podmanNotFoundWarning
	}
	runner := hollow.Runner{Podman: podman, Image: image}
	if _, err := runner.RemoveLeftovers(); err != nil {
		service.Unavailable = cleanupFailedReason
		return localExecutor{service: service}, cleanupFailedWarning
	}
	service.Runner = runner
	return localExecutor{service: service}, ""
}

// localExecutor adapts the execution service to Mio's IPC executor contract.
type localExecutor struct {
	service *execution.Service
}

func (e localExecutor) Execute(ctx context.Context, request protocol.ExecutionRequest, events localipc.ExecutionEvents) (protocol.ExecutionResult, error) {
	return e.service.Execute(ctx, request, execution.Events{Started: events.Started, Output: events.Output})
}
