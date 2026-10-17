package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// startedExample is ADR-009's execution.started example, byte for byte.
const startedExample = `{"executionId": "exe_01HZX3", "actionHash": "b4f47c942fc6499aea49d76df1f89c630f8683bd69c98f292b9e03eed4dd44e2", "startedAt": "2026-10-16T12:00:01.250Z"}`

// startedCanonical was rendered independently with Python's json module
// (sorted keys, compact separators, ensure_ascii=False).
const startedCanonical = `{"actionHash":"b4f47c942fc6499aea49d76df1f89c630f8683bd69c98f292b9e03eed4dd44e2","executionId":"exe_01HZX3","startedAt":"2026-10-16T12:00:01.250Z"}`

func startedExampleDecoded() ExecutionStarted {
	return ExecutionStarted{
		ExecutionID: "exe_01HZX3",
		ActionHash:  vector1ActionHash,
		StartedAt:   time.Date(2026, time.October, 16, 12, 0, 1, 250*int(time.Millisecond), time.UTC),
	}
}

func TestDecodeExecutionStarted_AcceptsADRExample(t *testing.T) {
	started, err := DecodeExecutionStarted([]byte(startedExample))
	if err != nil {
		t.Fatalf("DecodeExecutionStarted() error = %v", err)
	}
	if want := startedExampleDecoded(); !reflect.DeepEqual(started, want) {
		t.Fatalf("DecodeExecutionStarted() = %+v, want %+v", started, want)
	}
}

func TestExecutionStarted_CanonicalJSON(t *testing.T) {
	canonical, err := startedExampleDecoded().CanonicalJSON()
	if err != nil || string(canonical) != startedCanonical {
		t.Fatalf("CanonicalJSON() = %s, %v; want %s", canonical, err, startedCanonical)
	}
	checkCanonicalStarted(t, []byte(startedExample), startedExampleDecoded())
	if canonical, err := (ExecutionStarted{}).CanonicalJSON(); !errors.Is(err, ErrInvalidExecutionStarted) || canonical != nil {
		t.Fatalf("CanonicalJSON() of the zero value = %q, %v; want nil and ErrInvalidExecutionStarted", canonical, err)
	}
}

func TestSecurity_DecodeExecutionStartedEnforcesFieldRules(t *testing.T) {
	tests := []struct {
		path   string
		value  any
		accept bool
	}{
		{path: "executionId", value: strings.Repeat("a", 128), accept: true},
		{path: "executionId", value: strings.Repeat("a", 129)},
		{path: "executionId", value: ""},
		{path: "executionId", value: "exe 1"},
		{path: "executionId", value: 1},
		{path: "actionHash", value: strings.Repeat("0", 64), accept: true},
		{path: "actionHash", value: strings.Repeat("a", 63)},
		{path: "actionHash", value: strings.Repeat("a", 65)},
		{path: "actionHash", value: strings.Repeat("A", 64)},
		{path: "actionHash", value: strings.Repeat("g", 64)},
		{path: "actionHash", value: true},
		{path: "startedAt", value: "2026-10-16T12:00:01.000Z", accept: true},
		{path: "startedAt", value: "2026-10-16T12:00:01Z"},
		{path: "startedAt", value: "2026-10-16T12:00:01.25Z"},
		{path: "startedAt", value: "2026-10-16T12:00:01.2500Z"},
		{path: "startedAt", value: "2026-10-16T12:00:01,250Z"},
		{path: "startedAt", value: "2026-10-16T12:00:01.250+00:00"},
		{path: "startedAt", value: "2026-10-16T12:00:01.250z"},
		{path: "startedAt", value: ""},
		{path: "startedAt", value: 1760616001},
	}
	for i, test := range tests {
		t.Run(fmt.Sprintf("%s/%d", test.path, i), func(t *testing.T) {
			fields := documentFields(t, startedExample)
			setField(t, fields, test.path, test.value)
			started, err := DecodeExecutionStarted(marshalFields(t, fields))
			if test.accept {
				if err != nil {
					t.Fatalf("%s = %v: error = %v, want acceptance", test.path, test.value, err)
				}
				return
			}
			if !errors.Is(err, ErrInvalidExecutionStarted) || !strings.Contains(err.Error(), test.path) {
				t.Fatalf("%s = %v: error = %v, want ErrInvalidExecutionStarted naming %s", test.path, test.value, err, test.path)
			}
			if !reflect.DeepEqual(started, ExecutionStarted{}) {
				t.Fatalf("%s = %v: returned a partial value %+v", test.path, test.value, started)
			}
		})
	}
}

