package hollow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/SynergyFlowLabs/scopra/packages/protocol"
)

const (
	// defaultPollInterval is how often the runner asks podman inspect whether
	// a container has started.
	defaultPollInterval = 50 * time.Millisecond
	// maxChunkBytes bounds one execution.output chunk (ADR-009).
	maxChunkBytes = 64 << 10
)

// ErrInvalidRunner reports a Runner that cannot run anything, such as one
// without an absolute path to podman.
var ErrInvalidRunner = errors.New("hollow: invalid runner configuration")

// Runner runs executions in rootless Podman containers (ADR-010).
type Runner struct {
	// Podman is the absolute path of the podman binary, resolved once at
	// startup and never searched for again.
	Podman string
	// Image is the configured container image, pinned by digest.
	Image string
	// pollInterval is how often the runner asks podman inspect whether the
	// container has started. Zero means the default.
	pollInterval time.Duration
	// helperTimeout bounds each helper call to podman. Zero means the
	// default.
	helperTimeout time.Duration
}

// RemoveLeftovers removes containers that an earlier run of Mio left behind.
func (r Runner) RemoveLeftovers() (int, error) {
	return 0, nil
}

// Events receives the progress messages of one execution, in order: at most
// one start notice, then output chunks. Callbacks run one at a time, must not
// block for long, and may keep the data they receive. A nil callback ignores
// its messages.
type Events struct {
	Started func(protocol.ExecutionStarted)
	Output  func(protocol.ExecutionOutput)
}

// Run runs request with workspacePath mounted (ADR-010) and returns its
// terminal result (ADR-009). Every outcome is a result: a workspace that
// cannot be mounted or an image that is not pinned is rejected without
// running podman; otherwise the container is run, observed through podman
// inspect, and removed. The error is only for an invalid request or runner.
// Cancellation and the timeout arrive in a later increment.
func (r Runner) Run(ctx context.Context, request protocol.ExecutionRequest, workspacePath string, events Events) (protocol.ExecutionResult, error) {
	if !filepath.IsAbs(r.Podman) {
		return protocol.ExecutionResult{}, fmt.Errorf("%w: the podman path %q is not absolute", ErrInvalidRunner, r.Podman)
	}
	// podmanRunArgs validates the request first, so the action hash is set
	// whenever the error is about the workspace or the image.
	args, actionHash, err := podmanRunArgs(RunSpec{Request: request, WorkspacePath: workspacePath, Image: r.Image})
	base := protocol.ExecutionResult{ExecutionID: request.ExecutionID, ActionHash: actionHash}
	switch {
	case errors.Is(err, ErrUnmountableWorkspace):
		return rejected(base, protocol.ErrorCodeWorkspaceUnavailable, "the workspace path cannot be mounted in the sandbox"), nil
	case errors.Is(err, ErrInvalidImage):
		return rejected(base, protocol.ErrorCodeContainmentUnavailable, "the sandbox image is not a reference pinned by digest"), nil
	case err != nil:
		return protocol.ExecutionResult{}, err
	}
	run := &execution{runner: r, request: request, base: base, name: "scopra-" + actionHash[:32], events: events}
	result := run.run(args)
	r.remove(run.name)
	return result, nil
}

func rejected(base protocol.ExecutionResult, code, message string) protocol.ExecutionResult {
	base.State = protocol.StateRejected
	base.Error = protocol.ResultError{Code: code, Message: message}
	return base
}

// remove deletes the container. A failure leaves it for the cleanup of
// labelled containers at Mio's next start.
func (r Runner) remove(name string) {
	_ = exec.Command(r.Podman, "rm", "--force", name).Run()
}

// execution is one run of one container.
type execution struct {
	runner  Runner
	request protocol.ExecutionRequest
	base    protocol.ExecutionResult
	name    string
	events  Events

	mu          sync.Mutex
	stdoutBytes uint64
	stderrBytes uint64
	chunks      uint64
	truncated   bool
}

// run launches podman and waits until it has exited and both streams are
// drained, then derives the result from podman inspect.
func (e *execution) run(args []string) protocol.ExecutionResult {
	stdoutRead, stdoutWrite, err := os.Pipe()
	if err != nil {
		return e.errored(protocol.ErrorCodeInternal, "the output pipes could not be created")
	}
	defer stdoutRead.Close()
	stderrRead, stderrWrite, err := os.Pipe()
	if err != nil {
		_ = stdoutWrite.Close()
		return e.errored(protocol.ErrorCodeInternal, "the output pipes could not be created")
	}
	defer stderrRead.Close()

	// Standard input stays nil, which os/exec connects to /dev/null.
	command := exec.Command(e.runner.Podman, args...)
	command.Stdout, command.Stderr = stdoutWrite, stderrWrite
	err = command.Start()
	_ = stdoutWrite.Close()
	_ = stderrWrite.Close()
	if err != nil {
		return e.errored(protocol.ErrorCodeStartFailed, "podman could not be started")
	}

	// The pipes are plain files, so Wait does not read them and can run while
	// the readers are still draining.
	exited := make(chan struct{})
	go func() {
		_ = command.Wait()
		close(exited)
	}()
	settled := make(chan struct{})
	final := make(chan watched, 1)
	go func() {
		state, err := e.watch(exited, settled)
		final <- watched{state, err}
	}()

	var readers sync.WaitGroup
	readers.Add(2)
	go e.read(stdoutRead, protocol.StreamStdout, settled, &readers)
	go e.read(stderrRead, protocol.StreamStderr, settled, &readers)
	readers.Wait()
	// The result needs the watcher's final state, so Run cannot return while
	// a start notice could still be sent.
	last := <-final
	<-exited
	return e.result(last.state, last.err, command.ProcessState.ExitCode())
}

