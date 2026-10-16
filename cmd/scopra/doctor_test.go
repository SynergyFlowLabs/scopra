package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

// doctorReportJSON is an independent mirror of the documented
// `scopra doctor --json` contract (schema version 1).
type doctorReportJSON struct {
	SchemaVersion int               `json:"schemaVersion"`
	ScopraVersion string            `json:"scopraVersion"`
	StateDir      string            `json:"stateDir"`
	Result        string            `json:"result"`
	Checks        []doctorCheckJSON `json:"checks"`
	NotChecked    []string          `json:"notChecked"`
}

type doctorCheckJSON struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
	Hint   string `json:"hint"`
}

func TestRunDoctorReportsRespondingDaemon(t *testing.T) {
	stateDir, stop := startStatusDaemon(t)
	defer stop()

	report, err := runDoctorJSON(t, "--state-dir", stateDir)
	if err != nil {
		t.Fatalf("run(doctor) error = %v, want nil", err)
	}
	assertChecks(t, report, "platform=pass", "state-directory=pass", "daemon=pass")
	if report.Result != "pass" {
		t.Fatalf("doctor result = %q, want pass", report.Result)
	}
	if report.StateDir != stateDir {
		t.Fatalf("doctor stateDir = %q, want %q", report.StateDir, stateDir)
	}
	if report.ScopraVersion == "" {
		t.Fatal("doctor scopraVersion is empty")
	}
	if daemon := findCheck(t, report, "daemon"); !strings.Contains(daemon.Detail, "test-version") {
		t.Fatalf("daemon detail = %q, want the version reported by Mio", daemon.Detail)
	}
}

func TestRunDoctorHumanReportStatesItsLimits(t *testing.T) {
	stateDir, stop := startStatusDaemon(t)
	defer stop()

	var stdout, stderr bytes.Buffer
	err := run(t.Context(), []string{"doctor", "--state-dir", stateDir}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run(doctor) error = %v; stderr = %q", err, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("run(doctor) wrote unexpected stderr: %q", stderr.String())
	}
	output := stdout.String()
	for _, want := range []string{
		"[pass] platform",
		"[pass] state-directory",
		"[pass] daemon",
		"test-version",
		stateDir,
		"Result: pass",
		"Not checked",
		"execution",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("human doctor report is missing %q:\n%s", want, output)
		}
	}
}

func TestRunDoctorFailsWhenDaemonIsNotRunning(t *testing.T) {
	stateDir := makePrivateStateDir(t)

	report, err := runDoctorJSON(t, "--state-dir", stateDir)
	requireCheckFailure(t, err)
	assertChecks(t, report, "platform=pass", "state-directory=pass", "daemon=fail")
	if report.Result != "fail" {
		t.Fatalf("doctor result = %q, want fail", report.Result)
	}
	if daemon := findCheck(t, report, "daemon"); !strings.Contains(daemon.Hint, "scoprad") {
		t.Fatalf("daemon hint = %q, want guidance to start scoprad", daemon.Hint)
	}
}

func TestSecurity_DoctorDoesNotCreateMissingStateDirectory(t *testing.T) {
	requireLinux(t)
	parent := filepath.Join(t.TempDir(), "missing")
	stateDir := filepath.Join(parent, "run")

	report, err := runDoctorJSON(t, "--state-dir", stateDir)
	requireCheckFailure(t, err)
	assertChecks(t, report, "platform=pass", "state-directory=warn", "daemon=fail")
	if _, statErr := os.Lstat(parent); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("doctor created part of the missing state directory (Lstat error = %v)", statErr)
	}
}

func TestSecurity_DoctorReportsInsecureStateDirectoryWithoutRepairingIt(t *testing.T) {
	stateDir := makePrivateStateDir(t)
	if err := os.Chmod(stateDir, 0o755); err != nil {
		t.Fatalf("Chmod(state dir) error = %v", err)
	}

	report, err := runDoctorJSON(t, "--state-dir", stateDir)
	requireCheckFailure(t, err)
	assertChecks(t, report, "platform=pass", "state-directory=fail", "daemon=skip")
	info, statErr := os.Stat(stateDir)
	if statErr != nil {
		t.Fatalf("Stat(state dir) error = %v", statErr)
	}
	if got := info.Mode().Perm(); got != 0o755 {
		t.Fatalf("state directory mode after doctor = %#o, want unchanged 0755", got)
	}
}