func TestDecodeExecutionStarted_RequiresExactKeys(t *testing.T) {
	for _, key := range []string{"executionId", "actionHash", "startedAt"} {
		t.Run("without "+key, func(t *testing.T) {
			fields := documentFields(t, startedExample)
			delete(fields, key)
			if _, err := DecodeExecutionStarted(marshalFields(t, fields)); !errors.Is(err, ErrInvalidExecutionStarted) || !strings.Contains(err.Error(), key+": is required") {
				t.Fatalf("error = %v, want ErrInvalidExecutionStarted saying %s is required", err, key)
			}
		})
	}
	for _, key := range []string{"endedAt", "sequence", "extra"} {
		t.Run("with "+key, func(t *testing.T) {
			fields := documentFields(t, startedExample)
			fields[key] = 1
			if _, err := DecodeExecutionStarted(marshalFields(t, fields)); !errors.Is(err, ErrInvalidExecutionStarted) || !strings.Contains(err.Error(), key) {
				t.Fatalf("error = %v, want ErrInvalidExecutionStarted naming %s", err, key)
			}
		})
	}
}

func TestSecurity_DecodeExecutionStartedRejectsAmbiguousJSON(t *testing.T) {
	checkAmbiguousJSON(t, []byte(startedExample), `"executionId": "exe_01HZX3"`, `"startedAt"`, "exe_01HZX3",
		func(payload []byte) (any, error) { return DecodeExecutionStarted(payload) },
		ErrInvalidExecutionStarted, ExecutionStarted{})
}

func TestExecutionStarted_ValidateAgainstTheRequest(t *testing.T) {
	if err := startedExampleDecoded().ValidateAgainst(adrExampleDecoded()); err != nil {
		t.Fatalf("ValidateAgainst() error = %v", err)
	}
	tests := []struct {
		name    string
		change  func(*ExecutionStarted)
		request func(*ExecutionRequest)
		wantErr error
		field   string
	}{
		{name: "another execution", change: func(s *ExecutionStarted) { s.ExecutionID = "exe_01HZX4" }, wantErr: ErrInvalidExecutionStarted, field: "executionId"},
		{name: "another action", change: func(s *ExecutionStarted) { s.ActionHash = strings.Repeat("0", 64) }, wantErr: ErrInvalidExecutionStarted, field: "actionHash"},
		{name: "a changed request", request: func(r *ExecutionRequest) { r.Reason += "." }, wantErr: ErrInvalidExecutionStarted, field: "actionHash"},
		{name: "an invalid value", change: func(s *ExecutionStarted) { s.StartedAt = time.Time{}.Add(time.Nanosecond) }, wantErr: ErrInvalidExecutionStarted, field: "startedAt"},
		{name: "an invalid request", request: func(r *ExecutionRequest) { r.Command.Shell = true }, wantErr: ErrInvalidExecutionRequest, field: "command.shell"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			started, request := startedExampleDecoded(), adrExampleDecoded()
			if test.change != nil {
				test.change(&started)
			}
			if test.request != nil {
				test.request(&request)
			}
			if err := started.ValidateAgainst(request); !errors.Is(err, test.wantErr) || !strings.Contains(err.Error(), test.field) {
				t.Fatalf("ValidateAgainst() error = %v, want %v naming %s", err, test.wantErr, test.field)
			}
		})
	}
}

func TestExecutionStarted_ValidateChecksTypedValues(t *testing.T) {
	tests := []struct {
		name   string
		change func(*ExecutionStarted)
		field  string
	}{
		{name: "zero value", change: func(s *ExecutionStarted) { *s = ExecutionStarted{} }, field: "executionId"},
		{name: "not in UTC", change: func(s *ExecutionStarted) { s.StartedAt = s.StartedAt.In(time.FixedZone("PKT", 5*60*60)) }, field: "startedAt"},
		{name: "below a millisecond", change: func(s *ExecutionStarted) { s.StartedAt = s.StartedAt.Add(time.Microsecond) }, field: "startedAt"},
		{name: "year beyond 9999", change: func(s *ExecutionStarted) { s.StartedAt = time.Date(10000, time.January, 1, 0, 0, 0, 0, time.UTC) }, field: "startedAt"},
		{name: "uppercase action hash", change: func(s *ExecutionStarted) { s.ActionHash = strings.ToUpper(s.ActionHash) }, field: "actionHash"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			started := startedExampleDecoded()
			test.change(&started)
			if err := started.Validate(); !errors.Is(err, ErrInvalidExecutionStarted) || !strings.Contains(err.Error(), test.field) {
				t.Fatalf("Validate() error = %v, want ErrInvalidExecutionStarted naming %s", err, test.field)
			}
		})
	}
}

