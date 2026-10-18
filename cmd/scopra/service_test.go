package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fakeSystemctlEnv names the directory of a fake systemctl scenario. When it
// is set, the test binary acts as systemctl instead of running tests.
const fakeSystemctlEnv = "SCOPRA_FAKE_SYSTEMCTL"

// fakeSystemctl imitates the systemctl --user commands that the service
// commands run. The scenario directory may hold fail-daemon-reload,
// fail-enable, or fail-disable to make that call fail, and enabled, active, or
// failed to describe the unit's state. Every call is appended to calls.log.
func fakeSystemctl(dir string, args []string) int {
	appendDoctorLine(dir, "calls.log", strings.Join(args, " "))
	if len(args) > 0 && args[0] == "--user" {
		args = args[1:]
	}
	if len(args) == 0 {
		return 125
	}
	switch args[0] {
	case "daemon-reload":
		if doctorFileExists(filepath.Join(dir, "fail-daemon-reload")) {
			fmt.Fprintln(os.Stderr, "Failed to reload daemon")
			return 1
		}
		return 0
	case "enable":
		if doctorFileExists(filepath.Join(dir, "fail-enable")) {
			fmt.Fprintln(os.Stderr, "Failed to enable unit")
			return 1
		}
		_ = os.WriteFile(filepath.Join(dir, "enabled"), nil, 0o600)
		for _, arg := range args[1:] {
			if arg == "--now" {
				_ = os.WriteFile(filepath.Join(dir, "active"), nil, 0o600)
			}
		}
		return 0
	case "disable":
		if doctorFileExists(filepath.Join(dir, "fail-disable")) {
			fmt.Fprintln(os.Stderr, "Failed to disable unit")
			return 1
		}
		_ = os.Remove(filepath.Join(dir, "enabled"))
		_ = os.Remove(filepath.Join(dir, "active"))
		return 0
	case "is-enabled":
		if doctorFileExists(filepath.Join(dir, "silent-is-enabled")) {
			return 2
		}
		if doctorFileExists(filepath.Join(dir, "enabled")) {
			fmt.Println("enabled")
			return 0
		}
		fmt.Println("disabled")
		return 1
	case "is-active":
		switch {
		case doctorFileExists(filepath.Join(dir, "failed")):
			fmt.Println("failed")
		case doctorFileExists(filepath.Join(dir, "active")):
			fmt.Println("active")
			return 0
		default:
			fmt.Println("inactive")
		}
		return 3
	}
	fmt.Fprintf(os.Stderr, "fake systemctl: unexpected call %q\n", args)
	return 125
}

// serviceSetup isolates the service commands from the host: a private home
// and config root, and a private prefix for state directories.
func serviceSetup(t *testing.T) (home string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_STATE_HOME", "")
	return home
}

// startFakeSystemctl creates a PATH entry whose systemctl is this test binary
// and selects a scenario.
func startFakeSystemctl(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("WriteFile(%q) error = %v", path, err)
		}
	}
	binDir := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("find the test binary: %v", err)
	}
	if err := os.Symlink(executable, filepath.Join(binDir, "systemctl")); err != nil {
		t.Fatalf("create the fake systemctl symlink: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv(fakeSystemctlEnv, dir)
	t.Setenv("GORACE", "atexit_sleep_ms=0")
	return dir
}

// fakeScoprad writes an executable file that stands in for scoprad.
func fakeScoprad(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "scoprad")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(scoprad) error = %v", err)
	}
	return path
}

// serviceUnitPathFor returns the unit path that the test's home implies.
func serviceUnitPathFor(home string) string {
	return filepath.Join(home, ".config", "systemd", "user", "scoprad.service")
}

// expectedUnit returns the exact unit that install writes for execStart.
func expectedUnit(execStart string) string {
	return "# Managed by scopra: `scopra service install` rewrites this file.\n" +
		"[Unit]\n" +
		"Description=Scopra Mio local execution daemon\n" +
		"Documentation=https://github.com/SynergyFlowLabs/scopra\n" +
		"StartLimitIntervalSec=60\n" +
		"StartLimitBurst=3\n" +
		"\n" +
		"[Service]\n" +
		"Type=simple\n" +
		"Delegate=cpu cpuset io memory pids\n" +
		"ExecStart=" + execStart + "\n" +
		"Restart=on-failure\n" +
		"RestartSec=2\n" +
		"UMask=0077\n" +
		"\n" +
		"[Install]\n" +
		"WantedBy=default.target\n"
}

