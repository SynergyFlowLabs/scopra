//go:build linux

package localipc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SynergyFlowLabs/scopra/packages/protocol"
)

func TestRunExecution_StreamsTheStartOutputAndResult(t *testing.T) {
	stdout := append([]byte("hello\n"), bytes.Repeat([]byte{'o'}, 40000)...)
	stderr := bytes.Repeat([]byte{0xff}, 64<<10)
	executor := &fakeExecutor{execute: func(_ context.Context, request protocol.ExecutionRequest, events ExecutionEvents) (protocol.ExecutionResult, error) {
		events.Started(startedFor(request))
		events.Output(protocol.ExecutionOutput{ExecutionID: request.ExecutionID, Sequence: 1, Stream: protocol.StreamStdout, Data: stdout[:6]})
		events.Output(protocol.ExecutionOutput{ExecutionID: request.ExecutionID, Sequence: 2, Stream: protocol.StreamStderr, Data: stderr})
		events.Output(protocol.ExecutionOutput{ExecutionID: request.ExecutionID, Sequence: 3, Stream: protocol.StreamStdout, Data: stdout[6:]})
		result := succeededResult(request, uint64(len(stdout)), uint64(len(stderr)))
		result.Output.Chunks = 3
		return result, nil
	}}
	stateDir := startExecutionDaemon(t, executor)

	var events []string
	var gotStdout, gotStderr []byte
	var notice protocol.ExecutionStarted
	result, err := RunExecution(requestContext(t), stateDir, testRunRequest(), RunEvents{
		Started: func(started protocol.ExecutionStarted) {
			notice = started
			events = append(events, "started")
		},
		Output: func(stream protocol.OutputStream, data []byte) {
			events = append(events, fmt.Sprintf("%s:%d", stream, len(data)))
			if stream == protocol.StreamStdout {
				gotStdout = append(gotStdout, data...)
			} else {
				gotStderr = append(gotStderr, data...)
			}
		},
	})
	if err != nil {
		t.Fatalf("RunExecution() error = %v", err)
	}

	// Each runner chunk is split at 32768 bytes, in order.
	want := "started|stdout:6|stderr:32768|stderr:32768|stdout:32768|stdout:7232"
	if got := strings.Join(events, "|"); got != want {
		t.Fatalf("events = %s, want %s", got, want)
	}
	if !bytes.Equal(gotStdout, stdout) || !bytes.Equal(gotStderr, stderr) {
		t.Fatal("the output bytes changed on the way")
	}
	request := executor.onlyRequest(t)
	if wantResult := executor.result(); !equalResults(result, wantResult) {
		t.Fatalf("RunExecution() = %+v, want %+v", result, wantResult)
	}
	if err := result.ValidateAgainst(request); err != nil {
		t.Fatalf("the result does not belong to the request: %v", err)
	}
	if err := notice.ValidateAgainst(request); err != nil {
		t.Fatalf("the start notice does not belong to the request: %v", err)
	}
}

