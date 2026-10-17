package protocol

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"
)

const (
	// maxOutputCount bounds output byte counts and chunk counts (2^30).
	maxOutputCount = 1 << 30
	// maxOutputChunkBytes bounds the data in one execution.output chunk.
	maxOutputChunkBytes = 64 << 10
	maxErrorCodeBytes   = 32
	maxErrorMessage     = 1024
)

// resultKeys lists the exact keys of each state's payload (ADR-009).
var resultKeys = map[ExecutionState][]string{
	StateSucceeded:   {"executionId", "actionHash", "state", "startedAt", "endedAt", "exitCode", "output"},
	StateFailed:      {"executionId", "actionHash", "state", "startedAt", "endedAt", "exitCode", "output"},
	StateTimedOut:    {"executionId", "actionHash", "state", "startedAt", "endedAt", "output"},
	StateCancelled:   {"executionId", "actionHash", "state", "startedAt", "endedAt", "output"},
	StateOutOfMemory: {"executionId", "actionHash", "state", "startedAt", "endedAt", "output"},
	StateErrored:     {"executionId", "actionHash", "state", "error", "output"},
	StateRejected:    {"executionId", "actionHash", "state", "error"},
}

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

// DecodeExecutionResult strictly decodes an execution.result payload and
// checks every ADR-009 rule. The state selects the exact key set; every other
// rule is ADR-007's. Every error wraps ErrInvalidExecutionResult, and no
// partially valid result is returned. Decoding does not authenticate the
// envelope; ValidateAgainst ties a result to its request.
func DecodeExecutionResult(payload []byte) (ExecutionResult, error) {
	if err := resultPayload.checkDocument(payload); err != nil {
		return ExecutionResult{}, err
	}
	result, err := decodeResultFields(payload)
	if err != nil {
		return ExecutionResult{}, err
	}
	if err := result.Validate(); err != nil {
		return ExecutionResult{}, err
	}
	return result, nil
}

// ExecutionResult decodes the payload of an execution.result envelope and
// fails without decoding for any other message type. The envelope itself must
// already have been decoded, verified, and checked for freshness and replay.
func (e Envelope) ExecutionResult() (ExecutionResult, error) {
	if err := e.expectType(MessageTypeExecutionResult); err != nil {
		return ExecutionResult{}, err
	}
	return DecodeExecutionResult(e.Payload)
}

// Validate checks every ADR-009 rule on a typed result, including that the
// fields its state does not use are zero.
func (r ExecutionResult) Validate() error {
	if !validIdentifier(r.ExecutionID) {
		return resultPayload.fieldError("executionId", identifierRule)
	}
	if !validActionHash(r.ActionHash) {
		return resultPayload.fieldError("actionHash", actionHashRule)
	}
	keys, ok := resultKeys[r.State]
	if !ok {
		return resultPayload.fieldError("state",
			"must be succeeded, failed, timed_out, cancelled, out_of_memory, errored, or rejected")
	}
	unused := fmt.Sprintf("must be unset for state %q", r.State)

	if slices.Contains(keys, "startedAt") {
		switch {
		case !validMillisecondTime(r.StartedAt):
			return resultPayload.fieldError("startedAt", millisecondTimeRule)
		case !validMillisecondTime(r.EndedAt):
			return resultPayload.fieldError("endedAt", millisecondTimeRule)
		case r.EndedAt.Before(r.StartedAt):
			return resultPayload.fieldError("endedAt", "must not be before startedAt")
		}
	} else if !r.StartedAt.IsZero() || !r.EndedAt.IsZero() {
		return resultPayload.fieldError("startedAt", unused)
	}

	switch r.State {
	case StateSucceeded:
		if r.ExitCode != 0 {
			return resultPayload.fieldError("exitCode", "must be 0 for state \"succeeded\"")
		}
	case StateFailed:
		if r.ExitCode < 1 || r.ExitCode > 255 {
			return resultPayload.fieldError("exitCode", "must be 1 to 255 for state \"failed\"")
		}
	default:
		if r.ExitCode != 0 {
			return resultPayload.fieldError("exitCode", unused)
		}
	}

	if slices.Contains(keys, "output") {
		if err := r.Output.validate(); err != nil {
			return err
		}
	} else if r.Output != (OutputSummary{}) {
		return resultPayload.fieldError("output", unused)
	}

	if slices.Contains(keys, "error") {
		switch {
		case !validErrorCode(r.Error.Code):
			return resultPayload.fieldError("error.code", "must match [a-z][a-z_]* and be at most 32 bytes")
		case !validText(r.Error.Message, maxErrorMessage):
			return resultPayload.fieldError("error.message", untrustedTextRule)
		}
	} else if r.Error != (ResultError{}) {
		return resultPayload.fieldError("error", unused)
	}
	return nil
}

