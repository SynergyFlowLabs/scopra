package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SynergyFlowLabs/scopra/internal/localipc"
	"github.com/SynergyFlowLabs/scopra/internal/workspace"
	"github.com/SynergyFlowLabs/scopra/internal/workspace/registry"
	"github.com/SynergyFlowLabs/scopra/packages/protocol"
)

// testWorkspaceID is a registration ID in the registry's format.
const testWorkspaceID = "0123456789abcdef0123456789abcdef"

func TestParseRunOptions_Defaults(t *testing.T) {
	options, err := parseRunOptions([]string{"--workspace", testWorkspaceID, "--", "go", "test", "./..."}, io.Discard)
	if err != nil {
		t.Fatalf("parseRunOptions() error = %v", err)
	}
	want := localipc.RunRequest{
		WorkspaceID:     testWorkspaceID,
		Command:         protocol.Command{Executable: "go", Arguments: []string{"test", "./..."}, WorkingDirectory: "."},
		WorkspaceAccess: protocol.WorkspaceReadOnly,
		Limits: protocol.Limits{
			TimeoutSeconds: 600,
			CPUMillicores:  2000,
			MemoryBytes:    2 << 30,
			PIDs:           512,
			OutputBytes:    16 << 20,
		},
		Reason: "local run by the workspace owner",
	}
	if !reflect.DeepEqual(options.run, want) {
		t.Fatalf("defaults = %+v, want %+v", options.run, want)
	}
	if options.jsonOutput {
		t.Fatal("--json is on by default")
	}
}

func TestParseRunOptions_Overrides(t *testing.T) {
	options, err := parseRunOptions([]string{
		"--workspace", testWorkspaceID,
		"--json",
		"--write",
		"--workdir", "sub/dir",
		"--timeout", "90s",
		"--memory", "1073741824",
		"--cpus", "4",
		"--pids", "64",
		"--output-limit", "4096",
		"--reason", "check the build",
		"--", "make", "build",
	}, io.Discard)
	if err != nil {
		t.Fatalf("parseRunOptions() error = %v", err)
	}
	want := localipc.RunRequest{
		WorkspaceID: testWorkspaceID,
		Command: protocol.Command{
			Executable:       "make",
			Arguments:        []string{"build"},
			WorkingDirectory: "sub/dir",
		},
		WorkspaceAccess: protocol.WorkspaceReadWrite,
		Limits: protocol.Limits{
			TimeoutSeconds: 90,
			CPUMillicores:  4000,
			MemoryBytes:    1 << 30,
			PIDs:           64,
			OutputBytes:    4096,
		},
		Reason: "check the build",
	}
	if !reflect.DeepEqual(options.run, want) {
		t.Fatalf("parseRunOptions() = %+v, want %+v", options.run, want)
	}
	if !options.jsonOutput {
		t.Fatal("--json was not recorded")
	}
}

func TestParseRunOptions_AcceptsTheBoundaries(t *testing.T) {
	options, err := parseRunOptions([]string{
		"--workspace", testWorkspaceID,
		"--timeout", "1s",
		"--memory", "1048576",
		"--cpus", "1000",
		"--pids", "65536",
		"--output-limit", "1073741824",
		"--", "go",
	}, io.Discard)
	if err != nil {
		t.Fatalf("parseRunOptions() error = %v", err)
	}
	want := protocol.Limits{TimeoutSeconds: 1, CPUMillicores: 1000000, MemoryBytes: 1 << 20, PIDs: 65536, OutputBytes: 1 << 30}
	if options.run.Limits != want {
		t.Fatalf("limits = %+v, want the protocol's upper bounds %+v", options.run.Limits, want)
	}
	if _, err := parseRunOptions([]string{"--workspace", testWorkspaceID, "--timeout", "86400s", "--", "go"}, io.Discard); err != nil {
		t.Fatalf("parseRunOptions(24h) error = %v", err)
	}
}

