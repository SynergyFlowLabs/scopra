package protocol

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// adrExampleRequest is the example payload from ADR-007, byte for byte.
const adrExampleRequest = `{
  "executionId": "exe_01HZX3",
  "principals": {"userId": "usr_1", "agentId": "agt_1", "clientId": "cli_1"},
  "resources": {"machineId": "mch_1", "workspaceId": "3f2a9c0b7d1e4f6a8b5c2d0e1f3a4b6c"},
  "grantId": "grt_1",
  "sessionId": "ses_1",
  "command": {
    "executable": "go",
    "arguments": ["test", "./..."],
    "shell": false,
    "workingDirectory": "."
  },
  "workspaceAccess": "read-only",
  "network": "none",
  "limits": {
    "timeoutSeconds": 600,
    "cpuMillicores": 2000,
    "memoryBytes": 2147483648,
    "pids": 512,
    "outputBytes": 1048576
  },
  "reason": "Run the unit tests after the fix",
  "createdAt": "2026-10-16T12:00:00Z",
  "expiresAt": "2026-10-16T12:15:00Z",
  "nonce": "kJ3b9sR0cT1uV2wX3yZ4aA"
}`

// adrExampleDecoded is the typed form of adrExampleRequest.
func adrExampleDecoded() ExecutionRequest {
	return ExecutionRequest{
		ExecutionID: "exe_01HZX3",
		Principals:  Principals{UserID: "usr_1", AgentID: "agt_1", ClientID: "cli_1"},
		Resources:   Resources{MachineID: "mch_1", WorkspaceID: "3f2a9c0b7d1e4f6a8b5c2d0e1f3a4b6c"},
		GrantID:     "grt_1",
		SessionID:   "ses_1",
		Command: Command{
			Executable:       "go",
			Arguments:        []string{"test", "./..."},
			Shell:            false,
			WorkingDirectory: ".",
		},
		WorkspaceAccess: WorkspaceReadOnly,
		Network:         NetworkNone,
		Limits: Limits{
			TimeoutSeconds: 600,
			CPUMillicores:  2000,
			MemoryBytes:    2147483648,
			PIDs:           512,
			OutputBytes:    1048576,
		},
		Reason:    "Run the unit tests after the fix",
		CreatedAt: time.Date(2026, time.October, 16, 12, 0, 0, 0, time.UTC),
		ExpiresAt: time.Date(2026, time.October, 16, 12, 15, 0, 0, time.UTC),
		Nonce:     "kJ3b9sR0cT1uV2wX3yZ4aA",
	}
}

func TestDecodeExecutionRequest_AcceptsADRExample(t *testing.T) {
	request, err := DecodeExecutionRequest([]byte(adrExampleRequest))
	if err != nil {
		t.Fatalf("DecodeExecutionRequest(ADR-007 example) error = %v", err)
	}
	if want := adrExampleDecoded(); !reflect.DeepEqual(request, want) {
		t.Fatalf("DecodeExecutionRequest(ADR-007 example) = %+v, want %+v", request, want)
	}
}

