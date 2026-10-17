package protocol

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// cancelExample is ADR-009's execution.cancel example, byte for byte.
const cancelExample = `{"executionId": "exe_01HZX3", "actionHash": "b4f47c942fc6499aea49d76df1f89c630f8683bd69c98f292b9e03eed4dd44e2", "principals": {"userId": "usr_1", "agentId": "agt_1", "clientId": "cli_1"}, "reason": "The user stopped the run"}`

// cancelCanonical was rendered independently with Python's json module
// (sorted keys, compact separators, ensure_ascii=False).
const cancelCanonical = `{"actionHash":"b4f47c942fc6499aea49d76df1f89c630f8683bd69c98f292b9e03eed4dd44e2","executionId":"exe_01HZX3","principals":{"agentId":"agt_1","clientId":"cli_1","userId":"usr_1"},"reason":"The user stopped the run"}`

func cancelExampleDecoded() ExecutionCancel {
	return ExecutionCancel{
		ExecutionID: "exe_01HZX3",
		ActionHash:  vector1ActionHash,
		Principals:  Principals{UserID: "usr_1", AgentID: "agt_1", ClientID: "cli_1"},
		Reason:      "The user stopped the run",
	}
}

func TestDecodeExecutionCancel_AcceptsADRExample(t *testing.T) {
	cancel, err := DecodeExecutionCancel([]byte(cancelExample))
	if err != nil {
		t.Fatalf("DecodeExecutionCancel() error = %v", err)
	}
	if want := cancelExampleDecoded(); !reflect.DeepEqual(cancel, want) {
		t.Fatalf("DecodeExecutionCancel() = %+v, want %+v", cancel, want)
	}
}

func TestExecutionCancel_CanonicalJSON(t *testing.T) {
	canonical, err := cancelExampleDecoded().CanonicalJSON()
	if err != nil || string(canonical) != cancelCanonical {
		t.Fatalf("CanonicalJSON() = %s, %v; want %s", canonical, err, cancelCanonical)
	}
	checkCanonicalCancel(t, []byte(cancelExample), cancelExampleDecoded())
	if canonical, err := (ExecutionCancel{}).CanonicalJSON(); !errors.Is(err, ErrInvalidExecutionCancel) || canonical != nil {
		t.Fatalf("CanonicalJSON() of the zero value = %q, %v; want nil and ErrInvalidExecutionCancel", canonical, err)
	}
}

func TestSecurity_DecodeExecutionCancelEnforcesFieldRules(t *testing.T) {
	long := func(n int) string { return strings.Repeat("a", n) }
	tests := []struct {
		path   string
		value  any
		accept bool
	}{
		{path: "executionId", value: long(128), accept: true},
		{path: "executionId", value: long(129)},
		{path: "executionId", value: ""},
		{path: "executionId", value: "exe 1"},
		{path: "executionId", value: 1},

		{path: "actionHash", value: strings.Repeat("0", 64), accept: true},
		{path: "actionHash", value: strings.Repeat("a", 63)},
		{path: "actionHash", value: strings.Repeat("a", 65)},
		{path: "actionHash", value: strings.Repeat("A", 64)},
		{path: "actionHash", value: strings.Repeat("g", 64)},
		{path: "actionHash", value: true},

		{path: "principals.userId", value: long(128), accept: true},
		{path: "principals.userId", value: ""},
		{path: "principals.userId", value: "usr/1"},
		{path: "principals.userId", value: long(129)},
		{path: "principals.userId", value: 1},
		{path: "principals.agentId", value: " agt"},
		{path: "principals.agentId", value: ""},
		{path: "principals.clientId", value: "cli\t1"},
		{path: "principals.clientId", value: ""},

		{path: "reason", value: long(1024), accept: true},
		{path: "reason", value: "Abbruch durch die Benutzerin 😀", accept: true},
		{path: "reason", value: long(1025)},
		{path: "reason", value: ""},
		{path: "reason", value: "line\nbreak"},
		{path: "reason", value: "\u202eevil"},
		{path: "reason", value: "\u200fhidden"},
		{path: "reason", value: "line\u2028separator"},
		{path: "reason", value: 1},
	}
	for i, test := range tests {
		t.Run(fmt.Sprintf("%s/%d", test.path, i), func(t *testing.T) {
			fields := documentFields(t, cancelExample)
			setField(t, fields, test.path, test.value)
			cancel, err := DecodeExecutionCancel(marshalFields(t, fields))
			if test.accept {
				if err != nil {
					t.Fatalf("%s = %.40v: error = %v, want acceptance", test.path, test.value, err)
				}
				return
			}
			if !errors.Is(err, ErrInvalidExecutionCancel) || !strings.Contains(err.Error(), test.path) {
				t.Fatalf("%s = %.40v: error = %v, want ErrInvalidExecutionCancel naming %s", test.path, test.value, err, test.path)
			}
			if !reflect.DeepEqual(cancel, ExecutionCancel{}) {
				t.Fatalf("%s = %.40v: returned a partial value %+v", test.path, test.value, cancel)
			}
		})
	}
}