// ValidateAgainst checks that r reports on request: the same execution ID,
// the request's action hash, and no more output than its limit allows. It
// validates both first.
func (r ExecutionResult) ValidateAgainst(request ExecutionRequest) error {
	actionHash, err := request.ActionHash()
	if err != nil {
		return err
	}
	if err := r.Validate(); err != nil {
		return err
	}
	switch {
	case r.ExecutionID != request.ExecutionID:
		return resultPayload.fieldError("executionId", "does not match the request")
	case r.ActionHash != actionHash:
		return resultPayload.fieldError("actionHash", "does not match the request's action hash")
	case r.Output.StdoutBytes+r.Output.StderrBytes > request.Limits.OutputBytes:
		return resultPayload.fieldError("output", "exceeds the request's limits.outputBytes")
	}
	return nil
}

// CanonicalJSON returns the RFC 8785 encoding of a valid result, with exactly
// its state's keys (ADR-008's writer). It fails for an invalid result.
func (r ExecutionResult) CanonicalJSON() ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	keys := resultKeys[r.State]
	members := []canonicalMember{
		{"executionId", canonicalString(r.ExecutionID)},
		{"actionHash", canonicalString(r.ActionHash)},
		{"state", canonicalString(string(r.State))},
	}
	if slices.Contains(keys, "startedAt") {
		members = append(members,
			canonicalMember{"startedAt", canonicalString(r.StartedAt.Format(millisecondTimeLayout))},
			canonicalMember{"endedAt", canonicalString(r.EndedAt.Format(millisecondTimeLayout))})
	}
	if slices.Contains(keys, "exitCode") {
		members = append(members, canonicalMember{"exitCode", strconv.AppendInt(nil, int64(r.ExitCode), 10)})
	}
	if slices.Contains(keys, "output") {
		members = append(members, canonicalMember{"output", canonicalObject(
			canonicalMember{"stdoutBytes", strconv.AppendUint(nil, r.Output.StdoutBytes, 10)},
			canonicalMember{"stderrBytes", strconv.AppendUint(nil, r.Output.StderrBytes, 10)},
			canonicalMember{"truncated", strconv.AppendBool(nil, r.Output.Truncated)},
			canonicalMember{"chunks", strconv.AppendUint(nil, r.Output.Chunks, 10)},
		)})
	}
	if slices.Contains(keys, "error") {
		members = append(members, canonicalMember{"error", canonicalObject(
			canonicalMember{"code", canonicalString(r.Error.Code)},
			canonicalMember{"message", canonicalString(r.Error.Message)},
		)})
	}
	return canonicalObject(members...), nil
}

// validate checks the output counts: each at most 2^30, and consistent with
// chunks of 1 to 64 KiB each, so that no chunk is empty and none too large.
func (o OutputSummary) validate() error {
	for _, count := range []struct {
		field string
		value uint64
	}{
		{"output.stdoutBytes", o.StdoutBytes},
		{"output.stderrBytes", o.StderrBytes},
		{"output.chunks", o.Chunks},
	} {
		if count.value > maxOutputCount {
			return resultPayload.fieldError(count.field, fmt.Sprintf("must be 0 to %d", maxOutputCount))
		}
	}
	total := o.StdoutBytes + o.StderrBytes
	if total < o.Chunks || total > o.Chunks*maxOutputChunkBytes {
		return resultPayload.fieldError("output",
			"byte counts must be at least chunks and at most chunks times 65536, because each chunk holds 1 to 65536 bytes")
	}
	return nil
}