func TestServiceInstallWritesAndStartsTheUnit(t *testing.T) {
	home := serviceSetup(t)
	scenario := startFakeSystemctl(t, nil)
	scoprad := fakeScoprad(t)
	stateDir := filepath.Join(home, "state")
	unitPath := serviceUnitPathFor(home)

	stdout, stderr, err := runCLI(t, "service", "install",
		"--state-dir", stateDir, "--sandbox-image", testSandboxImage, "--scoprad", scoprad)
	if err != nil {
		t.Fatalf("service install error = %v; stderr = %q", err, stderr)
	}
	if want := "Installed " + unitPath + ".\nScopra Mio is enabled and running.\n"; stdout != want {
		t.Fatalf("service install stdout = %q, want %q", stdout, want)
	}
	if stderr != "" {
		t.Fatalf("service install stderr = %q, want none", stderr)
	}

	content, err := os.ReadFile(unitPath)
	if err != nil {
		t.Fatalf("ReadFile(unit) error = %v", err)
	}
	wantExecStart := scoprad + " --state-dir=" + stateDir + " --sandbox-image=" + testSandboxImage
	if want := expectedUnit(wantExecStart); string(content) != want {
		t.Fatalf("unit content =\n%s\nwant\n%s", content, want)
	}
	info, err := os.Stat(unitPath)
	if err != nil {
		t.Fatalf("Stat(unit) error = %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("unit mode = %o, want 600", info.Mode().Perm())
	}
	if calls, want := fakePodmanCalls(t, scenario), []string{"--user daemon-reload", "--user enable --now scoprad.service"}; !equalStrings(calls, want) {
		t.Fatalf("systemctl calls = %q, want %q", calls, want)
	}

	// Reinstalling the same unit is idempotent and still applies it.
	stdout, stderr, err = runCLI(t, "service", "install",
		"--state-dir", stateDir, "--sandbox-image", testSandboxImage, "--scoprad", scoprad)
	if err != nil || stderr != "" || !strings.HasPrefix(stdout, "Installed ") {
		t.Fatalf("reinstall = %q, %q, %v; want success", stdout, stderr, err)
	}
}

func TestServiceInstallWithoutAnImageSaysSo(t *testing.T) {
	home := serviceSetup(t)
	startFakeSystemctl(t, nil)
	scoprad := fakeScoprad(t)

	stdout, stderr, err := runCLI(t, "service", "install", "--scoprad", scoprad)
	if err != nil {
		t.Fatalf("service install error = %v; stderr = %q", err, stderr)
	}
	if !strings.Contains(stdout, "No sandbox image was configured") {
		t.Fatalf("install output = %q, want the missing-image note", stdout)
	}
	content, err := os.ReadFile(serviceUnitPathFor(home))
	if err != nil {
		t.Fatalf("ReadFile(unit) error = %v", err)
	}
	if strings.Contains(string(content), "--sandbox-image") {
		t.Fatalf("unit names an image that was not given:\n%s", content)
	}
}

func TestServiceInstallEscapesExecStartArguments(t *testing.T) {
	home := serviceSetup(t)
	startFakeSystemctl(t, nil)
	scoprad := fakeScoprad(t)
	stateDir := filepath.Join(home, `st ate%$"x`)

	if _, stderr, err := runCLI(t, "service", "install", "--state-dir", stateDir, "--scoprad", scoprad); err != nil {
		t.Fatalf("service install error = %v; stderr = %q", err, stderr)
	}
	content, err := os.ReadFile(serviceUnitPathFor(home))
	if err != nil {
		t.Fatalf("ReadFile(unit) error = %v", err)
	}
	wantLine := "ExecStart=" + scoprad + ` "--state-dir=` + home + `/st ate%%$$\"x"`
	if !strings.Contains(string(content), wantLine+"\n") {
		t.Fatalf("unit lacks the escaped ExecStart %q:\n%s", wantLine, content)
	}
}

func TestServiceInstallRefusesAForeignUnit(t *testing.T) {
	home := serviceSetup(t)
	scenario := startFakeSystemctl(t, nil)
	scoprad := fakeScoprad(t)
	unitPath := serviceUnitPathFor(home)
	if err := os.MkdirAll(filepath.Dir(unitPath), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	const foreign = "[Service]\nExecStart=/usr/bin/something-else\n"
	if err := os.WriteFile(unitPath, []byte(foreign), 0o600); err != nil {
		t.Fatalf("WriteFile(foreign unit) error = %v", err)
	}

	stdout, _, err := runCLI(t, "service", "install", "--scoprad", scoprad)
	if err == nil || !strings.Contains(err.Error(), "not written by Scopra") {
		t.Fatalf("service install error = %v, want a refusal naming the foreign unit", err)
	}
	if stdout != "" {
		t.Fatalf("refused install wrote stdout %q", stdout)
	}
	if content, readErr := os.ReadFile(unitPath); readErr != nil || string(content) != foreign {
		t.Fatalf("foreign unit changed: %q, %v", content, readErr)
	}
	if calls := fakePodmanCalls(t, scenario); len(calls) != 0 {
		t.Fatalf("refused install ran systemctl: %q", calls)
	}

	stdout, stderr, err := runCLI(t, "service", "install", "--scoprad", scoprad, "--force")
	if err != nil || stderr != "" {
		t.Fatalf("service install --force error = %v; stderr = %q", err, stderr)
	}
	if !strings.HasPrefix(stdout, "Installed ") {
		t.Fatalf("service install --force stdout = %q", stdout)
	}
	if content, _ := os.ReadFile(unitPath); !strings.Contains(string(content), "Managed by scopra") {
		t.Fatalf("--force did not replace the foreign unit:\n%s", content)
	}
}

func TestServiceInstallRollsBack(t *testing.T) {
	t.Run("daemon-reload fails", func(t *testing.T) {
		home := serviceSetup(t)
		scenario := startFakeSystemctl(t, map[string]string{"fail-daemon-reload": ""})
		scoprad := fakeScoprad(t)
		_, _, err := runCLI(t, "service", "install", "--scoprad", scoprad)
		if err == nil {
			t.Fatal("service install error = nil, want the reload failure")
		}
		if _, statErr := os.Lstat(serviceUnitPathFor(home)); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("failed install left a unit behind (Lstat error = %v)", statErr)
		}
		want := []string{"--user daemon-reload", "--user daemon-reload"}
		if calls := fakePodmanCalls(t, scenario); !equalStrings(calls, want) {
			t.Fatalf("systemctl calls = %q, want %q", calls, want)
		}
	})

	t.Run("enable fails and a previous unit is restored", func(t *testing.T) {
		home := serviceSetup(t)
		scoprad := fakeScoprad(t)
		unitPath := serviceUnitPathFor(home)
		startFakeSystemctl(t, nil)
		if _, stderr, err := runCLI(t, "service", "install", "--sandbox-image", testSandboxImage, "--scoprad", scoprad); err != nil {
			t.Fatalf("first install error = %v; stderr = %q", err, stderr)
		}
		before, err := os.ReadFile(unitPath)
		if err != nil {
			t.Fatalf("ReadFile(unit) error = %v", err)
		}

		scenario := startFakeSystemctl(t, map[string]string{"fail-enable": ""})
		_, _, err = runCLI(t, "service", "install", "--sandbox-image", strings.ReplaceAll(testSandboxImage, "0123456789abcdef", "fedcba9876543210"), "--scoprad", scoprad)
		if err == nil {
			t.Fatal("second install error = nil, want the enable failure")
		}
		after, readErr := os.ReadFile(unitPath)
		if readErr != nil || string(after) != string(before) {
			t.Fatalf("failed install did not restore the previous unit:\n%s\n%v", after, readErr)
		}
		want := []string{
			"--user daemon-reload", "--user enable --now scoprad.service", "--user daemon-reload",
		}
		if calls := fakePodmanCalls(t, scenario); !equalStrings(calls, want) {
			t.Fatalf("systemctl calls = %q, want %q", calls, want)
		}
	})
}

func TestServiceInstallRequiresSystemctlAndScoprad(t *testing.T) {
	t.Run("no systemctl", func(t *testing.T) {
		home := serviceSetup(t)
		scoprad := fakeScoprad(t)
		_, _, err := runCLI(t, "service", "install", "--scoprad", scoprad)
		if err == nil || !strings.Contains(err.Error(), "systemctl") {
			t.Fatalf("service install error = %v, want a missing systemctl error", err)
		}
		if _, statErr := os.Lstat(serviceUnitPathFor(home)); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("install wrote a unit without systemctl (Lstat error = %v)", statErr)
		}
		if _, statErr := os.Lstat(filepath.Dir(serviceUnitPathFor(home))); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("install created the unit directory without systemctl (Lstat error = %v)", statErr)
		}
	})

	t.Run("scoprad does not exist", func(t *testing.T) {
		serviceSetup(t)
		scenario := startFakeSystemctl(t, nil)
		missing := filepath.Join(t.TempDir(), "scoprad")
		_, _, err := runCLI(t, "service", "install", "--scoprad", missing)
		if err == nil || !strings.Contains(err.Error(), "scoprad") {
			t.Fatalf("service install error = %v, want a scoprad error", err)
		}
		if calls := fakePodmanCalls(t, scenario); len(calls) != 0 {
			t.Fatalf("install ran systemctl without scoprad: %q", calls)
		}
	})

	t.Run("scoprad is not executable", func(t *testing.T) {
		serviceSetup(t)
		startFakeSystemctl(t, nil)
		path := filepath.Join(t.TempDir(), "scoprad")
		if err := os.WriteFile(path, []byte("not a program"), 0o600); err != nil {
			t.Fatalf("WriteFile(scoprad) error = %v", err)
		}
		_, _, err := runCLI(t, "service", "install", "--scoprad", path)
		if err == nil || !strings.Contains(err.Error(), "executable") {
			t.Fatalf("service install error = %v, want a not-executable error", err)
		}
	})
}