func TestDecodeExecutionCancel_RequiresExactKeys(t *testing.T) {
	for _, path := range []string{"executionId", "actionHash", "principals", "reason", "principals.userId", "principals.agentId", "principals.clientId"} {
		t.Run("without "+path, func(t *testing.T) {
			fields := documentFields(t, cancelExample)
			object, key := parentOf(t, fields, path)
			delete(object, key)
			if _, err := DecodeExecutionCancel(marshalFields(t, fields)); !errors.Is(err, ErrInvalidExecutionCancel) || !strings.Contains(err.Error(), path+": is required") {
				t.Fatalf("error = %v, want ErrInvalidExecutionCancel saying %s is required", err, path)
			}
		})
	}
	for _, path := range []string{"extra", "startedAt", "principals.extra", "principals.machineId"} {
		t.Run("with "+path, func(t *testing.T) {
			fields := documentFields(t, cancelExample)
			object, key := parentOf(t, fields, path)
			object[key] = "x"
			if _, err := DecodeExecutionCancel(marshalFields(t, fields)); !errors.Is(err, ErrInvalidExecutionCancel) || !strings.Contains(err.Error(), path) {
				t.Fatalf("error = %v, want ErrInvalidExecutionCancel naming %s", err, path)
			}
		})
	}
	t.Run("principals not an object", func(t *testing.T) {
		fields := documentFields(t, cancelExample)
		fields["principals"] = []any{"usr_1"}
		if _, err := DecodeExecutionCancel(marshalFields(t, fields)); !errors.Is(err, ErrInvalidExecutionCancel) || !strings.Contains(err.Error(), "principals") {
			t.Fatalf("error = %v, want ErrInvalidExecutionCancel naming principals", err)
		}
	})
}

