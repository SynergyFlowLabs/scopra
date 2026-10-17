package hollow

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SynergyFlowLabs/scopra/packages/protocol"
)

// fakePodmanEnv names the directory of a fake Podman scenario. When it is
// set, the test binary acts as podman instead of running tests, so the runner
// can be tested without Podman (ADR-010 increment 3a).
const fakePodmanEnv = "HOLLOW_FAKE_PODMAN"

func TestMain(m *testing.M) {
	if dir := os.Getenv(fakePodmanEnv); dir != "" {
		os.Exit(fakePodman(dir, os.Args[1:]))
	}
	os.Exit(m.Run())
}

// fakeScenario scripts one container.
type fakeScenario struct {
	// StartDelayMS passes before the container records its start.
	StartDelayMS int `json:"startDelayMs"`
	// NeverStart makes podman fail before the container starts, writing
	// PodmanStderr and exiting with PodmanExit.
	NeverStart   bool   `json:"neverStart"`
	PodmanExit   int    `json:"podmanExit"`
	PodmanStderr string `json:"podmanStderr"`
	// Writes happen in order after the start.
	Writes    []fakeWrite `json:"writes"`
	ExitCode  int         `json:"exitCode"`
	OOMKilled bool        `json:"oomKilled"`
}

// fakeWrite writes Bytes bytes of the stream's pattern, or Text.
type fakeWrite struct {
	Stream string `json:"stream"`
	Bytes  int    `json:"bytes"`
	Text   string `json:"text"`
}

// fakeState is the container state that fake `podman inspect` reports.
type fakeState struct {
	StartedAt  time.Time `json:"StartedAt"`
	FinishedAt time.Time `json:"FinishedAt"`
	ExitCode   int       `json:"ExitCode"`
	OOMKilled  bool      `json:"OOMKilled"`
}

// pattern returns the n bytes that fake writes produce for a stream, so that
// tests can check that what arrived is a prefix of what was written.
func pattern(stream string, offset, n int) []byte {
	alphabet := "abcdefghijklmnopqrstuvwxyz"
	if stream == "stderr" {
		alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	}
	data := make([]byte, n)
	for i := range data {
		data[i] = alphabet[(offset+i)%len(alphabet)]
	}
	return data
}