func TestSecurity_DecodeExecutionRequestEnforcesFieldRules(t *testing.T) {
	long := func(n int) string { return strings.Repeat("a", n) }
	nonceOf := func(n int) string { return base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x5a}, n)) }
	arguments := func(n int) []any {
		values := make([]any, n)
		for i := range values {
			values[i] = "x"
		}
		return values
	}
	tests := []struct {
		path   string
		value  any
		accept bool
	}{
		{path: "executionId", value: long(128), accept: true},
		{path: "executionId", value: long(129)},
		{path: "executionId", value: ""},
		{path: "executionId", value: "exe 1"},
		{path: "executionId", value: "exé"},
		{path: "principals.userId", value: ""},
		{path: "principals.agentId", value: "agt/1"},
		{path: "principals.clientId", value: long(129)},
		{path: "resources.machineId", value: " mch"},
		{path: "grantId", value: "grt\t1"},
		{path: "sessionId", value: ""},

		{path: "resources.workspaceId", value: strings.Repeat("0", 32), accept: true},
		{path: "resources.workspaceId", value: strings.Repeat("a", 31)},
		{path: "resources.workspaceId", value: strings.Repeat("a", 33)},
		{path: "resources.workspaceId", value: strings.Repeat("A", 32)},
		{path: "resources.workspaceId", value: strings.Repeat("g", 32)},

		{path: "command.executable", value: "/usr/bin/go", accept: true},
		{path: "command.executable", value: "./gradlew", accept: true},
		{path: "command.executable", value: "./scripts/test.sh", accept: true},
		{path: "command.executable", value: "my tool", accept: true},
		{path: "command.executable", value: "...", accept: true},
		{path: "command.executable", value: long(4095), accept: true},
		{path: "command.executable", value: long(4096)},
		{path: "command.executable", value: ""},
		{path: "command.executable", value: "."},
		{path: "command.executable", value: ".."},
		{path: "command.executable", value: "/"},
		{path: "command.executable", value: "/usr/bin/"},
		{path: "command.executable", value: "/usr//bin/go"},
		{path: "command.executable", value: "/usr/bin/../go"},
		{path: "command.executable", value: "./"},
		{path: "command.executable", value: "./."},
		{path: "command.executable", value: "./../x"},
		{path: "command.executable", value: ".//x"},
		{path: "command.executable", value: "./a/../b"},
		{path: "command.executable", value: "bin/go"},
		{path: "command.executable", value: "go\n"},
		{path: "command.executable", value: "go\x7f"},
		{path: "command.executable", value: "go\u0085"},
		{path: "command.executable", value: "go\u202e"},
		{path: "command.executable", value: "go\u2028"},
		{path: "command.executable", value: "go\u2029"},

		{path: "command.arguments", value: []any{}, accept: true},
		{path: "command.arguments", value: []any{"", "tab\there", "new\nline", "\u202e", "\u2028", "\u2029"}, accept: true},
		{path: "command.arguments", value: arguments(4096), accept: true},
		{path: "command.arguments", value: arguments(4097)},
		{path: "command.arguments", value: []any{long(131072)}, accept: true},
		{path: "command.arguments", value: []any{long(131073)}},
		{path: "command.arguments", value: "test"},
		{path: "command.arguments", value: []any{1}},

		{path: "command.shell", value: true},
		{path: "command.shell", value: "false"},
		{path: "command.shell", value: 0},

		{path: "command.workingDirectory", value: "src/app", accept: true},
		{path: "command.workingDirectory", value: long(4095), accept: true},
		{path: "command.workingDirectory", value: long(4096)},
		{path: "command.workingDirectory", value: ""},
		{path: "command.workingDirectory", value: "/"},
		{path: "command.workingDirectory", value: "/srv"},
		{path: "command.workingDirectory", value: ".."},
		{path: "command.workingDirectory", value: "../x"},
		{path: "command.workingDirectory", value: "a/../b"},
		{path: "command.workingDirectory", value: "./a"},
		{path: "command.workingDirectory", value: "a/"},
		{path: "command.workingDirectory", value: "a//b"},
		{path: "command.workingDirectory", value: "a/./b"},
		{path: "command.workingDirectory", value: "src\t"},
		{path: "command.workingDirectory", value: "src\u2066"},
		{path: "command.workingDirectory", value: "src\u2028"},
		{path: "command.workingDirectory", value: "src\u2029"},

		{path: "workspaceAccess", value: "read-write", accept: true},
		{path: "workspaceAccess", value: "readonly"},
		{path: "workspaceAccess", value: "READ-ONLY"},
		{path: "workspaceAccess", value: ""},
		{path: "network", value: "host"},
		{path: "network", value: "None"},
		{path: "network", value: ""},

		{path: "limits.timeoutSeconds", value: 1, accept: true},
		{path: "limits.timeoutSeconds", value: 86400, accept: true},
		{path: "limits.timeoutSeconds", value: 0},
		{path: "limits.timeoutSeconds", value: 86401},
		{path: "limits.cpuMillicores", value: 1, accept: true},
		{path: "limits.cpuMillicores", value: 1000000, accept: true},
		{path: "limits.cpuMillicores", value: 0},
		{path: "limits.cpuMillicores", value: 1000001},
		{path: "limits.memoryBytes", value: 1 << 20, accept: true},
		{path: "limits.memoryBytes", value: 1 << 40, accept: true},
		{path: "limits.memoryBytes", value: 1<<20 - 1},
		{path: "limits.memoryBytes", value: 1<<40 + 1},
		{path: "limits.pids", value: 1, accept: true},
		{path: "limits.pids", value: 65536, accept: true},
		{path: "limits.pids", value: 0},
		{path: "limits.pids", value: 65537},
		{path: "limits.outputBytes", value: 1, accept: true},
		{path: "limits.outputBytes", value: 1 << 30, accept: true},
		{path: "limits.outputBytes", value: 0},
		{path: "limits.outputBytes", value: 1<<30 + 1},
		{path: "limits.timeoutSeconds", value: json.RawMessage("-1")},
		{path: "limits.timeoutSeconds", value: json.RawMessage("1.5")},
		{path: "limits.timeoutSeconds", value: json.RawMessage("600.0")},
		{path: "limits.timeoutSeconds", value: json.RawMessage("6e2")},
		{path: "limits.timeoutSeconds", value: json.RawMessage("18446744073709551616")},
		{path: "limits.timeoutSeconds", value: "600"},

		{path: "reason", value: long(1024), accept: true},
		{path: "reason", value: "Tests für Ümlaute", accept: true},
		{path: "reason", value: long(1025)},
		{path: "reason", value: ""},
		{path: "reason", value: "line\nbreak"},
		{path: "reason", value: "\u200fhidden"},
		{path: "reason", value: "\u202eevil"},
		{path: "reason", value: "line\u2028separator"},
		{path: "reason", value: "paragraph\u2029separator"},

		{path: "expiresAt", value: "2026-10-16T13:00:00Z", accept: true},
		{path: "expiresAt", value: "2026-10-16T13:00:01Z"},
		{path: "expiresAt", value: "2026-10-16T12:00:00Z"},
		{path: "expiresAt", value: "2026-10-16T11:59:59Z"},
		{path: "expiresAt", value: "2026-10-16T12:15:00.000Z"},
		{path: "createdAt", value: "2026-10-16T12:00:00+00:00"},
		{path: "createdAt", value: "2026-10-16T12:00:00.5Z"},
		{path: "createdAt", value: "2026-10-16T12:00:00.000Z"},
		{path: "createdAt", value: "2026-10-16 12:00:00Z"},
		{path: "createdAt", value: "2026-10-16T12:00:00z"},
		{path: "createdAt", value: "2026-10-16T12:00Z"},
		{path: "createdAt", value: ""},
		{path: "createdAt", value: 1760616000},

		{path: "nonce", value: nonceOf(16), accept: true},
		{path: "nonce", value: nonceOf(128), accept: true},
		{path: "nonce", value: nonceOf(15)},
		{path: "nonce", value: nonceOf(129)},
		{path: "nonce", value: "kJ3b9sR0cT1uV2wX3yZ4aA=="},
		{path: "nonce", value: "kJ3b9sR0cT1uV2wX3yZ4aB"},
		{path: "nonce", value: "kJ3b9sR0cT1uV2wX3yZ4a+"},
		{path: "nonce", value: ""},
	}
	for i, test := range tests {
		t.Run(fmt.Sprintf("%s/%d", test.path, i), func(t *testing.T) {
			fields := requestFields(t)
			setField(t, fields, test.path, test.value)
			request, err := DecodeExecutionRequest(marshalFields(t, fields))
			if test.accept {
				if err != nil {
					t.Fatalf("%s = %.80q: error = %v, want acceptance", test.path, fmt.Sprint(test.value), err)
				}
				return
			}
			if !errors.Is(err, ErrInvalidExecutionRequest) || !strings.Contains(err.Error(), test.path) {
				t.Fatalf("%s = %.80q: error = %v, want ErrInvalidExecutionRequest naming %s", test.path, fmt.Sprint(test.value), err, test.path)
			}
			if !reflect.DeepEqual(request, ExecutionRequest{}) {
				t.Fatalf("%s = %.80q: returned a partial request %+v", test.path, fmt.Sprint(test.value), request)
			}
		})
	}
}

