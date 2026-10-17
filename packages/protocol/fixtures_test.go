package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureDir holds the Messenger v1 reference payloads (Step 21), shared with
// other implementations. See packages/test-fixtures/README.md.
const fixtureDir = "../test-fixtures/messenger/v1"

type fixtureManifest struct {
	Version         int       `json:"version"`
	ProtocolVersion string    `json:"protocolVersion"`
	Fixtures        []fixture `json:"fixtures"`
}

// fixture is one reference payload. A valid fixture has a canonical form, and
// an action hash if it is a request; an invalid one names the field that its
// rejection must mention.
type fixture struct {
	Name         string `json:"name"`
	MessageType  string `json:"messageType"`
	Payload      string `json:"payload"`
	Canonical    string `json:"canonical,omitempty"`
	ActionHash   string `json:"actionHash,omitempty"`
	InvalidField string `json:"invalidField,omitempty"`
}

func (f fixture) valid() bool { return f.InvalidField == "" }

// fixtureCodec decodes one payload type and returns its canonical form and,
// for requests, its action hash.
type fixtureCodec struct {
	invalid error
	decode  func([]byte) (canonical []byte, actionHash string, err error)
}

func canonicalOf[T interface{ CanonicalJSON() ([]byte, error) }](decode func([]byte) (T, error)) func([]byte) ([]byte, string, error) {
	return func(payload []byte) ([]byte, string, error) {
		value, err := decode(payload)
		if err != nil {
			return nil, "", err
		}
		canonical, err := value.CanonicalJSON()
		return canonical, "", err
	}
}

var fixtureCodecs = map[MessageType]fixtureCodec{
	MessageTypeExecutionRequest: {ErrInvalidExecutionRequest, func(payload []byte) ([]byte, string, error) {
		request, err := DecodeExecutionRequest(payload)
		if err != nil {
			return nil, "", err
		}
		canonical, err := request.CanonicalJSON()
		if err != nil {
			return nil, "", err
		}
		actionHash, err := request.ActionHash()
		return canonical, actionHash, err
	}},
	MessageTypeExecutionResult:  {ErrInvalidExecutionResult, canonicalOf(DecodeExecutionResult)},
	MessageTypeExecutionStarted: {ErrInvalidExecutionStarted, canonicalOf(DecodeExecutionStarted)},
	MessageTypeExecutionOutput:  {ErrInvalidExecutionOutput, canonicalOf(DecodeExecutionOutput)},
	MessageTypeExecutionCancel:  {ErrInvalidExecutionCancel, canonicalOf(DecodeExecutionCancel)},
}

