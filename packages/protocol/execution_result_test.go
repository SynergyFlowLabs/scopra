package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// resultExamples holds one execution.result payload for each state. The
// failed example is ADR-009's, byte for byte.
var resultExamples = map[ExecutionState]string{
	StateSucceeded:   `{"executionId": "exe_01HZX3", "actionHash": "b4f47c942fc6499aea49d76df1f89c630f8683bd69c98f292b9e03eed4dd44e2", "state": "succeeded", "startedAt": "2026-10-16T12:00:01.250Z", "endedAt": "2026-10-16T12:00:02.000Z", "exitCode": 0, "output": {"stdoutBytes": 64, "stderrBytes": 0, "truncated": false, "chunks": 1}}`,
	StateFailed:      `{"executionId": "exe_01HZX3", "actionHash": "b4f47c942fc6499aea49d76df1f89c630f8683bd69c98f292b9e03eed4dd44e2", "state": "failed", "startedAt": "2026-10-16T12:00:01.250Z", "endedAt": "2026-10-16T12:00:09.004Z", "exitCode": 1, "output": {"stdoutBytes": 64, "stderrBytes": 2048, "truncated": false, "chunks": 3}}`,
	StateTimedOut:    `{"executionId": "exe_01HZX3", "actionHash": "b4f47c942fc6499aea49d76df1f89c630f8683bd69c98f292b9e03eed4dd44e2", "state": "timed_out", "startedAt": "2026-10-16T12:00:01.250Z", "endedAt": "2026-10-16T12:10:01.250Z", "output": {"stdoutBytes": 0, "stderrBytes": 0, "truncated": false, "chunks": 0}}`,
	StateCancelled:   `{"executionId": "exe_01HZX3", "actionHash": "b4f47c942fc6499aea49d76df1f89c630f8683bd69c98f292b9e03eed4dd44e2", "state": "cancelled", "startedAt": "2026-10-16T12:00:01.250Z", "endedAt": "2026-10-16T12:00:05.500Z", "output": {"stdoutBytes": 10, "stderrBytes": 0, "truncated": false, "chunks": 1}}`,
	StateOutOfMemory: `{"executionId": "exe_01HZX3", "actionHash": "b4f47c942fc6499aea49d76df1f89c630f8683bd69c98f292b9e03eed4dd44e2", "state": "out_of_memory", "startedAt": "2026-10-16T12:00:01.250Z", "endedAt": "2026-10-16T12:00:30.000Z", "output": {"stdoutBytes": 1048576, "stderrBytes": 0, "truncated": true, "chunks": 16}}`,
	StateErrored:     `{"executionId": "exe_01HZX3", "actionHash": "b4f47c942fc6499aea49d76df1f89c630f8683bd69c98f292b9e03eed4dd44e2", "state": "errored", "error": {"code": "start_failed", "message": "executable not found: go"}, "output": {"stdoutBytes": 0, "stderrBytes": 0, "truncated": false, "chunks": 0}}`,
	StateRejected:    `{"executionId": "exe_01HZX3", "actionHash": "b4f47c942fc6499aea49d76df1f89c630f8683bd69c98f292b9e03eed4dd44e2", "state": "rejected", "error": {"code": "workspace_unavailable", "message": "the workspace is not registered"}}`,
}

// Canonical forms of two examples, rendered independently with Python's json
// module (sorted keys, compact separators, ensure_ascii=False).
const (
	failedResultCanonical   = `{"actionHash":"b4f47c942fc6499aea49d76df1f89c630f8683bd69c98f292b9e03eed4dd44e2","endedAt":"2026-10-16T12:00:09.004Z","executionId":"exe_01HZX3","exitCode":1,"output":{"chunks":3,"stderrBytes":2048,"stdoutBytes":64,"truncated":false},"startedAt":"2026-10-16T12:00:01.250Z","state":"failed"}`
	rejectedResultCanonical = `{"actionHash":"b4f47c942fc6499aea49d76df1f89c630f8683bd69c98f292b9e03eed4dd44e2","error":{"code":"workspace_unavailable","message":"the workspace is not registered"},"executionId":"exe_01HZX3","state":"rejected"}`
)