// decodeResultFields reads the state first, because it selects the exact key
// set, and then decodes each field.
func decodeResultFields(payload []byte) (ExecutionResult, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(payload, &object); err != nil {
		return ExecutionResult{}, resultPayload.fieldError("payload", "must be a single JSON object")
	}
	stateRaw, ok := object["state"]
	if !ok {
		return ExecutionResult{}, resultPayload.fieldError("state", "is required")
	}
	var result ExecutionResult
	if err := resultPayload.decodeFields([]payloadField{{stateRaw, "state", &result.State}}); err != nil {
		return ExecutionResult{}, err
	}
	keys, ok := resultKeys[result.State]
	if !ok {
		return ExecutionResult{}, resultPayload.fieldError("state",
			"must be succeeded, failed, timed_out, cancelled, out_of_memory, errored, or rejected")
	}
	top, err := resultPayload.objectFields(payload, "", keys...)
	if err != nil {
		return ExecutionResult{}, err
	}
	if err := resultPayload.decodeFields([]payloadField{
		{top["executionId"], "executionId", &result.ExecutionID},
		{top["actionHash"], "actionHash", &result.ActionHash},
	}); err != nil {
		return ExecutionResult{}, err
	}

	if slices.Contains(keys, "startedAt") {
		if result.StartedAt, err = resultPayload.millisecondTime(top["startedAt"], "startedAt"); err != nil {
			return ExecutionResult{}, err
		}
		if result.EndedAt, err = resultPayload.millisecondTime(top["endedAt"], "endedAt"); err != nil {
			return ExecutionResult{}, err
		}
	}
	if slices.Contains(keys, "exitCode") {
		// Through uint64, so that any sign, including -0, is rejected.
		var code uint64
		if err := resultPayload.decodeFields([]payloadField{{top["exitCode"], "exitCode", &code}}); err != nil {
			return ExecutionResult{}, err
		}
		if code > 255 {
			return ExecutionResult{}, resultPayload.fieldError("exitCode", "must be 0 to 255")
		}
		result.ExitCode = int(code)
	}
	if slices.Contains(keys, "output") {
		output, err := resultPayload.objectFields(top["output"], "output", "stdoutBytes", "stderrBytes", "truncated", "chunks")
		if err != nil {
			return ExecutionResult{}, err
		}
		if err := resultPayload.decodeFields([]payloadField{
			{output["stdoutBytes"], "output.stdoutBytes", &result.Output.StdoutBytes},
			{output["stderrBytes"], "output.stderrBytes", &result.Output.StderrBytes},
			{output["truncated"], "output.truncated", &result.Output.Truncated},
			{output["chunks"], "output.chunks", &result.Output.Chunks},
		}); err != nil {
			return ExecutionResult{}, err
		}
	}
	if slices.Contains(keys, "error") {
		resultError, err := resultPayload.objectFields(top["error"], "error", "code", "message")
		if err != nil {
			return ExecutionResult{}, err
		}
		if err := resultPayload.decodeFields([]payloadField{
			{resultError["code"], "error.code", &result.Error.Code},
			{resultError["message"], "error.message", &result.Error.Message},
		}); err != nil {
			return ExecutionResult{}, err
		}
	}
	return result, nil
}

// validErrorCode reports whether code matches [a-z][a-z_]* and is at most 32
// bytes (ADR-009, as ADR-006).
func validErrorCode(code string) bool {
	if code == "" || len(code) > maxErrorCodeBytes || code[0] < 'a' || code[0] > 'z' {
		return false
	}
	for i := 1; i < len(code); i++ {
		if !(code[i] >= 'a' && code[i] <= 'z' || code[i] == '_') {
			return false
		}
	}
	return true
}
