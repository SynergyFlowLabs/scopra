package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SynergyFlowLabs/scopra/internal/journal"
	"github.com/SynergyFlowLabs/scopra/internal/localipc"
	"github.com/SynergyFlowLabs/scopra/packages/protocol"
)

func TestParseLogsOptionsDefaults(t *testing.T) {
	options, err := parseLogsOptions(nil, io.Discard)
	if err != nil {
		t.Fatalf("parseLogsOptions() error = %v", err)
	}
	want := localipc.LogFilter{Limit: localipc.DefaultLogLimit}
	if options.filter != want || options.jsonOutput {
		t.Fatalf("defaults = %+v, want %+v without JSON", options, want)
	}
	if localipc.DefaultLogLimit != 20 || localipc.MaxLogLimit != 100 {
		t.Fatalf("log limits = %d/%d, want 20/100", localipc.DefaultLogLimit, localipc.MaxLogLimit)
	}
}

func TestParseLogsOptionsOverrides(t *testing.T) {
	options, err := parseLogsOptions([]string{
		"--json", "--state-dir", "/tmp/state", "--workspace", testWorkspaceID, "--limit", "7",
	}, io.Discard)
	if err != nil {
		t.Fatalf("parseLogsOptions() error = %v", err)
	}
	want := localipc.LogFilter{WorkspaceID: testWorkspaceID, Limit: 7}
	if options.filter != want || !options.jsonOutput || options.stateDir != "/tmp/state" {
		t.Fatalf("options = %+v, want %+v with JSON", options, want)
	}
}

func TestParseLogsOptionsRejectsBadInput(t *testing.T) {
	tests := map[string][]string{
		"zero limit":            {"--limit", "0"},
		"negative limit":        {"--limit", "-1"},
		"limit too high":        {"--limit", "101"},
		"limit is not a number": {"--limit", "many"},
		"bad workspace":         {"--workspace", "not-a-workspace-id"},
		"uppercase workspace":   {"--workspace", strings.ToUpper(testWorkspaceID)},
		"positional argument":   {"today"},
		"unknown flag":          {"--follow"},
	}
	for name, args := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := parseLogsOptions(args, io.Discard)
			if !isUsageError(err) {
				t.Fatalf("parseLogsOptions(%q) error = %v, want a usage error", args, err)
			}
		})
	}
}

func TestLogsCLI_PrintsNewestFirst(t *testing.T) {
	stateDir := startLogsDaemon(t, []protocol.ExecutionResult{
		logResult("local-0000000000000000000000000000000a", protocol.StateSucceeded, 0),
		logResult("local-0000000000000000000000000000000b", protocol.StateFailed, 7),
		logResult("local-0000000000000000000000000000000c", protocol.StateRejected, 0),
	})

	stdout, stderr, err := runCLI(t, "logs", "--state-dir", stateDir)
	if err != nil {
		t.Fatalf("logs error = %v; stderr = %q", err, stderr)
	}
	lines := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("logs printed %d lines, want 3:\n%s", len(lines), stdout)
	}
	for index, want := range []struct {
		state  string
		detail string
	}{
		{"rejected", "error busy"},
		{"failed", "exit 7"},
		{"succeeded", "exit 0"},
	} {
		if !strings.Contains(lines[index], want.state) || !strings.Contains(lines[index], want.detail) {
			t.Fatalf("line %d = %q, want %q and %q", index, lines[index], want.state, want.detail)
		}
		if !strings.Contains(lines[index], testWorkspaceID) || !strings.Contains(lines[index], "local-") {
			t.Fatalf("line %d = %q, want the workspace and execution IDs", index, lines[index])
		}
	}
	if !strings.Contains(lines[0], "duration -") {
		t.Fatalf("rejected line = %q, want no duration", lines[0])
	}
	if !strings.Contains(lines[2], "stdout 12 B") || !strings.Contains(lines[2], "stderr 3 B") {
		t.Fatalf("succeeded line = %q, want the stream byte counts", lines[2])
	}
	if !strings.Contains(lines[2], "duration 1.5s") {
		t.Fatalf("succeeded line = %q, want its duration", lines[2])
	}
}