// resultExampleDecoded is the typed form of resultExamples[state].
func resultExampleDecoded(state ExecutionState) ExecutionResult {
	at := func(seconds, milliseconds int) time.Time {
		return time.Date(2026, time.October, 16, 12, 0, seconds, milliseconds*int(time.Millisecond), time.UTC)
	}
	result := ExecutionResult{ExecutionID: "exe_01HZX3", ActionHash: vector1ActionHash, State: state}
	switch state {
	case StateSucceeded:
		result.StartedAt, result.EndedAt = at(1, 250), at(2, 0)
		result.Output = OutputSummary{StdoutBytes: 64, Chunks: 1}
	case StateFailed:
		result.StartedAt, result.EndedAt, result.ExitCode = at(1, 250), at(9, 4), 1
		result.Output = OutputSummary{StdoutBytes: 64, StderrBytes: 2048, Chunks: 3}
	case StateTimedOut:
		result.StartedAt, result.EndedAt = at(1, 250), at(1, 250).Add(10*time.Minute)
	case StateCancelled:
		result.StartedAt, result.EndedAt = at(1, 250), at(5, 500)
		result.Output = OutputSummary{StdoutBytes: 10, Chunks: 1}
	case StateOutOfMemory:
		result.StartedAt, result.EndedAt = at(1, 250), at(30, 0)
		result.Output = OutputSummary{StdoutBytes: 1048576, Truncated: true, Chunks: 16}
	case StateErrored:
		result.Error = ResultError{Code: ErrorCodeStartFailed, Message: "executable not found: go"}
	case StateRejected:
		result.Error = ResultError{Code: ErrorCodeWorkspaceUnavailable, Message: "the workspace is not registered"}
	}
	return result
}

func TestDecodeExecutionResult_AcceptsEveryStateExample(t *testing.T) {
	if len(resultExamples) != 7 {
		t.Fatalf("resultExamples has %d states, want the 7 of ADR-009", len(resultExamples))
	}
	for _, state := range slices.Sorted(maps.Keys(resultExamples)) {
		t.Run(string(state), func(t *testing.T) {
			result, err := DecodeExecutionResult([]byte(resultExamples[state]))
			if err != nil {
				t.Fatalf("DecodeExecutionResult() error = %v", err)
			}
			if want := resultExampleDecoded(state); !reflect.DeepEqual(result, want) {
				t.Fatalf("DecodeExecutionResult() = %+v, want %+v", result, want)
			}
		})
	}
}

func TestExecutionResult_CanonicalJSON(t *testing.T) {
	for state, want := range map[ExecutionState]string{StateFailed: failedResultCanonical, StateRejected: rejectedResultCanonical} {
		if canonical, err := resultExampleDecoded(state).CanonicalJSON(); err != nil || string(canonical) != want {
			t.Fatalf("%s: CanonicalJSON() = %s, %v; want %s", state, canonical, err, want)
		}
	}
	for _, state := range slices.Sorted(maps.Keys(resultExamples)) {
		t.Run(string(state), func(t *testing.T) {
			checkCanonicalResult(t, []byte(resultExamples[state]), resultExampleDecoded(state))
		})
	}
	if canonical, err := (ExecutionResult{}).CanonicalJSON(); !errors.Is(err, ErrInvalidExecutionResult) || canonical != nil {
		t.Fatalf("CanonicalJSON() of the zero value = %q, %v; want nil and ErrInvalidExecutionResult", canonical, err)
	}
}

