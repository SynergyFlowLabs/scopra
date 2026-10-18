//go:build linux

package localipc

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SynergyFlowLabs/scopra/internal/journal"
	"github.com/SynergyFlowLabs/scopra/packages/protocol"
)

// recordedAppend is one call the daemon made to the run journal.
type recordedAppend struct {
	workspaceID string
	result      protocol.ExecutionResult
}

// fakeRunJournal is an in-memory RunJournal. Its entries are newest first.
type fakeRunJournal struct {
	mu         sync.Mutex
	entries    []journal.Entry
	appends    []recordedAppend
	broken     bool
	entriesErr error
	appendErr  error
}

func (f *fakeRunJournal) Append(workspaceID string, result protocol.ExecutionResult) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.broken {
		return journal.ErrBroken
	}
	f.appends = append(f.appends, recordedAppend{workspaceID: workspaceID, result: result})
	if f.appendErr != nil {
		f.broken = true
		return f.appendErr
	}
	return nil
}

func (f *fakeRunJournal) Entries(workspaceID string, limit int) ([]journal.Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.entriesErr != nil {
		return nil, f.entriesErr
	}
	result := make([]journal.Entry, 0, limit)
	for _, entry := range f.entries {
		if workspaceID != "" && entry.WorkspaceID != workspaceID {
			continue
		}
		if len(result) == limit {
			break
		}
		result = append(result, entry)
	}
	return result, nil
}

func (f *fakeRunJournal) Broken() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.broken
}

func (f *fakeRunJournal) recorded() []recordedAppend {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]recordedAppend(nil), f.appends...)
}

func TestLogs_RoundTrip(t *testing.T) {
	newest := testLogEntry(t, testWorkspaceID, protocol.StateSucceeded)
	other := testLogEntry(t, "0123456789abcdef0123456789abcdef", protocol.StateFailed)
	oldest := testLogEntry(t, testWorkspaceID, protocol.StateRejected)
	runJournal := &fakeRunJournal{entries: []journal.Entry{newest, other, oldest}}
	stateDir := startExecutionDaemonWithJournal(t, &fakeExecutor{}, runJournal)

	all, err := Logs(t.Context(), stateDir, LogFilter{Limit: 2})
	if err != nil {
		t.Fatalf("Logs() error = %v", err)
	}
	if len(all) != 2 || all[0].State != newest.State || all[1].WorkspaceID != other.WorkspaceID {
		t.Fatalf("Logs() = %+v, want the two newest entries in order", all)
	}

	filtered, err := Logs(t.Context(), stateDir, LogFilter{WorkspaceID: testWorkspaceID, Limit: journal.MaxLimit})
	if err != nil {
		t.Fatalf("Logs(filtered) error = %v", err)
	}
	if len(filtered) != 2 || filtered[0].WorkspaceID != testWorkspaceID || filtered[1].WorkspaceID != testWorkspaceID {
		t.Fatalf("Logs(filtered) = %+v, want only the workspace's entries", filtered)
	}
}

func TestLogs_UnreadableJournal(t *testing.T) {
	runJournal := &fakeRunJournal{entriesErr: errors.New("the run journal is unreadable")}
	stateDir := startExecutionDaemonWithJournal(t, &fakeExecutor{}, runJournal)

	_, err := Logs(t.Context(), stateDir, LogFilter{Limit: 1})
	var replyErr *ReplyError
	if !errors.As(err, &replyErr) {
		t.Fatalf("Logs() error = %v, want *ReplyError", err)
	}
	if replyErr.Code != CodeInternal || !strings.Contains(replyErr.Message, "unreadable") {
		t.Fatalf("Logs() error = %+v, want internal and the journal's message", replyErr)
	}
}

func TestLogs_NotServedWithoutExecutions(t *testing.T) {
	stateDir, daemon := listenForTest(t)
	serveInBackground(t, daemon)

	if _, err := Logs(t.Context(), stateDir, LogFilter{Limit: 1}); err == nil {
		t.Fatal("Logs() succeeded without executions enabled")
	}
}

