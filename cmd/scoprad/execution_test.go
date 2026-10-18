package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SynergyFlowLabs/scopra/internal/localipc"
	"github.com/SynergyFlowLabs/scopra/packages/protocol"
)

// fakePodmanEnv names the directory of a fake Podman scenario. When it is
// set, the test binary acts as podman instead of running tests, so that Mio's
// wiring can be tested without Podman (ADR-011 increment 2).
const fakePodmanEnv = "SCOPRAD_FAKE_PODMAN"

func TestMain(m *testing.M) {
	if dir := os.Getenv(fakePodmanEnv); dir != "" {
		os.Exit(fakePodman(dir, os.Args[1:]))
	}
	os.Exit(m.Run())
}

// fakePodmanInfo is podman info output from a host that meets ADR-010.
const fakePodmanInfo = `{"host":{"cgroupVersion":"v2","cgroupControllers":["cpu","memory","pids"],"security":{"rootless":true}},"version":{"Version":"5.2.0"}}`

// fakePodmanScenario scripts the one container that the fake runs.
type fakePodmanScenario struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exitCode"`
}

// fakeContainerState is the container state that fake podman inspect reports.
type fakeContainerState struct {
	StartedAt  time.Time `json:"StartedAt"`
	FinishedAt time.Time `json:"FinishedAt"`
	ExitCode   int       `json:"ExitCode"`
	OOMKilled  bool      `json:"OOMKilled"`
}

// fakePodman imitates the podman commands that Mio's startup and one run use.
func fakePodman(dir string, args []string) int {
	if len(args) == 0 {
		return 125
	}
	appendLine(dir, "calls.log", strings.Join(args, " "))
	switch args[0] {
	case "info":
		fmt.Println(fakePodmanInfo)
		return 0
	case "image":
		if len(args) == 3 && args[1] == "exists" {
			if fileExists(filepath.Join(dir, "image-missing")) {
				return 1
			}
			return 0
		}
	case "ps":
		if fileExists(filepath.Join(dir, "ps-broken")) {
			return 125
		}
		if data, err := os.ReadFile(filepath.Join(dir, "leftovers.txt")); err == nil {
			_, _ = os.Stdout.Write(data)
		}
		return 0
	case "run":
		return fakeRun(dir, args[1:])
	case "inspect":
		name := args[len(args)-1]
		data, err := os.ReadFile(statePath(dir, name))
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error: no such container")
			return 125
		}
		fmt.Printf("[{\"Id\":\"0123\",\"Name\":%q,\"State\":%s}]\n", name, data)
		return 0
	case "rm":
		name := args[len(args)-1]
		appendLine(dir, "removed.log", name)
		_ = os.Remove(statePath(dir, name))
		return 0
	}
	fmt.Fprintf(os.Stderr, "fake podman: unexpected call %q\n", args)
	return 125
}

// fakeRun writes the scripted output and the container state, then exits with
// the scripted status.
func fakeRun(dir string, args []string) int {
	var name string
	for _, arg := range args {
		if value, ok := strings.CutPrefix(arg, "--name="); ok {
			name = value
		}
	}
	if name == "" {
		fmt.Fprintln(os.Stderr, "fake podman: run without --name")
		return 125
	}
	var scenario fakePodmanScenario
	if data, err := os.ReadFile(filepath.Join(dir, "scenario.json")); err == nil {
		_ = json.Unmarshal(data, &scenario)
	}
	started := time.Now().UTC()
	_ = writeContainerState(dir, name, fakeContainerState{StartedAt: started, ExitCode: 0})
	if scenario.Stdout != "" {
		_, _ = io.WriteString(os.Stdout, scenario.Stdout)
	}
	if scenario.Stderr != "" {
		_, _ = io.WriteString(os.Stderr, scenario.Stderr)
	}
	_ = writeContainerState(dir, name, fakeContainerState{
		StartedAt:  started,
		FinishedAt: time.Now().UTC(),
		ExitCode:   scenario.ExitCode,
	})
	return scenario.ExitCode & 0xff
}

func statePath(dir, name string) string {
	return filepath.Join(dir, "container-"+name+".json")
}

// writeContainerState replaces the state atomically, as podman updates it.
func writeContainerState(dir, name string, state fakeContainerState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	temporary := statePath(dir, name) + ".tmp"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, statePath(dir, name))
}

func appendLine(dir, name, line string) {
	file, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	_, _ = file.WriteString(line + "\n")
	_ = file.Close()
}

func fileExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// startFakePodman prepares a fake Podman scenario and puts a test binary
// symlink named podman first on PATH, so that Mio's exec.LookPath finds it.
func startFakePodman(t *testing.T, scenario fakePodmanScenario) string {
	t.Helper()
	dir := t.TempDir()
	data, err := json.Marshal(scenario)
	if err != nil {
		t.Fatalf("encode the scenario: %v", err)
	}
	writeFile(t, filepath.Join(dir, "scenario.json"), string(data), 0o600)
	binDir := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("find the test binary: %v", err)
	}
	if err := os.Symlink(executable, filepath.Join(binDir, "podman")); err != nil {
		t.Fatalf("create the fake podman symlink: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv(fakePodmanEnv, dir)
	// A race-enabled binary sleeps for a second before exiting unless told
	// otherwise, and every fake podman call is one.
	t.Setenv("GORACE", "atexit_sleep_ms=0")
	return dir
}

// startScoprad starts Mio in the background and waits until it reports that
// it is listening. The returned function stops it, and is also registered as
// a cleanup.
func startScoprad(t *testing.T, args ...string) (string, *signalOnWrite, func()) {
	t.Helper()
	stateDir := makeStateDir(t)
	ctx, cancel := context.WithCancel(t.Context())
	stderr := &signalOnWrite{written: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, append([]string{"--state-dir", stateDir}, args...), io.Discard, stderr)
	}()
	select {
	case <-stderr.written:
	case err := <-done:
		cancel()
		t.Fatalf("scoprad returned before listening: %v", err)
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("scoprad did not report startup")
	}
	if !strings.Contains(stderr.String(), "listening") {
		cancel()
		t.Fatalf("startup output = %q, want listening", stderr.String())
	}
	var once sync.Once
	stop := func() {
		once.Do(func() {
			cancel()
			select {
			case err := <-done:
				if err != nil {
					t.Errorf("scoprad after cancellation error = %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Error("scoprad did not stop after cancellation")
			}
		})
	}
	t.Cleanup(stop)
	return stateDir, stderr, stop
}

// fakeCalls returns the podman calls that the fake recorded.
func fakeCalls(t *testing.T, dir string) [][]string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "calls.log"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatalf("read the fake podman calls: %v", err)
	}
	var calls [][]string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		calls = append(calls, strings.Fields(line))
	}
	return calls
}

// runCalls returns the fake's podman run calls.
func runCalls(t *testing.T, dir string) [][]string {
	t.Helper()
	var runs [][]string
	for _, call := range fakeCalls(t, dir) {
		if len(call) > 0 && call[0] == "run" {
			runs = append(runs, call)
		}
	}
	return runs
}

// readLines returns the lines of a file, or nil when it does not exist.
func readLines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return strings.Fields(string(data))
}

func waitForText(t *testing.T, writer *signalOnWrite, text string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(writer.String(), text) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("stderr = %q, want %q", writer.String(), text)
}

// testRun returns a local run request like scopra run will send (ADR-011).
func testRun(workspaceID string) localipc.RunRequest {
	return localipc.RunRequest{
		WorkspaceID: workspaceID,
		Command: protocol.Command{
			Executable:       "go",
			Arguments:        []string{"test", "./..."},
			WorkingDirectory: ".",
		},
		WorkspaceAccess: protocol.WorkspaceReadOnly,
		Limits: protocol.Limits{
			TimeoutSeconds: 60,
			CPUMillicores:  1000,
			MemoryBytes:    512 << 20,
			PIDs:           64,
			OutputBytes:    1 << 20,
		},
		Reason: "local run by the workspace owner",
	}
}