func TestSecurity_ParseRunOptionsRejectsBadInput(t *testing.T) {
	// Each case names the check that must reject it, so that a later check
	// cannot make the test pass for the wrong reason.
	tests := map[string]struct {
		args []string
		want string
	}{
		"no workspace":                {[]string{"--", "go"}, "run needs --workspace ID or --path PATH"},
		"both workspace and path":     {[]string{"--workspace", testWorkspaceID, "--path", "/tmp/x", "--", "go"}, "not both"},
		"malformed workspace ID":      {[]string{"--workspace", "ABC", "--", "go"}, "scopra workspace list"},
		"uppercase workspace ID":      {[]string{"--workspace", strings.ToUpper(testWorkspaceID), "--", "go"}, "scopra workspace list"},
		"no executable":               {[]string{"--workspace", testWorkspaceID, "--"}, "needs EXECUTABLE after --"},
		"flags without a separator":   {[]string{"--workspace", testWorkspaceID}, "run needs -- before the command"},
		"unterminated flags":          {[]string{"--workspace", testWorkspaceID, "go", "test"}, "run needs -- before the command"},
		"no separator before command": {[]string{"--workspace", testWorkspaceID, "--write", "go", "test"}, "run needs -- before the command"},
		"positional before --":        {[]string{"--workspace", testWorkspaceID, "oops", "--", "go"}, "takes no arguments before --"},
		"empty executable":            {[]string{"--workspace", testWorkspaceID, "--", ""}, "invalid run"},
		"relative executable":         {[]string{"--workspace", testWorkspaceID, "--", "bin/echo"}, "invalid run"},
		"dot executable":              {[]string{"--workspace", testWorkspaceID, "--", "."}, "invalid run"},
		"absolute workdir":            {[]string{"--workspace", testWorkspaceID, "--workdir", "/etc", "--", "go"}, "invalid run"},
		"parent workdir":              {[]string{"--workspace", testWorkspaceID, "--workdir", "..", "--", "go"}, "invalid run"},
		"empty workdir":               {[]string{"--workspace", testWorkspaceID, "--workdir", "", "--", "go"}, "invalid run"},
		"traversing workdir":          {[]string{"--workspace", testWorkspaceID, "--workdir", "a/../../b", "--", "go"}, "invalid run"},
		"reason too long":             {[]string{"--workspace", testWorkspaceID, "--reason", strings.Repeat("a", 1025), "--", "go"}, "invalid run"},
		"reason with a newline":       {[]string{"--workspace", testWorkspaceID, "--reason", "first\nsecond", "--", "go"}, "invalid run"},
		"reason with a separator":     {[]string{"--workspace", testWorkspaceID, "--reason", "first\u2028second", "--", "go"}, "invalid run"},
		"timeout zero":                {[]string{"--workspace", testWorkspaceID, "--timeout", "0s", "--", "go"}, "--timeout must be at least 1s"},
		"timeout over a day":          {[]string{"--workspace", testWorkspaceID, "--timeout", "25h", "--", "go"}, "--timeout must be at most"},
		"timeout fractional seconds":  {[]string{"--workspace", testWorkspaceID, "--timeout", "1500ms", "--", "go"}, "whole number of seconds"},
		"memory too small":            {[]string{"--workspace", testWorkspaceID, "--memory", "1048575", "--", "go"}, "invalid run"},
		"memory too large":            {[]string{"--workspace", testWorkspaceID, "--memory", "1099511627777", "--", "go"}, "invalid run"},
		"memory not a number":         {[]string{"--workspace", testWorkspaceID, "--memory", "2GiB", "--", "go"}, "invalid value"},
		"zero CPUs":                   {[]string{"--workspace", testWorkspaceID, "--cpus", "0", "--", "go"}, "--cpus must be 1 to 1000"},
		"fractional CPUs":             {[]string{"--workspace", testWorkspaceID, "--cpus", "1.5", "--", "go"}, "invalid value"},
		"too many CPUs":               {[]string{"--workspace", testWorkspaceID, "--cpus", "1001", "--", "go"}, "--cpus must be 1 to 1000"},
		"zero pids":                   {[]string{"--workspace", testWorkspaceID, "--pids", "0", "--", "go"}, "invalid run"},
		"too many pids":               {[]string{"--workspace", testWorkspaceID, "--pids", "65537", "--", "go"}, "invalid run"},
		"zero output limit":           {[]string{"--workspace", testWorkspaceID, "--output-limit", "0", "--", "go"}, "invalid run"},
		"unknown flag":                {[]string{"--workspace", testWorkspaceID, "--sudo", "--", "go"}, "flag provided but not defined"},
		"argument with NUL":           {[]string{"--workspace", testWorkspaceID, "--", "go", "a\x00b"}, "invalid run"},
		"argument too long":           {[]string{"--workspace", testWorkspaceID, "--", "go", strings.Repeat("x", 131073)}, "invalid run"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			var stderr bytes.Buffer
			_, err := parseRunOptions(test.args, &stderr)
			if !isUsageError(err) {
				t.Fatalf("parseRunOptions() error = %v, want a usage error", err)
			}
			if !strings.Contains(stderr.String(), test.want) {
				t.Fatalf("stderr = %q, want %q", stderr.String(), test.want)
			}
		})
	}
}

