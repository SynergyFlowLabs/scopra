package localipc

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/SynergyFlowLabs/scopra/internal/workspace/registry"
)

const (
	protocolVersion       = 1
	statusCommand         = "status"
	statusRunning         = "running"
	maxRequestIDBytes     = 32
	maxDaemonVersionBytes = 128
	maxJSONDepth          = 16
)

// ErrInvalidMessage reports a malformed or unsupported local IPC message.
var ErrInvalidMessage = errors.New("localipc: invalid message")

// request is a decoded IPC request. Each command uses only its own fields.
type request struct {
	ProtocolVersion int
	RequestID       string
	Command         string
	Path            string // workspace.add
	ID              string // workspace.remove
	Cursor          string // workspace.list
}

// requestHeader holds the fields that every request carries.
type requestHeader struct {
	ProtocolVersion int    `json:"protocolVersion"`
	RequestID       string `json:"requestId"`
	Command         string `json:"command"`
}

// requestKeys lists the exact keys of each command's request.
var requestKeys = map[string][]string{
	statusCommand:          {"protocolVersion", "requestId", "command"},
	addWorkspaceCommand:    {"protocolVersion", "requestId", "command", "path"},
	listWorkspacesCommand:  {"protocolVersion", "requestId", "command", "cursor"},
	removeWorkspaceCommand: {"protocolVersion", "requestId", "command", "id"},
}

// StatusResponse is the versioned read-only status reply from Mio.
type StatusResponse struct {
	ProtocolVersion int    `json:"protocolVersion"`
	RequestID       string `json:"requestId"`
	Status          string `json:"status"`
	DaemonVersion   string `json:"daemonVersion"`
}

func encodeRequest(message request) ([]byte, error) {
	if err := validateRequest(message); err != nil {
		return nil, err
	}
	header := requestHeader{
		ProtocolVersion: message.ProtocolVersion,
		RequestID:       message.RequestID,
		Command:         message.Command,
	}
	switch message.Command {
	case addWorkspaceCommand:
		return json.Marshal(struct {
			requestHeader
			Path string `json:"path"`
		}{header, message.Path})
	case listWorkspacesCommand:
		return json.Marshal(struct {
			requestHeader
			Cursor string `json:"cursor"`
		}{header, message.Cursor})
	case removeWorkspaceCommand:
		return json.Marshal(struct {
			requestHeader
			ID string `json:"id"`
		}{header, message.ID})
	default:
		return json.Marshal(header)
	}
}

// decodeRequest reads the command first, then requires exactly that
// command's keys and decodes each field strictly.
func decodeRequest(encoded []byte) (request, error) {
	if err := checkJSONDocument(encoded); err != nil {
		return request{}, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		return request{}, fmt.Errorf("%w: top-level JSON value must be an object", ErrInvalidMessage)
	}
	command, err := jsonString(fields["command"])
	if err != nil {
		return request{}, err
	}
	keys, ok := requestKeys[command]
	if !ok {
		return request{}, fmt.Errorf("%w: unsupported command", ErrInvalidMessage)
	}
	if err := validateObjectKeys(encoded, keys); err != nil {
		return request{}, err
	}

	message := request{Command: command}
	if err := json.Unmarshal(fields["protocolVersion"], &message.ProtocolVersion); err != nil {
		return request{}, fmt.Errorf("%w: malformed protocol version", ErrInvalidMessage)
	}
	if message.RequestID, err = jsonString(fields["requestId"]); err != nil {
		return request{}, err
	}
	switch command {
	case addWorkspaceCommand:
		message.Path, err = jsonString(fields["path"])
	case listWorkspacesCommand:
		message.Cursor, err = jsonString(fields["cursor"])
	case removeWorkspaceCommand:
		message.ID, err = jsonString(fields["id"])
	}
	if err != nil {
		return request{}, err
	}
	if err := validateRequest(message); err != nil {
		return request{}, err
	}
	return message, nil
}

func validateRequest(message request) error {
	if message.ProtocolVersion != protocolVersion || !validRequestID(message.RequestID) {
		return fmt.Errorf("%w: unsupported version or request ID", ErrInvalidMessage)
	}
	var valid bool
	switch message.Command {
	case statusCommand:
		valid = message.Path == "" && message.ID == "" && message.Cursor == ""
	case addWorkspaceCommand:
		valid = validRequestPath(message.Path) && message.ID == "" && message.Cursor == ""
	case listWorkspacesCommand:
		valid = (message.Cursor == "" || validWorkspaceID(message.Cursor)) && message.Path == "" && message.ID == ""
	case removeWorkspaceCommand:
		valid = validWorkspaceID(message.ID) && message.Path == "" && message.Cursor == ""
	}
	if !valid {
		return fmt.Errorf("%w: unsupported command or fields", ErrInvalidMessage)
	}
	return nil
}

// validRequestPath reports whether path may be sent to Mio. Mio itself
// decides whether the path is absolute and acceptable as a workspace.
func validRequestPath(path string) bool {
	return path != "" && len(path) <= registry.MaxPathBytes && utf8.ValidString(path)
}

// jsonString decodes a JSON string. Null and every other type are rejected.
func jsonString(raw json.RawMessage) (string, error) {
	raw = bytes.TrimSpace(raw)
	var value string
	if len(raw) == 0 || raw[0] != '"' || json.Unmarshal(raw, &value) != nil {
		return "", fmt.Errorf("%w: expected a JSON string", ErrInvalidMessage)
	}
	return value, nil
}