func TestRunExecution_MioBuildsTheRequestFromThePeer(t *testing.T) {
	executor := &fakeExecutor{execute: func(_ context.Context, request protocol.ExecutionRequest, _ ExecutionEvents) (protocol.ExecutionResult, error) {
		return succeededResult(request, 0, 0), nil
	}}
	stateDir := startExecutionDaemon(t, executor)
	before := time.Now().UTC().Truncate(time.Second)
	if _, err := RunExecution(requestContext(t), stateDir, testRunRequest(), RunEvents{}); err != nil {
		t.Fatalf("RunExecution() error = %v", err)
	}
	after := time.Now().UTC()

	request := executor.onlyRequest(t)
	run := testRunRequest()
	wantPrincipals := protocol.Principals{UserID: fmt.Sprintf("local:%d", os.Geteuid()), AgentID: "local", ClientID: "local-socket"}
	switch {
	case request.Principals != wantPrincipals:
		t.Fatalf("Principals = %+v, want %+v", request.Principals, wantPrincipals)
	case request.Resources != (protocol.Resources{MachineID: "local", WorkspaceID: run.WorkspaceID}):
		t.Fatalf("Resources = %+v", request.Resources)
	case request.GrantID != "local" || request.SessionID != "local" || request.Network != protocol.NetworkNone:
		t.Fatalf("grant, session, or network = %q, %q, %q", request.GrantID, request.SessionID, request.Network)
	case request.Command.Executable != run.Command.Executable || strings.Join(request.Command.Arguments, " ") != "test ./..." ||
		request.WorkspaceAccess != run.WorkspaceAccess || request.Limits != run.Limits || request.Reason != run.Reason:
		t.Fatalf("the run's fields changed: %+v", request)
	case request.CreatedAt.Before(before) || request.CreatedAt.After(after) || request.ExpiresAt.Sub(request.CreatedAt) != time.Hour:
		t.Fatalf("createdAt %v and expiresAt %v, want now and one hour later", request.CreatedAt, request.ExpiresAt)
	case !strings.HasPrefix(request.ExecutionID, "local-"):
		t.Fatalf("ExecutionID = %q", request.ExecutionID)
	}
}

func TestSecurity_DaemonDropsMalformedRunRequests(t *testing.T) {
	executor := &fakeExecutor{execute: func(_ context.Context, request protocol.ExecutionRequest, _ ExecutionEvents) (protocol.ExecutionResult, error) {
		return succeededResult(request, 0, 0), nil
	}}
	stateDir := startExecutionDaemon(t, executor)
	frame := func(run string) string { return requestPrefix + `"command":"execution.run","run":` + run + `}` }

	// The valid request proves that the harness reaches the executor.
	if replies := sendRawRun(t, stateDir, frame(validRunJSON)); len(replies) != 1 || executor.calls() != 1 {
		t.Fatalf("a valid run got %d replies and %d executor calls, want 1 and 1", len(replies), executor.calls())
	}

	payloads := map[string]string{
		"not an object":    frame(`"go test"`),
		"a network key":    frame(strings.Replace(validRunJSON, `"reason"`, `"network":"none","reason"`, 1)),
		"a missing reason": frame(strings.Replace(validRunJSON, `,"reason":"local run by the workspace owner"`, ``, 1)),
		"a shell command":  frame(strings.Replace(validRunJSON, `"shell":false`, `"shell":true`, 1)),
		"a null limit":     frame(strings.Replace(validRunJSON, `"pids":512`, `"pids":null`, 1)),
		"a lone surrogate": frame(strings.Replace(validRunJSON, `"test"`, `"\ud800"`, 1)),
		"invalid UTF-8":    frame(strings.Replace(validRunJSON, `"test"`, "\"\xff\"", 1)),
		"a bad workspace":  frame(strings.Replace(validRunJSON, testWorkspaceID, strings.ToUpper(testWorkspaceID), 1)),
		"a zero timeout":   frame(strings.Replace(validRunJSON, `"timeoutSeconds":600`, `"timeoutSeconds":0`, 1)),
		"no run":           requestPrefix + `"command":"execution.run"}`,
		"an extra key":     requestPrefix + `"command":"execution.run","path":"/tmp","run":` + validRunJSON + `}`,
		"a wrong version":  strings.Replace(frame(validRunJSON), `"protocolVersion":1`, `"protocolVersion":2`, 1),
	}
	for name, payload := range payloads {
		t.Run(name, func(t *testing.T) {
			if replies := sendRawRun(t, stateDir, payload); len(replies) != 0 {
				t.Fatalf("Mio replied %q to a malformed run", replies)
			}
		})
	}
	if calls := executor.calls(); calls != 1 {
		t.Fatalf("the executor ran %d times, want only the valid run", calls)
	}
}

