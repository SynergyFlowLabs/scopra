package protocol

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// outputExample is ADR-009's execution.output example, byte for byte.
const outputExample = `{"executionId": "exe_01HZX3", "sequence": 1, "stream": "stdout", "data": "b2sgIAlnaXRodWIuY29tL1N5bmVyZ3lGbG93TGFicy9zY29wcmEvcGFja2FnZXMvcHJvdG9jb2wJMC4wNjBzCg"}`

// outputCanonical was rendered independently with Python's json module
// (sorted keys, compact separators, ensure_ascii=False).
const outputCanonical = `{"data":"b2sgIAlnaXRodWIuY29tL1N5bmVyZ3lGbG93TGFicy9zY29wcmEvcGFja2FnZXMvcHJvdG9jb2wJMC4wNjBzCg","executionId":"exe_01HZX3","sequence":1,"stream":"stdout"}`

func outputExampleDecoded() ExecutionOutput {
	return ExecutionOutput{
		ExecutionID: "exe_01HZX3",
		Sequence:    1,
		Stream:      StreamStdout,
		Data:        []byte("ok  \tgithub.com/SynergyFlowLabs/scopra/packages/protocol\t0.060s\n"),
	}
}

func TestDecodeExecutionOutput_AcceptsADRExample(t *testing.T) {
	output, err := DecodeExecutionOutput([]byte(outputExample))
	if err != nil {
		t.Fatalf("DecodeExecutionOutput() error = %v", err)
	}
	if want := outputExampleDecoded(); !reflect.DeepEqual(output, want) {
		t.Fatalf("DecodeExecutionOutput() = %+v, want %+v", output, want)
	}
}

func TestExecutionOutput_CanonicalJSON(t *testing.T) {
	canonical, err := outputExampleDecoded().CanonicalJSON()
	if err != nil || string(canonical) != outputCanonical {
		t.Fatalf("CanonicalJSON() = %s, %v; want %s", canonical, err, outputCanonical)
	}
	checkCanonicalOutput(t, []byte(outputExample), outputExampleDecoded())
	if canonical, err := (ExecutionOutput{}).CanonicalJSON(); !errors.Is(err, ErrInvalidExecutionOutput) || canonical != nil {
		t.Fatalf("CanonicalJSON() of the zero value = %q, %v; want nil and ErrInvalidExecutionOutput", canonical, err)
	}
}

func TestExecutionOutput_KeepsBinaryDataExact(t *testing.T) {
	every := make([]byte, 256)
	for i := range every {
		every[i] = byte(i)
	}
	for name, data := range map[string][]byte{"every byte value": every, "one NUL byte": {0}, "invalid UTF-8": []byte("\xff\xfe\xc3")} {
		t.Run(name, func(t *testing.T) {
			output := outputExampleDecoded()
			output.Stream, output.Data = StreamStderr, data
			canonical, err := output.CanonicalJSON()
			if err != nil {
				t.Fatalf("CanonicalJSON() error = %v", err)
			}
			if encoded := `"data":"` + base64.RawURLEncoding.EncodeToString(data) + `"`; !bytes.Contains(canonical, []byte(encoded)) {
				t.Fatalf("CanonicalJSON() = %s, want it to contain %s", canonical, encoded)
			}
			decoded, err := DecodeExecutionOutput(canonical)
			if err != nil || !reflect.DeepEqual(decoded, output) {
				t.Fatalf("DecodeExecutionOutput(canonical) = %+v, %v; want %+v", decoded, err, output)
			}
		})
	}
}

func TestSecurity_DecodeExecutionOutputEnforcesFieldRules(t *testing.T) {
	encode := func(n int) string { return base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x5a}, n)) }
	tests := []struct {
		path   string
		value  any
		accept bool
	}{
		{path: "executionId", value: strings.Repeat("a", 128), accept: true},
		{path: "executionId", value: strings.Repeat("a", 129)},
		{path: "executionId", value: ""},
		{path: "executionId", value: 1},

		{path: "sequence", value: 1 << 30, accept: true},
		{path: "sequence", value: 0},
		{path: "sequence", value: 1<<30 + 1},
		{path: "sequence", value: json.RawMessage("-1")},
		{path: "sequence", value: json.RawMessage("1.0")},
		{path: "sequence", value: json.RawMessage("1e0")},
		{path: "sequence", value: "1"},

		{path: "stream", value: "stderr", accept: true},
		{path: "stream", value: "STDOUT"},
		{path: "stream", value: "stdin"},
		{path: "stream", value: ""},
		{path: "stream", value: 1},

		{path: "data", value: "AA", accept: true},
		{path: "data", value: "-_8", accept: true},
		{path: "data", value: encode(65536), accept: true},
		{path: "data", value: encode(65537)},
		{path: "data", value: ""},
		{path: "data", value: "AA=="},
		{path: "data", value: "+/8"},
		{path: "data", value: "AB"},
		{path: "data", value: "AA AA"},
		{path: "data", value: "AA\nAA"},
		{path: "data", value: "A"},
		{path: "data", value: 5},
	}
	for i, test := range tests {
		t.Run(fmt.Sprintf("%s/%d", test.path, i), func(t *testing.T) {
			fields := documentFields(t, outputExample)
			setField(t, fields, test.path, test.value)
			output, err := DecodeExecutionOutput(marshalFields(t, fields))
			if test.accept {
				if err != nil {
					t.Fatalf("%s = %.40v: error = %v, want acceptance", test.path, test.value, err)
				}
				return
			}
			if !errors.Is(err, ErrInvalidExecutionOutput) || !strings.Contains(err.Error(), test.path) {
				t.Fatalf("%s = %.40v: error = %v, want ErrInvalidExecutionOutput naming %s", test.path, test.value, err, test.path)
			}
			if !reflect.DeepEqual(output, ExecutionOutput{}) {
				t.Fatalf("%s = %.40v: returned a partial value %+v", test.path, test.value, output)
			}
		})
	}
}

