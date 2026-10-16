package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/SynergyFlowLabs/scopra/internal/workspace"
	"github.com/SynergyFlowLabs/scopra/internal/workspace/registry"
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
	assertChecks(t, report, "platform=pass", "state-directory=pass", "workspace-registry=pass", "workspaces=pass", "daemon=pass")
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
		"[pass] workspace-registry",
		"[pass] workspaces",
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
	if strings.Contains(output, "revalidation") {
		t.Errorf("human doctor report still lists workspace revalidation as not checked:\n%s", output)
	}
}

func TestRunDoctorFailsWhenDaemonIsNotRunning(t *testing.T) {
	stateDir := makePrivateStateDir(t)

	report, err := runDoctorJSON(t, "--state-dir", stateDir)
	requireCheckFailure(t, err)
	assertChecks(t, report, "platform=pass", "state-directory=pass", "workspace-registry=pass", "workspaces=pass", "daemon=fail")
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
	assertChecks(t, report, "platform=pass", "state-directory=warn", "workspace-registry=pass", "workspaces=pass", "daemon=fail")
	if _, statErr := os.Lstat(parent); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("doctor created part of the missing state directory (Lstat error = %v)", statErr)
	}
	if detail := findCheck(t, report, "workspace-registry").Detail; !strings.Contains(detail, "no workspaces are registered") {
		t.Fatalf("workspace-registry detail = %q, want no registrations reported", detail)
	}
}

func TestRunDoctorChecksWorkspaceRegistry(t *testing.T) {
	tests := []struct {
		name       string
		content    string // empty means no registry file
		mode       os.FileMode
		wantStatus string
		wantDetail string
		// wantWorkspaces is the revalidation status: the registrations
		// below name directories that do not exist.
		wantWorkspaces string
	}{
		{name: "no registry file", wantStatus: "pass", wantDetail: "no workspaces are registered", wantWorkspaces: "pass"},
		{name: "one registration", content: registryWithPaths("/srv/a"), mode: 0o600, wantStatus: "pass", wantDetail: "1 workspace is registered", wantWorkspaces: "warn"},
		{name: "two registrations", content: registryWithPaths("/srv/a", "/srv/b"), mode: 0o600, wantStatus: "pass", wantDetail: "2 workspaces are registered", wantWorkspaces: "warn"},
		{name: "unsupported version", content: `{"version":2,"workspaces":[]}`, mode: 0o600, wantStatus: "fail", wantDetail: "registry file is invalid", wantWorkspaces: "skip"},
		{name: "group-readable file", content: registryWithPaths(), mode: 0o640, wantStatus: "fail", wantDetail: "insecure registry file", wantWorkspaces: "skip"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stateDir := makePrivateStateDir(t)
			file := filepath.Join(stateDir, "workspaces.json")
			if test.content != "" {
				writeRegistryFile(t, file, test.content, test.mode)
			}

			report, err := runDoctorJSON(t, "--state-dir", stateDir)
			requireCheckFailure(t, err) // Mio is not running
			assertChecks(t, report, "platform=pass", "state-directory=pass",
				"workspace-registry="+test.wantStatus, "workspaces="+test.wantWorkspaces, "daemon=fail")
			wantWorkspacesDetail := map[string]string{
				"pass": "no workspaces are registered",
				"skip": "not run because the workspace-registry check failed",
			}[test.wantWorkspaces]
			if detail := findCheck(t, report, "workspaces").Detail; wantWorkspacesDetail != "" && detail != wantWorkspacesDetail {
				t.Fatalf("workspaces detail = %q, want %q", detail, wantWorkspacesDetail)
			}
			// scoprad refuses to start with a bad registry, so the hint for a
			// stopped Mio must send the user to the registry first.
			daemon := findCheck(t, report, "daemon")
			if test.wantStatus == "fail" {
				const blocked = "scoprad will not start until the problem reported by the workspace-registry check is fixed; "
				if !strings.HasPrefix(daemon.Hint, blocked) || !strings.Contains(daemon.Hint, "then start Mio in the foreground with `scoprad`") {
					t.Fatalf("daemon hint = %q, want it to point at the registry before starting scoprad", daemon.Hint)
				}
			} else if daemon.Hint != "Start Mio in the foreground with `scoprad` (pass the same --state-dir, if any)." {
				t.Fatalf("daemon hint = %q, want the plain start hint", daemon.Hint)
			}
			if daemon.Detail != "Scopra Mio is not running (its socket is missing or refuses connections)" {
				t.Fatalf("daemon detail = %q, want it unchanged", daemon.Detail)
			}
			check := findCheck(t, report, "workspace-registry")
			if !strings.Contains(check.Detail, test.wantDetail) {
				t.Fatalf("workspace-registry detail = %q, want it to contain %q", check.Detail, test.wantDetail)
			}
			if test.wantStatus == "fail" {
				for _, want := range []string{file, "scoprad will not start", "unregisters every workspace"} {
					if !strings.Contains(check.Hint, want) {
						t.Fatalf("workspace-registry hint = %q, want it to contain %q", check.Hint, want)
					}
				}
			}
			if want := []string{"enrollment", "relay", "policy", "containment", "execution"}; !slices.Equal(report.NotChecked, want) {
				t.Fatalf("doctor notChecked = %q, want %q", report.NotChecked, want)
			}
			if test.content == "" {
				if _, err := os.Lstat(file); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("doctor created a registry file (Lstat error = %v)", err)
				}
				return
			}
			requireUnchangedRegistryFile(t, file, test.content, test.mode)
		})
	}
}