func TestParseRunOptions_SingleExecutableHasNoArguments(t *testing.T) {
	options, err := parseRunOptions([]string{"--workspace", testWorkspaceID, "--", "go"}, io.Discard)
	if err != nil {
		t.Fatalf("parseRunOptions() error = %v", err)
	}
	if options.run.Command.Arguments == nil || len(options.run.Command.Arguments) != 0 {
		t.Fatalf("arguments = %#v, want an empty non-nil slice", options.run.Command.Arguments)
	}
}

func TestRunCLI_RejectsABadCommandBeforeReachingMio(t *testing.T) {
	// The state directory does not exist, so any attempt to dial Mio would
	// report a different error.
	missing := filepath.Join(t.TempDir(), "missing")
	var stdout, stderr bytes.Buffer
	err := run(t.Context(), []string{"run", "--state-dir", missing, "--workspace", testWorkspaceID, "--", "bin/echo"}, &stdout, &stderr)
	if !isUsageError(err) {
		t.Fatalf("run() error = %v, want a usage error", err)
	}
	if code := exitCode(err, &stderr); code != 2 {
		t.Fatalf("exitCode() = %d, want 2", code)
	}
	if _, err := os.Lstat(missing); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("run touched the state directory (Lstat error = %v)", err)
	}
}

func TestRunCLI_HumanOutputGoesToTheMatchingStreams(t *testing.T) {
	daemon := startRunDaemon(t, &recordingExecutor{execute: func(_ context.Context, request protocol.ExecutionRequest, events localipc.ExecutionEvents) (protocol.ExecutionResult, error) {
		events.Started(startedFor(request))
		events.Output(outputFor(request, 1, protocol.StreamStdout, []byte("out one\n")))
		events.Output(outputFor(request, 2, protocol.StreamStderr, []byte("err one\n")))
		events.Output(outputFor(request, 3, protocol.StreamStdout, []byte{0xff, 0xfe}))
		result := succeededFor(request)
		result.Output = protocol.OutputSummary{StdoutBytes: 10, StderrBytes: 8, Chunks: 3}
		return result, nil
	}})

	stdout, stderr, err := runCLI(t, "run", "--state-dir", daemon.stateDir, "--workspace", testWorkspaceID, "--", "go", "test")
	if err != nil {
		t.Fatalf("run() error = %v; stderr = %q", err, stderr)
	}
	if stdout != "out one\n\xff\xfe" {
		t.Fatalf("stdout = %q, want the command's stdout, unchanged", stdout)
	}
	if stderr != "err one\n" {
		t.Fatalf("stderr = %q, want the command's stderr, unchanged", stderr)
	}
	request := daemon.executor.onlyRequest(t)
	if request.Command.Executable != "go" || len(request.Command.Arguments) != 1 {
		t.Fatalf("executor request command = %+v, want go test", request.Command)
	}
}