func TestSecurity_RunExecutionRejectsAFifthConcurrentRun(t *testing.T) {
	const runLimit = 4 // ADR-011
	release := make(chan struct{})
	entered := make(chan struct{}, runLimit+1)
	executor := &fakeExecutor{execute: func(ctx context.Context, request protocol.ExecutionRequest, _ ExecutionEvents) (protocol.ExecutionResult, error) {
		entered <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
		}
		return succeededResult(request, 0, 0), nil
	}}
	stateDir := startExecutionDaemon(t, executor)
	ctx := requestContext(t)

	type outcome struct {
		result protocol.ExecutionResult
		err    error
	}
	outcomes := make(chan outcome, runLimit)
	for range runLimit {
		go func() {
			result, err := RunExecution(ctx, stateDir, testRunRequest(), RunEvents{})
			outcomes <- outcome{result, err}
		}()
	}
	for range runLimit {
		waitFor(t, entered, "a concurrent run to reach the executor")
	}

	result, err := RunExecution(ctx, stateDir, testRunRequest(), RunEvents{})
	if err != nil {
		t.Fatalf("RunExecution() of a fifth run error = %v", err)
	}
	if result.State != protocol.StateRejected || result.Error.Code != protocol.ErrorCodeBusy {
		t.Fatalf("a fifth run = %+v, want rejected with busy", result)
	}
	if !strings.HasPrefix(result.ExecutionID, "local-") || result.Validate() != nil {
		t.Fatalf("the busy result is not a valid result for a local execution: %+v", result)
	}
	if calls := executor.calls(); calls != runLimit {
		t.Fatalf("the executor ran %d times, want %d", calls, runLimit)
	}

	close(release)
	for range runLimit {
		if got := <-outcomes; got.err != nil || got.result.State != protocol.StateSucceeded {
			t.Fatalf("a concurrent run = %+v, %v; want succeeded", got.result, got.err)
		}
	}
	// The finished runs free their places.
	if result, err := RunExecution(ctx, stateDir, testRunRequest(), RunEvents{}); err != nil || result.State != protocol.StateSucceeded {
		t.Fatalf("a run after the others finished = %+v, %v; want succeeded", result, err)
	}
}

func TestSecurity_DisconnectCancelsTheRun(t *testing.T) {
	entered := make(chan struct{}, 1)
	cancelled := make(chan struct{})
	executor := &fakeExecutor{execute: func(ctx context.Context, request protocol.ExecutionRequest, events ExecutionEvents) (protocol.ExecutionResult, error) {
		events.Started(startedFor(request))
		entered <- struct{}{}
		<-ctx.Done()
		close(cancelled)
		return cancelledResult(request), nil
	}}
	stateDir := startExecutionDaemon(t, executor)

	ctx, cancel := context.WithCancel(requestContext(t))
	done := make(chan error, 1)
	go func() {
		_, err := RunExecution(ctx, stateDir, testRunRequest(), RunEvents{})
		done <- err
	}()
	waitFor(t, entered, "the run to start")
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("RunExecution() after cancellation error = %v, want context.Canceled", err)
	}
	waitFor(t, cancelled, "the executor's context to be cancelled after the client disconnected")
}

func TestSecurity_AStalledReaderCancelsTheRun(t *testing.T) {
	cancelled := make(chan struct{})
	executor := &fakeExecutor{execute: func(ctx context.Context, request protocol.ExecutionRequest, events ExecutionEvents) (protocol.ExecutionResult, error) {
		events.Started(startedFor(request))
		chunk := bytes.Repeat([]byte{'x'}, 64<<10)
		for sequence := uint64(1); ctx.Err() == nil; sequence++ {
			events.Output(protocol.ExecutionOutput{ExecutionID: request.ExecutionID, Sequence: sequence, Stream: protocol.StreamStdout, Data: chunk})
		}
		close(cancelled)
		return cancelledResult(request), nil
	}}
	stateDir, daemon := listenForTest(t)
	if err := daemon.EnableExecutions(executor); err != nil {
		t.Fatalf("EnableExecutions() error = %v", err)
	}
	if daemon.frameWriteTimeout != 30*time.Second {
		t.Fatalf("frameWriteTimeout = %v, want ADR-011's 30 seconds", daemon.frameWriteTimeout)
	}
	daemon.frameWriteTimeout = 200 * time.Millisecond
	serveInBackground(t, daemon)

	connection := dialRaw(t, stateDir)
	defer connection.Close()
	if err := writeFrame(connection, []byte(requestPrefix+`"command":"execution.run","run":`+validRunJSON+`}`)); err != nil {
		t.Fatalf("writeFrame() error = %v", err)
	}
	// The client never reads, so the socket buffers fill and a frame write
	// misses its deadline.
	waitFor(t, cancelled, "the executor's context to be cancelled for a stalled reader")
}