// watched is the state that podman inspect reported after podman exited.
type watched struct {
	state inspectState
	err   error
}

// watch polls podman inspect until the container has started, sends the start
// notice, and closes settled so that output can follow. It then waits for
// podman to exit and returns the final state from one last inspect. If podman
// exits before a poll sees the start, that last inspect decides whether the
// container started, and settled closes only after it.
func (e *execution) watch(exited <-chan struct{}, settled chan<- struct{}) (inspectState, error) {
	interval := e.runner.pollInterval
	if interval <= 0 {
		interval = defaultPollInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	sent := false
polling:
	for {
		if state, err := e.inspect(); err == nil && !state.StartedAt.IsZero() {
			e.sendStarted(state.StartedAt)
			sent = true
			close(settled)
			break
		}
		select {
		case <-exited:
			break polling
		case <-ticker.C:
		}
	}
	<-exited
	state, err := e.inspect()
	if !sent {
		if err == nil && !state.StartedAt.IsZero() {
			e.sendStarted(state.StartedAt)
		}
		close(settled)
	}
	return state, err
}

func (e *execution) sendStarted(startedAt time.Time) {
	if e.events.Started != nil {
		e.events.Started(protocol.ExecutionStarted{ExecutionID: e.base.ExecutionID, ActionHash: e.base.ActionHash, StartedAt: startedAt.UTC().Truncate(time.Millisecond)})
	}
}

// read drains one stream in chunks of at most 64 KiB. It holds a chunk until
// the start question is settled, so the start notice always comes first; the
// pipe meanwhile applies backpressure instead of memory growing.
func (e *execution) read(file *os.File, stream protocol.OutputStream, settled <-chan struct{}, readers *sync.WaitGroup) {
	defer readers.Done()
	buffer := make([]byte, maxChunkBytes)
	for {
		n, err := file.Read(buffer)
		if n > 0 {
			<-settled
			e.emit(stream, buffer[:n])
		}
		if err != nil {
			return
		}
	}
}

// emit numbers and sends one chunk, keeping the total within
// limits.outputBytes: beyond it, output is discarded and marked truncated
// (ADR-009). The lock keeps sequence order equal to delivery order.
func (e *execution) emit(stream protocol.OutputStream, data []byte) {
	e.mu.Lock()
	defer e.mu.Unlock()
	remaining := e.request.Limits.OutputBytes - (e.stdoutBytes + e.stderrBytes)
	if uint64(len(data)) > remaining {
		data = data[:remaining]
		e.truncated = true
	}
	if len(data) == 0 {
		return
	}
	e.chunks++
	if stream == protocol.StreamStdout {
		e.stdoutBytes += uint64(len(data))
	} else {
		e.stderrBytes += uint64(len(data))
	}
	if e.events.Output != nil {
		e.events.Output(protocol.ExecutionOutput{ExecutionID: e.base.ExecutionID, Sequence: e.chunks, Stream: stream, Data: bytes.Clone(data)})
	}
}

func (e *execution) outputSummary() protocol.OutputSummary {
	e.mu.Lock()
	defer e.mu.Unlock()
	return protocol.OutputSummary{StdoutBytes: e.stdoutBytes, StderrBytes: e.stderrBytes, Truncated: e.truncated, Chunks: e.chunks}
}

// result maps the final podman inspect to the terminal state, never podman
// run's own exit status, which a command can also produce (125, 126, or 127).
// The start time is the one the start notice carried.
func (e *execution) result(state inspectState, inspectErr error, podmanExit int) protocol.ExecutionResult {
	if inspectErr != nil {
		return e.errored(protocol.ErrorCodeContainmentFailed, "podman inspect failed after the container exited")
	}
	if state.StartedAt.IsZero() {
		return e.errored(protocol.ErrorCodeStartFailed, fmt.Sprintf("the container did not start; podman exited with status %d", podmanExit))
	}
	result := e.base
	result.Output = e.outputSummary()
	result.StartedAt = state.StartedAt.UTC().Truncate(time.Millisecond)
	result.EndedAt = state.FinishedAt.UTC().Truncate(time.Millisecond)
	if result.EndedAt.Before(result.StartedAt) {
		result.EndedAt = result.StartedAt
	}
	switch {
	case state.OOMKilled:
		result.State = protocol.StateOutOfMemory
	case state.ExitCode == 0:
		result.State = protocol.StateSucceeded
	case state.ExitCode >= 1 && state.ExitCode <= 255:
		result.State, result.ExitCode = protocol.StateFailed, state.ExitCode
	default:
		return e.errored(protocol.ErrorCodeContainmentFailed, fmt.Sprintf("the container reported exit code %d", state.ExitCode))
	}
	return result
}

func (e *execution) errored(code, message string) protocol.ExecutionResult {
	result := e.base
	result.State = protocol.StateErrored
	result.Error = protocol.ResultError{Code: code, Message: message}
	result.Output = e.outputSummary()
	return result
}

// inspectState holds the parts of podman inspect's State that the runner
// needs.
type inspectState struct {
	OOMKilled  bool      `json:"OOMKilled"`
	ExitCode   int       `json:"ExitCode"`
	StartedAt  time.Time `json:"StartedAt"`
	FinishedAt time.Time `json:"FinishedAt"`
}

func (e *execution) inspect() (inspectState, error) {
	output, err := exec.Command(e.runner.Podman, "inspect", "--type", "container", "--format", "json", e.name).Output()
	if err != nil {
		return inspectState{}, fmt.Errorf("podman inspect: %w", err)
	}
	var containers []struct {
		State inspectState `json:"State"`
	}
	if err := json.Unmarshal(output, &containers); err != nil || len(containers) != 1 {
		return inspectState{}, fmt.Errorf("podman inspect: unexpected output")
	}
	return containers[0].State, nil
}