func TestSecurity_LogsRejectsMalformedRequestsAndStaysReadOnly(t *testing.T) {
	runJournal := &fakeRunJournal{entries: []journal.Entry{testLogEntry(t, testWorkspaceID, protocol.StateSucceeded)}}
	stateDir := startExecutionDaemonWithJournal(t, &fakeExecutor{}, runJournal)

	payloads := []string{
		logsRequestPrefix + `"command":"execution.logs","limit":1,"unexpected":true}`,
		logsRequestPrefix + `"command":"execution.logs","workspaceId":"not-hex","limit":1}`,
		logsRequestPrefix + `"command":"execution.logs","limit":0}`,
		logsRequestPrefix + `"command":"execution.logs","limit":` + "101" + `}`,
	}
	for _, payload := range payloads {
		connection, err := net.DialTimeout("unix", filepath.Join(stateDir, "scoprad.sock"), time.Second)
		if err != nil {
			t.Fatalf("DialTimeout() error = %v", err)
		}
		if err := writeFrame(connection, []byte(payload)); err != nil {
			t.Fatalf("writeFrame() error = %v", err)
		}
		if reply, err := readFrame(connection); err == nil {
			t.Fatalf("Mio replied %s to a malformed logs request", reply)
		}
		_ = connection.Close()
	}
	if appends := runJournal.recorded(); len(appends) != 0 {
		t.Fatalf("malformed logs requests wrote %d journal entries, want none", len(appends))
	}
	entries, err := Logs(requestContext(t), stateDir, LogFilter{Limit: 1})
	if err != nil || len(entries) != 1 {
		t.Fatalf("Logs() after malformed requests = %+v, %v; want the journal unchanged", entries, err)
	}
}

func TestRunExecution_JournalsTheResult(t *testing.T) {
	runJournal := &fakeRunJournal{}
	executor := &fakeExecutor{execute: func(_ context.Context, request protocol.ExecutionRequest, _ ExecutionEvents) (protocol.ExecutionResult, error) {
		return succeededResult(request, 3, 4), nil
	}}
	stateDir := startExecutionDaemonWithJournal(t, executor, runJournal)

	result, err := RunExecution(requestContext(t), stateDir, testRunRequest(), RunEvents{})
	if err != nil {
		t.Fatalf("RunExecution() error = %v", err)
	}
	appends := runJournal.recorded()
	if len(appends) != 1 {
		t.Fatalf("the daemon journaled %d results, want 1", len(appends))
	}
	if appends[0].workspaceID != testWorkspaceID || !equalResults(appends[0].result, result) {
		t.Fatalf("journaled %+v, want the result for %s", appends[0], testWorkspaceID)
	}
}

func TestRunExecution_JournalsARejection(t *testing.T) {
	runJournal := &fakeRunJournal{}
	executor := &fakeExecutor{execute: func(_ context.Context, request protocol.ExecutionRequest, _ ExecutionEvents) (protocol.ExecutionResult, error) {
		actionHash, err := request.ActionHash()
		if err != nil {
			t.Errorf("ActionHash() error = %v", err)
		}
		return protocol.ExecutionResult{
			ExecutionID: request.ExecutionID,
			ActionHash:  actionHash,
			State:       protocol.StateRejected,
			Error:       protocol.ResultError{Code: protocol.ErrorCodeWorkspaceUnavailable, Message: "gone"},
		}, nil
	}}
	stateDir := startExecutionDaemonWithJournal(t, executor, runJournal)

	result, err := RunExecution(requestContext(t), stateDir, testRunRequest(), RunEvents{})
	if err != nil || result.State != protocol.StateRejected {
		t.Fatalf("RunExecution() = %+v, %v; want the rejection", result, err)
	}
	appends := runJournal.recorded()
	if len(appends) != 1 || appends[0].result.State != protocol.StateRejected ||
		appends[0].result.Error.Code != protocol.ErrorCodeWorkspaceUnavailable {
		t.Fatalf("journaled %+v, want the rejection", appends)
	}
}