func TestRunExecution_RunsInHollowWithTheFakePodman(t *testing.T) {
	requireLinux(t)
	fakeDir := startFakePodman(t, fakePodmanScenario{Stdout: "hello from the sandbox\n", Stderr: "a warning\n"})
	stateDir, _, _ := startScoprad(t, "--sandbox-image="+testSandboxImage)
	workspaceDir := makeStateDir(t)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	added, err := localipc.AddWorkspace(ctx, stateDir, workspaceDir)
	if err != nil {
		t.Fatalf("AddWorkspace() error = %v", err)
	}

	var stdout, stderr bytes.Buffer
	var order []string
	var notice protocol.ExecutionStarted
	result, err := localipc.RunExecution(ctx, stateDir, testRun(added.ID), localipc.RunEvents{
		Started: func(started protocol.ExecutionStarted) {
			notice = started
			order = append(order, "started")
		},
		Output: func(stream protocol.OutputStream, data []byte) {
			order = append(order, string(stream))
			if stream == protocol.StreamStdout {
				stdout.Write(data)
			} else {
				stderr.Write(data)
			}
		},
	})
	if err != nil {
		t.Fatalf("RunExecution() error = %v", err)
	}
	if result.State != protocol.StateSucceeded || result.ExitCode != 0 {
		t.Fatalf("RunExecution() result = %+v, want succeeded with exit code 0", result)
	}
	if stdout.String() != "hello from the sandbox\n" || stderr.String() != "a warning\n" {
		t.Fatalf("streams = %q and %q, want the fake's output", stdout.String(), stderr.String())
	}
	if len(order) < 1 || order[0] != "started" {
		t.Fatalf("events = %q, want the start notice first", order)
	}
	if notice.ExecutionID != result.ExecutionID || notice.ActionHash != result.ActionHash {
		t.Fatalf("the start notice does not describe the result: %+v vs %+v", notice, result)
	}
	if result.Output.StdoutBytes != uint64(len("hello from the sandbox\n")) ||
		result.Output.StderrBytes != uint64(len("a warning\n")) {
		t.Fatalf("output summary = %+v, want the bytes the fake wrote", result.Output)
	}

	runs := runCalls(t, fakeDir)
	if len(runs) != 1 {
		t.Fatalf("podman run calls = %d, want 1", len(runs))
	}
	arguments := strings.Join(runs[0], " ")
	for _, want := range []string{
		"--network=none",
		`--mount=type=bind,"src=` + workspaceDir + `",dst=/workspace,ro=true`,
		testSandboxImage,
	} {
		if !strings.Contains(arguments, want) {
			t.Fatalf("podman run %q does not contain %q", arguments, want)
		}
	}
	removals := readLines(t, filepath.Join(fakeDir, "removed.log"))
	if len(removals) != 1 || !strings.HasPrefix(removals[0], "scopra-") {
		t.Fatalf("removed containers = %q, want the run's container", removals)
	}
}

func TestSecurity_RunExecutionRejectsAnUnknownWorkspaceWithoutCallingPodman(t *testing.T) {
	requireLinux(t)
	fakeDir := startFakePodman(t, fakePodmanScenario{Stdout: "ran\n"})
	stateDir, _, _ := startScoprad(t, "--sandbox-image="+testSandboxImage)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	result, err := localipc.RunExecution(ctx, stateDir, testRun(strings.Repeat("f", 32)), localipc.RunEvents{})
	if err != nil {
		t.Fatalf("RunExecution() error = %v", err)
	}
	if result.State != protocol.StateRejected || result.Error.Code != protocol.ErrorCodeWorkspaceUnavailable {
		t.Fatalf("RunExecution() result = %+v, want a workspace_unavailable rejection", result)
	}
	if runs := runCalls(t, fakeDir); len(runs) != 0 {
		t.Fatalf("podman run calls = %d, want none", len(runs))
	}
}

func TestSecurity_RunExecutionRejectsAWorkspaceThatChanged(t *testing.T) {
	requireLinux(t)
	fakeDir := startFakePodman(t, fakePodmanScenario{Stdout: "ran\n"})
	stateDir, _, _ := startScoprad(t, "--sandbox-image="+testSandboxImage)
	workspaceDir := makeStateDir(t)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	added, err := localipc.AddWorkspace(ctx, stateDir, workspaceDir)
	if err != nil {
		t.Fatalf("AddWorkspace() error = %v", err)
	}
	// Move the recorded directory away and put a new one at its path, so the
	// registration's fingerprint no longer matches.
	if err := os.Rename(workspaceDir, workspaceDir+"-moved"); err != nil {
		t.Fatalf("Rename() error = %v", err)
	}
	if err := os.Mkdir(workspaceDir, 0o700); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}

	result, err := localipc.RunExecution(ctx, stateDir, testRun(added.ID), localipc.RunEvents{})
	if err != nil {
		t.Fatalf("RunExecution() error = %v", err)
	}
	if result.State != protocol.StateRejected || result.Error.Code != protocol.ErrorCodeWorkspaceUnavailable {
		t.Fatalf("RunExecution() result = %+v, want a workspace_unavailable rejection", result)
	}
	if runs := runCalls(t, fakeDir); len(runs) != 0 {
		t.Fatalf("podman run calls = %d, want none", len(runs))
	}
}

func TestRunExecution_WithoutASandboxImageIsRejected(t *testing.T) {
	requireLinux(t)
	// No podman either, so the image check cannot be confused with the
	// Podman check, whatever the host has installed.
	t.Setenv("PATH", t.TempDir())
	stateDir, _, _ := startScoprad(t)
	workspaceDir := makeStateDir(t)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	added, err := localipc.AddWorkspace(ctx, stateDir, workspaceDir)
	if err != nil {
		t.Fatalf("AddWorkspace() error = %v", err)
	}
	result, err := localipc.RunExecution(ctx, stateDir, testRun(added.ID), localipc.RunEvents{})
	if err != nil {
		t.Fatalf("RunExecution() error = %v", err)
	}
	if result.State != protocol.StateRejected || result.Error.Code != protocol.ErrorCodeContainmentUnavailable {
		t.Fatalf("RunExecution() result = %+v, want a containment_unavailable rejection", result)
	}
	if !strings.Contains(result.Error.Message, "--sandbox-image") {
		t.Fatalf("rejection message = %q, want the missing setting named", result.Error.Message)
	}
}

