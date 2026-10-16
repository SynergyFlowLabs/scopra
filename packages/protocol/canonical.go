package protocol

import "fmt"

// CanonicalJSON returns the canonical form of r (ADR-008).
func (r ExecutionRequest) CanonicalJSON() ([]byte, error) {
	return nil, fmt.Errorf("%w: not implemented", ErrInvalidExecutionRequest)
}

// ActionHash returns the action hash of r (ADR-008).
func (r ExecutionRequest) ActionHash() (string, error) {
	return "", fmt.Errorf("%w: not implemented", ErrInvalidExecutionRequest)
}