func TestRunExecution_OnlyTheRequestHasTheTwoSecondDeadline(t *testing.T) {
	executor := &fakeExecutor{execute: func(ctx context.Context, request protocol.ExecutionRequest, events ExecutionEvents) (protocol.ExecutionResult, error) {
		events.Started(startedFor(request))
		select {
		case <-time.After(connectionTimeout + 500*time.Millisecond):
		case <-ctx.Done():
			return cancelledResult(request), nil
		}
		events.Output(protocol.ExecutionOutput{ExecutionID: request.ExecutionID, Sequence: 1, Stream: protocol.StreamStdout, Data: []byte("late")})
		return succeededResult(request, 4, 0), nil
	}}
	stateDir := startExecutionDaemon(t, executor)

	var output []byte
	ctx, cancel := context.WithTimeout(t.Context(), 4*connectionTimeout)
	defer cancel()
	result, err := RunExecution(ctx, stateDir, testRunRequest(), RunEvents{
		Output: func(_ protocol.OutputStream, data []byte) { output = append(output, data...) },
	})
	if err != nil || result.State != protocol.StateSucceeded || string(output) != "late" {
		t.Fatalf("a run longer than the request deadline = %+v, %q, %v; want succeeded with its output", result, output, err)
	}

	// A client that does not finish its request within the deadline is
	// dropped before any executor runs.
	connection := dialRaw(t, stateDir)
	defer connection.Close()
	if _, err := connection.Write([]byte{0, 0}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	start := time.Now()
	if err := connection.SetReadDeadline(time.Now().Add(2 * connectionTimeout)); err != nil {
		t.Fatalf("SetReadDeadline() error = %v", err)
	}
	if reply, err := readFrame(connection); err == nil {
		t.Fatalf("Mio replied %q to an unfinished request", reply)
	}
	if elapsed := time.Since(start); elapsed >= 2*connectionTimeout {
		t.Fatalf("Mio kept an unfinished request open for %v", elapsed)
	}
	if calls := executor.calls(); calls != 1 {
		t.Fatalf("the executor ran %d times, want 1", calls)
	}
}

func TestSecurity_ShutdownCancelsRuns(t *testing.T) {
	entered := make(chan struct{}, 1)
	executor := &fakeExecutor{execute: func(ctx context.Context, request protocol.ExecutionRequest, events ExecutionEvents) (protocol.ExecutionResult, error) {
		events.Started(startedFor(request))
		entered <- struct{}{}
		<-ctx.Done()
		return cancelledResult(request), nil
	}}
	stateDir, daemon := listenForTest(t)
	if err := daemon.EnableExecutions(executor); err != nil {
		t.Fatalf("EnableExecutions() error = %v", err)
	}
	serveCtx, stopServing := context.WithCancel(context.Background())
	defer stopServing()
	served := make(chan error, 1)
	go func() { served <- daemon.Serve(serveCtx) }()

	type outcome struct {
		result protocol.ExecutionResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := RunExecution(requestContext(t), stateDir, testRunRequest(), RunEvents{})
		done <- outcome{result, err}
	}()
	waitFor(t, entered, "the run to start")
	stopServing()

	select {
	case err := <-served:
		if err != nil {
			t.Fatalf("Serve() error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve() did not return while a run was in progress")
	}
	got := <-done
	if got.err != nil || got.result.State != protocol.StateCancelled {
		t.Fatalf("RunExecution() during shutdown = %+v, %v; want the cancelled result", got.result, got.err)
	}
}

func TestSecurity_RunEndsWithoutAResultWhenTheExecutorMisbehaves(t *testing.T) {
	tests := map[string]func(context.Context, protocol.ExecutionRequest, ExecutionEvents) (protocol.ExecutionResult, error){
		"an executor error": func(_ context.Context, request protocol.ExecutionRequest, _ ExecutionEvents) (protocol.ExecutionResult, error) {
			return succeededResult(request, 0, 0), errors.New("the executor failed")
		},
		"another execution's result": func(_ context.Context, request protocol.ExecutionRequest, _ ExecutionEvents) (protocol.ExecutionResult, error) {
			request.ExecutionID = "local-other"
			return succeededResult(request, 0, 0), nil
		},
		"an invalid result": func(_ context.Context, request protocol.ExecutionRequest, _ ExecutionEvents) (protocol.ExecutionResult, error) {
			result := succeededResult(request, 0, 0)
			result.ExitCode = 3
			return result, nil
		},
		"another execution's start notice": func(_ context.Context, request protocol.ExecutionRequest, events ExecutionEvents) (protocol.ExecutionResult, error) {
			other := request
			other.ExecutionID = "local-other"
			events.Started(startedFor(other))
			return succeededResult(request, 0, 0), nil
		},
		"output before the start notice": func(_ context.Context, request protocol.ExecutionRequest, events ExecutionEvents) (protocol.ExecutionResult, error) {
			events.Output(protocol.ExecutionOutput{ExecutionID: request.ExecutionID, Sequence: 1, Stream: protocol.StreamStdout, Data: []byte("x")})
			return succeededResult(request, 1, 0), nil
		},
		"output of another execution": func(_ context.Context, request protocol.ExecutionRequest, events ExecutionEvents) (protocol.ExecutionResult, error) {
			events.Started(startedFor(request))
			events.Output(protocol.ExecutionOutput{ExecutionID: "local-other", Sequence: 1, Stream: protocol.StreamStdout, Data: []byte("x")})
			return succeededResult(request, 1, 0), nil
		},
		"an invalid output chunk": func(_ context.Context, request protocol.ExecutionRequest, events ExecutionEvents) (protocol.ExecutionResult, error) {
			events.Started(startedFor(request))
			events.Output(protocol.ExecutionOutput{ExecutionID: request.ExecutionID, Sequence: 0, Stream: protocol.StreamStdout, Data: []byte("x")})
			return succeededResult(request, 1, 0), nil
		},
		"two start notices": func(_ context.Context, request protocol.ExecutionRequest, events ExecutionEvents) (protocol.ExecutionResult, error) {
			events.Started(startedFor(request))
			events.Started(startedFor(request))
			return succeededResult(request, 0, 0), nil
		},
	}
	for name, execute := range tests {
		t.Run(name, func(t *testing.T) {
			stateDir := startExecutionDaemon(t, &fakeExecutor{execute: execute})
			result, err := RunExecution(requestContext(t), stateDir, testRunRequest(), RunEvents{})
			if !errors.Is(err, ErrNoRunResult) {
				t.Fatalf("RunExecution() = %+v, %v; want ErrNoRunResult", result, err)
			}
		})
	}
}

func TestSecurity_RunsNeedAnEnabledExecutor(t *testing.T) {
	stateDir, daemon := listenForTest(t)
	serveInBackground(t, daemon)
	ctx := requestContext(t)
	if result, err := RunExecution(ctx, stateDir, testRunRequest(), RunEvents{}); !errors.Is(err, ErrNoRunResult) {
		t.Fatalf("RunExecution() without an executor = %+v, %v; want the connection closed without a reply", result, err)
	}
	if _, err := Status(ctx, stateDir); err != nil {
		t.Fatalf("Status() after a refused run error = %v", err)
	}
}

func TestSecurity_EnableExecutionsOnlyBeforeServe(t *testing.T) {
	stateDir, daemon := listenForTest(t)
	if err := daemon.EnableExecutions(nil); err == nil {
		t.Fatal("EnableExecutions(nil) error = nil, want an error")
	}
	serveInBackground(t, daemon)
	if _, err := Status(requestContext(t), stateDir); err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if err := daemon.EnableExecutions(&fakeExecutor{}); !errors.Is(err, ErrDaemonAlreadyServing) {
		t.Fatalf("EnableExecutions() while serving error = %v, want ErrDaemonAlreadyServing", err)
	}
}

func TestRunExecution_ChecksItsArgumentsFirst(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	var noContext context.Context
	if _, err := RunExecution(noContext, missing, testRunRequest(), RunEvents{}); !errors.Is(err, ErrInvalidContext) {
		t.Fatalf("RunExecution(nil) error = %v, want ErrInvalidContext", err)
	}
	run := testRunRequest()
	run.Limits.PIDs = 0
	if _, err := RunExecution(requestContext(t), missing, run, RunEvents{}); !errors.Is(err, ErrInvalidMessage) {
		t.Fatalf("RunExecution() of an invalid run error = %v, want ErrInvalidMessage", err)
	}
	if _, err := os.Lstat(missing); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("RunExecution() touched the state directory (Lstat error = %v)", err)
	}
	if _, err := RunExecution(requestContext(t), missing, testRunRequest(), RunEvents{}); !errors.Is(err, ErrDaemonUnavailable) {
		t.Fatalf("RunExecution() without Mio error = %v, want ErrDaemonUnavailable", err)
	}
}

// fakeExecutor records the requests it receives and runs execute for each.
type fakeExecutor struct {
	execute func(context.Context, protocol.ExecutionRequest, ExecutionEvents) (protocol.ExecutionResult, error)

	mu         sync.Mutex
	requests   []protocol.ExecutionRequest
	lastResult protocol.ExecutionResult
}

func (f *fakeExecutor) Execute(ctx context.Context, request protocol.ExecutionRequest, events ExecutionEvents) (protocol.ExecutionResult, error) {
	f.mu.Lock()
	f.requests = append(f.requests, request)
	f.mu.Unlock()
	result, err := f.execute(ctx, request, events)
	f.mu.Lock()
	f.lastResult = result
	f.mu.Unlock()
	return result, err
}

func (f *fakeExecutor) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

func (f *fakeExecutor) result() protocol.ExecutionResult {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastResult
}

func (f *fakeExecutor) onlyRequest(t *testing.T) protocol.ExecutionRequest {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) != 1 {
		t.Fatalf("the executor ran %d times, want once", len(f.requests))
	}
	return f.requests[0]
}