func TestServiceInstallRejectsBadInputs(t *testing.T) {
	home := serviceSetup(t)
	scoprad := fakeScoprad(t)
	tests := map[string][]string{
		"unpinned image":       {"service", "install", "--scoprad", scoprad, "--sandbox-image", "alpine:3.20"},
		"positional argument":  {"service", "install", "extra"},
		"unknown install flag": {"service", "install", "--system", "--scoprad", scoprad},
	}
	for name, args := range tests {
		t.Run(name, func(t *testing.T) {
			_, _, err := runCLI(t, args...)
			if !isUsageError(err) {
				t.Fatalf("run(%q) error = %v, want a usage error", args, err)
			}
		})
	}
	if _, statErr := os.Lstat(serviceUnitPathFor(home)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("a rejected install wrote a unit (Lstat error = %v)", statErr)
	}
}

func TestServiceUninstall(t *testing.T) {
	home := serviceSetup(t)
	scenario := startFakeSystemctl(t, nil)
	scoprad := fakeScoprad(t)
	unitPath := serviceUnitPathFor(home)
	if _, stderr, err := runCLI(t, "service", "install", "--scoprad", scoprad); err != nil {
		t.Fatalf("service install error = %v; stderr = %q", err, stderr)
	}

	stdout, stderr, err := runCLI(t, "service", "uninstall")
	if err != nil || stderr != "" {
		t.Fatalf("service uninstall error = %v; stderr = %q", err, stderr)
	}
	if want := "Removed " + unitPath + "; Scopra Mio is stopped and disabled.\n"; stdout != want {
		t.Fatalf("service uninstall stdout = %q, want %q", stdout, want)
	}
	if _, statErr := os.Lstat(unitPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("uninstall left the unit behind (Lstat error = %v)", statErr)
	}
	want := []string{
		"--user daemon-reload", "--user enable --now scoprad.service",
		"--user disable --now scoprad.service", "--user daemon-reload",
	}
	if calls := fakePodmanCalls(t, scenario); !equalStrings(calls, want) {
		t.Fatalf("systemctl calls = %q, want %q", calls, want)
	}

	stdout, stderr, err = runCLI(t, "service", "uninstall")
	if err != nil || stderr != "" || stdout != "Scopra Mio is not installed.\n" {
		t.Fatalf("second uninstall = %q, %q, %v", stdout, stderr, err)
	}
	if calls := fakePodmanCalls(t, scenario); len(calls) != len(want) {
		t.Fatalf("second uninstall ran systemctl: %q", calls)
	}
}