func loadFixtureManifest(t *testing.T) fixtureManifest {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(fixtureDir, "manifest.json"))
	if err != nil {
		t.Fatalf("read the fixture manifest: %v", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var manifest fixtureManifest
	if err := decoder.Decode(&manifest); err != nil {
		t.Fatalf("decode the fixture manifest: %v", err)
	}
	return manifest
}

func readFixturePayload(t *testing.T, f fixture) []byte {
	t.Helper()
	payload, err := os.ReadFile(filepath.Join(fixtureDir, filepath.FromSlash(f.Payload)))
	if err != nil {
		t.Fatalf("%s: read the payload: %v", f.Name, err)
	}
	return payload
}

func TestFixtures_ManifestIsComplete(t *testing.T) {
	manifest := loadFixtureManifest(t)
	if manifest.Version != 1 || manifest.ProtocolVersion != ProtocolVersionV1 {
		t.Fatalf("manifest version %d for protocol %q, want 1 for %q", manifest.Version, manifest.ProtocolVersion, ProtocolVersionV1)
	}
	names, payloads := map[string]bool{}, map[string]bool{}
	coverage := map[MessageType][2]int{}
	for _, f := range manifest.Fixtures {
		if f.Name == "" || names[f.Name] {
			t.Errorf("fixture name %q is empty or repeated", f.Name)
		}
		names[f.Name] = true
		if payloads[f.Payload] {
			t.Errorf("%s: payload %q is used twice", f.Name, f.Payload)
		}
		payloads[f.Payload] = true
		if _, ok := fixtureCodecs[MessageType(f.MessageType)]; !ok {
			t.Errorf("%s: no payload rules for message type %q", f.Name, f.MessageType)
		}
		counts := coverage[MessageType(f.MessageType)]
		switch {
		case f.valid():
			counts[0]++
			if !strings.HasPrefix(f.Payload, "valid/") || f.Canonical == "" {
				t.Errorf("%s: a valid fixture needs a payload under valid/ and a canonical form", f.Name)
			}
			if wantHash := MessageType(f.MessageType) == MessageTypeExecutionRequest; (f.ActionHash != "") != wantHash {
				t.Errorf("%s: action hash %q; requests need one and other types have none", f.Name, f.ActionHash)
			}
		default:
			counts[1]++
			if !strings.HasPrefix(f.Payload, "invalid/") || f.Canonical != "" || f.ActionHash != "" {
				t.Errorf("%s: an invalid fixture needs a payload under invalid/ and no canonical form or action hash", f.Name)
			}
		}
		coverage[MessageType(f.MessageType)] = counts
	}
	for messageType := range fixtureCodecs {
		if counts := coverage[messageType]; counts[0] == 0 || counts[1] == 0 {
			t.Errorf("message type %q has %d valid and %d invalid fixtures, want at least one of each", messageType, counts[0], counts[1])
		}
	}
	for _, dir := range []string{"valid", "invalid"} {
		err := filepath.WalkDir(filepath.Join(fixtureDir, dir), func(name string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			relative, err := filepath.Rel(fixtureDir, name)
			if err != nil {
				return err
			}
			if !payloads[filepath.ToSlash(relative)] {
				t.Errorf("%s is not listed in the manifest", path.Join(dir, entry.Name()))
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}
	for payload := range payloads {
		if _, err := os.Stat(filepath.Join(fixtureDir, filepath.FromSlash(payload))); err != nil {
			t.Errorf("manifest lists %s: %v", payload, err)
		}
	}
}

func TestFixtures_ValidPayloadsDecodeToTheirCanonicalForm(t *testing.T) {
	for _, f := range loadFixtureManifest(t).Fixtures {
		if !f.valid() {
			continue
		}
		t.Run(f.Name, func(t *testing.T) {
			codec := fixtureCodecs[MessageType(f.MessageType)]
			payload := readFixturePayload(t, f)
			canonical, actionHash, err := codec.decode(payload)
			if err != nil {
				t.Fatalf("decode %s: %v", f.Payload, err)
			}
			if string(canonical) != f.Canonical {
				t.Fatalf("canonical form =\n%s\nwant\n%s", canonical, f.Canonical)
			}
			if actionHash != f.ActionHash {
				t.Fatalf("action hash = %q, want %q", actionHash, f.ActionHash)
			}
			if reference := jcsReference(t, payload); !bytes.Equal(canonical, reference) {
				t.Fatalf("canonical form = %q, want the RFC 8785 rendering %q", canonical, reference)
			}
			again, _, err := codec.decode(canonical)
			if err != nil || !bytes.Equal(again, canonical) {
				t.Fatalf("decoding the canonical form = %q, %v; want the same bytes", again, err)
			}
		})
	}
}

func TestSecurity_FixturesRejectInvalidPayloads(t *testing.T) {
	for _, f := range loadFixtureManifest(t).Fixtures {
		if f.valid() {
			continue
		}
		t.Run(f.Name, func(t *testing.T) {
			codec := fixtureCodecs[MessageType(f.MessageType)]
			_, _, err := codec.decode(readFixturePayload(t, f))
			if !errors.Is(err, codec.invalid) || !strings.Contains(err.Error(), f.InvalidField) {
				t.Fatalf("decode %s: error = %v, want %v naming %s", f.Payload, err, codec.invalid, f.InvalidField)
			}
		})
	}
}