// startExecutionDaemon serves Mio's IPC with executor in a new state
// directory and returns the state directory.
func startExecutionDaemon(t *testing.T, executor Executor) string {
	t.Helper()
	stateDir, daemon := listenForTest(t)
	if err := daemon.EnableExecutions(executor); err != nil {
		t.Fatalf("EnableExecutions() error = %v", err)
	}
	serveInBackground(t, daemon)
	return stateDir
}

func dialRaw(t *testing.T, stateDir string) net.Conn {
	t.Helper()
	connection, err := net.DialTimeout("unix", filepath.Join(stateDir, "scoprad.sock"), time.Second)
	if err != nil {
		t.Fatalf("DialTimeout() error = %v", err)
	}
	return connection
}

// sendRawRun sends payload as a request frame and returns every reply frame
// until Mio closes the connection.
func sendRawRun(t *testing.T, stateDir, payload string) []string {
	t.Helper()
	connection := dialRaw(t, stateDir)
	defer connection.Close()
	if err := connection.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("SetDeadline() error = %v", err)
	}
	if err := writeFrame(connection, []byte(payload)); err != nil {
		t.Fatalf("writeFrame() error = %v", err)
	}
	var replies []string
	for {
		reply, err := readFrame(connection)
		if err != nil {
			return replies
		}
		replies = append(replies, string(reply))
	}
}

func waitFor(t *testing.T, signal <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}