func TestRunExecution_RefusesWhenTheJournalIsBroken(t *testing.T) {
	runJournal := &fakeRunJournal{broken: true}
	executor := &fakeExecutor{}
	stateDir := startExecutionDaemonWithJournal(t, executor, runJournal)

	result, err := RunExecution(requestContext(t), stateDir, testRunRequest(), RunEvents{})
	if err != nil {
		t.Fatalf("RunExecution() error = %v, want a typed refusal", err)
	}
	if result.State != protocol.StateErrored || result.Error.Code != protocol.ErrorCodeInternal ||
		result.Error.Message != journalUnavailableMessage {
		t.Fatalf("RunExecution() = %+v, want errored/internal with the journal message", result)
	}
	if calls := executor.calls(); calls != 0 {
		t.Fatalf("the executor ran %d times with a broken journal, want none", calls)
	}
	if appends := runJournal.recorded(); len(appends) != 0 {
		t.Fatalf("the daemon appended %d entries to a broken journal", len(appends))
	}
}

func TestRunExecution_AnAppendFailureStopsLaterRuns(t *testing.T) {
	runJournal := &fakeRunJournal{appendErr: errors.New("disk full")}
	executor := &fakeExecutor{execute: func(_ context.Context, request protocol.ExecutionRequest, _ ExecutionEvents) (protocol.ExecutionResult, error) {
		return succeededResult(request, 0, 0), nil
	}}
	stateDir := startExecutionDaemonWithJournal(t, executor, runJournal)

	first, err := RunExecution(requestContext(t), stateDir, testRunRequest(), RunEvents{})
	if err != nil || first.State != protocol.StateSucceeded {
		t.Fatalf("the first run = %+v, %v; want its real result despite the append failure", first, err)
	}
	if !runJournal.Broken() {
		t.Fatal("the journal is not broken after a failed append")
	}
	second, err := RunExecution(requestContext(t), stateDir, testRunRequest(), RunEvents{})
	if err != nil {
		t.Fatalf("the second run error = %v, want a typed refusal", err)
	}
	if second.State != protocol.StateErrored || second.Error.Code != protocol.ErrorCodeInternal {
		t.Fatalf("the second run = %+v, want errored/internal", second)
	}
	if calls := executor.calls(); calls != 1 {
		t.Fatalf("the executor ran %d times, want only the first run", calls)
	}
}

func TestRunExecution_JournalsTheBusyRefusal(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{}, maxConcurrentRuns)
	executor := &fakeExecutor{execute: func(ctx context.Context, request protocol.ExecutionRequest, _ ExecutionEvents) (protocol.ExecutionResult, error) {
		entered <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
		}
		return succeededResult(request, 0, 0), nil
	}}
	runJournal := &fakeRunJournal{}
	stateDir := startExecutionDaemonWithJournal(t, executor, runJournal)
	ctx := requestContext(t)

	done := make(chan struct{}, maxConcurrentRuns)
	for range maxConcurrentRuns {
		go func() {
			_, _ = RunExecution(ctx, stateDir, testRunRequest(), RunEvents{})
			done <- struct{}{}
		}()
	}
	for range maxConcurrentRuns {
		waitFor(t, entered, "a concurrent run to reach the executor")
	}
	result, err := RunExecution(ctx, stateDir, testRunRequest(), RunEvents{})
	if err != nil || result.Error.Code != protocol.ErrorCodeBusy {
		t.Fatalf("the fifth run = %+v, %v; want busy", result, err)
	}
	close(release)
	for range maxConcurrentRuns {
		<-done
	}

	var busy int
	for _, append := range runJournal.recorded() {
		if append.result.State == protocol.StateRejected && append.result.Error.Code == protocol.ErrorCodeBusy {
			busy++
		}
	}
	if busy != 1 {
		t.Fatalf("the journal holds %d busy refusals, want 1", busy)
	}
}

// startExecutionDaemonWithJournal serves Mio's IPC with executor and runJournal
// in a new state directory and returns the state directory.
func startExecutionDaemonWithJournal(t *testing.T, executor Executor, runJournal RunJournal) string {
	t.Helper()
	stateDir, daemon := listenForTest(t)
	if err := daemon.EnableExecutions(executor, runJournal); err != nil {
		t.Fatalf("EnableExecutions() error = %v", err)
	}
	serveInBackground(t, daemon)
	return stateDir
}