func TestRunCLI_PropagatesFlagsToMio(t *testing.T) {
	daemon := startRunDaemon(t, &recordingExecutor{execute: func(_ context.Context, request protocol.ExecutionRequest, _ localipc.ExecutionEvents) (protocol.ExecutionResult, error) {
		return succeededFor(request), nil
	}})
	_, stderr, err := runCLI(t, "run", "--state-dir", daemon.stateDir,
		"--workspace", testWorkspaceID,
		"--write",
		"--workdir", "sub",
		"--timeout", "30s",
		"--memory", "1073741824",
		"--cpus", "3",
		"--pids", "32",
		"--output-limit", "2048",
		"--reason", "a reason",
		"--", "./gradlew", "check")
	if err != nil {
		t.Fatalf("run() error = %v; stderr = %q", err, stderr)
	}
	request := daemon.executor.onlyRequest(t)
	switch {
	case request.WorkspaceAccess != protocol.WorkspaceReadWrite:
		t.Fatalf("workspace access = %q, want read-write", request.WorkspaceAccess)
	case request.Command.WorkingDirectory != "sub":
		t.Fatalf("working directory = %q, want sub", request.Command.WorkingDirectory)
	case request.Reason != "a reason":
		t.Fatalf("reason = %q", request.Reason)
	case request.Limits != (protocol.Limits{TimeoutSeconds: 30, CPUMillicores: 3000, MemoryBytes: 1 << 30, PIDs: 32, OutputBytes: 2048}):
		t.Fatalf("limits = %+v, want the flags", request.Limits)
	case len(request.Command.Arguments) != 1 || request.Command.Arguments[0] != "check":
		t.Fatalf("arguments = %q", request.Command.Arguments)
	}
}

func TestRunCLI_JSONOutputIsOneObjectPerLine(t *testing.T) {
	data := append([]byte("hello\n"), bytes.Repeat([]byte{0xff}, 40000)...)
	daemon := startRunDaemon(t, &recordingExecutor{execute: func(_ context.Context, request protocol.ExecutionRequest, events localipc.ExecutionEvents) (protocol.ExecutionResult, error) {
		events.Started(startedFor(request))
		events.Output(outputFor(request, 1, protocol.StreamStdout, data[:6]))
		events.Output(outputFor(request, 2, protocol.StreamStdout, data[6:]))
		result := succeededFor(request)
		result.Output = protocol.OutputSummary{StdoutBytes: uint64(len(data)), Chunks: 2}
		return result, nil
	}})

	stdout, stderr, err := runCLI(t, "run", "--state-dir", daemon.stateDir, "--json", "--workspace", testWorkspaceID, "--", "go")
	if err != nil {
		t.Fatalf("run() error = %v; stderr = %q", err, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want no diagnostics besides the JSON", stderr)
	}
	lines := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
	// Mio splits the 40000-byte chunk at 32768 bytes, so the three output
	// frames are 6, 32768, and 7232 bytes.
	wantTypes := []string{"started", "output", "output", "output", "result"}
	if len(lines) != len(wantTypes) {
		t.Fatalf("JSON output has %d lines, want %d: %q", len(lines), len(wantTypes), stdout)
	}
	var got []byte
	for i, line := range lines {
		var object map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &object); err != nil {
			t.Fatalf("line %d is not one JSON object: %v", i+1, err)
		}
		if kind := jsonStringField(t, object, "type"); kind != wantTypes[i] {
			t.Fatalf("line %d type = %q, want %q", i+1, kind, wantTypes[i])
		}
		if wantTypes[i] != "output" {
			continue
		}
		encoded := jsonStringField(t, object, "data")
		decoded, err := base64.RawURLEncoding.DecodeString(encoded)
		if err != nil || base64.RawURLEncoding.EncodeToString(decoded) != encoded {
			t.Fatalf("line %d data is not canonical unpadded base64url: %q", i+1, encoded)
		}
		got = append(got, decoded...)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("the JSON output bytes differ from what the executor sent")
	}
}