func TestSecurity_DecodeExecutionRequestRejectsAmbiguousJSON(t *testing.T) {
	example := []byte(adrExampleRequest)
	replace := func(old, replacement string) []byte {
		t.Helper()
		if !bytes.Contains(example, []byte(old)) {
			t.Fatalf("the ADR example does not contain %q", old)
		}
		return bytes.Replace(example, []byte(old), []byte(replacement), 1)
	}
	appended := func(suffix string) []byte { return append(slices.Clone(example), suffix...) }
	tests := []struct {
		name     string
		payload  []byte
		wantErr  error // also required, in addition to ErrInvalidExecutionRequest
		wantText string
	}{
		{name: "empty", payload: nil, wantText: "payload"},
		{name: "oversized", payload: appended(strings.Repeat(" ", MaxPayloadBytes)), wantErr: ErrPayloadTooLarge},
		{name: "trailing value", payload: appended(" {}"), wantText: "payload"},
		{name: "trailing garbage", payload: appended("x"), wantText: "payload"},
		{name: "truncated", payload: example[:len(example)-1], wantText: "payload"},
		{name: "top-level array", payload: []byte("[" + adrExampleRequest + "]"), wantText: "payload"},
		{name: "duplicate top-level key", payload: replace(`"grantId": "grt_1",`, `"grantId": "grt_1", "grantId": "grt_2",`), wantErr: ErrDuplicateJSONKey, wantText: "grantId"},
		{name: "duplicate nested key", payload: replace(`"pids": 512,`, `"pids": 512, "pids": 1,`), wantErr: ErrDuplicateJSONKey, wantText: "limits.pids"},
		{name: "null value", payload: replace(`"grantId": "grt_1"`, `"grantId": null`), wantText: "grantId"},
		{name: "nested null", payload: replace(`"pids": 512`, `"pids": null`), wantText: "limits.pids"},
		{name: "null shell", payload: replace(`"shell": false`, `"shell": null`), wantText: "command.shell"},
		{name: "null argument", payload: replace(`["test", "./..."]`, `["test", null]`), wantText: "command.arguments"},
		{name: "null object", payload: replace(`"limits": {`, `"limits": null, "unused": {`), wantText: "limits"},
		{name: "invalid UTF-8", payload: replace("Run the unit tests", "Run the \xff unit tests"), wantText: "UTF-8"},
		{name: "lone high surrogate", payload: replace("after the fix", `after the fix \ud800`), wantText: "escape"},
		{name: "lone low surrogate", payload: replace("after the fix", `after the fix \udc00`), wantText: "escape"},
		{name: "reversed surrogate pair", payload: replace("after the fix", `after the fix \udc00\ud800`), wantText: "escape"},
		{name: "high surrogate before a non-surrogate", payload: replace("after the fix", `after the fix \ud800\u0041`), wantText: "escape"},
		{name: "high surrogate at the end of a string", payload: replace(`"./..."`, `"./...\ud800"`), wantText: "escape"},
		{name: "escaped NUL", payload: replace(`"./..."`, `"./...\u0000"`), wantText: "escape"},
		{name: "unknown top-level field", payload: replace(`"grantId": "grt_1",`, `"grantId": "grt_1", "env": {},`), wantText: "env"},
		{name: "unknown nested field", payload: replace(`"shell": false,`, `"shell": false, "env": [],`), wantText: "command.env"},
		{name: "control characters in an unknown field", payload: replace(`"grantId": "grt_1",`, `"grantId": "grt_1", "\u001b[2J": 1,`), wantText: `\x1b[2J`},
		{name: "control characters in a null field", payload: replace(`"grantId": "grt_1",`, `"grantId": "grt_1", "\u001b[2J": null,`), wantText: `\x1b[2J`},
		{name: "control characters in a duplicate field", payload: replace(`"grantId": "grt_1",`, `"grantId": "grt_1", "\u001b[2J": 1, "\u001b[2J": 2,`), wantErr: ErrDuplicateJSONKey, wantText: `\x1b[2J`},
		{name: "nesting over 64 levels", payload: replace(`["test", "./..."]`, strings.Repeat("[", 70)+strings.Repeat("]", 70)), wantText: "nesting"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request, err := DecodeExecutionRequest(test.payload)
			if !errors.Is(err, ErrInvalidExecutionRequest) || (test.wantErr != nil && !errors.Is(err, test.wantErr)) {
				t.Fatalf("DecodeExecutionRequest() error = %v, want ErrInvalidExecutionRequest (and %v)", err, test.wantErr)
			}
			if !strings.Contains(err.Error(), test.wantText) {
				t.Fatalf("DecodeExecutionRequest() error = %q, want it to contain %q", err, test.wantText)
			}
			if strings.ContainsRune(err.Error(), '\x1b') {
				t.Fatalf("DecodeExecutionRequest() error contains a raw control character: %q", err)
			}
			if !reflect.DeepEqual(request, ExecutionRequest{}) {
				t.Fatalf("DecodeExecutionRequest() returned a partial request %+v", request)
			}
		})
	}
}

