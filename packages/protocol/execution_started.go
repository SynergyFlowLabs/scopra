package protocol

import (
	"errors"
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

// DecodeExecutionStarted strictly decodes an execution.started payload
// (ADR-009) with ADR-007's strict-document rules and exact keys. Every error
// wraps ErrInvalidExecutionStarted, and no partially valid value is returned.
func DecodeExecutionStarted(payload []byte) (ExecutionStarted, error) {
	if err := startedPayload.checkDocument(payload); err != nil {
		return ExecutionStarted{}, err
	}
	fields, err := startedPayload.objectFields(payload, "", "executionId", "actionHash", "startedAt")
	if err != nil {
		return ExecutionStarted{}, err
	}
	var started ExecutionStarted
	if err := startedPayload.decodeFields([]payloadField{
		{fields["executionId"], "executionId", &started.ExecutionID},
		{fields["actionHash"], "actionHash", &started.ActionHash},
	}); err != nil {
		return ExecutionStarted{}, err
	}
	if started.StartedAt, err = startedPayload.millisecondTime(fields["startedAt"], "startedAt"); err != nil {
		return ExecutionStarted{}, err
	}
	if err := started.Validate(); err != nil {
		return ExecutionStarted{}, err
	}
	return started, nil
}

// Validate checks every ADR-009 rule on a typed value.
func (s ExecutionStarted) Validate() error {
	switch {
	case !validIdentifier(s.ExecutionID):
		return startedPayload.fieldError("executionId", identifierRule)
	case !validActionHash(s.ActionHash):
		return startedPayload.fieldError("actionHash", actionHashRule)
	case !validMillisecondTime(s.StartedAt):
		return startedPayload.fieldError("startedAt", millisecondTimeRule)
	}
	return nil
}

// ValidateAgainst checks that s reports on request: the same execution ID and
// the request's action hash. It validates both first.
func (s ExecutionStarted) ValidateAgainst(request ExecutionRequest) error {
	actionHash, err := request.ActionHash()
	if err != nil {
		return err
	}
	if err := s.Validate(); err != nil {
		return err
	}
	switch {
	case s.ExecutionID != request.ExecutionID:
		return startedPayload.fieldError("executionId", "does not match the request")
	case s.ActionHash != actionHash:
		return startedPayload.fieldError("actionHash", "does not match the request's action hash")
	}
	return nil
}

// CanonicalJSON returns the RFC 8785 encoding of a valid value (ADR-008's
// writer). It fails for an invalid value.
func (s ExecutionStarted) CanonicalJSON() ([]byte, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return canonicalObject(
		canonicalMember{"executionId", canonicalString(s.ExecutionID)},
		canonicalMember{"actionHash", canonicalString(s.ActionHash)},
		canonicalMember{"startedAt", canonicalString(s.StartedAt.Format(millisecondTimeLayout))},
	), nil
}

// ExecutionStarted decodes the payload of an execution.started envelope and
// fails without decoding for any other message type. The envelope itself must
// already have been decoded, verified, and checked for freshness and replay.
func (e Envelope) ExecutionStarted() (ExecutionStarted, error) {
	if err := e.expectType(MessageTypeExecutionStarted); err != nil {
		return ExecutionStarted{}, err
	}
	return DecodeExecutionStarted(e.Payload)
}