func TestRunCLI_ExitStatuses(t *testing.T) {
	tests := map[string]struct {
		result     func(protocol.ExecutionRequest) protocol.ExecutionResult
		wantCode   int
		wantStderr string
	}{
		"the command succeeded": {
			result:   succeededFor,
			wantCode: 0,
		},
		"the command failed": {
			result: func(request protocol.ExecutionRequest) protocol.ExecutionResult {
				result := succeededFor(request)
				result.State, result.ExitCode = protocol.StateFailed, 3
				return result
			},
			wantCode: 3,
		},
		"the run timed out": {
			result: func(request protocol.ExecutionRequest) protocol.ExecutionResult {
				result := succeededFor(request)
				result.State = protocol.StateTimedOut
				return result
			},
			wantCode: 124,
		},
		"the run was cancelled": {
			result: func(request protocol.ExecutionRequest) protocol.ExecutionResult {
				result := succeededFor(request)
				result.State = protocol.StateCancelled
				return result
			},
			wantCode: 130,
		},
		"the run ran out of memory": {
			result: func(request protocol.ExecutionRequest) protocol.ExecutionResult {
				result := succeededFor(request)
				result.State = protocol.StateOutOfMemory
				return result
			},
			wantCode: 137,
		},
		"the run was rejected": {
			result: func(request protocol.ExecutionRequest) protocol.ExecutionResult {
				return rejectedFor(request, protocol.ErrorCodeContainmentUnavailable, "no sandbox image")
			},
			wantCode:   1,
			wantStderr: "containment_unavailable: no sandbox image",
		},
		"the run errored": {
			result: func(request protocol.ExecutionRequest) protocol.ExecutionResult {
				return protocol.ExecutionResult{
					ExecutionID: request.ExecutionID,
					ActionHash:  hashOf(request),
					State:       protocol.StateErrored,
					Error:       protocol.ResultError{Code: protocol.ErrorCodeStartFailed, Message: "the executable was not found"},
				}
			},
			wantCode:   1,
			wantStderr: "start_failed: the executable was not found",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			daemon := startRunDaemon(t, &recordingExecutor{execute: func(_ context.Context, request protocol.ExecutionRequest, _ localipc.ExecutionEvents) (protocol.ExecutionResult, error) {
				return test.result(request), nil
			}})
			var stdout, stderr bytes.Buffer
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			err := run(ctx, []string{"run", "--state-dir", daemon.stateDir, "--workspace", testWorkspaceID, "--", "go"}, &stdout, &stderr)
			if code := exitCode(err, &stderr); code != test.wantCode {
				t.Fatalf("exitCode() = %d, want %d; stderr = %q", code, test.wantCode, stderr.String())
			}
			if test.wantStderr == "" {
				if strings.Contains(stderr.String(), "error:") {
					t.Fatalf("stderr = %q, want no error line", stderr.String())
				}
				return
			}
			if !strings.Contains(stderr.String(), test.wantStderr) {
				t.Fatalf("stderr = %q, want %q", stderr.String(), test.wantStderr)
			}
		})
	}
}

func TestSecurity_RunCLI_CtrlCCancelsTheRun(t *testing.T) {
	began := make(chan struct{})
	cancelled := make(chan struct{})
	daemon := startRunDaemon(t, &recordingExecutor{execute: func(ctx context.Context, request protocol.ExecutionRequest, events localipc.ExecutionEvents) (protocol.ExecutionResult, error) {
		events.Started(startedFor(request))
		close(began)
		<-ctx.Done()
		close(cancelled)
		return protocol.ExecutionResult{}, errors.New("cancelled")
	}})

	ctx, cancel := context.WithCancel(t.Context())
	var stdout, stderr bytes.Buffer
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, []string{"run", "--state-dir", daemon.stateDir, "--workspace", testWorkspaceID, "--", "sleep", "100"}, &stdout, &stderr)
	}()
	select {
	case <-began:
	case <-time.After(5 * time.Second):
		t.Fatal("the run never started")
	}
	cancel()
	err := <-done
	if code := exitCode(err, &stderr); code != 130 {
		t.Fatalf("exitCode() = %d, want 130; stderr = %q", code, stderr.String())
	}
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("the executor was not cancelled")
	}
	if strings.Contains(stderr.String(), "error:") {
		t.Fatalf("stderr = %q, want no error line after Ctrl-C", stderr.String())
	}
}

func TestRunCLI_ReportsAMissingMio(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	var stdout, stderr bytes.Buffer
	err := run(t.Context(), []string{"run", "--state-dir", missing, "--workspace", testWorkspaceID, "--", "go"}, &stdout, &stderr)
	if !errors.Is(err, localipc.ErrDaemonUnavailable) {
		t.Fatalf("run() error = %v, want ErrDaemonUnavailable", err)
	}
	if code := exitCode(err, &stderr); code != 1 {
		t.Fatalf("exitCode() = %d, want 1", code)
	}
}

func TestRunCLI_ResolvesAPathLikeWorkspaceRemove(t *testing.T) {
	stateDir, executor := startWorkspaceRunDaemon(t)
	workspaceDir := makeWorkspaceDir(t, t.TempDir())
	id := addWorkspace(t, stateDir, workspaceDir)

	for name, path := range map[string]string{
		"the recorded path": workspaceDir,
		"a symlink to it":   symlinkTo(t, workspaceDir),
	} {
		t.Run(name, func(t *testing.T) {
			_, stderr, err := runCLI(t, "run", "--state-dir", stateDir, "--path", path, "--", "go")
			if err != nil {
				t.Fatalf("run(--path) error = %v; stderr = %q", err, stderr)
			}
			request := executor.onlyRequest(t)
			if request.Resources.WorkspaceID != id {
				t.Fatalf("workspace ID = %q, want %q", request.Resources.WorkspaceID, id)
			}
			executor.reset()
		})
	}
}