func TestSecurity_DoctorLeavesStaleSocketInPlace(t *testing.T) {
	stateDir := makePrivateStateDir(t)
	socketPath := filepath.Join(stateDir, "scoprad.sock")
	listener := listenUnixSocket(t, socketPath, 0o600)
	if err := listener.Close(); err != nil {
		t.Fatalf("Close(stale listener) error = %v", err)
	}
	before, err := os.Lstat(socketPath)
	if err != nil {
		t.Fatalf("Lstat(stale socket) error = %v", err)
	}

	report, err := runDoctorJSON(t, "--state-dir", stateDir)
	requireCheckFailure(t, err)
	assertChecks(t, report, "platform=pass", "state-directory=pass", "daemon=fail")
	after, err := os.Lstat(socketPath)
	if err != nil {
		t.Fatalf("doctor removed the stale socket: %v", err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("doctor replaced the stale socket")
	}
}

func TestSecurity_DoctorRefusesInsecureSocketWithoutConnecting(t *testing.T) {
	stateDir := makePrivateStateDir(t)
	listener := listenUnixSocket(t, filepath.Join(stateDir, "scoprad.sock"), 0o666)

	report, err := runDoctorJSON(t, "--state-dir", stateDir)
	requireCheckFailure(t, err)
	assertChecks(t, report, "platform=pass", "state-directory=pass", "daemon=fail")
	if daemon := findCheck(t, report, "daemon"); !strings.Contains(daemon.Detail, "unsafe") {
		t.Fatalf("daemon detail = %q, want the socket reported as unsafe", daemon.Detail)
	}

	// A completed connect() would already be queued in the listener backlog.
	if err := listener.SetDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
		t.Fatalf("SetDeadline() error = %v", err)
	}
	if connection, err := listener.AcceptUnix(); err == nil {
		_ = connection.Close()
		t.Fatal("doctor connected to a socket with unsafe permissions")
	}
}

func TestRunDoctorReportsInvalidDaemonResponse(t *testing.T) {
	stateDir := makePrivateStateDir(t)
	listener := listenUnixSocket(t, filepath.Join(stateDir, "scoprad.sock"), 0o600)
	served := make(chan error, 1)
	go func() { served <- serveMalformedStatus(listener) }()

	report, err := runDoctorJSON(t, "--state-dir", stateDir)
	requireCheckFailure(t, err)
	assertChecks(t, report, "platform=pass", "state-directory=pass", "daemon=fail")
	if daemon := findCheck(t, report, "daemon"); !strings.Contains(daemon.Hint, "version") {
		t.Fatalf("daemon hint = %q, want version guidance for an invalid response", daemon.Hint)
	}
	if err := <-served; err != nil {
		t.Fatalf("fake daemon error = %v", err)
	}
}

func TestRunDoctorChecksDefaultStateDirectory(t *testing.T) {
	requireLinux(t)
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	report, err := runDoctorJSON(t)
	requireCheckFailure(t, err)
	if want := filepath.Join(stateHome, "scopra"); report.StateDir != want {
		t.Fatalf("doctor stateDir = %q, want the scoprad default %q", report.StateDir, want)
	}
	assertChecks(t, report, "platform=pass", "state-directory=warn", "daemon=fail")
}

func TestRunDoctorFailsWhenStateDirectoryCannotBeResolved(t *testing.T) {
	requireLinux(t)
	tests := []struct {
		name      string
		stateHome string
		home      string
	}{
		{name: "neither XDG_STATE_HOME nor HOME"},
		{name: "relative XDG_STATE_HOME", stateHome: "relative/state", home: t.TempDir()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("XDG_STATE_HOME", tt.stateHome)
			t.Setenv("HOME", tt.home)
			// A usable cache directory must not become a fallback.
			t.Setenv("XDG_CACHE_HOME", t.TempDir())

			report, err := runDoctorJSON(t)
			requireCheckFailure(t, err)
			assertChecks(t, report, "platform=pass", "state-directory=fail", "daemon=skip")
			if report.StateDir != "" {
				t.Fatalf("doctor stateDir = %q, want empty when it cannot be resolved", report.StateDir)
			}
			hint := findCheck(t, report, "state-directory").Hint
			for _, want := range []string{"XDG_STATE_HOME", "--state-dir"} {
				if !strings.Contains(hint, want) {
					t.Fatalf("state-directory hint = %q, want it to mention %s", hint, want)
				}
			}
		})
	}
}

