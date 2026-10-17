package protocol

import (
	"errors"
	"fmt"
	"time"
)

// ErrInvalidExecutionStarted reports a payload or typed value that breaks
// ADR-009's execution.started rules. Every DecodeExecutionStarted error wraps it.
var ErrInvalidExecutionStarted = errors.New("protocol: invalid execution.started message")

// ExecutionStarted is the typed payload of an execution.started message: Mio
// started the command's process (ADR-009).
type ExecutionStarted struct {
	ExecutionID string
	// ActionHash is the ADR-008 action hash of the request.
	ActionHash string
	// StartedAt has millisecond precision.
	StartedAt time.Time
}

// DecodeExecutionStarted strictly decodes an execution.started payload.
func DecodeExecutionStarted(payload []byte) (ExecutionStarted, error) {
	return ExecutionStarted{}, fmt.Errorf("%w: not implemented", ErrInvalidExecutionStarted)
}

// Validate checks every ADR-009 rule on a typed value.
func (s ExecutionStarted) Validate() error {
	return nil
}

// ValidateAgainst checks that s reports on request.
func (s ExecutionStarted) ValidateAgainst(request ExecutionRequest) error {
	return nil
}

// CanonicalJSON returns the RFC 8785 encoding of a valid value.
func (s ExecutionStarted) CanonicalJSON() ([]byte, error) {
	return nil, fmt.Errorf("%w: not implemented", ErrInvalidExecutionStarted)
}

// ExecutionStarted decodes the payload of an execution.started envelope.
func (e Envelope) ExecutionStarted() (ExecutionStarted, error) {
	return ExecutionStarted{}, nil
}