func TestSecurity_DecodeExecutionResultEnforcesFieldRules(t *testing.T) {
	long := func(n int) string { return strings.Repeat("a", n) }
	tests := []struct {
		state  ExecutionState
		set    map[string]any
		field  string
		accept bool
	}{
		{state: StateFailed, set: map[string]any{"executionId": long(128)}, accept: true},
		{state: StateFailed, set: map[string]any{"executionId": long(129)}, field: "executionId"},
		{state: StateFailed, set: map[string]any{"executionId": ""}, field: "executionId"},
		{state: StateFailed, set: map[string]any{"executionId": "exe 1"}, field: "executionId"},

		{state: StateFailed, set: map[string]any{"actionHash": strings.Repeat("0", 64)}, accept: true},
		{state: StateFailed, set: map[string]any{"actionHash": strings.Repeat("a", 63)}, field: "actionHash"},
		{state: StateFailed, set: map[string]any{"actionHash": strings.Repeat("a", 65)}, field: "actionHash"},
		{state: StateFailed, set: map[string]any{"actionHash": strings.Repeat("A", 64)}, field: "actionHash"},
		{state: StateFailed, set: map[string]any{"actionHash": strings.Repeat("g", 64)}, field: "actionHash"},

		{state: StateFailed, set: map[string]any{"state": "running"}, field: "state"},
		{state: StateFailed, set: map[string]any{"state": "Failed"}, field: "state"},
		{state: StateFailed, set: map[string]any{"state": ""}, field: "state"},
		{state: StateFailed, set: map[string]any{"state": 1}, field: "state"},
		{state: StateFailed, set: map[string]any{"executionId": 1}, field: "executionId"},
		{state: StateFailed, set: map[string]any{"actionHash": true}, field: "actionHash"},
		{state: StateFailed, set: map[string]any{"startedAt": 1760616001}, field: "startedAt"},
		{state: StateFailed, set: map[string]any{"endedAt": []any{}}, field: "endedAt"},
		{state: StateRejected, set: map[string]any{"error.code": 1}, field: "error.code"},
		{state: StateRejected, set: map[string]any{"error.message": []any{"x"}}, field: "error.message"},

		{state: StateFailed, set: map[string]any{"endedAt": "2026-10-16T12:00:01.250Z"}, accept: true},
		{state: StateFailed, set: map[string]any{"endedAt": "2026-10-16T12:00:01.249Z"}, field: "endedAt"},
		{state: StateFailed, set: map[string]any{"startedAt": "2026-10-16T12:00:01Z"}, field: "startedAt"},
		{state: StateFailed, set: map[string]any{"startedAt": "2026-10-16T12:00:01.25Z"}, field: "startedAt"},
		{state: StateFailed, set: map[string]any{"startedAt": "2026-10-16T12:00:01.2500Z"}, field: "startedAt"},
		{state: StateFailed, set: map[string]any{"startedAt": "2026-10-16T12:00:01,250Z"}, field: "startedAt"},
		{state: StateFailed, set: map[string]any{"startedAt": "2026-10-16T12:00:01.250+00:00"}, field: "startedAt"},
		{state: StateFailed, set: map[string]any{"startedAt": "2026-10-16T12:00:01.250z"}, field: "startedAt"},
		{state: StateFailed, set: map[string]any{"startedAt": ""}, field: "startedAt"},
		{state: StateFailed, set: map[string]any{"endedAt": "2026-10-16T12:00:09Z"}, field: "endedAt"},

		{state: StateFailed, set: map[string]any{"exitCode": 255}, accept: true},
		{state: StateFailed, set: map[string]any{"exitCode": 0}, field: "exitCode"},
		{state: StateFailed, set: map[string]any{"exitCode": 256}, field: "exitCode"},
		{state: StateFailed, set: map[string]any{"exitCode": json.RawMessage("-1")}, field: "exitCode"},
		{state: StateFailed, set: map[string]any{"exitCode": json.RawMessage("1.0")}, field: "exitCode"},
		{state: StateFailed, set: map[string]any{"exitCode": "1"}, field: "exitCode"},
		{state: StateSucceeded, set: map[string]any{"exitCode": 1}, field: "exitCode"},
		{state: StateSucceeded, set: map[string]any{"exitCode": json.RawMessage("-0")}, field: "exitCode"},

		{state: StateFailed, set: map[string]any{"output.stdoutBytes": 1 << 30, "output.chunks": 1 << 30, "output.stderrBytes": 0}, accept: true},
		{state: StateFailed, set: map[string]any{"output.stdoutBytes": 1<<30 + 1, "output.chunks": 1 << 30, "output.stderrBytes": 0}, field: "output.stdoutBytes"},
		{state: StateFailed, set: map[string]any{"output.stderrBytes": 1<<30 + 1, "output.chunks": 1 << 30, "output.stdoutBytes": 0}, field: "output.stderrBytes"},
		{state: StateFailed, set: map[string]any{"output.chunks": 1<<30 + 1, "output.stdoutBytes": 1 << 30, "output.stderrBytes": 1 << 30}, field: "output.chunks"},
		{state: StateFailed, set: map[string]any{"output.chunks": 0}, field: "output"},
		{state: StateFailed, set: map[string]any{"output.chunks": 2113}, field: "output"},
		{state: StateFailed, set: map[string]any{"output.chunks": 2112}, accept: true},
		{state: StateFailed, set: map[string]any{"output.chunks": 1, "output.stdoutBytes": 65536, "output.stderrBytes": 1}, field: "output"},
		{state: StateFailed, set: map[string]any{"output.chunks": 1, "output.stdoutBytes": 65536, "output.stderrBytes": 0}, accept: true},
		{state: StateFailed, set: map[string]any{"output.truncated": "false"}, field: "output.truncated"},
		{state: StateFailed, set: map[string]any{"output.truncated": 0}, field: "output.truncated"},
		{state: StateFailed, set: map[string]any{"output.stdoutBytes": json.RawMessage("64.0")}, field: "output.stdoutBytes"},

		{state: StateErrored, set: map[string]any{"error.code": long(32)}, accept: true},
		{state: StateErrored, set: map[string]any{"error.code": "new_code"}, accept: true},
		{state: StateErrored, set: map[string]any{"error.code": ErrorCodeBusy}, accept: true},
		{state: StateErrored, set: map[string]any{"error.code": long(33)}, field: "error.code"},
		{state: StateErrored, set: map[string]any{"error.code": ""}, field: "error.code"},
		{state: StateErrored, set: map[string]any{"error.code": "Busy"}, field: "error.code"},
		{state: StateErrored, set: map[string]any{"error.code": "1abc"}, field: "error.code"},
		{state: StateErrored, set: map[string]any{"error.code": "_abc"}, field: "error.code"},
		{state: StateErrored, set: map[string]any{"error.code": "a-b"}, field: "error.code"},
		{state: StateRejected, set: map[string]any{"error.code": "a b"}, field: "error.code"},
		{state: StateRejected, set: map[string]any{"error.message": long(1024)}, accept: true},
		{state: StateRejected, set: map[string]any{"error.message": long(1025)}, field: "error.message"},
		{state: StateRejected, set: map[string]any{"error.message": ""}, field: "error.message"},
		{state: StateRejected, set: map[string]any{"error.message": "line\nbreak"}, field: "error.message"},
		{state: StateRejected, set: map[string]any{"error.message": "\u202eevil"}, field: "error.message"},
		{state: StateRejected, set: map[string]any{"error.message": "line\u2028separator"}, field: "error.message"},
	}
	for i, test := range tests {
		t.Run(fmt.Sprintf("%s/%d", test.state, i), func(t *testing.T) {
			fields := resultFields(t, test.state)
			for path, value := range test.set {
				setField(t, fields, path, value)
			}
			result, err := DecodeExecutionResult(marshalFields(t, fields))
			if test.accept {
				if err != nil {
					t.Fatalf("%v: error = %v, want acceptance", test.set, err)
				}
				return
			}
			if !errors.Is(err, ErrInvalidExecutionResult) || !strings.Contains(err.Error(), test.field) {
				t.Fatalf("%v: error = %v, want ErrInvalidExecutionResult naming %s", test.set, err, test.field)
			}
			if !reflect.DeepEqual(result, ExecutionResult{}) {
				t.Fatalf("%v: returned a partial result %+v", test.set, result)
			}
		})
	}
}