func TestDecodeExecutionRequest_PreservesTransmittedText(t *testing.T) {
	payload := strings.Replace(adrExampleRequest, `["test", "./..."]`,
		`["\u00e9", "\u00C9", "\ud83d\ude00", "\uD83D\uDE00", "a\/b\"c", "tab\tnew\nline"]`, 1)
	request, err := DecodeExecutionRequest([]byte(payload))
	if err != nil {
		t.Fatalf("DecodeExecutionRequest() error = %v", err)
	}
	if want := []string{"é", "É", "😀", "😀", `a/b"c`, "tab\tnew\nline"}; !slices.Equal(request.Command.Arguments, want) {
		t.Fatalf("arguments = %q, want %q", request.Command.Arguments, want)
	}
}

func TestDecodeExecutionRequest_RequiresEveryFieldAndNoOthers(t *testing.T) {
	required := []string{
		"executionId",
		"principals", "principals.userId", "principals.agentId", "principals.clientId",
		"resources", "resources.machineId", "resources.workspaceId",
		"grantId", "sessionId",
		"command", "command.executable", "command.arguments", "command.shell", "command.workingDirectory",
		"workspaceAccess", "network",
		"limits", "limits.timeoutSeconds", "limits.cpuMillicores", "limits.memoryBytes", "limits.pids", "limits.outputBytes",
		"reason", "createdAt", "expiresAt", "nonce",
	}
	if all := fieldPaths("", requestFields(t)); !slices.Equal(slices.Sorted(slices.Values(all)), slices.Sorted(slices.Values(required))) {
		t.Fatalf("the ADR example has fields %q, want exactly %q", all, required)
	}
	for _, path := range required {
		t.Run("missing "+path, func(t *testing.T) {
			fields := requestFields(t)
			object, key := parentOf(t, fields, path)
			delete(object, key)
			if _, err := DecodeExecutionRequest(marshalFields(t, fields)); !errors.Is(err, ErrInvalidExecutionRequest) || !strings.Contains(err.Error(), path+": is required") {
				t.Fatalf("DecodeExecutionRequest(without %s) error = %v, want ErrInvalidExecutionRequest saying it is required", path, err)
			}
		})
	}
	for _, path := range []string{"extra", "principals.extra", "resources.extra", "command.extra", "limits.extra"} {
		t.Run("unknown "+path, func(t *testing.T) {
			fields := requestFields(t)
			object, key := parentOf(t, fields, path)
			object[key] = 1
			if _, err := DecodeExecutionRequest(marshalFields(t, fields)); !errors.Is(err, ErrInvalidExecutionRequest) || !strings.Contains(err.Error(), path) {
				t.Fatalf("DecodeExecutionRequest(with %s) error = %v, want ErrInvalidExecutionRequest naming it", path, err)
			}
		})
	}
	for _, path := range []string{"principals", "resources", "command", "limits"} {
		t.Run(path+" not an object", func(t *testing.T) {
			fields := requestFields(t)
			setField(t, fields, path, []any{})
			if _, err := DecodeExecutionRequest(marshalFields(t, fields)); !errors.Is(err, ErrInvalidExecutionRequest) || !strings.Contains(err.Error(), path) {
				t.Fatalf("DecodeExecutionRequest(%s = []) error = %v, want ErrInvalidExecutionRequest naming it", path, err)
			}
		})
	}
}