func TestDecodeExecutionOutput_RequiresExactKeys(t *testing.T) {
	for _, key := range []string{"executionId", "sequence", "stream", "data"} {
		t.Run("without "+key, func(t *testing.T) {
			fields := documentFields(t, outputExample)
			delete(fields, key)
			if _, err := DecodeExecutionOutput(marshalFields(t, fields)); !errors.Is(err, ErrInvalidExecutionOutput) || !strings.Contains(err.Error(), key+": is required") {
				t.Fatalf("error = %v, want ErrInvalidExecutionOutput saying %s is required", err, key)
			}
		})
	}
	for _, key := range []string{"actionHash", "truncated", "extra"} {
		t.Run("with "+key, func(t *testing.T) {
			fields := documentFields(t, outputExample)
			fields[key] = 1
			if _, err := DecodeExecutionOutput(marshalFields(t, fields)); !errors.Is(err, ErrInvalidExecutionOutput) || !strings.Contains(err.Error(), key) {
				t.Fatalf("error = %v, want ErrInvalidExecutionOutput naming %s", err, key)
			}
		})
	}
}

func TestSecurity_DecodeExecutionOutputRejectsAmbiguousJSON(t *testing.T) {
	checkAmbiguousJSON(t, []byte(outputExample), `"executionId": "exe_01HZX3"`, `"sequence"`, "exe_01HZX3",
		func(payload []byte) (any, error) { return DecodeExecutionOutput(payload) },
		ErrInvalidExecutionOutput, ExecutionOutput{})
}

func TestExecutionOutput_ValidateChecksTypedValues(t *testing.T) {
	tests := []struct {
		name   string
		change func(*ExecutionOutput)
		field  string
	}{
		{name: "zero value", change: func(o *ExecutionOutput) { *o = ExecutionOutput{} }, field: "executionId"},
		{name: "nil data", change: func(o *ExecutionOutput) { o.Data = nil }, field: "data"},
		{name: "empty data", change: func(o *ExecutionOutput) { o.Data = []byte{} }, field: "data"},
		{name: "data over 64 KiB", change: func(o *ExecutionOutput) { o.Data = make([]byte, 65537) }, field: "data"},
		{name: "sequence 0", change: func(o *ExecutionOutput) { o.Sequence = 0 }, field: "sequence"},
		{name: "sequence over 2^30", change: func(o *ExecutionOutput) { o.Sequence = 1<<30 + 1 }, field: "sequence"},
		{name: "unknown stream", change: func(o *ExecutionOutput) { o.Stream = "stdin" }, field: "stream"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			output := outputExampleDecoded()
			test.change(&output)
			if err := output.Validate(); !errors.Is(err, ErrInvalidExecutionOutput) || !strings.Contains(err.Error(), test.field) {
				t.Fatalf("Validate() error = %v, want ErrInvalidExecutionOutput naming %s", err, test.field)
			}
		})
	}
}

func FuzzDecodeExecutionOutput(f *testing.F) {
	f.Add([]byte(outputExample))
	f.Add([]byte(outputCanonical))
	f.Fuzz(func(t *testing.T, payload []byte) {
		output, err := DecodeExecutionOutput(payload)
		if err != nil {
			if !errors.Is(err, ErrInvalidExecutionOutput) {
				t.Fatalf("DecodeExecutionOutput() error %v does not wrap ErrInvalidExecutionOutput", err)
			}
			return
		}
		checkCanonicalOutput(t, payload, output)
	})
}

// FuzzExecutionOutputValues builds valid chunks from fuzzed bytes, so that
// most inputs exercise the base64url encoding and the round trip.
func FuzzExecutionOutputValues(f *testing.F) {
	f.Add([]byte("ok\n"), uint64(1), false)
	f.Add([]byte{0, 0xff, 0xfe, '"', '\\'}, uint64(1<<30), true)
	f.Fuzz(func(t *testing.T, data []byte, sequence uint64, stderr bool) {
		output := ExecutionOutput{ExecutionID: "exe_01HZX3", Sequence: sequence, Stream: StreamStdout, Data: data}
		if stderr {
			output.Stream = StreamStderr
		}
		if output.Validate() != nil {
			return
		}
		canonical, err := output.CanonicalJSON()
		if err != nil {
			t.Fatalf("CanonicalJSON() of a valid value error = %v", err)
		}
		checkCanonicalOutput(t, canonical, output)
	})
}

func checkCanonicalOutput(t *testing.T, payload []byte, output ExecutionOutput) {
	t.Helper()
	canonical, err := output.CanonicalJSON()
	if err != nil {
		t.Fatalf("CanonicalJSON() of an accepted value error = %v", err)
	}
	if want := jcsReference(t, payload); !bytes.Equal(canonical, want) {
		t.Fatalf("CanonicalJSON() = %q, want the RFC 8785 rendering %q", canonical, want)
	}
	if decoded, err := DecodeExecutionOutput(canonical); err != nil || !reflect.DeepEqual(decoded, output) {
		t.Fatalf("DecodeExecutionOutput(canonical) = %+v, %v; want %+v", decoded, err, output)
	}
}