func TestDecodeExecutionResult_RequiresEachStatesExactKeys(t *testing.T) {
	foreign := map[string]any{
		"startedAt": "2026-10-16T12:00:01.250Z",
		"endedAt":   "2026-10-16T12:00:02.000Z",
		"exitCode":  0,
		"output":    map[string]any{"stdoutBytes": 0, "stderrBytes": 0, "truncated": false, "chunks": 0},
		"error":     map[string]any{"code": "internal", "message": "x"},
	}
	for _, state := range slices.Sorted(maps.Keys(resultExamples)) {
		present := resultFields(t, state)
		for _, key := range slices.Sorted(maps.Keys(present)) {
			t.Run(fmt.Sprintf("%s without %s", state, key), func(t *testing.T) {
				fields := resultFields(t, state)
				delete(fields, key)
				if _, err := DecodeExecutionResult(marshalFields(t, fields)); !errors.Is(err, ErrInvalidExecutionResult) || !strings.Contains(err.Error(), key+": is required") {
					t.Fatalf("error = %v, want ErrInvalidExecutionResult saying %s is required", err, key)
				}
			})
		}
		for _, key := range slices.Sorted(maps.Keys(foreign)) {
			if _, ok := present[key]; ok {
				continue
			}
			t.Run(fmt.Sprintf("%s with %s", state, key), func(t *testing.T) {
				fields := resultFields(t, state)
				fields[key] = foreign[key]
				if _, err := DecodeExecutionResult(marshalFields(t, fields)); !errors.Is(err, ErrInvalidExecutionResult) || !strings.Contains(err.Error(), key) {
					t.Fatalf("error = %v, want ErrInvalidExecutionResult naming %s", err, key)
				}
			})
		}
	}
	nested := []struct {
		state  ExecutionState
		object string
		keys   []string
	}{
		{StateFailed, "output", []string{"stdoutBytes", "stderrBytes", "truncated", "chunks"}},
		{StateRejected, "error", []string{"code", "message"}},
	}
	for _, test := range nested {
		for _, key := range test.keys {
			t.Run(fmt.Sprintf("%s without %s.%s", test.state, test.object, key), func(t *testing.T) {
				fields := resultFields(t, test.state)
				object, member := parentOf(t, fields, test.object+"."+key)
				delete(object, member)
				if _, err := DecodeExecutionResult(marshalFields(t, fields)); !errors.Is(err, ErrInvalidExecutionResult) || !strings.Contains(err.Error(), test.object+"."+key+": is required") {
					t.Fatalf("error = %v, want ErrInvalidExecutionResult saying %s.%s is required", err, test.object, key)
				}
			})
		}
		t.Run(fmt.Sprintf("%s with %s.extra", test.state, test.object), func(t *testing.T) {
			fields := resultFields(t, test.state)
			object, _ := parentOf(t, fields, test.object+".extra")
			object["extra"] = 1
			if _, err := DecodeExecutionResult(marshalFields(t, fields)); !errors.Is(err, ErrInvalidExecutionResult) || !strings.Contains(err.Error(), test.object+".extra") {
				t.Fatalf("error = %v, want ErrInvalidExecutionResult naming %s.extra", err, test.object)
			}
		})
		t.Run(fmt.Sprintf("%s with %s not an object", test.state, test.object), func(t *testing.T) {
			fields := resultFields(t, test.state)
			fields[test.object] = []any{}
			if _, err := DecodeExecutionResult(marshalFields(t, fields)); !errors.Is(err, ErrInvalidExecutionResult) || !strings.Contains(err.Error(), test.object) {
				t.Fatalf("error = %v, want ErrInvalidExecutionResult naming %s", err, test.object)
			}
		})
	}
}