func TestServiceUninstallRefusesAForeignUnit(t *testing.T) {
	home := serviceSetup(t)
	scenario := startFakeSystemctl(t, nil)
	unitPath := serviceUnitPathFor(home)
	if err := os.MkdirAll(filepath.Dir(unitPath), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(unitPath, []byte("[Service]\nExecStart=/usr/bin/other\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(unit) error = %v", err)
	}
	_, _, err := runCLI(t, "service", "uninstall")
	if err == nil || !strings.Contains(err.Error(), "not written by Scopra") {
		t.Fatalf("service uninstall error = %v, want a refusal", err)
	}
	if _, statErr := os.Lstat(unitPath); !errors.Is(statErr, nil) {
		t.Fatalf("foreign unit was removed (Lstat error = %v)", statErr)
	}
	if calls := fakePodmanCalls(t, scenario); len(calls) != 0 {
		t.Fatalf("refused uninstall ran systemctl: %q", calls)
	}
}

func TestServiceStatus(t *testing.T) {
	home := serviceSetup(t)
	scoprad := fakeScoprad(t)
	unitPath := serviceUnitPathFor(home)

	t.Run("not installed", func(t *testing.T) {
		stdout, stderr, err := runCLI(t, "service", "status")
		if err != nil || stderr != "" {
			t.Fatalf("service status error = %v; stderr = %q", err, stderr)
		}
		if want := "Scopra Mio service: not installed\nUnit: " + unitPath + "\n"; stdout != want {
			t.Fatalf("service status = %q, want %q", stdout, want)
		}
		var report serviceStatusJSON
		stdout, _, err = runCLI(t, "service", "status", "--json")
		if err != nil {
			t.Fatalf("service status --json error = %v", err)
		}
		if err := decodeJSONLine(t, stdout, &report); err != nil {
			t.Fatalf("status --json is not one JSON object: %v", err)
		}
		if report.SchemaVersion != 1 || report.Installed || report.UnitPath != unitPath {
			t.Fatalf("status report = %+v, want installed:false and schema 1", report)
		}
	})

	t.Run("installed and running", func(t *testing.T) {
		scenario := startFakeSystemctl(t, nil)
		if _, stderr, err := runCLI(t, "service", "install", "--scoprad", scoprad); err != nil {
			t.Fatalf("service install error = %v; stderr = %q", err, stderr)
		}
		stdout, stderr, err := runCLI(t, "service", "status", "--json")
		if err != nil || stderr != "" {
			t.Fatalf("service status error = %v; stderr = %q", err, stderr)
		}
		var report serviceStatusJSON
		if err := decodeJSONLine(t, stdout, &report); err != nil {
			t.Fatalf("status --json is not one JSON object: %v", err)
		}
		if !report.Installed || !report.Managed || report.Enabled != "enabled" || report.Active != "active" {
			t.Fatalf("status report = %+v, want installed, managed, enabled, active", report)
		}
		// Status is read-only: it runs only the two queries.
		calls := fakePodmanCalls(t, scenario)
		if len(calls) != 4 || calls[2] != "--user is-enabled scoprad.service" || calls[3] != "--user is-active scoprad.service" {
			t.Fatalf("status calls = %q, want install calls plus is-enabled and is-active", calls)
		}
	})

	t.Run("installed and failed", func(t *testing.T) {
		home := serviceSetup(t)
		unitPath := serviceUnitPathFor(home)
		startFakeSystemctl(t, map[string]string{"enabled": "", "failed": ""})
		if err := os.MkdirAll(filepath.Dir(unitPath), 0o700); err != nil {
			t.Fatalf("MkdirAll() error = %v", err)
		}
		if err := os.WriteFile(unitPath, []byte(expectedUnit("/bin/scoprad --state-dir=/tmp/x")), 0o600); err != nil {
			t.Fatalf("WriteFile(unit) error = %v", err)
		}
		stdout, _, err := runCLI(t, "service", "status", "--json")
		if err != nil {
			t.Fatalf("service status error = %v", err)
		}
		var report serviceStatusJSON
		if err := decodeJSONLine(t, stdout, &report); err != nil {
			t.Fatalf("status --json is not one JSON object: %v", err)
		}
		if report.Enabled != "enabled" || report.Active != "failed" {
			t.Fatalf("status report = %+v, want enabled and failed", report)
		}
	})

	t.Run("installed without systemctl", func(t *testing.T) {
		home := serviceSetup(t)
		unitPath := serviceUnitPathFor(home)
		if err := os.MkdirAll(filepath.Dir(unitPath), 0o700); err != nil {
			t.Fatalf("MkdirAll() error = %v", err)
		}
		if err := os.WriteFile(unitPath, []byte(expectedUnit("/bin/scoprad")), 0o600); err != nil {
			t.Fatalf("WriteFile(unit) error = %v", err)
		}
		_, _, err := runCLI(t, "service", "status")
		if err == nil || !strings.Contains(err.Error(), "systemctl") {
			t.Fatalf("service status error = %v, want a missing systemctl error", err)
		}
	})
}

func TestSecurity_ServiceRefusesInsecureDirectories(t *testing.T) {
	t.Run("a writable ancestor", func(t *testing.T) {
		home := serviceSetup(t)
		scoprad := fakeScoprad(t)
		startFakeSystemctl(t, nil)
		systemdDir := filepath.Join(home, ".config", "systemd")
		if err := os.MkdirAll(systemdDir, 0o700); err != nil {
			t.Fatalf("MkdirAll() error = %v", err)
		}
		if err := os.Chmod(systemdDir, 0o777); err != nil {
			t.Fatalf("Chmod() error = %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(systemdDir, 0o700) })

		_, _, err := runCLI(t, "service", "install", "--scoprad", scoprad)
		if err == nil || !strings.Contains(err.Error(), "writable") {
			t.Fatalf("service install error = %v, want the writable-ancestor refusal", err)
		}
		if _, statErr := os.Lstat(serviceUnitPathFor(home)); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("install wrote a unit under a writable directory (Lstat error = %v)", statErr)
		}
	})

	t.Run("a symlinked unit", func(t *testing.T) {
		home := serviceSetup(t)
		scoprad := fakeScoprad(t)
		startFakeSystemctl(t, nil)
		unitPath := serviceUnitPathFor(home)
		if err := os.MkdirAll(filepath.Dir(unitPath), 0o700); err != nil {
			t.Fatalf("MkdirAll() error = %v", err)
		}
		target := filepath.Join(t.TempDir(), "target")
		if err := os.WriteFile(target, []byte("target"), 0o600); err != nil {
			t.Fatalf("WriteFile(target) error = %v", err)
		}
		if err := os.Symlink(target, unitPath); err != nil {
			t.Fatalf("Symlink() error = %v", err)
		}

		_, _, err := runCLI(t, "service", "install", "--scoprad", scoprad)
		if err == nil || !strings.Contains(err.Error(), "regular file") {
			t.Fatalf("service install error = %v, want a symlink refusal", err)
		}
		if content, readErr := os.ReadFile(target); readErr != nil || string(content) != "target" {
			t.Fatalf("install wrote through the symlink: %q, %v", content, readErr)
		}
	})
}

// equalStrings reports whether two string slices are equal.
func equalStrings(left, right []string) bool {
	return slices.Equal(left, right)
}

// decodeJSONLine decodes exactly one JSON value from data.
func decodeJSONLine(t *testing.T, data string, target any) error {
	t.Helper()
	return json.Unmarshal([]byte(data), target)
}

func TestServiceDispatches(t *testing.T) {
	serviceSetup(t)
	for name, args := range map[string][]string{
		"no subcommand":      {"service"},
		"unknown subcommand": {"service", "explode"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := runCLI(t, args...); !isUsageError(err) {
				t.Fatalf("run(%q) error = %v, want a usage error", args, err)
			}
		})
	}
}

func TestSystemdExecArg(t *testing.T) {
	tests := map[string]struct {
		value string
		want  string
	}{
		"plain":              {"/usr/bin/scoprad", "/usr/bin/scoprad"},
		"percent and dollar": {"/srv/a%b$c", "/srv/a%%b$$c"},
		"spaces":             {"/srv/a b", `"/srv/a b"`},
		"quotes and slashes": {`/srv/a"b\c`, `"/srv/a\"b\\c"`},
		"single quote":       {"/srv/a'b", `"/srv/a'b"`},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := systemdExecArg(test.value)
			if err != nil {
				t.Fatalf("systemdExecArg(%q) error = %v", test.value, err)
			}
			if got != test.want {
				t.Fatalf("systemdExecArg(%q) = %q, want %q", test.value, got, test.want)
			}
		})
	}
	for name, value := range map[string]string{
		"empty": "", "newline": "/srv/a\nb", "carriage return": "/srv/a\rb", "tab": "/srv/a\tb",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := systemdExecArg(value); err == nil {
				t.Fatalf("systemdExecArg(%q) error = nil, want a refusal", value)
			}
		})
	}
}

