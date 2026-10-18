// Package execution runs Mio's local executions in Scopra Hollow (ADR-011).
// It sits between the local IPC protocol, which validates the request, and
// Hollow, which contains the command: it resolves and revalidates the
// workspace, and passes only the recorded canonical path to the sandbox.
package execution

import (
	"context"
	"errors"
	"fmt"

	"github.com/SynergyFlowLabs/scopra/internal/hollow"
	"github.com/SynergyFlowLabs/scopra/internal/workspace"
	"github.com/SynergyFlowLabs/scopra/internal/workspace/registry"
	"github.com/SynergyFlowLabs/scopra/packages/protocol"
)

// ErrInvalidService reports a Service that cannot execute anything, such as
// one without a workspace registry or validator, or a nil context.
var ErrInvalidService = errors.New("execution: invalid service")

// Rejection messages. They are fixed text, so they satisfy ADR-009's rules and
// never carry a path or other client-chosen data.
const (
	messageWorkspaceNotRegistered = "the workspace is not registered"
	messageWorkspaceChanged       = "the workspace failed revalidation"
	messageNoContainment          = "containment is unavailable"
)

// WorkspaceRegistry is Mio's workspace registry as this service needs it.
// *registry.Registry implements it.
type WorkspaceRegistry interface {
	Get(id string) (registry.Record, error)
}

// WorkspaceValidator revalidates a recorded workspace immediately before use
// (ADR-004). *workspace.Validator implements it.
type WorkspaceValidator interface {
	Revalidate(recorded workspace.Workspace) error
}

// Runner runs one request in the sandbox. hollow.Runner implements it.
type Runner interface {
	Run(ctx context.Context, request protocol.ExecutionRequest, workspacePath string, events hollow.Events) (protocol.ExecutionResult, error)
}

// Events receives a run's progress messages, in order: at most one start
// notice, then output chunks.
type Events struct {
	Started func(protocol.ExecutionStarted)
	Output  func(protocol.ExecutionOutput)
}

// Service executes local runs in Hollow (ADR-011). It is safe for concurrent
// use when its dependencies are. Every outcome is a result, except when the
// runner itself fails, which is a Mio fault and returns an error.
type Service struct {
	// Workspaces looks up the registration named by the request.
	Workspaces WorkspaceRegistry
	// Validator rechecks the registration immediately before the run.
	Validator WorkspaceValidator
	// Runner runs the request in the sandbox. A nil Runner refuses every run.
	Runner Runner
	// Unavailable, when non-empty, is why this Mio cannot offer containment.
	// It becomes the rejection message of every run, and it wins over Runner,
	// so a configuration mistake can never run a command.
	Unavailable string
}

// Execute runs one local request: it looks the workspace up, revalidates it,
// and hands the record's canonical path to Hollow. A workspace that is not
// registered or no longer matches its record is rejected with
// workspace_unavailable; missing containment is rejected with
// containment_unavailable. The client never supplies a path.
func (s *Service) Execute(ctx context.Context, request protocol.ExecutionRequest, events Events) (protocol.ExecutionResult, error) {
	if s == nil || ctx == nil || s.Workspaces == nil || s.Validator == nil {
		return protocol.ExecutionResult{}, ErrInvalidService
	}
	record, err := s.Workspaces.Get(request.Resources.WorkspaceID)
	if err != nil {
		return reject(request, protocol.ErrorCodeWorkspaceUnavailable, messageWorkspaceNotRegistered)
	}
	if err := s.Validator.Revalidate(record.Workspace); err != nil {
		return reject(request, protocol.ErrorCodeWorkspaceUnavailable, messageWorkspaceChanged)
	}
	if s.Runner == nil || s.Unavailable != "" {
		reason := s.Unavailable
		if reason == "" {
			reason = messageNoContainment
		}
		return reject(request, protocol.ErrorCodeContainmentUnavailable, reason)
	}
	return s.Runner.Run(ctx, request, record.Path, hollow.Events{Started: events.Started, Output: events.Output})
}

// reject returns a rejected result for request. The action hash is recomputed
// from the request, so the result always reports on the request that was
// decoded, not on a transmitted claim.
func reject(request protocol.ExecutionRequest, code, message string) (protocol.ExecutionResult, error) {
	hash, err := request.ActionHash()
	if err != nil {
		return protocol.ExecutionResult{}, fmt.Errorf("execution: compute the action hash: %w", err)
	}
	return protocol.ExecutionResult{
		ExecutionID: request.ExecutionID,
		ActionHash:  hash,
		State:       protocol.StateRejected,
		Error:       protocol.ResultError{Code: code, Message: message},
	}, nil
}