func TestEnvelope_ExecutionRequestChecksTheMessageType(t *testing.T) {
	envelope := Envelope{Type: MessageTypeExecutionRequest, Payload: json.RawMessage(adrExampleRequest)}
	request, err := envelope.ExecutionRequest()
	if err != nil || !reflect.DeepEqual(request, adrExampleDecoded()) {
		t.Fatalf("ExecutionRequest() = %+v, %v; want the ADR example", request, err)
	}
	for _, other := range []MessageType{
		MessageTypeExecutionResult, MessageTypeExecutionCancel, MessageTypeMachineRevocation, MessageTypeAcknowledgement, "execution.request.v2",
	} {
		envelope.Type = other
		request, err := envelope.ExecutionRequest()
		if !errors.Is(err, ErrUnexpectedMessageType) || errors.Is(err, ErrInvalidExecutionRequest) || !reflect.DeepEqual(request, ExecutionRequest{}) {
			t.Fatalf("ExecutionRequest() with type %q = %+v, %v; want ErrUnexpectedMessageType without decoding", other, request, err)
		}
	}
}

func TestEnvelope_ExecutionRequestAfterDecodeEnvelope(t *testing.T) {
	var payload bytes.Buffer
	if err := json.Compact(&payload, []byte(adrExampleRequest)); err != nil {
		t.Fatalf("json.Compact() error = %v", err)
	}
	now := time.Date(2026, time.October, 16, 12, 0, 0, 0, time.UTC)
	envelope := validTestEnvelope(t, now)
	envelope.Payload = payload.Bytes()
	envelope.PayloadHash = hashTestPayload(envelope.Payload)

	decoded, err := DecodeEnvelope(bytes.NewReader(marshalTestEnvelope(t, envelope)))
	if err != nil {
		t.Fatalf("DecodeEnvelope() error = %v", err)
	}
	request, err := decoded.ExecutionRequest()
	if err != nil || !reflect.DeepEqual(request, adrExampleDecoded()) {
		t.Fatalf("ExecutionRequest() = %+v, %v; want the ADR example", request, err)
	}
}

