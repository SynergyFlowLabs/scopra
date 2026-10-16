package protocol

import (
	"errors"
	"fmt"
	"time"
)

// ErrInvalidExecutionResult reports a payload or typed result that breaks
// ADR-009. Every DecodeExecutionResult error wraps it.
var ErrInvalidExecutionResult = errors.New("protocol: invalid execution result")

// ExecutionState is the terminal state that an execution.result reports
// (ADR-009).
type ExecutionState string

const (
	// StateSucceeded: the command exited with code 0.
	StateSucceeded ExecutionState = "succeeded"
	// StateFailed: the command exited with a code from 1 to 255.
	StateFailed ExecutionState = "failed"
	// StateTimedOut: Mio killed the command at limits.timeoutSeconds.
	StateTimedOut ExecutionState = "timed_out"
	// StateCancelled: Mio killed the command after execution.cancel.
	StateCancelled ExecutionState = "cancelled"
	// StateOutOfMemory: the sandbox killed the command at limits.memoryBytes.
	StateOutOfMemory ExecutionState = "out_of_memory"
	// StateErrored: the command could not start, or the sandbox or Mio failed.
	StateErrored ExecutionState = "errored"
	// StateRejected: Mio refused the request; the command never started.
	StateRejected ExecutionState = "rejected"
)

// Error codes that Mio sends (ADR-009). Receivers must also accept unknown
// well-formed codes, which they treat as generic errors.
const (
	ErrorCodeExpired                = "expired"                 // rejected
	ErrorCodeUnauthorized           = "unauthorized"            // rejected
	ErrorCodePolicyDenied           = "policy_denied"           // rejected
	ErrorCodeWorkspaceUnavailable   = "workspace_unavailable"   // rejected
	ErrorCodeContainmentUnavailable = "containment_unavailable" // rejected
	ErrorCodeBusy                   = "busy"                    // rejected
	ErrorCodeStartFailed            = "start_failed"            // errored
	ErrorCodeContainmentFailed      = "containment_failed"      // errored
	ErrorCodeInternal               = "internal"                // errored
)

// ExecutionResult is the typed payload of an execution.result (ADR-009). The
// state selects which fields apply; the others must be zero.
type ExecutionResult struct {
	ExecutionID string
	// ActionHash is the ADR-008 action hash of the request.
	ActionHash string
	State      ExecutionState
	// StartedAt and EndedAt apply to succeeded, failed, timed_out, cancelled,
	// and out_of_memory, with millisecond precision.
	StartedAt time.Time
	EndedAt   time.Time
	// ExitCode applies to succeeded (0) and failed (1 to 255).
	ExitCode int
	// Output applies to every state except rejected.
	Output OutputSummary
	// Error applies to errored and rejected.
	Error ResultError
}

// OutputSummary counts the output that Mio sent in execution.output chunks.
// It never holds output itself.
type OutputSummary struct {
	StdoutBytes uint64
	StderrBytes uint64
	// Truncated reports that output beyond limits.outputBytes was discarded.
	Truncated bool
	// Chunks is the number of chunks sent, which is the last sequence number.
	Chunks uint64
}

// ResultError explains an errored or rejected execution. Message is
// untrusted text.
type ResultError struct {
	Code    string
	Message string
}

// DecodeExecutionResult strictly decodes an execution.result payload.
func DecodeExecutionResult(payload []byte) (ExecutionResult, error) {
	return ExecutionResult{}, fmt.Errorf("%w: not implemented", ErrInvalidExecutionResult)
}

// Validate checks every ADR-009 rule on a typed result.
func (r ExecutionResult) Validate() error {
	return nil
}

// ValidateAgainst checks that r reports on request.
func (r ExecutionResult) ValidateAgainst(request ExecutionRequest) error {
	return nil
}

// CanonicalJSON returns the RFC 8785 encoding of a valid result.
func (r ExecutionResult) CanonicalJSON() ([]byte, error) {
	return nil, fmt.Errorf("%w: not implemented", ErrInvalidExecutionResult)
}