func TestRunDoctorEscapesControlCharactersInRegistryHint(t *testing.T) {
	requireLinux(t)
	stateDir := filepath.Join(t.TempDir(), "run\x1b[2J\nforged")
	if err := os.Mkdir(stateDir, 0o700); err != nil {
		t.Fatalf("Mkdir(state dir) error = %v", err)
	}
	writeRegistryFile(t, filepath.Join(stateDir, "workspaces.json"), `{"version":2,"workspaces":[]}`, 0o600)

	var stdout, stderr bytes.Buffer
	err := run(t.Context(), []string{"doctor", "--state-dir", stateDir}, &stdout, &stderr)
	requireCheckFailure(t, err)
	output := stdout.String()
	if !strings.Contains(output, "[fail] workspace-registry") {
		t.Fatalf("human report = %q, want a failing workspace-registry check", output)
	}
	if strings.ContainsRune(output, '\x1b') || strings.Contains(output, "\nforged") {
		t.Fatalf("human report contains raw control characters: %q", output)
	}
	if !strings.Contains(output, `run\x1b[2J\nforged/workspaces.json`) {
		t.Fatalf("human report = %q, want the registry path shown escaped", output)
	}
}

func TestRunDoctorRevalidatesRegisteredWorkspaces(t *testing.T) {
	removeDirs := func(n int) func(*testing.T, []registry.Record) {
		return func(t *testing.T, records []registry.Record) {
			for _, record := range records[:n] {
				if err := os.Remove(record.Path); err != nil {
					t.Fatalf("Remove(%q) error = %v", record.Path, err)
				}
			}
		}
	}
	tests := []struct {
		name       string
		count      int
		breakDirs  func(*testing.T, []registry.Record)
		wantStatus string
		wantDetail []string
		wantStale  int // leading records whose ID and path the detail names
	}{
		{name: "one qualifies", count: 1, wantStatus: "pass", wantDetail: []string{"the registered workspace still qualifies"}},
		{name: "all qualify", count: 2, wantStatus: "pass", wantDetail: []string{"all 2 registered workspaces still qualify"}},
		{name: "deleted directory", count: 2, breakDirs: removeDirs(1), wantStatus: "warn",
			wantDetail: []string{"1 of 2 registered workspaces no longer qualifies: ", " (missing)"}, wantStale: 1},
		{name: "replaced directory", count: 2, breakDirs: func(t *testing.T, records []registry.Record) {
			// The replacement exists before the original is removed, so it
			// cannot reuse the original's inode number (see ADR-004).
			replacement := records[0].Path + ".new"
			if err := os.Mkdir(replacement, 0o700); err != nil {
				t.Fatalf("Mkdir(replacement) error = %v", err)
			}
			removeDirs(1)(t, records)
			if err := os.Rename(replacement, records[0].Path); err != nil {
				t.Fatalf("Rename(replacement) error = %v", err)
			}
		}, wantStatus: "warn", wantDetail: []string{"1 of 2 registered workspaces no longer qualifies: ", " (changed since it was registered)"}, wantStale: 1},
		{name: "file in place of the directory", count: 1, breakDirs: func(t *testing.T, records []registry.Record) {
			removeDirs(1)(t, records)
			if err := os.WriteFile(records[0].Path, nil, 0o600); err != nil {
				t.Fatalf("WriteFile(replacement) error = %v", err)
			}
		}, wantStatus: "warn", wantDetail: []string{"1 of 1 registered workspaces no longer qualifies: ", " (not a directory)"}, wantStale: 1},
		{name: "now contains the home directory", count: 1, breakDirs: func(t *testing.T, records []registry.Record) {
			t.Setenv("HOME", filepath.Join(records[0].Path, "home"))
		}, wantStatus: "warn", wantDetail: []string{" (is or contains a protected location)"}, wantStale: 1},
		{name: "two of three missing", count: 3, breakDirs: removeDirs(2), wantStatus: "warn",
			wantDetail: []string{"2 of 3 registered workspaces no longer qualify: ", " (missing); "}, wantStale: 2},
		{name: "more stale than are listed", count: 7, breakDirs: removeDirs(7), wantStatus: "warn",
			wantDetail: []string{"7 of 7 registered workspaces no longer qualify: ", "; and 2 more"}, wantStale: 5},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stateDir := makePrivateStateDir(t)
			t.Setenv("HOME", t.TempDir())
			records := registerWorkspaces(t, stateDir, test.count)
			if test.breakDirs != nil {
				test.breakDirs(t, records)
			}
			file := filepath.Join(stateDir, registry.FileName)
			before, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("ReadFile(registry) error = %v", err)
			}

			report, err := runDoctorJSON(t, "--state-dir", stateDir)
			requireCheckFailure(t, err) // Mio is not running; a warning alone exits 0
			assertChecks(t, report, "platform=pass", "state-directory=pass", "workspace-registry=pass",
				"workspaces="+test.wantStatus, "daemon=fail")
			check := findCheck(t, report, "workspaces")
			for _, want := range test.wantDetail {
				if !strings.Contains(check.Detail, want) {
					t.Fatalf("workspaces detail = %q, want it to contain %q", check.Detail, want)
				}
			}
			for i, record := range records {
				named := strings.Contains(check.Detail, record.ID+" "+record.Path+" (")
				if named != (i < test.wantStale) {
					t.Fatalf("workspaces detail = %q; naming record %d (%s) = %t, want %t", check.Detail, i, record.Path, named, i < test.wantStale)
				}
			}
			if test.wantStatus == "warn" {
				for _, want := range []string{"scopra workspace remove ID", "does not change the directory"} {
					if !strings.Contains(check.Hint, want) {
						t.Fatalf("workspaces hint = %q, want it to contain %q", check.Hint, want)
					}
				}
			}
			if after, err := os.ReadFile(file); err != nil || !bytes.Equal(after, before) {
				t.Fatalf("doctor changed the registry file (read error = %v)", err)
			}
			if test.name == "deleted directory" {
				if _, err := os.Lstat(records[0].Path); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("doctor recreated a deleted workspace (Lstat error = %v)", err)
				}
			}
		})
	}
}

