package localipc

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/SynergyFlowLabs/scopra/packages/protocol"
)

const (
	runCommand = "execution.run"
	// maxConcurrentRuns bounds executions, so that runs can never hold all
	// maxConcurrentConnections (ADR-011).
	maxConcurrentRuns = 4
	// maxOutputFrameData bounds the bytes in one output frame, so that their
	// base64url form and the frame's other fields fit in MaxFrameBytes.
	maxOutputFrameData = 32 << 10
	// defaultFrameWriteTimeout bounds writing one reply frame of a run.
	defaultFrameWriteTimeout = 30 * time.Second

	startedFrame = "started"
	outputFrame  = "output"
	resultFrame  = "result"
)

// ErrNoRunResult reports a run whose connection ended before its result
// frame, so the run's outcome is unknown.
var ErrNoRunResult = errors.New("localipc: Mio ended the run without a result")

// Executor runs local executions for Mio (ADR-011).
type Executor interface {
	Execute(ctx context.Context, request protocol.ExecutionRequest, events ExecutionEvents) (protocol.ExecutionResult, error)
}

// ExecutionEvents receives an execution's start notice and output chunks.
type ExecutionEvents struct {
	Started func(protocol.ExecutionStarted)
	Output  func(protocol.ExecutionOutput)
}

// RunRequest is what a client asks Mio to run (ADR-011).
type RunRequest struct {
	WorkspaceID     string
	Command         protocol.Command
	WorkspaceAccess protocol.WorkspaceAccess
	Limits          protocol.Limits
	Reason          string
}

// RunEvents receives a run's start notice and output frames.
type RunEvents struct {
	Started func(protocol.ExecutionStarted)
	Output  func(stream protocol.OutputStream, data []byte)
}

// EnableExecutions makes the daemon serve execution.run with executor.
func (d *Daemon) EnableExecutions(executor Executor) error {
	return errors.New("localipc: EnableExecutions is not implemented")
}

// RunExecution asks Mio to run run locally and returns its result.
func RunExecution(ctx context.Context, stateDir string, run RunRequest, events RunEvents) (protocol.ExecutionResult, error) {
	return protocol.ExecutionResult{}, errors.New("localipc: RunExecution is not implemented")
}

func encodeRun(run RunRequest) ([]byte, error) {
	return nil, errors.New("localipc: encodeRun is not implemented")
}

func buildExecutionRequest(run []byte, uid uint32, now time.Time) (protocol.ExecutionRequest, error) {
	return protocol.ExecutionRequest{}, errors.New("localipc: buildExecutionRequest is not implemented")
}

// runFrame is one decoded reply frame of a run.
type runFrame struct {
	kind    string
	started protocol.ExecutionStarted
	stream  protocol.OutputStream
	data    []byte
	result  protocol.ExecutionResult
}

func decodeRunFrame(encoded []byte, requestID string) (runFrame, error) {
	return runFrame{}, errors.New("localipc: decodeRunFrame is not implemented")
}

func encodeStartedFrame(requestID string, started protocol.ExecutionStarted) ([]byte, error) {
	return nil, errors.New("localipc: encodeStartedFrame is not implemented")
}

func encodeOutputFrame(requestID string, stream protocol.OutputStream, data []byte) ([]byte, error) {
	return nil, errors.New("localipc: encodeOutputFrame is not implemented")
}

func encodeResultFrame(requestID string, result protocol.ExecutionResult) ([]byte, error) {
	return nil, errors.New("localipc: encodeResultFrame is not implemented")
}

func readRunReplies(reader io.Reader, requestID string, events RunEvents) (protocol.ExecutionResult, error) {
	return protocol.ExecutionResult{}, errors.New("localipc: readRunReplies is not implemented")
}