func TestLogsCLI_ShowsTruncatedOutput(t *testing.T) {
	truncated := logResult("local-0000000000000000000000000000000d", protocol.StateSucceeded, 0)
	truncated.Output.Truncated = true
	stateDir := startLogsDaemon(t, []protocol.ExecutionResult{truncated})

	stdout, _, err := runCLI(t, "logs", "--state-dir", stateDir)
	if err != nil {
		t.Fatalf("logs error = %v", err)
	}
	if !strings.Contains(stdout, "truncated") {
		t.Fatalf("logs = %q, want the truncation marker", stdout)
	}
}

func TestLogsCLI_FiltersAndLimits(t *testing.T) {
	otherWorkspace := "fedcba9876543210fedcba9876543210"
	stateDir := startLogsDaemonWith(t, []logSeed{
		{workspaceID: testWorkspaceID, result: logResult("local-0000000000000000000000000000000a", protocol.StateSucceeded, 0)},
		{workspaceID: otherWorkspace, result: logResult("local-0000000000000000000000000000000b", protocol.StateSucceeded, 0)},
		{workspaceID: testWorkspaceID, result: logResult("local-0000000000000000000000000000000c", protocol.StateSucceeded, 0)},
	})

	stdout, _, err := runCLI(t, "logs", "--state-dir", stateDir, "--workspace", testWorkspaceID, "--limit", "3")
	if err != nil {
		t.Fatalf("logs error = %v", err)
	}
	lines := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], "local-0000000000000000000000000000000c") ||
		!strings.Contains(lines[1], "local-0000000000000000000000000000000a") {
		t.Fatalf("filtered logs = %q, want the newest two entries of one workspace", stdout)
	}
	if strings.Contains(stdout, otherWorkspace) {
		t.Fatalf("filtered logs include another workspace: %q", stdout)
	}
}

func TestLogsCLI_Empty(t *testing.T) {
	stateDir := startLogsDaemon(t, nil)
	stdout, stderr, err := runCLI(t, "logs", "--state-dir", stateDir)
	if err != nil || stderr != "" {
		t.Fatalf("logs = %q, %v; want a clean empty answer", stderr, err)
	}
	if !strings.Contains(stdout, "No runs have been recorded.") {
		t.Fatalf("empty logs = %q, want the empty message", stdout)
	}

	stdout, _, err = runCLI(t, "logs", "--state-dir", stateDir, "--workspace", testWorkspaceID)
	if err != nil {
		t.Fatalf("filtered logs error = %v", err)
	}
	if !strings.Contains(stdout, "No runs have been recorded for workspace "+testWorkspaceID+".") {
		t.Fatalf("filtered empty logs = %q, want the workspace in the message", stdout)
	}
}

func TestLogsCLI_JSON(t *testing.T) {
	stateDir := startLogsDaemon(t, []protocol.ExecutionResult{
		logResult("local-0000000000000000000000000000000a", protocol.StateSucceeded, 0),
		logResult("local-0000000000000000000000000000000b", protocol.StateTimedOut, 0),
	})

	stdout, stderr, err := runCLI(t, "logs", "--state-dir", stateDir, "--json")
	if err != nil {
		t.Fatalf("logs --json error = %v; stderr = %q", err, stderr)
	}
	if lines := strings.Count(strings.TrimSpace(stdout), "\n"); lines != 0 {
		t.Fatalf("logs --json wrote %d extra lines: %q", lines, stdout)
	}
	var output struct {
		SchemaVersion int             `json:"schemaVersion"`
		Entries       []journal.Entry `json:"entries"`
	}
	if err := json.Unmarshal([]byte(stdout), &output); err != nil {
		t.Fatalf("logs --json output is not one JSON object: %v; %q", err, stdout)
	}
	if output.SchemaVersion != logsSchemaVersion || len(output.Entries) != 2 {
		t.Fatalf("logs --json = %+v, want schema 1 and two entries", output)
	}
	if output.Entries[0].State != protocol.StateTimedOut || output.Entries[1].State != protocol.StateSucceeded {
		t.Fatalf("log order = %s, %s; want newest first", output.Entries[0].State, output.Entries[1].State)
	}
	if output.Entries[0].ExecutionID != "local-0000000000000000000000000000000b" ||
		output.Entries[0].WorkspaceID != testWorkspaceID {
		t.Fatalf("entry identity = %+v, want the last run", output.Entries[0])
	}
}