func TestSecurity_DecodeExecutionResultRejectsAmbiguousJSON(t *testing.T) {
	failed, rejected := []byte(resultExamples[StateFailed]), []byte(resultExamples[StateRejected])
	replace := func(document []byte, old, replacement string) []byte {
		t.Helper()
		if !bytes.Contains(document, []byte(old)) {
			t.Fatalf("the example does not contain %q", old)
		}
		return bytes.Replace(document, []byte(old), []byte(replacement), 1)
	}
	tests := []struct {
		name     string
		payload  []byte
		wantErr  error
		wantText string
	}{
		{name: "empty", payload: nil, wantText: "payload"},
		{name: "oversized", payload: append(slices.Clone(failed), strings.Repeat(" ", MaxPayloadBytes)...), wantErr: ErrPayloadTooLarge},
		{name: "trailing value", payload: append(slices.Clone(failed), " {}"...), wantText: "payload"},
		{name: "top-level array", payload: []byte("[" + resultExamples[StateFailed] + "]"), wantText: "payload"},
		{name: "null value", payload: replace(failed, `"exitCode": 1`, `"exitCode": null`), wantText: "exitCode"},
		{name: "nested null", payload: replace(failed, `"chunks": 3`, `"chunks": null`), wantText: "output.chunks"},
		{name: "duplicate key", payload: replace(failed, `"exitCode": 1,`, `"exitCode": 1, "exitCode": 2,`), wantErr: ErrDuplicateJSONKey, wantText: "exitCode"},
		{name: "duplicate nested key", payload: replace(failed, `"chunks": 3`, `"chunks": 3, "chunks": 4`), wantErr: ErrDuplicateJSONKey, wantText: "output.chunks"},
		{name: "duplicate state", payload: replace(failed, `"state": "failed",`, `"state": "failed", "state": "succeeded",`), wantErr: ErrDuplicateJSONKey, wantText: "state"},
		{name: "invalid UTF-8", payload: replace(rejected, "not registered", "not \xff registered"), wantText: "UTF-8"},
		{name: "lone surrogate", payload: replace(rejected, "not registered", `not \ud800 registered`), wantText: "escape"},
		{name: "escaped NUL", payload: replace(rejected, "not registered", `not \u0000 registered`), wantText: "escape"},
		{name: "nesting over 64 levels", payload: replace(failed, `"exitCode": 1`, `"exitCode": `+strings.Repeat("[", 70)+strings.Repeat("]", 70)), wantText: "nesting"},
		{name: "control characters in an unknown field", payload: replace(failed, `"exitCode": 1,`, `"exitCode": 1, "\u001b[2J": 1,`), wantText: `\x1b[2J`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := DecodeExecutionResult(test.payload)
			if !errors.Is(err, ErrInvalidExecutionResult) || errors.Is(err, ErrInvalidExecutionRequest) || (test.wantErr != nil && !errors.Is(err, test.wantErr)) {
				t.Fatalf("error = %v, want ErrInvalidExecutionResult (and %v), not ErrInvalidExecutionRequest", err, test.wantErr)
			}
			if !strings.Contains(err.Error(), test.wantText) || strings.ContainsRune(err.Error(), '\x1b') {
				t.Fatalf("error = %q, want it to contain %q and no raw control character", err, test.wantText)
			}
			if !reflect.DeepEqual(result, ExecutionResult{}) {
				t.Fatalf("returned a partial result %+v", result)
			}
		})
	}
}

