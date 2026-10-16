package protocol

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// The normative test vectors from ADR-008, copied verbatim.
const (
	vector1Canonical  = `{"command":{"arguments":["test","./..."],"executable":"go","shell":false,"workingDirectory":"."},"createdAt":"2026-10-16T12:00:00Z","executionId":"exe_01HZX3","expiresAt":"2026-10-16T12:15:00Z","grantId":"grt_1","limits":{"cpuMillicores":2000,"memoryBytes":2147483648,"outputBytes":1048576,"pids":512,"timeoutSeconds":600},"network":"none","nonce":"kJ3b9sR0cT1uV2wX3yZ4aA","principals":{"agentId":"agt_1","clientId":"cli_1","userId":"usr_1"},"reason":"Run the unit tests after the fix","resources":{"machineId":"mch_1","workspaceId":"3f2a9c0b7d1e4f6a8b5c2d0e1f3a4b6c"},"sessionId":"ses_1","workspaceAccess":"read-only"}`
	vector1ActionHash = "b4f47c942fc6499aea49d76df1f89c630f8683bd69c98f292b9e03eed4dd44e2"

	vector2CanonicalBytes = 709
	vector2ArgumentsHex   = `22 61 72 67 75 6d 65 6e 74 73 22 3a 5b 22 74 61 62 5c 74 68 65 72 65 22 2c 22 6e 65 77 5c 6e 6c 69 6e 65 22
2c 22 5c 75 30 30 30 31 22 2c 22 7f 22 2c 22 e2 80 a8 22 2c 22 71 75 6f 74 65 5c 22 20 62 61 63 6b 5c 5c 73
6c 61 73 68 22 2c 22 c3 a9 22 2c 22 f0 9f 98 80 22 2c 22 22 5d`
	vector2ActionHash = "9df45c503097070244bdcf27d06fb6c66e9328ee908d234ae03f3831a5731533"

	// testActionHashLabel is ADR-008's label, kept here as an independent copy.
	testActionHashLabel = "scopra/execution.request/v1\n"
)

// vector2Request is ADR-008's second test vector.
func vector2Request() ExecutionRequest {
	request := adrExampleDecoded()
	request.Command = Command{
		Executable:       "./scripts/test.sh",
		Arguments:        []string{"tab\there", "new\nline", "\u0001", "\u007f", "\u2028", "quote\" back\\slash", "\u00e9", "\U0001F600", ""},
		Shell:            false,
		WorkingDirectory: "src/app",
	}
	request.WorkspaceAccess = WorkspaceReadWrite
	request.Limits.MemoryBytes = 1 << 40
	return request
}

func TestExecutionRequest_CanonicalJSONMatchesADRVector1(t *testing.T) {
	canonical, err := adrExampleDecoded().CanonicalJSON()
	if err != nil {
		t.Fatalf("CanonicalJSON() error = %v", err)
	}
	if string(canonical) != vector1Canonical {
		t.Fatalf("CanonicalJSON() =\n%s\nwant\n%s", canonical, vector1Canonical)
	}
	if hash, err := adrExampleDecoded().ActionHash(); err != nil || hash != vector1ActionHash {
		t.Fatalf("ActionHash() = %q, %v; want %q", hash, err, vector1ActionHash)
	}
}

func TestExecutionRequest_CanonicalJSONMatchesADRVector2(t *testing.T) {
	request := vector2Request()
	canonical, err := request.CanonicalJSON()
	if err != nil {
		t.Fatalf("CanonicalJSON() error = %v", err)
	}
	if len(canonical) != vector2CanonicalBytes {
		t.Fatalf("CanonicalJSON() is %d bytes, want %d: %q", len(canonical), vector2CanonicalBytes, canonical)
	}
	arguments, err := hex.DecodeString(strings.Join(strings.Fields(vector2ArgumentsHex), ""))
	if err != nil {
		t.Fatalf("decode the vector's arguments: %v", err)
	}
	if !bytes.Contains(canonical, arguments) {
		t.Fatalf("CanonicalJSON() = %q, want it to contain the arguments member %q", canonical, arguments)
	}
	if hash, err := request.ActionHash(); err != nil || hash != vector2ActionHash {
		t.Fatalf("ActionHash() = %q, %v; want %q", hash, err, vector2ActionHash)
	}
}

