package protocol

import (
	"errors"
	"fmt"
	"time"
)

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

// DecodeExecutionRequest strictly decodes an execution.request payload.
func DecodeExecutionRequest(payload []byte) (ExecutionRequest, error) {
	return ExecutionRequest{}, fmt.Errorf("%w: not implemented", ErrInvalidExecutionRequest)
}

// ExecutionRequest decodes the payload of an execution.request envelope.
func (e Envelope) ExecutionRequest() (ExecutionRequest, error) {
	return ExecutionRequest{}, nil
}

// Validate checks every ADR-007 field rule on a typed request.
func (r ExecutionRequest) Validate() error {
	return nil
}

// ValidateAt validates r and enforces its validity window against now.
func (r ExecutionRequest) ValidateAt(now time.Time) error {
	return nil
}