func TestRunDoctorRejectsOverlongStateDirectory(t *testing.T) {
	requireLinux(t)
	stateDir := filepath.Join(t.TempDir(), strings.Repeat("d", 100))
	if err := os.Mkdir(stateDir, 0o700); err != nil {
		t.Fatalf("Mkdir(state dir) error = %v", err)
	}

	report, err := runDoctorJSON(t, "--state-dir", stateDir)
	requireCheckFailure(t, err)
	assertChecks(t, report, "platform=pass", "state-directory=fail", "daemon=skip")
	if hint := findCheck(t, report, "state-directory").Hint; !strings.Contains(hint, "shorter") {
		t.Fatalf("state-directory hint = %q, want guidance to choose a shorter path", hint)
	}
}

func TestOverallResultReportsMostSevereStatus(t *testing.T) {
	tests := []struct {
		name     string
		statuses []checkStatus
		want     checkStatus
	}{
		{
			name:     "all checks pass",
			statuses: []checkStatus{checkPass, checkPass},
			want:     checkPass,
		},
		{
			name:     "warning without failure",
			statuses: []checkStatus{checkPass, checkWarn, checkPass},
			want:     checkWarn,
		},
		{
			name:     "failure outranks warning",
			statuses: []checkStatus{checkWarn, checkFail, checkSkip},
			want:     checkFail,
		},
		{
			name:     "skip alone is not a failure",
			statuses: []checkStatus{checkPass, checkSkip},
			want:     checkPass,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			checks := make([]doctorCheck, 0, len(test.statuses))
			for _, status := range test.statuses {
				checks = append(checks, doctorCheck{Status: status})
			}
			if got := overallResult(checks); got != test.want {
				t.Fatalf("overallResult(%q) = %q, want %q", test.statuses, got, test.want)
			}
		})
	}
}

func TestRunDoctorRejectsInvalidUsage(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "positional argument", args: []string{"doctor", "extra"}},
		{name: "repair flag is not supported", args: []string{"doctor", "--fix"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := run(t.Context(), test.args, &stdout, &stderr)
			var usageErr *usageError
			if !errors.As(err, &usageErr) {
				t.Fatalf("run(%q) error = %v, want a usage error (exit status 2)", test.args, err)
			}
			if stdout.Len() != 0 {
				t.Fatalf("run(%q) wrote unexpected stdout: %q", test.args, stdout.String())
			}
			if !strings.Contains(stderr.String(), "doctor [--state-dir PATH] [--json]") {
				t.Fatalf("usage output = %q, want doctor usage", stderr.String())
			}
		})
	}
}

func TestRunDoctorReturnsCancellationWithoutReport(t *testing.T) {
	stateDir := makePrivateStateDir(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	var stdout, stderr bytes.Buffer
	err := run(ctx, []string{"doctor", "--state-dir", stateDir}, &stdout, &stderr)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("run(doctor) with canceled context error = %v, want context.Canceled", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("run(doctor) wrote a report after cancellation: %q", stdout.String())
	}
}

func TestRunDoctorEscapesControlCharactersInHumanReport(t *testing.T) {
	requireLinux(t)
	stateDir := filepath.Join(t.TempDir(), "run\x1b[2J\nforged")

	var stdout, stderr bytes.Buffer
	err := run(t.Context(), []string{"doctor", "--state-dir", stateDir}, &stdout, &stderr)
	requireCheckFailure(t, err)
	output := stdout.String()
	if strings.ContainsRune(output, '\x1b') {
		t.Fatalf("human report contains a raw escape character: %q", output)
	}
	if strings.Contains(output, "\nforged") {
		t.Fatalf("human report lets a path inject a line: %q", output)
	}
	if !strings.Contains(output, `run\x1b[2J\nforged`) {
		t.Fatalf("human report = %q, want the control characters shown escaped", output)
	}
}

func TestDiagnoseFailsClosedOnUnsupportedPlatform(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "run")

	report := diagnose(t.Context(), stateDir, platformInfo{goos: "darwin", goarch: "arm64"})

	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("json.Marshal(report) error = %v", err)
	}
	var decoded doctorReportJSON
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("json.Unmarshal(report) error = %v", err)
	}
	assertReportInvariants(t, decoded)
	assertChecks(t, decoded, "platform=fail", "state-directory=skip", "daemon=skip")
	if decoded.Result != "fail" {
		t.Fatalf("doctor result = %q, want fail", decoded.Result)
	}
	platform := findCheck(t, decoded, "platform")
	if !strings.Contains(platform.Detail, "darwin/arm64") {
		t.Fatalf("platform detail = %q, want the unsupported platform named", platform.Detail)
	}
}