func TestServiceInstallUsesTheConfigHome(t *testing.T) {
	home := serviceSetup(t)
	startFakeSystemctl(t, nil)
	scoprad := fakeScoprad(t)
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)

	stdout, stderr, err := runCLI(t, "service", "install", "--scoprad", scoprad)
	if err != nil {
		t.Fatalf("service install error = %v; stderr = %q", err, stderr)
	}
	wantUnit := filepath.Join(configHome, "systemd", "user", "scoprad.service")
	if !strings.Contains(stdout, wantUnit) {
		t.Fatalf("install output = %q, want the unit under $XDG_CONFIG_HOME", stdout)
	}
	if _, err := os.Stat(wantUnit); err != nil {
		t.Fatalf("unit not under $XDG_CONFIG_HOME: %v", err)
	}
	if _, err := os.Stat(serviceUnitPathFor(home)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("install wrote to the home directory despite $XDG_CONFIG_HOME: %v", err)
	}
}

func TestServiceInstallRejectsARelativeConfigHome(t *testing.T) {
	serviceSetup(t)
	startFakeSystemctl(t, nil)
	scoprad := fakeScoprad(t)
	t.Setenv("XDG_CONFIG_HOME", "relative")

	_, _, err := runCLI(t, "service", "install", "--scoprad", scoprad)
	if err == nil || !strings.Contains(err.Error(), "$XDG_CONFIG_HOME") {
		t.Fatalf("service install error = %v, want the XDG_CONFIG_HOME refusal", err)
	}
}

