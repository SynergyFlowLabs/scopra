package protocol

import (
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// requestTimeLayout is the only accepted timestamp form: RFC 3339 in UTC with
// second precision.
const requestTimeLayout = "2006-01-02T15:04:05Z"

// Bounds from ADR-007. The limit bounds are protocol sanity checks, not
// policy: effective limits are the minimum of the request, the grant, and the
// local maximum that Scopra Rupes enforces.
const (
	// MaxExecutionRequestLifetime bounds expiresAt minus createdAt.
	MaxExecutionRequestLifetime = time.Hour
	// MaxCommandArguments bounds the number of command arguments.
	MaxCommandArguments = 4096
	// MaxCommandArgumentBytes bounds each argument, as Linux MAX_ARG_STRLEN does.
	MaxCommandArgumentBytes = 128 << 10
	// MaxCommandPathBytes bounds the executable and the working directory.
	MaxCommandPathBytes = 4095
	// MaxReasonBytes bounds the agent's explanation.
	MaxReasonBytes = 1024

	MaxTimeoutSeconds = 86400
	MaxCPUMillicores  = 1_000_000
	MinMemoryBytes    = 1 << 20
	MaxMemoryBytes    = 1 << 40
	MaxPIDs           = 65536
	MaxOutputBytes    = 1 << 30
)

var (
	// ErrInvalidExecutionRequest reports a payload or typed request that
	// breaks ADR-007. Every DecodeExecutionRequest error wraps it.
	ErrInvalidExecutionRequest = errors.New("protocol: invalid execution request")
	// ErrExecutionRequestNotYetValid reports a creation time beyond the
	// allowed clock skew.
	ErrExecutionRequestNotYetValid = errors.New("protocol: execution request creation time is in the future")
	// ErrExecutionRequestExpired reports a request at or after its expiry.
	ErrExecutionRequestExpired = errors.New("protocol: execution request has expired")
	// ErrUnexpectedMessageType reports an envelope of another message type.
	ErrUnexpectedMessageType = errors.New("protocol: unexpected message type")
)

// ExecutionRequest is the typed payload of an execution.request envelope
// (ADR-007). A decoded request is structurally valid only: it is not
// authenticated, authorized, or permitted to run.
type ExecutionRequest struct {
	// ExecutionID names the execution; a receiver must not start an ID twice.
	ExecutionID     string
	Principals      Principals
	Resources       Resources
	GrantID         string
	SessionID       string
	Command         Command
	WorkspaceAccess WorkspaceAccess
	Network         NetworkAccess
	Limits          Limits
	// Reason is the agent's untrusted explanation. It grants nothing.
	Reason    string
	CreatedAt time.Time
	ExpiresAt time.Time
	Nonce     string
}

// Principals identifies who asks: the human user, the agent, and the client.
type Principals struct {
	UserID   string
	AgentID  string
	ClientID string
}

// Resources identifies where the command runs. WorkspaceID is a Mio
// registration ID, which must be revalidated before use (ADR-004).
type Resources struct {
	MachineID   string
	WorkspaceID string
}

// Command is a direct executable with an argument array. Shell is always
// false in version 1.
type Command struct {
	Executable       string
	Arguments        []string
	Shell            bool
	WorkingDirectory string
}

// WorkspaceAccess is how the workspace is mounted.
type WorkspaceAccess string

const (
	WorkspaceReadOnly  WorkspaceAccess = "read-only"
	WorkspaceReadWrite WorkspaceAccess = "read-write"
)

// NetworkAccess is the network the command may use. Version 1 allows none.
type NetworkAccess string

const NetworkNone NetworkAccess = "none"

// Limits are the requested resource limits.
type Limits struct {
	TimeoutSeconds uint64
	CPUMillicores  uint64
	MemoryBytes    uint64
	PIDs           uint64
	OutputBytes    uint64
}

// DecodeExecutionRequest strictly decodes an execution.request payload and
// checks every ADR-007 rule. Every field is required, objects must have
// exactly the listed keys, null is never allowed, and decoded text equals the
// transmitted text. Every error wraps ErrInvalidExecutionRequest, and no
// partially valid request is returned. Decoding does not authenticate the
// envelope, check replay state, or authorize anything.
func DecodeExecutionRequest(payload []byte) (ExecutionRequest, error) {
	if err := requestPayload.checkDocument(payload); err != nil {
		return ExecutionRequest{}, err
	}
	request, err := decodeRequestFields(payload)
	if err != nil {
		return ExecutionRequest{}, err
	}
	if err := request.Validate(); err != nil {
		return ExecutionRequest{}, err
	}
	return request, nil
}

// ExecutionRequest decodes the payload of an execution.request envelope and
// fails without decoding for any other message type. The envelope itself must
// already have been decoded, verified, and checked for freshness and replay.
func (e Envelope) ExecutionRequest() (ExecutionRequest, error) {
	if e.Type != MessageTypeExecutionRequest {
		return ExecutionRequest{}, fmt.Errorf("%w: %q, want %q", ErrUnexpectedMessageType, e.Type, MessageTypeExecutionRequest)
	}
	return DecodeExecutionRequest(e.Payload)
}

// Validate checks every ADR-007 field rule on a typed request, including one
// built in memory; nil Arguments means no arguments.
func (r ExecutionRequest) Validate() error {
	for _, identifier := range []struct{ field, value string }{
		{"executionId", r.ExecutionID},
		{"principals.userId", r.Principals.UserID},
		{"principals.agentId", r.Principals.AgentID},
		{"principals.clientId", r.Principals.ClientID},
		{"resources.machineId", r.Resources.MachineID},
		{"grantId", r.GrantID},
		{"sessionId", r.SessionID},
	} {
		if !validIdentifier(identifier.value) {
			return requestPayload.fieldError(identifier.field, "must be 1 to 128 bytes of A-Z, a-z, 0-9, '-', '_', '.', or ':'")
		}
	}
	switch {
	case !validRegistrationID(r.Resources.WorkspaceID):
		return requestPayload.fieldError("resources.workspaceId", "must be 32 lowercase hexadecimal characters")
	case !validExecutable(r.Command.Executable):
		return requestPayload.fieldError("command.executable",
			"must be a bare name, a clean absolute path, or ./ and a clean relative path, with no control, format, or line or paragraph separator characters")
	case !validArguments(r.Command.Arguments):
		return requestPayload.fieldError("command.arguments", fmt.Sprintf(
			"must hold at most %d strings of valid UTF-8 without NUL, each at most %d bytes", MaxCommandArguments, MaxCommandArgumentBytes))
	case r.Command.Shell:
		return requestPayload.fieldError("command.shell", "must be false in version 1")
	case !validWorkingDirectory(r.Command.WorkingDirectory):
		return requestPayload.fieldError("command.workingDirectory",
			"must be . or a clean relative path with no .. component and no control, format, or line or paragraph separator characters")
	case r.WorkspaceAccess != WorkspaceReadOnly && r.WorkspaceAccess != WorkspaceReadWrite:
		return requestPayload.fieldError("workspaceAccess", `must be "read-only" or "read-write"`)
	case r.Network != NetworkNone:
		return requestPayload.fieldError("network", `must be "none" in version 1`)
	}
	if err := r.Limits.validate(); err != nil {
		return err
	}
	switch {
	case !validText(r.Reason, MaxReasonBytes):
		return requestPayload.fieldError("reason", "must be 1 to 1024 bytes of valid UTF-8 with no control, format, or line or paragraph separator characters")
	case !validRequestTime(r.CreatedAt):
		return requestPayload.fieldError("createdAt", "must be a UTC time with second precision between years 0 and 9999")
	case !validRequestTime(r.ExpiresAt):
		return requestPayload.fieldError("expiresAt", "must be a UTC time with second precision between years 0 and 9999")
	case !r.ExpiresAt.After(r.CreatedAt) || r.ExpiresAt.Sub(r.CreatedAt) > MaxExecutionRequestLifetime:
		return requestPayload.fieldError("expiresAt", "must be after createdAt and at most 1h later")
	case !validNonce(r.Nonce):
		return requestPayload.fieldError("nonce", "must be unpadded base64url encoding 16 to 128 bytes")
	}
	return nil
}

// ValidateAt validates r and enforces its validity window against now:
// createdAt may be at most AllowedClockSkew in the future, and expiry is
// strict. The envelope that carries r has its own, shorter lifetime.
func (r ExecutionRequest) ValidateAt(now time.Time) error {
	if err := r.Validate(); err != nil {
		return err
	}
	if now.IsZero() {
		return fmt.Errorf("%w: missing validation time", ErrInvalidExecutionRequest)
	}
	if r.CreatedAt.After(now.Add(AllowedClockSkew)) {
		return ErrExecutionRequestNotYetValid
	}
	if !now.Before(r.ExpiresAt) {
		return ErrExecutionRequestExpired
	}
	return nil
}

func (l Limits) validate() error {
	for _, limit := range []struct {
		field           string
		value, min, max uint64
	}{
		{"limits.timeoutSeconds", l.TimeoutSeconds, 1, MaxTimeoutSeconds},
		{"limits.cpuMillicores", l.CPUMillicores, 1, MaxCPUMillicores},
		{"limits.memoryBytes", l.MemoryBytes, MinMemoryBytes, MaxMemoryBytes},
		{"limits.pids", l.PIDs, 1, MaxPIDs},
		{"limits.outputBytes", l.OutputBytes, 1, MaxOutputBytes},
	} {
		if limit.value < limit.min || limit.value > limit.max {
			return requestPayload.fieldError(limit.field, fmt.Sprintf("must be %d to %d", limit.min, limit.max))
		}
	}
	return nil
}

// decodeRequestFields decodes each field into its typed target after
// checking that every object has exactly its listed keys.
func decodeRequestFields(payload []byte) (ExecutionRequest, error) {
	top, err := requestPayload.objectFields(payload, "", "executionId", "principals", "resources", "grantId", "sessionId",
		"command", "workspaceAccess", "network", "limits", "reason", "createdAt", "expiresAt", "nonce")
	if err != nil {
		return ExecutionRequest{}, err
	}
	principals, err := requestPayload.objectFields(top["principals"], "principals", "userId", "agentId", "clientId")
	if err != nil {
		return ExecutionRequest{}, err
	}
	resources, err := requestPayload.objectFields(top["resources"], "resources", "machineId", "workspaceId")
	if err != nil {
		return ExecutionRequest{}, err
	}
	command, err := requestPayload.objectFields(top["command"], "command", "executable", "arguments", "shell", "workingDirectory")
	if err != nil {
		return ExecutionRequest{}, err
	}
	limits, err := requestPayload.objectFields(top["limits"], "limits", "timeoutSeconds", "cpuMillicores", "memoryBytes", "pids", "outputBytes")
	if err != nil {
		return ExecutionRequest{}, err
	}

	var request ExecutionRequest
	var createdAt, expiresAt string
	if err := requestPayload.decodeFields([]payloadField{
		{top["executionId"], "executionId", &request.ExecutionID},
		{principals["userId"], "principals.userId", &request.Principals.UserID},
		{principals["agentId"], "principals.agentId", &request.Principals.AgentID},
		{principals["clientId"], "principals.clientId", &request.Principals.ClientID},
		{resources["machineId"], "resources.machineId", &request.Resources.MachineID},
		{resources["workspaceId"], "resources.workspaceId", &request.Resources.WorkspaceID},
		{top["grantId"], "grantId", &request.GrantID},
		{top["sessionId"], "sessionId", &request.SessionID},
		{command["executable"], "command.executable", &request.Command.Executable},
		{command["arguments"], "command.arguments", &request.Command.Arguments},
		{command["shell"], "command.shell", &request.Command.Shell},
		{command["workingDirectory"], "command.workingDirectory", &request.Command.WorkingDirectory},
		{top["workspaceAccess"], "workspaceAccess", &request.WorkspaceAccess},
		{top["network"], "network", &request.Network},
		{limits["timeoutSeconds"], "limits.timeoutSeconds", &request.Limits.TimeoutSeconds},
		{limits["cpuMillicores"], "limits.cpuMillicores", &request.Limits.CPUMillicores},
		{limits["memoryBytes"], "limits.memoryBytes", &request.Limits.MemoryBytes},
		{limits["pids"], "limits.pids", &request.Limits.PIDs},
		{limits["outputBytes"], "limits.outputBytes", &request.Limits.OutputBytes},
		{top["reason"], "reason", &request.Reason},
		{top["createdAt"], "createdAt", &createdAt},
		{top["expiresAt"], "expiresAt", &expiresAt},
		{top["nonce"], "nonce", &request.Nonce},
	}); err != nil {
		return ExecutionRequest{}, err
	}
	var ok bool
	if request.CreatedAt, ok = parseRequestTime(createdAt); !ok {
		return ExecutionRequest{}, requestPayload.fieldError("createdAt", "must be RFC 3339 in UTC with second precision (YYYY-MM-DDTHH:MM:SSZ)")
	}
	if request.ExpiresAt, ok = parseRequestTime(expiresAt); !ok {
		return ExecutionRequest{}, requestPayload.fieldError("expiresAt", "must be RFC 3339 in UTC with second precision (YYYY-MM-DDTHH:MM:SSZ)")
	}
	return request, nil
}

func parseRequestTime(value string) (time.Time, bool) {
	parsed, err := time.Parse(requestTimeLayout, value)
	if err != nil || parsed.Format(requestTimeLayout) != value {
		return time.Time{}, false
	}
	return parsed, true
}

// validRequestTime reports whether t has the wire form's precision and range.
func validRequestTime(t time.Time) bool {
	return t.Location() == time.UTC && t.Nanosecond() == 0 && t.Year() >= 0 && t.Year() <= 9999
}

// validRegistrationID reports whether id is a Mio registration ID: 32
// lowercase hexadecimal characters (ADR-005).
func validRegistrationID(id string) bool {
	if len(id) != 32 {
		return false
	}
	for i := 0; i < len(id); i++ {
		if !(id[i] >= '0' && id[i] <= '9' || id[i] >= 'a' && id[i] <= 'f') {
			return false
		}
	}
	return true
}

// validExecutable accepts a bare name looked up by the sandbox, a clean
// absolute path, or ./ followed by a clean relative path below the working
// directory.
func validExecutable(executable string) bool {
	if !validText(executable, MaxCommandPathBytes) {
		return false
	}
	switch {
	case !strings.Contains(executable, "/"):
		return executable != "." && executable != ".."
	case strings.HasPrefix(executable, "/"):
		return executable != "/" && path.Clean(executable) == executable
	case strings.HasPrefix(executable, "./"):
		return validRelativePath(executable[len("./"):])
	default:
		return false
	}
}

func validWorkingDirectory(dir string) bool {
	return validText(dir, MaxCommandPathBytes) && (dir == "." || validRelativePath(dir))
}

// validRelativePath reports whether p is a clean relative path below its
// base: not empty or ".", not absolute, and with no ".." component.
func validRelativePath(p string) bool {
	return p != "" && p != "." && !strings.HasPrefix(p, "/") && path.Clean(p) == p &&
		!slices.Contains(strings.Split(p, "/"), "..")
}

func validArguments(arguments []string) bool {
	if len(arguments) > MaxCommandArguments {
		return false
	}
	for _, argument := range arguments {
		if len(argument) > MaxCommandArgumentBytes || !utf8.ValidString(argument) || strings.IndexByte(argument, 0) >= 0 {
			return false
		}
	}
	return true
}

// validText reports whether text is 1 to maxBytes bytes of valid UTF-8 with
// no control (Cc), format (Cf), line separator (Zl), or paragraph separator
// (Zp) characters, any of which could spoof a display.
func validText(text string, maxBytes int) bool {
	if text == "" || len(text) > maxBytes || !utf8.ValidString(text) {
		return false
	}
	for _, char := range text {
		if unicode.In(char, unicode.Cc, unicode.Cf, unicode.Zl, unicode.Zp) {
			return false
		}
	}
	return true
}