func FuzzDecodeExecutionStarted(f *testing.F) {
	f.Add([]byte(startedExample))
	f.Add([]byte(startedCanonical))
	f.Fuzz(func(t *testing.T, payload []byte) {
		started, err := DecodeExecutionStarted(payload)
		if err != nil {
			if !errors.Is(err, ErrInvalidExecutionStarted) {
				t.Fatalf("DecodeExecutionStarted() error %v does not wrap ErrInvalidExecutionStarted", err)
			}
			return
		}
		checkCanonicalStarted(t, payload, started)
	})
}

func checkCanonicalStarted(t *testing.T, payload []byte, started ExecutionStarted) {
	t.Helper()
	canonical, err := started.CanonicalJSON()
	if err != nil {
		t.Fatalf("CanonicalJSON() of an accepted value error = %v", err)
	}
	if want := jcsReference(t, payload); !bytes.Equal(canonical, want) {
		t.Fatalf("CanonicalJSON() = %q, want the RFC 8785 rendering %q", canonical, want)
	}
	if decoded, err := DecodeExecutionStarted(canonical); err != nil || !reflect.DeepEqual(decoded, started) {
		t.Fatalf("DecodeExecutionStarted(canonical) = %+v, %v; want %+v", decoded, err, started)
	}
}

// documentFields returns a JSON object document as nested maps.
func documentFields(t testing.TB, document string) map[string]any {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(document))
	decoder.UseNumber()
	var fields map[string]any
	if err := decoder.Decode(&fields); err != nil {
		t.Fatalf("decode the example: %v", err)
	}
	return fields
}

// checkAmbiguousJSON applies the shared strict-document cases to one payload
// type. member is a "key": value pair in example, nextKey a quoted key that
// follows it, and text a string value to corrupt. Every error must wrap
// invalid and no other payload's error value.
func checkAmbiguousJSON(t *testing.T, example []byte, member, nextKey, text string,
	decode func([]byte) (any, error), invalid error, zero any) {
	t.Helper()
	replace := func(old, replacement string) []byte {
		t.Helper()
		if !bytes.Contains(example, []byte(old)) {
			t.Fatalf("the example does not contain %q", old)
		}
		return bytes.Replace(example, []byte(old), []byte(replacement), 1)
	}
	key := member[:strings.Index(member, ":")]
	tests := []struct {
		name     string
		payload  []byte
		wantErr  error
		wantText string
	}{
		{name: "empty", payload: nil, wantText: "payload"},
		{name: "oversized", payload: append(slices.Clone(example), strings.Repeat(" ", MaxPayloadBytes)...), wantErr: ErrPayloadTooLarge},
		{name: "trailing value", payload: append(slices.Clone(example), " {}"...), wantText: "payload"},
		{name: "top-level array", payload: []byte("[" + string(example) + "]"), wantText: "payload"},
		{name: "null value", payload: replace(member, key+": null"), wantText: strings.Trim(key, `"`)},
		{name: "duplicate key", payload: replace(nextKey, member+", "+nextKey), wantErr: ErrDuplicateJSONKey, wantText: strings.Trim(key, `"`)},
		{name: "invalid UTF-8", payload: replace(text, text+"\xff"), wantText: "UTF-8"},
		{name: "lone surrogate", payload: replace(text, text+`\ud800`), wantText: "escape"},
		{name: "escaped NUL", payload: replace(text, text+`\u0000`), wantText: "escape"},
		{name: "nesting over 64 levels", payload: replace(nextKey, `"deep": `+strings.Repeat("[", 70)+strings.Repeat("]", 70)+`, `+nextKey), wantText: "nesting"},
		{name: "control characters in an unknown field", payload: replace(nextKey, `"\u001b[2J": 1, `+nextKey), wantText: `\x1b[2J`},
	}
	others := []error{ErrInvalidExecutionRequest, ErrInvalidExecutionResult, ErrInvalidExecutionStarted, ErrInvalidExecutionOutput, ErrInvalidExecutionCancel}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value, err := decode(test.payload)
			if !errors.Is(err, invalid) || (test.wantErr != nil && !errors.Is(err, test.wantErr)) {
				t.Fatalf("error = %v, want %v (and %v)", err, invalid, test.wantErr)
			}
			for _, other := range others {
				if other != invalid && errors.Is(err, other) {
					t.Fatalf("error = %v also wraps %v", err, other)
				}
			}
			if !strings.Contains(err.Error(), test.wantText) || strings.ContainsRune(err.Error(), '\x1b') {
				t.Fatalf("error = %q, want it to contain %q and no raw control character", err, test.wantText)
			}
			if !reflect.DeepEqual(value, zero) {
				t.Fatalf("returned a partial value %+v", value)
			}
		})
	}
}