func TestExecutionResult_ValidateAgainstTheRequest(t *testing.T) {
	request := adrExampleDecoded()
	for _, state := range slices.Sorted(maps.Keys(resultExamples)) {
		if err := resultExampleDecoded(state).ValidateAgainst(request); err != nil {
			t.Fatalf("%s: ValidateAgainst() error = %v", state, err)
		}
	}
	tests := []struct {
		name    string
		change  func(*ExecutionResult)
		request func(*ExecutionRequest)
		wantErr error
		field   string
	}{
		{name: "another execution", change: func(r *ExecutionResult) { r.ExecutionID = "exe_01HZX4" }, wantErr: ErrInvalidExecutionResult, field: "executionId"},
		{name: "another action", change: func(r *ExecutionResult) { r.ActionHash = strings.Repeat("0", 64) }, wantErr: ErrInvalidExecutionResult, field: "actionHash"},
		{name: "a changed request", request: func(r *ExecutionRequest) { r.Reason += "." }, wantErr: ErrInvalidExecutionResult, field: "actionHash"},
		{name: "output over the limit", change: func(r *ExecutionResult) { r.Output.StdoutBytes, r.Output.Chunks = 1048577, 17 }, wantErr: ErrInvalidExecutionResult, field: "output"},
		{name: "an invalid result", change: func(r *ExecutionResult) { r.State = "running" }, wantErr: ErrInvalidExecutionResult, field: "state"},
		{name: "an invalid request", request: func(r *ExecutionRequest) { r.Command.Shell = true }, wantErr: ErrInvalidExecutionRequest, field: "command.shell"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, againstRequest := resultExampleDecoded(StateOutOfMemory), adrExampleDecoded()
			if test.change != nil {
				test.change(&result)
			}
			if test.request != nil {
				test.request(&againstRequest)
			}
			if err := result.ValidateAgainst(againstRequest); !errors.Is(err, test.wantErr) || !strings.Contains(err.Error(), test.field) {
				t.Fatalf("ValidateAgainst() error = %v, want %v naming %s", err, test.wantErr, test.field)
			}
		})
	}
}