func TestServiceInstallFindsScopradOnPath(t *testing.T) {
	serviceSetup(t)
	startFakeSystemctl(t, nil)
	binDir := t.TempDir()
	path := filepath.Join(binDir, "scoprad")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(scoprad) error = %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	if _, stderr, err := runCLI(t, "service", "install"); err != nil {
		t.Fatalf("service install error = %v; stderr = %q", err, stderr)
	}
}

func TestServiceInstallRejectsAControlCharacterStateDir(t *testing.T) {
	serviceSetup(t)
	startFakeSystemctl(t, nil)
	scoprad := fakeScoprad(t)
	stateDir := filepath.Join(t.TempDir(), "state\ninjected")

	_, _, err := runCLI(t, "service", "install", "--state-dir", stateDir, "--scoprad", scoprad)
	if err == nil || !strings.Contains(err.Error(), "control characters") {
		t.Fatalf("service install error = %v, want the control-character refusal", err)
	}
}

func TestServiceUninstallStopsWhenDisableFails(t *testing.T) {
	home := serviceSetup(t)
	startFakeSystemctl(t, nil)
	scoprad := fakeScoprad(t)
	if _, stderr, err := runCLI(t, "service", "install", "--scoprad", scoprad); err != nil {
		t.Fatalf("service install error = %v; stderr = %q", err, stderr)
	}
	scenario := startFakeSystemctl(t, map[string]string{"fail-disable": ""})

	_, _, err := runCLI(t, "service", "uninstall")
	if err == nil || !strings.Contains(err.Error(), "disable") {
		t.Fatalf("service uninstall error = %v, want the disable failure", err)
	}
	if _, statErr := os.Lstat(serviceUnitPathFor(home)); statErr != nil {
		t.Fatalf("uninstall removed the unit after a failed disable: %v", statErr)
	}
	if calls, want := fakePodmanCalls(t, scenario), []string{"--user disable --now scoprad.service"}; !equalStrings(calls, want) {
		t.Fatalf("systemctl calls = %q, want %q", calls, want)
	}
}