func TestSecurity_RunCLI_RefusesAnUnregisteredPath(t *testing.T) {
	stateDir, executor := startWorkspaceRunDaemon(t)
	unknown := makeWorkspaceDir(t, t.TempDir())

	var stdout, stderr bytes.Buffer
	err := run(t.Context(), []string{"run", "--state-dir", stateDir, "--path", unknown, "--", "go"}, &stdout, &stderr)
	if !errors.Is(err, errWorkspacePathNotRegistered) {
		t.Fatalf("run(--path) error = %v, want errWorkspacePathNotRegistered", err)
	}
	if calls := executor.calls(); calls != 0 {
		t.Fatalf("the executor ran %d times, want none", calls)
	}
}

func TestRunCLI_HelpListsTheCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := run(t.Context(), []string{"--help"}, &stdout, &stderr); err == nil {
		t.Fatal("run(--help) error = nil, want flag.ErrHelp")
	}
	if !strings.Contains(stderr.String(), "run [--state-dir PATH]") {
		t.Fatalf("usage = %q, want the run command", stderr.String())
	}
}

func TestRunCLI_RunHelpReturnsSuccessSentinel(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := run(t.Context(), []string{"run", "--help"}, &stdout, &stderr); !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("run(--help) error = %v, want flag.ErrHelp", err)
	}
}

// runDaemon is an in-process Mio serving execution.run with executor.
type runDaemon struct {
	stateDir string
	executor *recordingExecutor
}

// startRunDaemon starts Mio with executor on a fresh state directory.
func startRunDaemon(t *testing.T, executor *recordingExecutor) runDaemon {
	t.Helper()
	return startRunDaemonOn(t, filepath.Join(t.TempDir(), "run"), executor)
}

// startRunDaemonOn starts Mio with executor on stateDir, creating it if
// necessary.
func startRunDaemonOn(t *testing.T, stateDir string, executor *recordingExecutor) runDaemon {
	t.Helper()
	requireLinux(t)
	daemon, err := localipc.Listen(stateDir, "test-version")
	if err != nil {
		t.Fatalf("localipc.Listen() error = %v", err)
	}
	if err := daemon.EnableExecutions(executor); err != nil {
		t.Fatalf("EnableExecutions() error = %v", err)
	}
	serveDaemon(t, daemon)
	return runDaemon{stateDir: daemon.StateDir(), executor: executor}
}

// startWorkspaceRunDaemon serves Mio with a real workspace registry and
// execution, as scoprad does, and returns the state directory.
func startWorkspaceRunDaemon(t *testing.T) (string, *recordingExecutor) {
	t.Helper()
	requireLinux(t)
	t.Setenv("HOME", t.TempDir())
	daemon, err := localipc.Listen(filepath.Join(t.TempDir(), "run"), "test-version")
	if err != nil {
		t.Fatalf("localipc.Listen() error = %v", err)
	}
	validator, err := workspace.NewValidator(daemon.StateDir())
	if err != nil {
		t.Fatalf("NewValidator() error = %v", err)
	}
	workspaces, err := registry.Open(daemon.StateDir(), validator)
	if err != nil {
		t.Fatalf("registry.Open() error = %v", err)
	}
	if err := daemon.EnableWorkspaces(workspaces); err != nil {
		t.Fatalf("EnableWorkspaces() error = %v", err)
	}
	executor := &recordingExecutor{execute: func(_ context.Context, request protocol.ExecutionRequest, _ localipc.ExecutionEvents) (protocol.ExecutionResult, error) {
		return succeededFor(request), nil
	}}
	if err := daemon.EnableExecutions(executor); err != nil {
		t.Fatalf("EnableExecutions() error = %v", err)
	}
	serveDaemon(t, daemon)
	return daemon.StateDir(), executor
}