func TestExecutionRequest_ValidateAtEnforcesTheRequestWindow(t *testing.T) {
	request := adrExampleDecoded()
	created, expires := request.CreatedAt, request.ExpiresAt
	tests := []struct {
		name    string
		now     time.Time
		wantErr error
	}{
		{name: "inside the window", now: created.Add(time.Minute)},
		{name: "30 seconds before creation", now: created.Add(-30 * time.Second)},
		{name: "31 seconds before creation", now: created.Add(-31 * time.Second), wantErr: ErrExecutionRequestNotYetValid},
		{name: "just before expiry", now: expires.Add(-time.Nanosecond)},
		{name: "at expiry", now: expires, wantErr: ErrExecutionRequestExpired},
		{name: "after expiry", now: expires.Add(time.Hour), wantErr: ErrExecutionRequestExpired},
		{name: "missing time", now: time.Time{}, wantErr: ErrInvalidExecutionRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := request.ValidateAt(test.now)
			if (test.wantErr == nil && err != nil) || (test.wantErr != nil && !errors.Is(err, test.wantErr)) {
				t.Fatalf("ValidateAt(%v) error = %v, want %v", test.now, err, test.wantErr)
			}
		})
	}
	if err := (ExecutionRequest{}).ValidateAt(created); !errors.Is(err, ErrInvalidExecutionRequest) {
		t.Fatalf("ValidateAt() on an invalid request error = %v, want ErrInvalidExecutionRequest", err)
	}
}