func TestRunDoctorWarnsWithoutFailingWhenOnlyWorkspacesAreStale(t *testing.T) {
	stateDir, stop := startStatusDaemon(t)
	defer stop()
	t.Setenv("HOME", t.TempDir())
	records := registerWorkspaces(t, stateDir, 1)
	if err := os.Remove(records[0].Path); err != nil {
		t.Fatalf("Remove(workspace) error = %v", err)
	}

	report, err := runDoctorJSON(t, "--state-dir", stateDir)
	if err != nil {
		t.Fatalf("run(doctor) error = %v, want nil because a warning alone does not fail", err)
	}
	assertChecks(t, report, "platform=pass", "state-directory=pass", "workspace-registry=pass", "workspaces=warn", "daemon=pass")
	if report.Result != "warn" {
		t.Fatalf("doctor result = %q, want warn", report.Result)
	}
}

func TestRunDoctorEscapesControlCharactersInStaleWorkspaces(t *testing.T) {
	requireLinux(t)
	stateDir := makePrivateStateDir(t)
	t.Setenv("HOME", t.TempDir())
	records := registerWorkspaces(t, stateDir, 1, "w\x1b[2J\nforged")
	if err := os.Remove(records[0].Path); err != nil {
		t.Fatalf("Remove(workspace) error = %v", err)
	}

	var stdout, stderr bytes.Buffer
	err := run(t.Context(), []string{"doctor", "--state-dir", stateDir}, &stdout, &stderr)
	requireCheckFailure(t, err) // Mio is not running
	output := stdout.String()
	if !strings.Contains(output, "[warn] workspaces") {
		t.Fatalf("human report = %q, want a warning for the workspaces check", output)
	}
	if strings.ContainsRune(output, '\x1b') || strings.Contains(output, "\nforged") {
		t.Fatalf("human report contains raw control characters: %q", output)
	}
	if !strings.Contains(output, `w\x1b[2J\nforged (missing)`) {
		t.Fatalf("human report = %q, want the stale path shown escaped", output)
	}
}