// runDoctorJSON runs `scopra doctor --json` and checks the invariants that
// every schema-v1 report must satisfy, whatever the individual results.
func runDoctorJSON(t *testing.T, args ...string) (doctorReportJSON, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	err := run(t.Context(), append([]string{"doctor", "--json"}, args...), &stdout, &stderr)
	if stderr.Len() != 0 {
		t.Fatalf("run(doctor --json) wrote stderr %q (error = %v), want none", stderr.String(), err)
	}
	var report doctorReportJSON
	if decodeErr := json.Unmarshal(stdout.Bytes(), &report); decodeErr != nil {
		t.Fatalf("doctor --json output is not JSON: %v; output = %q", decodeErr, stdout.String())
	}
	assertReportInvariants(t, report)
	return report, err
}

func assertReportInvariants(t *testing.T, report doctorReportJSON) {
	t.Helper()
	if report.SchemaVersion != 1 {
		t.Fatalf("doctor schemaVersion = %d, want 1", report.SchemaVersion)
	}
	for _, check := range report.Checks {
		if check.Detail == "" {
			t.Errorf("doctor check %q has no detail", check.Name)
		}
		if (check.Status == "warn" || check.Status == "fail") && check.Hint == "" {
			t.Errorf("doctor check %q is %s without a remediation hint", check.Name, check.Status)
		}
	}
	if !slices.Contains(report.NotChecked, "execution") {
		t.Errorf("doctor notChecked = %q, want execution listed as not checked", report.NotChecked)
	}
}

func assertChecks(t *testing.T, report doctorReportJSON, want ...string) {
	t.Helper()
	got := make([]string, 0, len(report.Checks))
	for _, check := range report.Checks {
		got = append(got, check.Name+"="+check.Status)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("doctor checks = %q, want %q", got, want)
	}
}

func findCheck(t *testing.T, report doctorReportJSON, name string) doctorCheckJSON {
	t.Helper()
	for _, check := range report.Checks {
		if check.Name == name {
			return check
		}
	}
	t.Fatalf("doctor report has no %q check: %+v", name, report.Checks)
	return doctorCheckJSON{}
}

func requireCheckFailure(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("run(doctor) error = nil, want a failing-check error (exit status 1)")
	}
	var usageErr *usageError
	if errors.As(err, &usageErr) || errors.Is(err, context.Canceled) {
		t.Fatalf("run(doctor) error = %v, want a failing-check error (exit status 1)", err)
	}
}

func makePrivateStateDir(t *testing.T) string {
	t.Helper()
	requireLinux(t)
	stateDir := filepath.Join(t.TempDir(), "run")
	if err := os.Mkdir(stateDir, 0o700); err != nil {
		t.Fatalf("Mkdir(state dir) error = %v", err)
	}
	return stateDir
}

func listenUnixSocket(t *testing.T, socketPath string, mode os.FileMode) *net.UnixListener {
	t.Helper()
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socketPath, Net: "unix"})
	if err != nil {
		t.Fatalf("ListenUnix() error = %v", err)
	}
	listener.SetUnlinkOnClose(false)
	t.Cleanup(func() { _ = listener.Close() })
	if err := os.Chmod(socketPath, mode); err != nil {
		t.Fatalf("Chmod(socket) error = %v", err)
	}
	return listener
}

// serveMalformedStatus answers one connection with a well-framed but
// schema-invalid status response, then waits for the client to hang up.
func serveMalformedStatus(listener *net.UnixListener) error {
	if err := listener.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		return err
	}
	connection, err := listener.AcceptUnix()
	if err != nil {
		return err
	}
	defer connection.Close()
	if err := connection.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		return err
	}
	payload := []byte(`{"protocolVersion":1}`)
	frame := binary.BigEndian.AppendUint32(nil, uint32(len(payload)))
	if _, err := connection.Write(append(frame, payload...)); err != nil {
		return err
	}
	_, err = io.Copy(io.Discard, connection)
	return err
}

func requireLinux(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("authenticated local IPC is Linux-only in this increment")
	}
}