func TestExecutionRequest_ValidateChecksTypedValues(t *testing.T) {
	if err := adrExampleDecoded().Validate(); err != nil {
		t.Fatalf("Validate(ADR example) error = %v", err)
	}
	noArguments := adrExampleDecoded()
	noArguments.Command.Arguments = nil
	if err := noArguments.Validate(); err != nil {
		t.Fatalf("Validate(nil arguments) error = %v, want nil arguments to mean none", err)
	}
	tests := []struct {
		name   string
		change func(*ExecutionRequest)
		field  string
	}{
		{name: "creation time not in UTC", change: func(r *ExecutionRequest) {
			r.CreatedAt = r.CreatedAt.In(time.FixedZone("PKT", 5*60*60))
		}, field: "createdAt"},
		{name: "fractional expiry", change: func(r *ExecutionRequest) { r.ExpiresAt = r.ExpiresAt.Add(time.Millisecond) }, field: "expiresAt"},
		{name: "year beyond 9999", change: func(r *ExecutionRequest) {
			r.CreatedAt = time.Date(10000, time.January, 1, 0, 0, 0, 0, time.UTC)
			r.ExpiresAt = r.CreatedAt.Add(time.Minute)
		}, field: "createdAt"},
		{name: "NUL in an argument", change: func(r *ExecutionRequest) { r.Command.Arguments = []string{"a\x00b"} }, field: "command.arguments"},
		{name: "invalid UTF-8 in an argument", change: func(r *ExecutionRequest) { r.Command.Arguments = []string{"\xff"} }, field: "command.arguments"},
		{name: "invalid UTF-8 in the executable", change: func(r *ExecutionRequest) { r.Command.Executable = "go\xff" }, field: "command.executable"},
		{name: "invalid UTF-8 in the working directory", change: func(r *ExecutionRequest) { r.Command.WorkingDirectory = "src\xff" }, field: "command.workingDirectory"},
		{name: "invalid UTF-8 in the reason", change: func(r *ExecutionRequest) { r.Reason = "\xff" }, field: "reason"},
		{name: "shell", change: func(r *ExecutionRequest) { r.Command.Shell = true }, field: "command.shell"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := adrExampleDecoded()
			test.change(&request)
			if err := request.Validate(); !errors.Is(err, ErrInvalidExecutionRequest) || !strings.Contains(err.Error(), test.field) {
				t.Fatalf("Validate() error = %v, want ErrInvalidExecutionRequest naming %s", err, test.field)
			}
		})
	}
}

func FuzzDecodeExecutionRequest(f *testing.F) {
	f.Add([]byte(adrExampleRequest))
	f.Add([]byte(`{}`))
	f.Add([]byte(strings.Replace(adrExampleRequest, `"./..."`, `"\u00e9\ud83d\ude00\t"`, 1)))
	f.Add([]byte(strings.Replace(adrExampleRequest, `"go"`, `"./gradlew"`, 1)))
	f.Add([]byte(strings.Replace(adrExampleRequest, `"pids": 512`, `"pids": null`, 1)))

	f.Fuzz(func(t *testing.T, payload []byte) {
		request, err := DecodeExecutionRequest(payload)
		if err != nil {
			if !errors.Is(err, ErrInvalidExecutionRequest) {
				t.Fatalf("DecodeExecutionRequest() error %v does not wrap ErrInvalidExecutionRequest", err)
			}
			return
		}
		if err := request.Validate(); err != nil {
			t.Fatalf("an accepted request fails Validate(): %v", err)
		}
		again, err := DecodeExecutionRequest(encodeTestRequest(t, request))
		if err != nil || !reflect.DeepEqual(again, request) {
			t.Fatalf("round trip = %+v, %v; want %+v", again, err, request)
		}
	})
}