func TestServiceStatusHumanAndForeign(t *testing.T) {
	home := serviceSetup(t)
	startFakeSystemctl(t, nil)
	scoprad := fakeScoprad(t)
	if _, stderr, err := runCLI(t, "service", "install", "--scoprad", scoprad); err != nil {
		t.Fatalf("service install error = %v; stderr = %q", err, stderr)
	}
	stdout, _, err := runCLI(t, "service", "status")
	if err != nil {
		t.Fatalf("service status error = %v", err)
	}
	for _, want := range []string{"installed (managed by scopra)", "Enabled: enabled", "Active: active"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("service status = %q, want %q", stdout, want)
		}
	}

	// A foreign unit is reported as such and still queried read-only.
	unitPath := serviceUnitPathFor(home)
	if err := os.WriteFile(unitPath, []byte("[Service]\nExecStart=/usr/bin/other\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(unit) error = %v", err)
	}
	stdout, _, err = runCLI(t, "service", "status", "--json")
	if err != nil {
		t.Fatalf("service status --json error = %v", err)
	}
	var report serviceStatusJSON
	if err := decodeJSONLine(t, stdout, &report); err != nil {
		t.Fatalf("status --json is not one JSON object: %v", err)
	}
	if report.Managed {
		t.Fatalf("foreign unit reported as managed: %+v", report)
	}
}

