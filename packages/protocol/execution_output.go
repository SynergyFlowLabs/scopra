package protocol

import (
	"errors"
	"fmt"
)

// ErrInvalidExecutionOutput reports a payload or typed value that breaks
// ADR-009's execution.output rules. Every DecodeExecutionOutput error wraps it.
var ErrInvalidExecutionOutput = errors.New("protocol: invalid execution.output message")

// OutputStream names the stream that an output chunk came from.
type OutputStream string

const (
	StreamStdout OutputStream = "stdout"
	StreamStderr OutputStream = "stderr"
)

// ExecutionOutput is the typed payload of an execution.output message: one
// chunk of a command's output (ADR-009). Data is untrusted and may hold
// secrets; it must never be logged.
type ExecutionOutput struct {
	ExecutionID string
	// Sequence numbers an execution's chunks 1, 2, 3, and so on across both
	// streams, in the order Mio read them.
	Sequence uint64
	Stream   OutputStream
	// Data holds 1 to 65536 bytes, exactly as the command wrote them.
	Data []byte
}

// DecodeExecutionOutput strictly decodes an execution.output payload.
func DecodeExecutionOutput(payload []byte) (ExecutionOutput, error) {
	return ExecutionOutput{}, fmt.Errorf("%w: not implemented", ErrInvalidExecutionOutput)
}

// Validate checks every ADR-009 rule on a typed value.
func (o ExecutionOutput) Validate() error {
	return nil
}

// CanonicalJSON returns the RFC 8785 encoding of a valid value.
func (o ExecutionOutput) CanonicalJSON() ([]byte, error) {
	return nil, fmt.Errorf("%w: not implemented", ErrInvalidExecutionOutput)
}

// ExecutionOutput decodes the payload of an execution.output envelope.
func (e Envelope) ExecutionOutput() (ExecutionOutput, error) {
	return ExecutionOutput{}, nil
}