// executionRequestJSON is an independent mirror of the ADR-007 wire shape,
// used to re-encode decoded requests.
type executionRequestJSON struct {
	ExecutionID string `json:"executionId"`
	Principals  struct {
		UserID   string `json:"userId"`
		AgentID  string `json:"agentId"`
		ClientID string `json:"clientId"`
	} `json:"principals"`
	Resources struct {
		MachineID   string `json:"machineId"`
		WorkspaceID string `json:"workspaceId"`
	} `json:"resources"`
	GrantID   string `json:"grantId"`
	SessionID string `json:"sessionId"`
	Command   struct {
		Executable       string   `json:"executable"`
		Arguments        []string `json:"arguments"`
		Shell            bool     `json:"shell"`
		WorkingDirectory string   `json:"workingDirectory"`
	} `json:"command"`
	WorkspaceAccess string `json:"workspaceAccess"`
	Network         string `json:"network"`
	Limits          struct {
		TimeoutSeconds uint64 `json:"timeoutSeconds"`
		CPUMillicores  uint64 `json:"cpuMillicores"`
		MemoryBytes    uint64 `json:"memoryBytes"`
		PIDs           uint64 `json:"pids"`
		OutputBytes    uint64 `json:"outputBytes"`
	} `json:"limits"`
	Reason    string `json:"reason"`
	CreatedAt string `json:"createdAt"`
	ExpiresAt string `json:"expiresAt"`
	Nonce     string `json:"nonce"`
}

func encodeTestRequest(t testing.TB, request ExecutionRequest) []byte {
	t.Helper()
	var wire executionRequestJSON
	wire.ExecutionID = request.ExecutionID
	wire.Principals.UserID = request.Principals.UserID
	wire.Principals.AgentID = request.Principals.AgentID
	wire.Principals.ClientID = request.Principals.ClientID
	wire.Resources.MachineID = request.Resources.MachineID
	wire.Resources.WorkspaceID = request.Resources.WorkspaceID
	wire.GrantID = request.GrantID
	wire.SessionID = request.SessionID
	wire.Command.Executable = request.Command.Executable
	wire.Command.Arguments = append([]string{}, request.Command.Arguments...)
	wire.Command.Shell = request.Command.Shell
	wire.Command.WorkingDirectory = request.Command.WorkingDirectory
	wire.WorkspaceAccess = string(request.WorkspaceAccess)
	wire.Network = string(request.Network)
	wire.Limits.TimeoutSeconds = request.Limits.TimeoutSeconds
	wire.Limits.CPUMillicores = request.Limits.CPUMillicores
	wire.Limits.MemoryBytes = request.Limits.MemoryBytes
	wire.Limits.PIDs = request.Limits.PIDs
	wire.Limits.OutputBytes = request.Limits.OutputBytes
	wire.Reason = request.Reason
	wire.CreatedAt = request.CreatedAt.Format("2006-01-02T15:04:05Z")
	wire.ExpiresAt = request.ExpiresAt.Format("2006-01-02T15:04:05Z")
	wire.Nonce = request.Nonce
	encoded, err := json.Marshal(wire)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return encoded
}

// requestFields returns the ADR-007 example as nested maps, so that a test
// can change one field and marshal the rest unchanged.
func requestFields(t testing.TB) map[string]any {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(adrExampleRequest))
	decoder.UseNumber()
	var fields map[string]any
	if err := decoder.Decode(&fields); err != nil {
		t.Fatalf("decode the ADR example: %v", err)
	}
	return fields
}

func marshalFields(t testing.TB, fields map[string]any) []byte {
	t.Helper()
	encoded, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return encoded
}

// parentOf returns the object that holds the last segment of a dotted path,
// and that segment.
func parentOf(t testing.TB, fields map[string]any, path string) (map[string]any, string) {
	t.Helper()
	segments := strings.Split(path, ".")
	object := fields
	for _, segment := range segments[:len(segments)-1] {
		child, ok := object[segment].(map[string]any)
		if !ok {
			t.Fatalf("no object at %q in %q", segment, path)
		}
		object = child
	}
	return object, segments[len(segments)-1]
}

func setField(t testing.TB, fields map[string]any, path string, value any) {
	t.Helper()
	object, key := parentOf(t, fields, path)
	if _, ok := object[key]; !ok {
		t.Fatalf("the ADR example has no field %q", path)
	}
	object[key] = value
}

// fieldPaths lists every field of fields as a dotted path, objects included.
func fieldPaths(prefix string, fields map[string]any) []string {
	var paths []string
	for key, value := range fields {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}
		paths = append(paths, path)
		if child, ok := value.(map[string]any); ok {
			paths = append(paths, fieldPaths(path, child)...)
		}
	}
	return paths
}