func fakePodman(dir string, args []string) int {
	calls, err := os.OpenFile(filepath.Join(dir, "calls.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err == nil {
		line, _ := json.Marshal(args)
		_, _ = calls.Write(append(line, '\n'))
		_ = calls.Close()
	}
	if len(args) == 0 {
		return 125
	}
	switch {
	case args[0] == "run":
		return fakeRun(dir, args[1:])
	case args[0] == "inspect" && len(args) == 6 && slices.Equal(args[1:5], []string{"--type", "container", "--format", "json"}):
		return fakeInspect(dir, args[5])
	case args[0] == "rm" && len(args) == 3 && args[1] == "--force":
		return fakeRemove(dir, args[2])
	}
	fmt.Fprintf(os.Stderr, "Error: unexpected fake podman call %q\n", args)
	return 125
}

func fakeRun(dir string, args []string) int {
	var name string
	for _, arg := range args {
		if value, ok := strings.CutPrefix(arg, "--name="); ok {
			name = value
		}
	}
	data, err := os.ReadFile(filepath.Join(dir, "scenario.json"))
	var scenario fakeScenario
	if name == "" || err != nil || json.Unmarshal(data, &scenario) != nil {
		fmt.Fprintln(os.Stderr, "Error: fake podman has no scenario or container name")
		return 125
	}
	state := fakeState{}
	if scenario.NeverStart {
		writeFakeState(dir, name, state)
		fmt.Fprint(os.Stderr, scenario.PodmanStderr)
		return scenario.PodmanExit
	}
	time.Sleep(time.Duration(scenario.StartDelayMS) * time.Millisecond)
	state.StartedAt = time.Now().UTC()
	writeFakeState(dir, name, state)
	offsets := map[string]int{}
	for _, write := range scenario.Writes {
		out := os.Stdout
		if write.Stream == "stderr" {
			out = os.Stderr
		}
		payload := []byte(write.Text)
		if write.Bytes > 0 {
			payload = pattern(write.Stream, offsets[write.Stream], write.Bytes)
		}
		offsets[write.Stream] += len(payload)
		_, _ = out.Write(payload)
	}
	state.FinishedAt = time.Now().UTC()
	state.ExitCode, state.OOMKilled = scenario.ExitCode, scenario.OOMKilled
	writeFakeState(dir, name, state)
	return scenario.ExitCode
}

func writeFakeState(dir, name string, state fakeState) {
	data, _ := json.Marshal(state)
	temporary := filepath.Join(dir, "container-"+name+".tmp")
	_ = os.WriteFile(temporary, data, 0o600)
	_ = os.Rename(temporary, filepath.Join(dir, "container-"+name+".json"))
}

func fakeInspect(dir, name string) int {
	data, err := os.ReadFile(filepath.Join(dir, "container-"+name+".json"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: no such container %s\n", name)
		return 125
	}
	fmt.Printf(`[{"Id":"0123","Name":%q,"State":%s}]`+"\n", name, data)
	return 0
}

func fakeRemove(dir, name string) int {
	log, err := os.OpenFile(filepath.Join(dir, "removed.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err == nil {
		_, _ = log.WriteString(name + "\n")
		_ = log.Close()
	}
	if err := os.Rename(filepath.Join(dir, "container-"+name+".json"), filepath.Join(dir, "removed-"+name+".json")); err != nil {
		fmt.Fprintf(os.Stderr, "Error: no such container %s\n", name)
		return 1
	}
	return 0
}

// fakeHost is one fake Podman scenario for a test.
type fakeHost struct {
	t   *testing.T
	dir string
}

func newFakeRunner(t *testing.T, scenario fakeScenario) (Runner, fakeHost) {
	t.Helper()
	dir := t.TempDir()
	data, err := json.Marshal(scenario)
	if err != nil {
		t.Fatalf("encode the scenario: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scenario.json"), data, 0o600); err != nil {
		t.Fatalf("write the scenario: %v", err)
	}
	t.Setenv(fakePodmanEnv, dir)
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("find the test binary: %v", err)
	}
	return Runner{Podman: executable, Image: testImage, pollInterval: 2 * time.Millisecond}, fakeHost{t: t, dir: dir}
}

func (h fakeHost) calls() [][]string {
	h.t.Helper()
	data, err := os.ReadFile(filepath.Join(h.dir, "calls.log"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		h.t.Fatalf("read the calls: %v", err)
	}
	var calls [][]string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var call []string
		if err := json.Unmarshal([]byte(line), &call); err != nil {
			h.t.Fatalf("decode a call: %v", err)
		}
		calls = append(calls, call)
	}
	return calls
}

func (h fakeHost) removals() []string {
	h.t.Helper()
	data, err := os.ReadFile(filepath.Join(h.dir, "removed.log"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		h.t.Fatalf("read the removals: %v", err)
	}
	return strings.Fields(string(data))
}

func (h fakeHost) finalState(name string) fakeState {
	h.t.Helper()
	data, err := os.ReadFile(filepath.Join(h.dir, "removed-"+name+".json"))
	if err != nil {
		h.t.Fatalf("read the final state of %s: %v", name, err)
	}
	var state fakeState
	if err := json.Unmarshal(data, &state); err != nil {
		h.t.Fatalf("decode the final state: %v", err)
	}
	return state
}

// recorder keeps the events of one run in order.
type recorder struct {
	mu      sync.Mutex
	started []protocol.ExecutionStarted
	outputs []protocol.ExecutionOutput
	order   []string
}

func (r *recorder) events() Events {
	return Events{
		Started: func(started protocol.ExecutionStarted) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.started = append(r.started, started)
			r.order = append(r.order, "started")
		},
		Output: func(output protocol.ExecutionOutput) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.outputs = append(r.outputs, output)
			r.order = append(r.order, "output")
		},
	}
}

// streams returns everything that arrived on each stream.
func (r *recorder) streams() (stdout, stderr []byte) {
	for _, output := range r.outputs {
		if output.Stream == protocol.StreamStdout {
			stdout = append(stdout, output.Data...)
		} else {
			stderr = append(stderr, output.Data...)
		}
	}
	return stdout, stderr
}

func containerName(t *testing.T, request protocol.ExecutionRequest) string {
	t.Helper()
	hash, err := request.ActionHash()
	if err != nil {
		t.Fatalf("ActionHash() error = %v", err)
	}
	return "scopra-" + hash[:32]
}

// checkRun verifies what every completed run must satisfy: the exact
// arguments, a result valid for the request whose counts match the chunks,
// contiguous sequence numbers, and exactly one removal of the container.
func checkRun(t *testing.T, runner Runner, host fakeHost, request protocol.ExecutionRequest, result protocol.ExecutionResult, events *recorder) {
	t.Helper()
	if err := result.ValidateAgainst(request); err != nil {
		t.Fatalf("result %+v is not valid for the request: %v", result, err)
	}
	args, err := PodmanRunArgs(RunSpec{Request: request, WorkspacePath: testWorkspace, Image: runner.Image})
	if err != nil {
		t.Fatalf("PodmanRunArgs() error = %v", err)
	}
	calls := host.calls()
	if len(calls) < 2 || !slices.Equal(calls[0], args) {
		t.Fatalf("first podman call = %q, want %q", calls, args)
	}
	name := containerName(t, request)
	if want := []string{"rm", "--force", name}; !slices.Equal(calls[len(calls)-1], want) {
		t.Fatalf("last podman call = %q, want %q", calls[len(calls)-1], want)
	}
	if removals := host.removals(); !slices.Equal(removals, []string{name}) {
		t.Fatalf("removals = %q, want exactly %s", removals, name)
	}
	var stdoutBytes, stderrBytes uint64
	for i, output := range events.outputs {
		if output.Sequence != uint64(i+1) || output.ExecutionID != request.ExecutionID || len(output.Data) < 1 || len(output.Data) > 65536 {
			t.Fatalf("chunk %d = sequence %d, %d bytes; want sequence %d and 1 to 65536 bytes", i, output.Sequence, len(output.Data), i+1)
		}
		if err := output.Validate(); err != nil {
			t.Fatalf("chunk %d is invalid: %v", i, err)
		}
		if output.Stream == protocol.StreamStdout {
			stdoutBytes += uint64(len(output.Data))
		} else {
			stderrBytes += uint64(len(output.Data))
		}
	}
	if result.State != protocol.StateRejected {
		want := protocol.OutputSummary{StdoutBytes: stdoutBytes, StderrBytes: stderrBytes, Truncated: result.Output.Truncated, Chunks: uint64(len(events.outputs))}
		if result.Output != want {
			t.Fatalf("output summary = %+v, want %+v", result.Output, want)
		}
	}
	for i, kind := range events.order {
		if kind == "started" && i != 0 {
			t.Fatalf("event order = %q, want the start notice first", events.order)
		}
	}
}

func runFake(t *testing.T, scenario fakeScenario, change func(*protocol.ExecutionRequest)) (Runner, fakeHost, protocol.ExecutionRequest, protocol.ExecutionResult, *recorder) {
	t.Helper()
	runner, host := newFakeRunner(t, scenario)
	request := fixtureRequest(t, "execution-request-adr007")
	if change != nil {
		change(&request)
	}
	events := &recorder{}
	result, err := runner.Run(t.Context(), request, testWorkspace, events.events())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	return runner, host, request, result, events
}

func TestRunner_ReportsASuccessfulRun(t *testing.T) {
	runner, host, request, result, events := runFake(t, fakeScenario{Writes: []fakeWrite{{Stream: "stdout", Text: "hello\n"}}}, nil)
	checkRun(t, runner, host, request, result, events)
	state := host.finalState(containerName(t, request))
	hash, _ := request.ActionHash()
	if len(events.started) != 1 || events.started[0] != (protocol.ExecutionStarted{
		ExecutionID: request.ExecutionID, ActionHash: hash, StartedAt: state.StartedAt.Truncate(time.Millisecond),
	}) {
		t.Fatalf("start notices = %+v, want one at %v", events.started, state.StartedAt)
	}
	want := protocol.ExecutionResult{
		ExecutionID: request.ExecutionID, ActionHash: hash, State: protocol.StateSucceeded,
		StartedAt: state.StartedAt.Truncate(time.Millisecond), EndedAt: state.FinishedAt.Truncate(time.Millisecond),
		Output: protocol.OutputSummary{StdoutBytes: 6, Chunks: 1},
	}
	if result != want {
		t.Fatalf("result = %+v, want %+v", result, want)
	}
	if stdout, stderr := events.streams(); string(stdout) != "hello\n" || len(stderr) != 0 {
		t.Fatalf("output = %q and %q, want hello on stdout", stdout, stderr)
	}
}

func TestRunner_ReportsAFailedRun(t *testing.T) {
	runner, host, request, result, events := runFake(t, fakeScenario{Writes: []fakeWrite{{Stream: "stderr", Text: "boom\n"}}, ExitCode: 3}, nil)
	checkRun(t, runner, host, request, result, events)
	if result.State != protocol.StateFailed || result.ExitCode != 3 || result.Output.StderrBytes != 5 {
		t.Fatalf("result = %+v, want failed with exit code 3 and 5 bytes of stderr", result)
	}
}

func TestRunner_TakesTheExitCodeFromInspect(t *testing.T) {
	// podman run also exits with 125 for its own errors; a command's own 125
	// must still be "failed", which only inspect can tell.
	runner, host, request, result, events := runFake(t, fakeScenario{ExitCode: 125}, nil)
	checkRun(t, runner, host, request, result, events)
	if result.State != protocol.StateFailed || result.ExitCode != 125 || len(events.started) != 1 {
		t.Fatalf("result = %+v with %d start notices, want failed with exit code 125 after a start", result, len(events.started))
	}
}

func TestRunner_ReportsOutOfMemory(t *testing.T) {
	runner, host, request, result, events := runFake(t, fakeScenario{Writes: []fakeWrite{{Stream: "stdout", Bytes: 10}}, ExitCode: 137, OOMKilled: true}, nil)
	checkRun(t, runner, host, request, result, events)
	if result.State != protocol.StateOutOfMemory || result.ExitCode != 0 || result.StartedAt.IsZero() {
		t.Fatalf("result = %+v, want out_of_memory with times and no exit code", result)
	}
}

func TestRunner_ReportsAStartFailure(t *testing.T) {
	stderr := "Error: crun: executable file `go` not found in $PATH: No such file or directory: OCI runtime attempted to invoke a command that was not found\n"
	runner, host, request, result, events := runFake(t, fakeScenario{NeverStart: true, PodmanExit: 127, PodmanStderr: stderr}, nil)
	checkRun(t, runner, host, request, result, events)
	if len(events.started) != 0 {
		t.Fatalf("start notices = %+v, want none for a container that never started", events.started)
	}
	if result.State != protocol.StateErrored || result.Error.Code != protocol.ErrorCodeStartFailed || !strings.Contains(result.Error.Message, "127") {
		t.Fatalf("result = %+v, want errored with start_failed mentioning exit status 127", result)
	}
	if _, got := events.streams(); string(got) != stderr {
		t.Fatalf("stderr = %q, want Podman's message %q", got, stderr)
	}
}

func TestRunner_SendsTheStartNoticeBeforeAnyOutput(t *testing.T) {
	runner, host, request, result, events := runFake(t, fakeScenario{
		StartDelayMS: 40,
		Writes:       []fakeWrite{{Stream: "stdout", Text: "first"}, {Stream: "stderr", Text: "second"}},
	}, nil)
	checkRun(t, runner, host, request, result, events)
	if len(events.order) == 0 || events.order[0] != "started" {
		t.Fatalf("event order = %q, want the start notice first", events.order)
	}
}

func TestRunner_ChunksOutputInOneSequence(t *testing.T) {
	writes := []fakeWrite{
		{Stream: "stdout", Bytes: 100000}, {Stream: "stderr", Bytes: 70000}, {Stream: "stdout", Bytes: 50000}, {Stream: "stderr", Bytes: 1},
	}
	runner, host, request, result, events := runFake(t, fakeScenario{Writes: writes}, nil)
	checkRun(t, runner, host, request, result, events)
	stdout, stderr := events.streams()
	if !bytes.Equal(stdout, pattern("stdout", 0, 150000)) || !bytes.Equal(stderr, pattern("stderr", 0, 70001)) {
		t.Fatalf("received %d and %d bytes, want exactly the 150000 and 70001 written", len(stdout), len(stderr))
	}
	if result.Output.Truncated || len(events.outputs) < 4 {
		t.Fatalf("output summary = %+v over %d chunks, want no truncation and several chunks", result.Output, len(events.outputs))
	}
}

func TestSecurity_RunnerTruncatesOutputAtTheLimit(t *testing.T) {
	writes := []fakeWrite{
		{Stream: "stdout", Bytes: 150000}, {Stream: "stderr", Bytes: 70000}, {Stream: "stdout", Bytes: 30000},
	}
	runner, host, request, result, events := runFake(t, fakeScenario{Writes: writes, ExitCode: 0},
		func(r *protocol.ExecutionRequest) { r.Limits.OutputBytes = 100000 })
	checkRun(t, runner, host, request, result, events)
	stdout, stderr := events.streams()
	if total := len(stdout) + len(stderr); total != 100000 || !result.Output.Truncated {
		t.Fatalf("sent %d bytes with truncated = %v, want exactly 100000 and truncated", total, result.Output.Truncated)
	}
	if !bytes.Equal(stdout, pattern("stdout", 0, len(stdout))) || !bytes.Equal(stderr, pattern("stderr", 0, len(stderr))) {
		t.Fatal("each stream's output must be a prefix of what was written")
	}
	// The command still ran to completion: the output was read and discarded.
	if result.State != protocol.StateSucceeded {
		t.Fatalf("result = %+v, want succeeded", result)
	}
}

func TestRunner_RejectsWithoutRunningPodman(t *testing.T) {
	tests := []struct {
		name      string
		workspace string
		image     string
		code      string
	}{
		{name: "unmountable workspace", workspace: "relative/path", image: testImage, code: protocol.ErrorCodeWorkspaceUnavailable},
		{name: "image without a digest", workspace: testWorkspace, image: "registry.example/runner:1.0", code: protocol.ErrorCodeContainmentUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner, host := newFakeRunner(t, fakeScenario{})
			runner.Image = test.image
			request := fixtureRequest(t, "execution-request-adr007")
			events := &recorder{}
			result, err := runner.Run(t.Context(), request, test.workspace, events.events())
			if err != nil {
				t.Fatalf("Run() error = %v", err)
			}
			if result.State != protocol.StateRejected || result.Error.Code != test.code {
				t.Fatalf("result = %+v, want rejected with %s", result, test.code)
			}
			if err := result.ValidateAgainst(request); err != nil {
				t.Fatalf("result is not valid for the request: %v", err)
			}
			if calls := host.calls(); len(calls) != 0 || len(events.order) != 0 {
				t.Fatalf("podman calls = %q and events = %q, want none", calls, events.order)
			}
		})
	}
}

func TestRunner_RefusesAnInvalidRequestOrConfiguration(t *testing.T) {
	runner, host := newFakeRunner(t, fakeScenario{})
	invalid := fixtureRequest(t, "execution-request-adr007")
	invalid.Command.Shell = true
	if _, err := runner.Run(t.Context(), invalid, testWorkspace, Events{}); !errors.Is(err, protocol.ErrInvalidExecutionRequest) {
		t.Fatalf("Run() with an invalid request error = %v, want ErrInvalidExecutionRequest", err)
	}
	for _, podman := range []string{"", "podman", "bin/podman"} {
		relative := runner
		relative.Podman = podman
		if _, err := relative.Run(t.Context(), fixtureRequest(t, "execution-request-adr007"), testWorkspace, Events{}); !errors.Is(err, ErrInvalidRunner) {
			t.Fatalf("Run() with podman path %q error = %v, want ErrInvalidRunner", podman, err)
		}
	}
	if calls := host.calls(); len(calls) != 0 {
		t.Fatalf("podman calls = %q, want none", calls)
	}
}