func TestExecutionResult_ValidateChecksTypedValues(t *testing.T) {
	tests := []struct {
		name   string
		state  ExecutionState
		change func(*ExecutionResult)
		field  string
	}{
		{name: "zero value", state: StateFailed, change: func(r *ExecutionResult) { *r = ExecutionResult{} }, field: "executionId"},
		{name: "start not in UTC", state: StateFailed, change: func(r *ExecutionResult) { r.StartedAt = r.StartedAt.In(time.FixedZone("PKT", 5*60*60)) }, field: "startedAt"},
		{name: "end below a millisecond", state: StateFailed, change: func(r *ExecutionResult) { r.EndedAt = r.EndedAt.Add(500 * time.Microsecond) }, field: "endedAt"},
		{name: "year beyond 9999", state: StateFailed, change: func(r *ExecutionResult) {
			r.StartedAt = time.Date(10000, time.January, 1, 0, 0, 0, 0, time.UTC)
			r.EndedAt = r.StartedAt
		}, field: "startedAt"},
		{name: "exit code above 255", state: StateFailed, change: func(r *ExecutionResult) { r.ExitCode = 256 }, field: "exitCode"},
		{name: "exit code for a killed command", state: StateTimedOut, change: func(r *ExecutionResult) { r.ExitCode = 137 }, field: "exitCode"},
		{name: "times for an errored command", state: StateErrored, change: func(r *ExecutionResult) { r.StartedAt = time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC) }, field: "startedAt"},
		{name: "output for a rejection", state: StateRejected, change: func(r *ExecutionResult) { r.Output.Chunks = 1 }, field: "output"},
		{name: "error for a success", state: StateSucceeded, change: func(r *ExecutionResult) { r.Error.Code = ErrorCodeInternal }, field: "error"},
		{name: "invalid UTF-8 in an error message", state: StateRejected, change: func(r *ExecutionResult) { r.Error.Message = "\xff" }, field: "error.message"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := resultExampleDecoded(test.state)
			if err := result.Validate(); err != nil {
				t.Fatalf("Validate() of the unchanged example error = %v", err)
			}
			test.change(&result)
			if err := result.Validate(); !errors.Is(err, ErrInvalidExecutionResult) || !strings.Contains(err.Error(), test.field) {
				t.Fatalf("Validate() error = %v, want ErrInvalidExecutionResult naming %s", err, test.field)
			}
		})
	}
}

func FuzzDecodeExecutionResult(f *testing.F) {
	for _, state := range slices.Sorted(maps.Keys(resultExamples)) {
		f.Add([]byte(resultExamples[state]))
	}
	f.Add([]byte(strings.Replace(resultExamples[StateRejected], "not registered", `not registered \u00e9\ud83d\ude00`, 1)))

	f.Fuzz(func(t *testing.T, payload []byte) {
		result, err := DecodeExecutionResult(payload)
		if err != nil {
			if !errors.Is(err, ErrInvalidExecutionResult) {
				t.Fatalf("DecodeExecutionResult() error %v does not wrap ErrInvalidExecutionResult", err)
			}
			return
		}
		if err := result.Validate(); err != nil {
			t.Fatalf("an accepted result fails Validate(): %v", err)
		}
		checkCanonicalResult(t, payload, result)
	})
}

// checkCanonicalResult verifies that the canonical form of an accepted result
// equals an independent RFC 8785 rendering of its payload, decodes to the same
// result, and is stable.
func checkCanonicalResult(t *testing.T, payload []byte, result ExecutionResult) {
	t.Helper()
	canonical, err := result.CanonicalJSON()
	if err != nil {
		t.Fatalf("CanonicalJSON() of an accepted result error = %v", err)
	}
	if want := jcsReference(t, payload); !bytes.Equal(canonical, want) {
		t.Fatalf("CanonicalJSON() = %q, want the RFC 8785 rendering %q", canonical, want)
	}
	decoded, err := DecodeExecutionResult(canonical)
	if err != nil || !reflect.DeepEqual(decoded, result) {
		t.Fatalf("DecodeExecutionResult(canonical) = %+v, %v; want %+v", decoded, err, result)
	}
	if again, err := decoded.CanonicalJSON(); err != nil || !bytes.Equal(again, canonical) {
		t.Fatalf("canonicalizing again = %q, %v; want %q", again, err, canonical)
	}
}

// resultFields returns resultExamples[state] as nested maps.
func resultFields(t testing.TB, state ExecutionState) map[string]any {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(resultExamples[state]))
	decoder.UseNumber()
	var fields map[string]any
	if err := decoder.Decode(&fields); err != nil {
		t.Fatalf("decode the %s example: %v", state, err)
	}
	return fields
}