func TestServiceStatusReportsAnUnansweredQuery(t *testing.T) {
	home := serviceSetup(t)
	startFakeSystemctl(t, map[string]string{"silent-is-enabled": ""})
	unitPath := serviceUnitPathFor(home)
	if err := os.MkdirAll(filepath.Dir(unitPath), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(unitPath, []byte(expectedUnit("/bin/scoprad")), 0o600); err != nil {
		t.Fatalf("WriteFile(unit) error = %v", err)
	}

	_, _, err := runCLI(t, "service", "status")
	if err == nil || !strings.Contains(err.Error(), "enabled state") {
		t.Fatalf("service status error = %v, want the query failure", err)
	}
}

func TestServiceInstallForceReplacesAForeignUnit(t *testing.T) {
	home := serviceSetup(t)
	scenario := startFakeSystemctl(t, nil)
	scoprad := fakeScoprad(t)
	unitPath := serviceUnitPathFor(home)
	if err := os.MkdirAll(filepath.Dir(unitPath), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	foreign := "[Service]\nExecStart=/usr/bin/other\n"
	if err := os.WriteFile(unitPath, []byte(foreign), 0o600); err != nil {
		t.Fatalf("WriteFile(unit) error = %v", err)
	}

	_, _, err := runCLI(t, "service", "install", "--scoprad", scoprad)
	if err == nil || !strings.Contains(err.Error(), "was not written by Scopra") {
		t.Fatalf("service install error = %v, want the foreign-unit refusal", err)
	}
	if content, readErr := os.ReadFile(unitPath); readErr != nil || string(content) != foreign {
		t.Fatalf("a refused install changed the foreign unit: %q, %v", content, readErr)
	}
	if calls := fakePodmanCalls(t, scenario); len(calls) != 0 {
		t.Fatalf("a refused install ran systemctl: %q", calls)
	}

	if _, stderr, err := runCLI(t, "service", "install", "--scoprad", scoprad, "--force"); err != nil {
		t.Fatalf("service install --force error = %v; stderr = %q", err, stderr)
	}
	content, err := os.ReadFile(unitPath)
	if err != nil {
		t.Fatalf("ReadFile(unit) error = %v", err)
	}
	if want := expectedUnit(scoprad + " --state-dir=" + filepath.Join(home, ".local", "state", "scopra")); string(content) != want {
		t.Fatalf("forced unit content =\n%s\nwant\n%s", content, want)
	}
}

func TestServiceStatusJSONWhenNotInstalled(t *testing.T) {
	serviceSetup(t)
	t.Setenv("PATH", "")
	stdout, stderr, err := runCLI(t, "service", "status", "--json")
	if err != nil || stderr != "" {
		t.Fatalf("service status --json = %q, %v", stderr, err)
	}
	var report map[string]any
	if err := decodeJSONLine(t, stdout, &report); err != nil {
		t.Fatalf("status --json is not one JSON object: %v", err)
	}
	for _, key := range []string{"schemaVersion", "unitPath", "installed"} {
		if _, ok := report[key]; !ok {
			t.Errorf("status --json lacks %q: %v", key, report)
		}
	}
	if len(report) != 3 {
		t.Fatalf("status --json for a missing unit has extra keys: %v", report)
	}
	if installed, _ := report["installed"].(bool); installed {
		t.Fatalf("status --json reports a missing unit as installed: %v", report)
	}
}

func TestServiceUninstallWithoutSystemctl(t *testing.T) {
	home := serviceSetup(t)
	startFakeSystemctl(t, nil)
	scoprad := fakeScoprad(t)
	if _, stderr, err := runCLI(t, "service", "install", "--scoprad", scoprad); err != nil {
		t.Fatalf("service install error = %v; stderr = %q", err, stderr)
	}
	t.Setenv("PATH", "")

	_, _, err := runCLI(t, "service", "uninstall")
	if err == nil || !strings.Contains(err.Error(), "systemctl") {
		t.Fatalf("service uninstall error = %v, want a missing systemctl error", err)
	}
	if _, statErr := os.Lstat(serviceUnitPathFor(home)); statErr != nil {
		t.Fatalf("uninstall removed the unit without systemctl: %v", statErr)
	}
}

func TestServiceInstallRefusesAnInsecureUnitDirectory(t *testing.T) {
	home := serviceSetup(t)
	startFakeSystemctl(t, nil)
	scoprad := fakeScoprad(t)
	unitDir := filepath.Dir(serviceUnitPathFor(home))
	if err := os.MkdirAll(unitDir, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.Chmod(unitDir, 0o777); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}

	_, _, err := runCLI(t, "service", "install", "--scoprad", scoprad)
	if err == nil || !strings.Contains(err.Error(), "writable by other users") {
		t.Fatalf("service install error = %v, want the insecure-directory refusal", err)
	}
	if _, statErr := os.Lstat(serviceUnitPathFor(home)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("install wrote a unit into an insecure directory: %v", statErr)
	}
}

func TestServiceInstallReportsAnUnwritableDirectory(t *testing.T) {
	home := serviceSetup(t)
	startFakeSystemctl(t, nil)
	scoprad := fakeScoprad(t)
	unitDir := filepath.Dir(serviceUnitPathFor(home))
	if err := os.MkdirAll(unitDir, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.Chmod(unitDir, 0o500); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}

	_, _, err := runCLI(t, "service", "install", "--scoprad", scoprad)
	if err == nil {
		t.Fatal("service install into a read-only directory succeeded")
	}
	if _, statErr := os.Lstat(serviceUnitPathFor(home)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("a failed write left a unit behind: %v", statErr)
	}
}
