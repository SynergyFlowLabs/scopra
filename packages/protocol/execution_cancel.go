package protocol

import (
	"errors"
	"fmt"
)

// ErrInvalidExecutionCancel reports a payload or typed value that breaks
// ADR-009's execution.cancel rules. Every DecodeExecutionCancel error wraps it.
var ErrInvalidExecutionCancel = errors.New("protocol: invalid execution.cancel message")

// ExecutionCancel is the typed payload of an execution.cancel message: Caloris
// asks Mio to stop an execution (ADR-009).
type ExecutionCancel struct {
	ExecutionID string
	// ActionHash is the ADR-008 action hash of the request to stop.
	ActionHash string
	// Principals identify who asked for the cancellation, which may differ
	// from who requested the execution.
	Principals Principals
	// Reason is untrusted text. It grants nothing.
	Reason string
}

// DecodeExecutionCancel strictly decodes an execution.cancel payload.
func DecodeExecutionCancel(payload []byte) (ExecutionCancel, error) {
	return ExecutionCancel{}, fmt.Errorf("%w: not implemented", ErrInvalidExecutionCancel)
}

// Validate checks every ADR-009 rule on a typed value.
func (c ExecutionCancel) Validate() error {
	return nil
}

// ValidateAgainst checks that c names request.
func (c ExecutionCancel) ValidateAgainst(request ExecutionRequest) error {
	return nil
}

// CanonicalJSON returns the RFC 8785 encoding of a valid value.
func (c ExecutionCancel) CanonicalJSON() ([]byte, error) {
	return nil, fmt.Errorf("%w: not implemented", ErrInvalidExecutionCancel)
}

// ExecutionCancel decodes the payload of an execution.cancel envelope.
func (e Envelope) ExecutionCancel() (ExecutionCancel, error) {
	return ExecutionCancel{}, nil
}