func encodeResponse(response StatusResponse) ([]byte, error) {
	if err := validateResponse(response, response.RequestID); err != nil {
		return nil, err
	}
	return json.Marshal(response)
}

func decodeResponse(encoded []byte, expectedRequestID string) (StatusResponse, error) {
	var response StatusResponse
	if err := decodeStrictJSON(encoded, &response, "protocolVersion", "requestId", "status", "daemonVersion"); err != nil {
		return StatusResponse{}, err
	}
	if err := validateResponse(response, expectedRequestID); err != nil {
		return StatusResponse{}, err
	}
	return response, nil
}

func validateResponse(response StatusResponse, expectedRequestID string) error {
	if response.ProtocolVersion != protocolVersion ||
		!validRequestID(response.RequestID) ||
		response.RequestID != expectedRequestID ||
		response.Status != statusRunning ||
		!validDaemonVersion(response.DaemonVersion) {
		return fmt.Errorf("%w: unsupported status response", ErrInvalidMessage)
	}
	return nil
}

func decodeStrictJSON(encoded []byte, destination any, expectedKeys ...string) error {
	if err := checkJSONDocument(encoded); err != nil {
		return err
	}
	if err := validateObjectKeys(encoded, expectedKeys); err != nil {
		return err
	}

	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("%w: decode JSON: %v", ErrInvalidMessage, err)
	}
	return nil
}

// checkJSONDocument checks the size, encoding, syntax, and keys of a complete
// JSON document.
func checkJSONDocument(encoded []byte) error {
	if len(encoded) == 0 {
		return fmt.Errorf("%w: empty JSON document", ErrInvalidMessage)
	}
	if len(encoded) > MaxFrameBytes {
		return ErrFrameTooLarge
	}
	// encoding/json silently replaces invalid UTF-8, which would change paths.
	if !utf8.Valid(encoded) {
		return fmt.Errorf("%w: JSON is not valid UTF-8", ErrInvalidMessage)
	}
	if !json.Valid(encoded) {
		return fmt.Errorf("%w: malformed or trailing JSON", ErrInvalidMessage)
	}
	return rejectDuplicateJSONKeys(encoded)
}

func validateObjectKeys(encoded []byte, expectedKeys []string) error {
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	openingToken, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("%w: malformed top-level object", ErrInvalidMessage)
	}
	openingDelimiter, ok := openingToken.(json.Delim)
	if !ok || openingDelimiter != '{' {
		return fmt.Errorf("%w: top-level JSON value must be an object", ErrInvalidMessage)
	}

	expected := make(map[string]struct{}, len(expectedKeys))
	for _, key := range expectedKeys {
		expected[key] = struct{}{}
	}
	seen := make(map[string]struct{}, len(expectedKeys))
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return fmt.Errorf("%w: malformed object key", ErrInvalidMessage)
		}
		key, ok := keyToken.(string)
		if !ok {
			return fmt.Errorf("%w: malformed object key", ErrInvalidMessage)
		}
		if _, ok := expected[key]; !ok {
			return fmt.Errorf("%w: unexpected or incorrectly-cased field", ErrInvalidMessage)
		}
		if _, exists := seen[key]; exists {
			return fmt.Errorf("%w: duplicate object key", ErrInvalidMessage)
		}
		seen[key] = struct{}{}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return fmt.Errorf("%w: malformed object value", ErrInvalidMessage)
		}
	}
	if _, err := decoder.Token(); err != nil {
		return fmt.Errorf("%w: malformed top-level object", ErrInvalidMessage)
	}
	if len(seen) != len(expected) {
		return fmt.Errorf("%w: missing required field", ErrInvalidMessage)
	}
	return nil
}

func rejectDuplicateJSONKeys(encoded []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	if err := scanJSONValue(decoder, 0); err != nil {
		return err
	}
	return nil
}

func scanJSONValue(decoder *json.Decoder, depth int) error {
	if depth > maxJSONDepth {
		return fmt.Errorf("%w: JSON nesting exceeds limit", ErrInvalidMessage)
	}

	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("%w: malformed JSON token", ErrInvalidMessage)
	}
	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		return nil
	}

	switch delimiter {
	case '{':
		keys := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return fmt.Errorf("%w: malformed object key", ErrInvalidMessage)
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("%w: malformed object key", ErrInvalidMessage)
			}
			if _, exists := keys[key]; exists {
				return fmt.Errorf("%w: duplicate object key", ErrInvalidMessage)
			}
			keys[key] = struct{}{}
			if err := scanJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
		if _, err := decoder.Token(); err != nil {
			return fmt.Errorf("%w: malformed object", ErrInvalidMessage)
		}
	case '[':
		for decoder.More() {
			if err := scanJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
		if _, err := decoder.Token(); err != nil {
			return fmt.Errorf("%w: malformed array", ErrInvalidMessage)
		}
	default:
		return fmt.Errorf("%w: unexpected JSON delimiter", ErrInvalidMessage)
	}
	return nil
}

func validRequestID(value string) bool {
	if len(value) != maxRequestIDBytes || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validDaemonVersion(value string) bool {
	if value == "" || len(value) > maxDaemonVersionBytes {
		return false
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			character == '.' || character == '_' || character == '-' || character == '+' {
			continue
		}
		return false
	}
	return true
}

func newRequestID() (string, error) {
	var randomBytes [maxRequestIDBytes / 2]byte
	if _, err := io.ReadFull(rand.Reader, randomBytes[:]); err != nil {
		return "", fmt.Errorf("generate local IPC request ID: %w", err)
	}
	return hex.EncodeToString(randomBytes[:]), nil
}
