package protocol

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
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

// DecodeExecutionOutput strictly decodes an execution.output payload (ADR-009)
// with ADR-007's strict-document rules and exact keys. Every error wraps
// ErrInvalidExecutionOutput, and no partially valid value is returned.
func DecodeExecutionOutput(payload []byte) (ExecutionOutput, error) {
	if err := outputPayload.checkDocument(payload); err != nil {
		return ExecutionOutput{}, err
	}
	fields, err := outputPayload.objectFields(payload, "", "executionId", "sequence", "stream", "data")
	if err != nil {
		return ExecutionOutput{}, err
	}
	var output ExecutionOutput
	var data string
	if err := outputPayload.decodeFields([]payloadField{
		{fields["executionId"], "executionId", &output.ExecutionID},
		{fields["sequence"], "sequence", &output.Sequence},
		{fields["stream"], "stream", &output.Stream},
		{fields["data"], "data", &data},
	}); err != nil {
		return ExecutionOutput{}, err
	}
	var ok bool
	if output.Data, ok = decodeCanonicalBase64URL(data); !ok {
		return ExecutionOutput{}, outputPayload.fieldError("data", "must be canonical unpadded base64url")
	}
	if err := output.Validate(); err != nil {
		return ExecutionOutput{}, err
	}
	return output, nil
}

// Validate checks every ADR-009 rule on a typed value.
func (o ExecutionOutput) Validate() error {
	switch {
	case !validIdentifier(o.ExecutionID):
		return outputPayload.fieldError("executionId", identifierRule)
	case o.Sequence < 1 || o.Sequence > maxOutputCount:
		return outputPayload.fieldError("sequence", fmt.Sprintf("must be 1 to %d", maxOutputCount))
	case o.Stream != StreamStdout && o.Stream != StreamStderr:
		return outputPayload.fieldError("stream", `must be "stdout" or "stderr"`)
	case len(o.Data) < 1 || len(o.Data) > maxOutputChunkBytes:
		return outputPayload.fieldError("data", fmt.Sprintf("must hold 1 to %d bytes", maxOutputChunkBytes))
	}
	return nil
}

// CanonicalJSON returns the RFC 8785 encoding of a valid value (ADR-008's
// writer), with data as unpadded base64url. It fails for an invalid value.
func (o ExecutionOutput) CanonicalJSON() ([]byte, error) {
	if err := o.Validate(); err != nil {
		return nil, err
	}
	return canonicalObject(
		canonicalMember{"executionId", canonicalString(o.ExecutionID)},
		canonicalMember{"sequence", strconv.AppendUint(nil, o.Sequence, 10)},
		canonicalMember{"stream", canonicalString(string(o.Stream))},
		canonicalMember{"data", canonicalString(base64.RawURLEncoding.EncodeToString(o.Data))},
	), nil
}

// ExecutionOutput decodes the payload of an execution.output envelope and
// fails without decoding for any other message type. The envelope itself must
// already have been decoded, verified, and checked for freshness and replay.
func (e Envelope) ExecutionOutput() (ExecutionOutput, error) {
	if err := e.expectType(MessageTypeExecutionOutput); err != nil {
		return ExecutionOutput{}, err
	}
	return DecodeExecutionOutput(e.Payload)
}

// decodeCanonicalBase64URL decodes unpadded base64url text that must
// re-encode exactly, which rejects padding, the standard alphabet, non-zero
// trailing bits, and the line breaks that the base64 decoder would otherwise
// skip. Validate bounds the decoded size; the payload limit bounds the text.
func decodeCanonicalBase64URL(text string) ([]byte, bool) {
	decoded, err := base64.RawURLEncoding.DecodeString(text)
	if err != nil || base64.RawURLEncoding.EncodeToString(decoded) != text {
		return nil, false
	}
	return decoded, true
}