func TestSecurity_DoctorReportsInsecureStateDirectoryWithoutRepairingIt(t *testing.T) {
	stateDir := makePrivateStateDir(t)
	if err := os.Chmod(stateDir, 0o755); err != nil {
		t.Fatalf("Chmod(state dir) error = %v", err)
	}

	report, err := runDoctorJSON(t, "--state-dir", stateDir)
	requireCheckFailure(t, err)
	assertChecks(t, report, "platform=pass", "state-directory=fail", "workspace-registry=skip", "workspaces=skip", "daemon=skip")
	if detail := findCheck(t, report, "workspaces").Detail; detail != "not run because the state-directory check failed" {
		t.Fatalf("workspaces detail = %q, want it skipped after the state-directory check", detail)
	}
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
	assertChecks(t, report, "platform=pass", "state-directory=pass", "workspace-registry=pass", "workspaces=pass", "daemon=fail")
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
	assertChecks(t, report, "platform=pass", "state-directory=pass", "workspace-registry=pass", "workspaces=pass", "daemon=fail")
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

func TestRunDoctorKeepsUnsafeSocketHintWithBadRegistry(t *testing.T) {
	stateDir := makePrivateStateDir(t)
	writeRegistryFile(t, filepath.Join(stateDir, "workspaces.json"), `{"version":2,"workspaces":[]}`, 0o600)
	listenUnixSocket(t, filepath.Join(stateDir, "scoprad.sock"), 0o666)

	report, err := runDoctorJSON(t, "--state-dir", stateDir)
	requireCheckFailure(t, err)
	assertChecks(t, report, "platform=pass", "state-directory=pass", "workspace-registry=fail", "workspaces=skip", "daemon=fail")
	const want = "scoprad will not replace it. Confirm that no trusted Mio uses it, remove it, then start scoprad."
	if hint := findCheck(t, report, "daemon").Hint; hint != want {
		t.Fatalf("daemon hint = %q, want the unchanged unsafe-socket hint %q", hint, want)
	}
}

func TestRunDoctorReportsInvalidDaemonResponse(t *testing.T) {
	stateDir := makePrivateStateDir(t)
	listener := listenUnixSocket(t, filepath.Join(stateDir, "scoprad.sock"), 0o600)
	served := make(chan error, 1)
	go func() { served <- serveMalformedStatus(listener) }()

	report, err := runDoctorJSON(t, "--state-dir", stateDir)
	requireCheckFailure(t, err)
	assertChecks(t, report, "platform=pass", "state-directory=pass", "workspace-registry=pass", "workspaces=pass", "daemon=fail")
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
	assertChecks(t, report, "platform=pass", "state-directory=warn", "workspace-registry=pass", "workspaces=pass", "daemon=fail")
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
			assertChecks(t, report, "platform=pass", "state-directory=fail", "workspace-registry=skip", "workspaces=skip", "daemon=skip")
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
	assertChecks(t, report, "platform=pass", "state-directory=fail", "workspace-registry=skip", "workspaces=skip", "daemon=skip")
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
	assertChecks(t, decoded, "platform=fail", "state-directory=skip", "workspace-registry=skip", "workspaces=skip", "daemon=skip")
	if detail := findCheck(t, decoded, "workspaces").Detail; detail != "not run because the platform check failed" {
		t.Fatalf("workspaces detail = %q, want it skipped after the platform check", detail)
	}
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

// registryWithPaths returns a valid registry file with one record per path.
func registryWithPaths(paths ...string) string {
	records := make([]string, 0, len(paths))
	for index, path := range paths {
		records = append(records, fmt.Sprintf(
			`{"id":"%032x","path":%q,"fingerprint":{"device":1,"inode":%d}}`, index+1, path, index+1))
	}
	return `{"version":1,"workspaces":[` + strings.Join(records, ",") + `]}`
}

func writeRegistryFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("Chmod(%q) error = %v", path, err)
	}
}

func requireUnchangedRegistryFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != mode {
		t.Fatalf("doctor changed the registry file (info = %v, error = %v)", info, err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != content {
		t.Fatalf("doctor changed the registry file content (error = %v)", err)
	}
}

// registerWorkspaces registers count new directories, named w0, w1, ...
// unless names are given, in the registry under stateDir with the validator
// configuration Mio uses. It returns the registrations sorted by path.
func registerWorkspaces(t *testing.T, stateDir string, count int, names ...string) []registry.Record {
	t.Helper()
	validator, err := workspace.NewValidator(stateDir)
	if err != nil {
		t.Fatalf("NewValidator() error = %v", err)
	}
	workspaces, err := registry.Open(stateDir, validator)
	if err != nil {
		t.Fatalf("registry.Open() error = %v", err)
	}
	for i := len(names); i < count; i++ {
		names = append(names, fmt.Sprintf("w%d", i))
	}
	base := t.TempDir()
	for _, name := range names {
		dir := filepath.Join(base, name)
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatalf("Mkdir(workspace) error = %v", err)
		}
		if _, err := workspaces.Add(dir); err != nil {
			t.Fatalf("Add(%q) error = %v", dir, err)
		}
	}
	records := workspaces.List()
	if len(records) != len(names) {
		t.Fatalf("registered %d workspaces, want %d", len(records), len(names))
	}
	return records
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