func TestSecurity_CanonicalJSONEscapesExactlyAsRFC8785(t *testing.T) {
	// Every C0 control except NUL (which ADR-007 forbids), then characters
	// that other encoders escape but JCS writes literally.
	var argument, want strings.Builder
	want.WriteString(`"`)
	for char := rune(1); char < 0x20; char++ {
		argument.WriteRune(char)
		switch char {
		case '\b':
			want.WriteString(`\b`)
		case '\t':
			want.WriteString(`\t`)
		case '\n':
			want.WriteString(`\n`)
		case '\f':
			want.WriteString(`\f`)
		case '\r':
			want.WriteString(`\r`)
		default:
			fmt.Fprintf(&want, `\u%04x`, char)
		}
	}
	literal := "\u007f<a&b>/\u2028\u2029é😀 "
	argument.WriteString(`"\` + literal)
	want.WriteString(`\"\\` + literal + `"`)

	request := adrExampleDecoded()
	request.Command.Arguments = []string{argument.String()}
	canonical, err := request.CanonicalJSON()
	if err != nil {
		t.Fatalf("CanonicalJSON() error = %v", err)
	}
	if member := `"arguments":[` + want.String() + `]`; !bytes.Contains(canonical, []byte(member)) {
		t.Fatalf("CanonicalJSON() = %q, want it to contain %q", canonical, member)
	}
}

func TestExecutionRequest_CanonicalJSONRoundTrips(t *testing.T) {
	for name, request := range map[string]ExecutionRequest{
		"vector 1":     adrExampleDecoded(),
		"vector 2":     vector2Request(),
		"no arguments": func() ExecutionRequest { r := adrExampleDecoded(); r.Command.Arguments = nil; return r }(),
	} {
		t.Run(name, func(t *testing.T) {
			canonical, err := request.CanonicalJSON()
			if err != nil {
				t.Fatalf("CanonicalJSON() error = %v", err)
			}
			decoded, err := DecodeExecutionRequest(canonical)
			if err != nil {
				t.Fatalf("DecodeExecutionRequest(canonical) error = %v", err)
			}
			if request.Command.Arguments == nil {
				request.Command.Arguments = []string{}
			}
			if !reflect.DeepEqual(decoded, request) {
				t.Fatalf("DecodeExecutionRequest(canonical) = %+v, want %+v", decoded, request)
			}
			again, err := decoded.CanonicalJSON()
			if err != nil || !bytes.Equal(again, canonical) {
				t.Fatalf("canonicalizing again = %q, %v; want the same bytes %q", again, err, canonical)
			}
		})
	}
}

func TestSecurity_ActionHashChangesWithEveryField(t *testing.T) {
	// shell and network have a single valid value in version 1, so no valid
	// request can differ in them; the vectors pin their canonical encoding.
	changes := map[string]func(*ExecutionRequest){
		"executionId":              func(r *ExecutionRequest) { r.ExecutionID = "exe_01HZX4" },
		"principals.userId":        func(r *ExecutionRequest) { r.Principals.UserID = "usr_2" },
		"principals.agentId":       func(r *ExecutionRequest) { r.Principals.AgentID = "agt_2" },
		"principals.clientId":      func(r *ExecutionRequest) { r.Principals.ClientID = "cli_2" },
		"resources.machineId":      func(r *ExecutionRequest) { r.Resources.MachineID = "mch_2" },
		"resources.workspaceId":    func(r *ExecutionRequest) { r.Resources.WorkspaceID = "3f2a9c0b7d1e4f6a8b5c2d0e1f3a4b6d" },
		"grantId":                  func(r *ExecutionRequest) { r.GrantID = "grt_2" },
		"sessionId":                func(r *ExecutionRequest) { r.SessionID = "ses_2" },
		"command.executable":       func(r *ExecutionRequest) { r.Command.Executable = "/usr/local/go/bin/go" },
		"arguments joined":         func(r *ExecutionRequest) { r.Command.Arguments = []string{"test ./..."} },
		"arguments with comma":     func(r *ExecutionRequest) { r.Command.Arguments = []string{`test","./...`} },
		"arguments extra empty":    func(r *ExecutionRequest) { r.Command.Arguments = []string{"test", "./...", ""} },
		"arguments reordered":      func(r *ExecutionRequest) { r.Command.Arguments = []string{"./...", "test"} },
		"arguments none":           func(r *ExecutionRequest) { r.Command.Arguments = []string{} },
		"arguments changed":        func(r *ExecutionRequest) { r.Command.Arguments = []string{"test", "./.."} },
		"command.workingDirectory": func(r *ExecutionRequest) { r.Command.WorkingDirectory = "src" },
		"workspaceAccess":          func(r *ExecutionRequest) { r.WorkspaceAccess = WorkspaceReadWrite },
		"limits.timeoutSeconds":    func(r *ExecutionRequest) { r.Limits.TimeoutSeconds++ },
		"limits.cpuMillicores":     func(r *ExecutionRequest) { r.Limits.CPUMillicores++ },
		"limits.memoryBytes":       func(r *ExecutionRequest) { r.Limits.MemoryBytes++ },
		"limits.pids":              func(r *ExecutionRequest) { r.Limits.PIDs++ },
		"limits.outputBytes":       func(r *ExecutionRequest) { r.Limits.OutputBytes++ },
		"reason":                   func(r *ExecutionRequest) { r.Reason += "." },
		"createdAt":                func(r *ExecutionRequest) { r.CreatedAt = r.CreatedAt.Add(-1e9) },
		"expiresAt":                func(r *ExecutionRequest) { r.ExpiresAt = r.ExpiresAt.Add(1e9) },
		"nonce":                    func(r *ExecutionRequest) { r.Nonce = "kJ3b9sR0cT1uV2wX3yZ4aQ" },
	}
	seen := map[string]string{vector1ActionHash: "the unchanged request"}
	for _, name := range slices.Sorted(maps.Keys(changes)) {
		request := adrExampleDecoded()
		changes[name](&request)
		if err := request.Validate(); err != nil {
			t.Fatalf("%s: the changed request is invalid: %v", name, err)
		}
		hash, err := request.ActionHash()
		if err != nil {
			t.Fatalf("%s: ActionHash() error = %v", name, err)
		}
		if other, ok := seen[hash]; ok {
			t.Fatalf("%s: ActionHash() = %s, the same as for %s", name, hash, other)
		}
		seen[hash] = name
	}
}

func TestExecutionRequest_ActionHashIgnoresTransmissionFormat(t *testing.T) {
	var compact, indented bytes.Buffer
	if err := json.Compact(&compact, []byte(adrExampleRequest)); err != nil {
		t.Fatalf("json.Compact() error = %v", err)
	}
	if err := json.Indent(&indented, compact.Bytes(), "", "\t\t"); err != nil {
		t.Fatalf("json.Indent() error = %v", err)
	}
	escaped := strings.NewReplacer(`"go"`, `"\u0067o"`, `"./..."`, `".\/..."`, `after the fix`, `after the fi\u0078`).Replace(adrExampleRequest)
	for name, payload := range map[string][]byte{
		"as in ADR-007":  []byte(adrExampleRequest),
		"compact":        compact.Bytes(),
		"re-indented":    indented.Bytes(),
		"keys reordered": marshalFields(t, requestFields(t)),
		"escaped":        []byte(escaped),
	} {
		t.Run(name, func(t *testing.T) {
			request, err := DecodeExecutionRequest(payload)
			if err != nil {
				t.Fatalf("DecodeExecutionRequest() error = %v", err)
			}
			if hash, err := request.ActionHash(); err != nil || hash != vector1ActionHash {
				t.Fatalf("ActionHash() = %q, %v; want %q", hash, err, vector1ActionHash)
			}
		})
	}
}

func TestExecutionRequest_CanonicalJSONRejectsInvalidRequests(t *testing.T) {
	invalid := adrExampleDecoded()
	invalid.Command.Shell = true
	for name, request := range map[string]ExecutionRequest{"zero value": {}, "shell": invalid} {
		t.Run(name, func(t *testing.T) {
			if canonical, err := request.CanonicalJSON(); !errors.Is(err, ErrInvalidExecutionRequest) || canonical != nil {
				t.Fatalf("CanonicalJSON() = %q, %v; want nil and ErrInvalidExecutionRequest", canonical, err)
			}
			if hash, err := request.ActionHash(); !errors.Is(err, ErrInvalidExecutionRequest) || hash != "" {
				t.Fatalf("ActionHash() = %q, %v; want \"\" and ErrInvalidExecutionRequest", hash, err)
			}
		})
	}
}

// checkCanonicalForm verifies ADR-008's properties for an accepted payload:
// the canonical form equals an independent RFC 8785 rendering of the payload,
// decodes to the same request, is stable, and is what the action hash covers.
func checkCanonicalForm(t *testing.T, payload []byte, request ExecutionRequest) {
	t.Helper()
	canonical, err := request.CanonicalJSON()
	if err != nil {
		t.Fatalf("CanonicalJSON() of an accepted request error = %v", err)
	}
	if want := jcsReference(t, payload); !bytes.Equal(canonical, want) {
		t.Fatalf("CanonicalJSON() = %q, want the RFC 8785 rendering %q", canonical, want)
	}
	decoded, err := DecodeExecutionRequest(canonical)
	if err != nil || !reflect.DeepEqual(decoded, request) {
		t.Fatalf("DecodeExecutionRequest(canonical) = %+v, %v; want %+v", decoded, err, request)
	}
	if again, err := decoded.CanonicalJSON(); err != nil || !bytes.Equal(again, canonical) {
		t.Fatalf("canonicalizing again = %q, %v; want %q", again, err, canonical)
	}
	sum := sha256.Sum256(append([]byte(testActionHashLabel), canonical...))
	if hash, err := request.ActionHash(); err != nil || hash != hex.EncodeToString(sum[:]) {
		t.Fatalf("ActionHash() = %q, %v; want %x", hash, err, sum)
	}
}

// FuzzCanonicalFieldValues places fuzzed text and numbers into an otherwise
// valid request, so that most inputs are valid requests with arbitrary
// content. checkCanonicalForm then compares the canonical form with the
// RFC 8785 reference and checks the round trip and the hash.
func FuzzCanonicalFieldValues(f *testing.F) {
	f.Add("test", "./...", "go", ".", "Run the unit tests after the fix", uint64(600))
	f.Add("tab\there", "\u2028\x7f<&>\"\\", "./gradlew", "src/app", "Tests für Ümlaute 😀", uint64(1))
	f.Add("", "\x01\x1f", "/usr/bin/go", "a/b/c", "x", uint64(86400))

	f.Fuzz(func(t *testing.T, first, second, executable, workingDirectory, reason string, timeoutSeconds uint64) {
		request := adrExampleDecoded()
		request.Command.Arguments = []string{first, second}
		request.Command.Executable = executable
		request.Command.WorkingDirectory = workingDirectory
		request.Reason = reason
		request.Limits.TimeoutSeconds = timeoutSeconds
		if request.Validate() != nil {
			return
		}
		payload := encodeTestRequest(t, request)
		decoded, err := DecodeExecutionRequest(payload)
		if err != nil {
			t.Fatalf("DecodeExecutionRequest() of a valid request error = %v; payload %q", err, payload)
		}
		if !reflect.DeepEqual(decoded, request) {
			t.Fatalf("DecodeExecutionRequest() = %+v, want %+v", decoded, request)
		}
		checkCanonicalForm(t, payload, decoded)
	})
}

// jcsReference renders any JSON document in the value domain of ADR-007 with
// RFC 8785 rules: keys sorted, no whitespace, minimal string escaping, and
// numbers as written (accepted payloads hold only plain integers). It is an
// independent reference for CanonicalJSON.
func jcsReference(t *testing.T, payload []byte) []byte {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	var document any
	if err := decoder.Decode(&document); err != nil {
		t.Fatalf("decode the payload: %v", err)
	}
	var out strings.Builder
	writeString := func(s string) {
		out.WriteByte('"')
		for _, char := range s {
			switch {
			case char == '"' || char == '\\':
				out.WriteByte('\\')
				out.WriteRune(char)
			case char == '\b':
				out.WriteString(`\b`)
			case char == '\t':
				out.WriteString(`\t`)
			case char == '\n':
				out.WriteString(`\n`)
			case char == '\f':
				out.WriteString(`\f`)
			case char == '\r':
				out.WriteString(`\r`)
			case char < 0x20:
				fmt.Fprintf(&out, `\u%04x`, char)
			default:
				out.WriteRune(char)
			}
		}
		out.WriteByte('"')
	}
	var write func(value any)
	write = func(value any) {
		switch value := value.(type) {
		case map[string]any:
			out.WriteByte('{')
			for i, key := range slices.Sorted(maps.Keys(value)) {
				if i > 0 {
					out.WriteByte(',')
				}
				writeString(key)
				out.WriteByte(':')
				write(value[key])
			}
			out.WriteByte('}')
		case []any:
			out.WriteByte('[')
			for i, element := range value {
				if i > 0 {
					out.WriteByte(',')
				}
				write(element)
			}
			out.WriteByte(']')
		case string:
			writeString(value)
		case json.Number:
			out.WriteString(value.String())
		case bool:
			out.WriteString(strconv.FormatBool(value))
		default:
			t.Fatalf("unexpected JSON value %T in an accepted payload", value)
		}
	}
	write(document)
	return []byte(out.String())
}