func TestLogsCLI_UsesTheDefaultLimit(t *testing.T) {
	results := make([]protocol.ExecutionResult, localipc.DefaultLogLimit+5)
	for index := range results {
		results[index] = logResult(fmt.Sprintf("local-%032x", index), protocol.StateSucceeded, 0)
	}
	stateDir := startLogsDaemon(t, results)

	stdout, _, err := runCLI(t, "logs", "--state-dir", stateDir)
	if err != nil {
		t.Fatalf("logs error = %v", err)
	}
	if lines := strings.Count(stdout, "\n"); lines != localipc.DefaultLogLimit {
		t.Fatalf("logs printed %d lines, want the default %d", lines, localipc.DefaultLogLimit)
	}
}

func TestLogsCLI_HelpListsTheCommand(t *testing.T) {
	var stdout, stderr strings.Builder
	if err := run(t.Context(), []string{"--help"}, &stdout, &stderr); err == nil {
		t.Fatal("run(--help) error = nil, want flag.ErrHelp")
	}
	if !strings.Contains(stderr.String(), "logs [--state-dir PATH]") {
		t.Fatalf("usage = %q, want the logs command", stderr.String())
	}
}

func TestLogsCLI_RejectsABadLimitBeforeDialing(t *testing.T) {
	var stdout, stderr strings.Builder
	err := run(t.Context(), []string{"logs", "--limit", "0"}, &stdout, &stderr)
	if !isUsageError(err) {
		t.Fatalf("logs --limit 0 error = %v, want a usage error", err)
	}
}

// logSeed is one entry to write to the test daemon's real journal.
type logSeed struct {
	workspaceID string
	result      protocol.ExecutionResult
}

// logResult returns a valid result for a journal test.
func logResult(executionID string, state protocol.ExecutionState, exitCode int) protocol.ExecutionResult {
	started := time.Date(2026, 10, 17, 5, 0, 0, 0, time.UTC)
	result := protocol.ExecutionResult{
		ExecutionID: executionID,
		State:       state,
		StartedAt:   started,
		EndedAt:     started.Add(1500 * time.Millisecond),
		ExitCode:    exitCode,
		Output:      protocol.OutputSummary{StdoutBytes: 12, StderrBytes: 3, Chunks: 2},
	}
	switch state {
	case protocol.StateRejected:
		result.StartedAt, result.EndedAt = time.Time{}, time.Time{}
		result.ExitCode, result.Output = 0, protocol.OutputSummary{}
		result.Error = protocol.ResultError{Code: protocol.ErrorCodeBusy, Message: "busy"}
	case protocol.StateErrored:
		result.StartedAt, result.EndedAt = time.Time{}, time.Time{}
		result.ExitCode = 0
		result.Error = protocol.ResultError{Code: protocol.ErrorCodeInternal, Message: "failed"}
	case protocol.StateTimedOut, protocol.StateCancelled, protocol.StateOutOfMemory:
		result.ExitCode = 0
	}
	return result
}

// startLogsDaemon serves Mio with a real run journal holding results for
// testWorkspaceID.
func startLogsDaemon(t *testing.T, results []protocol.ExecutionResult) string {
	t.Helper()
	seeds := make([]logSeed, 0, len(results))
	for _, result := range results {
		seeds = append(seeds, logSeed{workspaceID: testWorkspaceID, result: result})
	}
	return startLogsDaemonWith(t, seeds)
}

// startLogsDaemonWith serves Mio with a real run journal holding seeds.
func startLogsDaemonWith(t *testing.T, seeds []logSeed) string {
	t.Helper()
	requireLinux(t)
	daemon, err := localipc.Listen(filepath.Join(t.TempDir(), "logs"), "test-version")
	if err != nil {
		t.Fatalf("localipc.Listen() error = %v", err)
	}
	t.Cleanup(func() { _ = daemon.Close() })
	runJournal, err := journal.Open(daemon.StateDir())
	if err != nil {
		t.Fatalf("journal.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = runJournal.Close() })
	for _, seed := range seeds {
		if err := runJournal.Append(seed.workspaceID, seed.result); err != nil {
			t.Fatalf("journal.Append() error = %v", err)
		}
	}
	executor := &recordingExecutor{execute: func(_ context.Context, request protocol.ExecutionRequest, _ localipc.ExecutionEvents) (protocol.ExecutionResult, error) {
		return succeededFor(request), nil
	}}
	if err := daemon.EnableExecutions(executor, runJournal); err != nil {
		t.Fatalf("EnableExecutions() error = %v", err)
	}
	serveDaemon(t, daemon)
	return daemon.StateDir()
}