// serveDaemon serves daemon until the test ends.
func serveDaemon(t *testing.T, daemon *localipc.Daemon) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- daemon.Serve(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("daemon did not stop")
		}
		_ = daemon.Close()
	})
}

// recordingExecutor records the requests it runs and runs execute for each.
type recordingExecutor struct {
	execute func(context.Context, protocol.ExecutionRequest, localipc.ExecutionEvents) (protocol.ExecutionResult, error)

	mu       sync.Mutex
	requests []protocol.ExecutionRequest
}

func (e *recordingExecutor) Execute(ctx context.Context, request protocol.ExecutionRequest, events localipc.ExecutionEvents) (protocol.ExecutionResult, error) {
	e.mu.Lock()
	e.requests = append(e.requests, request)
	e.mu.Unlock()
	return e.execute(ctx, request, events)
}

func (e *recordingExecutor) calls() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.requests)
}

func (e *recordingExecutor) reset() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.requests = nil
}

func (e *recordingExecutor) onlyRequest(t *testing.T) protocol.ExecutionRequest {
	t.Helper()
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.requests) != 1 {
		t.Fatalf("the executor ran %d times, want once", len(e.requests))
	}
	return e.requests[0]
}

func hashOf(request protocol.ExecutionRequest) string {
	hash, _ := request.ActionHash()
	return hash
}

func startedFor(request protocol.ExecutionRequest) protocol.ExecutionStarted {
	return protocol.ExecutionStarted{
		ExecutionID: request.ExecutionID,
		ActionHash:  hashOf(request),
		StartedAt:   time.Date(2026, 10, 17, 12, 0, 1, 250_000_000, time.UTC),
	}
}

func outputFor(request protocol.ExecutionRequest, sequence uint64, stream protocol.OutputStream, data []byte) protocol.ExecutionOutput {
	return protocol.ExecutionOutput{ExecutionID: request.ExecutionID, Sequence: sequence, Stream: stream, Data: data}
}

// succeededFor returns a valid succeeded result for request.
func succeededFor(request protocol.ExecutionRequest) protocol.ExecutionResult {
	return protocol.ExecutionResult{
		ExecutionID: request.ExecutionID,
		ActionHash:  hashOf(request),
		State:       protocol.StateSucceeded,
		StartedAt:   time.Date(2026, 10, 17, 12, 0, 1, 250_000_000, time.UTC),
		EndedAt:     time.Date(2026, 10, 17, 12, 0, 2, 500_000_000, time.UTC),
	}
}

func rejectedFor(request protocol.ExecutionRequest, code, message string) protocol.ExecutionResult {
	return protocol.ExecutionResult{
		ExecutionID: request.ExecutionID,
		ActionHash:  hashOf(request),
		State:       protocol.StateRejected,
		Error:       protocol.ResultError{Code: code, Message: message},
	}
}

// jsonStringField reads a string field of a decoded JSON object.
func jsonStringField(t *testing.T, object map[string]json.RawMessage, name string) string {
	t.Helper()
	value, ok := object[name]
	if !ok {
		t.Fatalf("the JSON object has no %s field", name)
	}
	var text string
	if err := json.Unmarshal(value, &text); err != nil {
		t.Fatalf("the %s field is not a string: %v", name, err)
	}
	return text
}

// symlinkTo creates a symlink to dir and returns its path.
func symlinkTo(t *testing.T, dir string) string {
	t.Helper()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}
	return link
}

func isUsageError(err error) bool {
	var usageErr *usageError
	return errors.As(err, &usageErr)
}

func TestRunCLI_ReportsAFailedWrite(t *testing.T) {
	daemon := startRunDaemon(t, &recordingExecutor{execute: func(_ context.Context, request protocol.ExecutionRequest, events localipc.ExecutionEvents) (protocol.ExecutionResult, error) {
		events.Started(startedFor(request))
		events.Output(outputFor(request, 1, protocol.StreamStdout, []byte("lost\n")))
		return succeededFor(request), nil
	}})
	var stderr bytes.Buffer
	err := run(t.Context(), []string{"run", "--state-dir", daemon.stateDir, "--workspace", testWorkspaceID, "--", "go"}, failingWriter{}, &stderr)
	if err == nil || !strings.Contains(err.Error(), "write run output") {
		t.Fatalf("run() error = %v, want a write failure", err)
	}
}

// failingWriter fails every write, as a closed pipe does.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