func TestRunExecution_RemovesLeftoverContainersAtStartup(t *testing.T) {
	requireLinux(t)
	leftover := "scopra-" + strings.Repeat("a", 32)
	fakeDir := startFakePodman(t, fakePodmanScenario{})
	writeFile(t, filepath.Join(fakeDir, "leftovers.txt"), leftover+"\nunrelated\n", 0o600)

	startScoprad(t, "--sandbox-image="+testSandboxImage)

	removals := readLines(t, filepath.Join(fakeDir, "removed.log"))
	if len(removals) != 1 || removals[0] != leftover {
		t.Fatalf("removed containers = %q, want only %q", removals, leftover)
	}
}

func TestSecurity_RunExecutionFailsClosedWhenCleanupFails(t *testing.T) {
	requireLinux(t)
	fakeDir := startFakePodman(t, fakePodmanScenario{})
	writeFile(t, filepath.Join(fakeDir, "ps-broken"), "", 0o600)
	stateDir, stderr, _ := startScoprad(t, "--sandbox-image="+testSandboxImage)
	waitForText(t, stderr, "leftover container cleanup failed")
	workspaceDir := makeStateDir(t)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if _, err := localipc.Status(ctx, stateDir); err != nil {
		t.Fatalf("Status() with refused executions error = %v, want a serving Mio", err)
	}
	added, err := localipc.AddWorkspace(ctx, stateDir, workspaceDir)
	if err != nil {
		t.Fatalf("AddWorkspace() error = %v", err)
	}
	result, err := localipc.RunExecution(ctx, stateDir, testRun(added.ID), localipc.RunEvents{})
	if err != nil {
		t.Fatalf("RunExecution() error = %v", err)
	}
	if result.State != protocol.StateRejected || result.Error.Code != protocol.ErrorCodeContainmentUnavailable {
		t.Fatalf("RunExecution() result = %+v, want a containment_unavailable rejection", result)
	}
	if !strings.Contains(result.Error.Message, "leftover") {
		t.Fatalf("rejection message = %q, want the cleanup failure named", result.Error.Message)
	}
	if runs := runCalls(t, fakeDir); len(runs) != 0 {
		t.Fatalf("podman run calls = %d, want none", len(runs))
	}
}

func TestRunExecution_ReportsAFailedCommand(t *testing.T) {
	requireLinux(t)
	startFakePodman(t, fakePodmanScenario{Stdout: "boom\n", ExitCode: 3})
	stateDir, _, _ := startScoprad(t, "--sandbox-image="+testSandboxImage)
	workspaceDir := makeStateDir(t)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	added, err := localipc.AddWorkspace(ctx, stateDir, workspaceDir)
	if err != nil {
		t.Fatalf("AddWorkspace() error = %v", err)
	}
	var stdout bytes.Buffer
	result, err := localipc.RunExecution(ctx, stateDir, testRun(added.ID), localipc.RunEvents{
		Output: func(stream protocol.OutputStream, data []byte) {
			if stream == protocol.StreamStdout {
				stdout.Write(data)
			}
		},
	})
	if err != nil {
		t.Fatalf("RunExecution() error = %v", err)
	}
	if result.State != protocol.StateFailed || result.ExitCode != 3 {
		t.Fatalf("RunExecution() result = %+v, want failed with exit code 3", result)
	}
	if stdout.String() != "boom\n" {
		t.Fatalf("stdout = %q, want the fake's output", stdout.String())
	}
}

func TestRunExecution_WithoutPodmanIsRejected(t *testing.T) {
	requireLinux(t)
	t.Setenv("PATH", t.TempDir())
	stateDir, stderr, _ := startScoprad(t, "--sandbox-image="+testSandboxImage)
	waitForText(t, stderr, "podman was not found")
	workspaceDir := makeStateDir(t)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	added, err := localipc.AddWorkspace(ctx, stateDir, workspaceDir)
	if err != nil {
		t.Fatalf("AddWorkspace() error = %v", err)
	}
	result, err := localipc.RunExecution(ctx, stateDir, testRun(added.ID), localipc.RunEvents{})
	if err != nil {
		t.Fatalf("RunExecution() error = %v", err)
	}
	if result.State != protocol.StateRejected || result.Error.Code != protocol.ErrorCodeContainmentUnavailable {
		t.Fatalf("RunExecution() result = %+v, want a containment_unavailable rejection", result)
	}
	if !strings.Contains(result.Error.Message, "podman") {
		t.Fatalf("rejection message = %q, want podman named", result.Error.Message)
	}
}
