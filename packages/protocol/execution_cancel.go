package protocol

import "errors"

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

// DecodeExecutionCancel strictly decodes an execution.cancel payload
// (ADR-009) with ADR-007's strict-document rules and exact keys at both
// levels. Every error wraps ErrInvalidExecutionCancel, and no partially valid
// value is returned. Decoding authorizes nothing: who may cancel is cloud
// policy, and the envelope must already have been verified.
func DecodeExecutionCancel(payload []byte) (ExecutionCancel, error) {
	if err := cancelPayload.checkDocument(payload); err != nil {
		return ExecutionCancel{}, err
	}
	top, err := cancelPayload.objectFields(payload, "", "executionId", "actionHash", "principals", "reason")
	if err != nil {
		return ExecutionCancel{}, err
	}
	principals, err := cancelPayload.objectFields(top["principals"], "principals", "userId", "agentId", "clientId")
	if err != nil {
		return ExecutionCancel{}, err
	}
	var cancel ExecutionCancel
	if err := cancelPayload.decodeFields([]payloadField{
		{top["executionId"], "executionId", &cancel.ExecutionID},
		{top["actionHash"], "actionHash", &cancel.ActionHash},
		{principals["userId"], "principals.userId", &cancel.Principals.UserID},
		{principals["agentId"], "principals.agentId", &cancel.Principals.AgentID},
		{principals["clientId"], "principals.clientId", &cancel.Principals.ClientID},
		{top["reason"], "reason", &cancel.Reason},
	}); err != nil {
		return ExecutionCancel{}, err
	}
	if err := cancel.Validate(); err != nil {
		return ExecutionCancel{}, err
	}
	return cancel, nil
}

// Validate checks every ADR-009 rule on a typed value.
func (c ExecutionCancel) Validate() error {
	for _, identifier := range []struct{ field, value string }{
		{"executionId", c.ExecutionID},
		{"principals.userId", c.Principals.UserID},
		{"principals.agentId", c.Principals.AgentID},
		{"principals.clientId", c.Principals.ClientID},
	} {
		if !validIdentifier(identifier.value) {
			return cancelPayload.fieldError(identifier.field, identifierRule)
		}
	}
	switch {
	case !validActionHash(c.ActionHash):
		return cancelPayload.fieldError("actionHash", actionHashRule)
	case !validText(c.Reason, MaxReasonBytes):
		return cancelPayload.fieldError("reason", untrustedTextRule)
	}
	return nil
}

// ValidateAgainst checks that c names request: the same execution ID and the
// request's action hash, so that Mio ignores a cancel that does not match the
// running execution. Principals are not compared, because the canceller may
// differ from the requester. It validates both first.
func (c ExecutionCancel) ValidateAgainst(request ExecutionRequest) error {
	actionHash, err := request.ActionHash()
	if err != nil {
		return err
	}
	if err := c.Validate(); err != nil {
		return err
	}
	switch {
	case c.ExecutionID != request.ExecutionID:
		return cancelPayload.fieldError("executionId", "does not match the request")
	case c.ActionHash != actionHash:
		return cancelPayload.fieldError("actionHash", "does not match the request's action hash")
	}
	return nil
}

// CanonicalJSON returns the RFC 8785 encoding of a valid value (ADR-008's
// writer). It fails for an invalid value.
func (c ExecutionCancel) CanonicalJSON() ([]byte, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return canonicalObject(
		canonicalMember{"executionId", canonicalString(c.ExecutionID)},
		canonicalMember{"actionHash", canonicalString(c.ActionHash)},
		canonicalMember{"principals", canonicalPrincipals(c.Principals)},
		canonicalMember{"reason", canonicalString(c.Reason)},
	), nil
}

// ExecutionCancel decodes the payload of an execution.cancel envelope and
// fails without decoding for any other message type. The envelope itself must
// already have been decoded, verified, and checked for freshness and replay.
func (e Envelope) ExecutionCancel() (ExecutionCancel, error) {
	if err := e.expectType(MessageTypeExecutionCancel); err != nil {
		return ExecutionCancel{}, err
	}
	return DecodeExecutionCancel(e.Payload)
}