func TestSecurity_DecodeExecutionCancelRejectsAmbiguousJSON(t *testing.T) {
	checkAmbiguousJSON(t, []byte(cancelExample), `"executionId": "exe_01HZX3"`, `"actionHash"`, "exe_01HZX3",
		func(payload []byte) (any, error) { return DecodeExecutionCancel(payload) },
		ErrInvalidExecutionCancel, ExecutionCancel{})
	for name, payload := range map[string]string{
		"nested null":      strings.Replace(cancelExample, `"userId": "usr_1"`, `"userId": null`, 1),
		"nested duplicate": strings.Replace(cancelExample, `"userId": "usr_1"`, `"userId": "usr_1", "userId": "usr_2"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeExecutionCancel([]byte(payload)); !errors.Is(err, ErrInvalidExecutionCancel) || !strings.Contains(err.Error(), "principals.userId") {
				t.Fatalf("error = %v, want ErrInvalidExecutionCancel naming principals.userId", err)
			}
		})
	}
}

func TestExecutionCancel_ValidateAgainstTheRequest(t *testing.T) {
	if err := cancelExampleDecoded().ValidateAgainst(adrExampleDecoded()); err != nil {
		t.Fatalf("ValidateAgainst() error = %v", err)
	}
	// ADR-009: the canceller may differ from the requester.
	byAnotherUser := cancelExampleDecoded()
	byAnotherUser.Principals = Principals{UserID: "usr_2", AgentID: "agt_2", ClientID: "cli_2"}
	if err := byAnotherUser.ValidateAgainst(adrExampleDecoded()); err != nil {
		t.Fatalf("ValidateAgainst() of a cancel by other principals error = %v, want nil", err)
	}
	tests := []struct {
		name    string
		change  func(*ExecutionCancel)
		request func(*ExecutionRequest)
		wantErr error
		field   string
	}{
		{name: "another execution", change: func(c *ExecutionCancel) { c.ExecutionID = "exe_01HZX4" }, wantErr: ErrInvalidExecutionCancel, field: "executionId"},
		{name: "another action", change: func(c *ExecutionCancel) { c.ActionHash = strings.Repeat("0", 64) }, wantErr: ErrInvalidExecutionCancel, field: "actionHash"},
		{name: "a changed request", request: func(r *ExecutionRequest) { r.Reason += "." }, wantErr: ErrInvalidExecutionCancel, field: "actionHash"},
		{name: "an invalid cancel", change: func(c *ExecutionCancel) { c.Reason = "" }, wantErr: ErrInvalidExecutionCancel, field: "reason"},
		{name: "an invalid request", request: func(r *ExecutionRequest) { r.Command.Shell = true }, wantErr: ErrInvalidExecutionRequest, field: "command.shell"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cancel, request := cancelExampleDecoded(), adrExampleDecoded()
			if test.change != nil {
				test.change(&cancel)
			}
			if test.request != nil {
				test.request(&request)
			}
			if err := cancel.ValidateAgainst(request); !errors.Is(err, test.wantErr) || !strings.Contains(err.Error(), test.field) {
				t.Fatalf("ValidateAgainst() error = %v, want %v naming %s", err, test.wantErr, test.field)
			}
		})
	}
}

func TestExecutionCancel_ValidateChecksTypedValues(t *testing.T) {
	tests := []struct {
		name   string
		change func(*ExecutionCancel)
		field  string
	}{
		{name: "zero value", change: func(c *ExecutionCancel) { *c = ExecutionCancel{} }, field: "executionId"},
		{name: "uppercase action hash", change: func(c *ExecutionCancel) { c.ActionHash = strings.ToUpper(c.ActionHash) }, field: "actionHash"},
		{name: "empty client", change: func(c *ExecutionCancel) { c.Principals.ClientID = "" }, field: "principals.clientId"},
		{name: "invalid UTF-8 in the reason", change: func(c *ExecutionCancel) { c.Reason = "\xff" }, field: "reason"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cancel := cancelExampleDecoded()
			test.change(&cancel)
			if err := cancel.Validate(); !errors.Is(err, ErrInvalidExecutionCancel) || !strings.Contains(err.Error(), test.field) {
				t.Fatalf("Validate() error = %v, want ErrInvalidExecutionCancel naming %s", err, test.field)
			}
		})
	}
}

func FuzzDecodeExecutionCancel(f *testing.F) {
	f.Add([]byte(cancelExample))
	f.Add([]byte(cancelCanonical))
	f.Add([]byte(strings.Replace(cancelExample, "The user stopped the run", `Abbruch \u00e9\ud83d\ude00 <&>`, 1)))
	f.Fuzz(func(t *testing.T, payload []byte) {
		cancel, err := DecodeExecutionCancel(payload)
		if err != nil {
			if !errors.Is(err, ErrInvalidExecutionCancel) {
				t.Fatalf("DecodeExecutionCancel() error %v does not wrap ErrInvalidExecutionCancel", err)
			}
			return
		}
		checkCanonicalCancel(t, payload, cancel)
	})
}

func checkCanonicalCancel(t *testing.T, payload []byte, cancel ExecutionCancel) {
	t.Helper()
	canonical, err := cancel.CanonicalJSON()
	if err != nil {
		t.Fatalf("CanonicalJSON() of an accepted value error = %v", err)
	}
	if want := jcsReference(t, payload); !bytes.Equal(canonical, want) {
		t.Fatalf("CanonicalJSON() = %q, want the RFC 8785 rendering %q", canonical, want)
	}
	if decoded, err := DecodeExecutionCancel(canonical); err != nil || !reflect.DeepEqual(decoded, cancel) {
		t.Fatalf("DecodeExecutionCancel(canonical) = %+v, %v; want %+v", decoded, err, cancel)
	}
}
