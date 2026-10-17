package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// doctorFakePodmanEnv names the directory of a fake Podman scenario. When it
// is set, the test binary acts as podman instead of running tests, so that
// the containment check can be exercised without Podman installed.
const doctorFakePodmanEnv = "SCOPRA_DOCTOR_FAKE_PODMAN"

// testSandboxImage is a reference pinned by digest, as ADR-010 requires.
const testSandboxImage = "runner@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestMain(m *testing.M) {
	if dir := os.Getenv(doctorFakePodmanEnv); dir != "" {
		os.Exit(fakePodman(dir, os.Args[1:]))
	}
	if dir := os.Getenv(fakeSystemctlEnv); dir != "" {
		os.Exit(fakeSystemctl(dir, os.Args[1:]))
	}
	// `scopra doctor` resolves podman from PATH. Point PATH at a private empty
	// directory so that no test depends on the host's Podman; tests that need
	// one call startFakePodman, which prepends its own directory.
	dir, err := os.MkdirTemp("", "scopra-test-path")
	if err != nil {
		fmt.Fprintln(os.Stderr, "scopra tests cannot create a private PATH:", err)
		os.Exit(1)
	}
	if err := os.Setenv("PATH", dir); err != nil {
		fmt.Fprintln(os.Stderr, "scopra tests cannot set PATH:", err)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// fakePodmanInfo is `podman info` output from a host that meets ADR-010.
const fakePodmanInfo = `{"host":{"cgroupVersion":"v2","cgroupControllers":["cpu","memory","pids"],"security":{"rootless":true}},"version":{"Version":"5.2.0"}}`

// fakePodman imitates the two read-only podman commands that doctor runs. The
// scenario directory may hold:
//
//   - info.json, the output of `podman info` (default: a healthy host);
//   - info-fails, which makes `podman info` fail;
//   - image-missing, which makes `podman image exists` exit 1;
//   - image-fails, which makes `podman image exists` fail differently;
//   - slow, which makes every call sleep for five seconds.
//
// Every call is appended to calls.log, so a test can prove that doctor ran
// nothing else.
func fakePodman(dir string, args []string) int {
	if len(args) == 0 {
		return 125
	}
	appendDoctorLine(dir, "calls.log", strings.Join(args, " "))
	if doctorFileExists(filepath.Join(dir, "slow")) {
		time.Sleep(5 * time.Second)
	}
	switch args[0] {
	case "info":
		if doctorFileExists(filepath.Join(dir, "info-fails")) {
			fmt.Fprintln(os.Stderr, "Error: cannot connect to Podman")
			return 125
		}
		if data, err := os.ReadFile(filepath.Join(dir, "info.json")); err == nil {
			_, _ = os.Stdout.Write(data)
			return 0
		}
		fmt.Println(fakePodmanInfo)
		return 0
	case "image":
		if len(args) == 3 && args[1] == "exists" {
			switch {
			case doctorFileExists(filepath.Join(dir, "image-missing")):
				return 1
			case doctorFileExists(filepath.Join(dir, "image-fails")):
				return 125
			}
			return 0
		}
	}
	fmt.Fprintf(os.Stderr, "fake podman: unexpected call %q\n", args)
	return 125
}

// startFakePodman creates a scenario directory and a PATH entry whose podman
// is this test binary, then selects the scenario for the test.
func startFakePodman(t *testing.T, files map[string]string) string {
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
	if err := os.Symlink(executable, filepath.Join(binDir, "podman")); err != nil {
		t.Fatalf("create the fake podman symlink: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv(doctorFakePodmanEnv, dir)
	// A race-enabled test binary sleeps for a second before exiting unless
	// told otherwise, and every fake podman call is one.
	t.Setenv("GORACE", "atexit_sleep_ms=0")
	return dir
}

// fakePodmanCalls returns the podman calls that the scenario received.
func fakePodmanCalls(t *testing.T, dir string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "calls.log"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatalf("ReadFile(calls.log) error = %v", err)
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

func appendDoctorLine(dir, name, line string) {
	file, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer file.Close()
	_, _ = fmt.Fprintln(file, line)
}

func doctorFileExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func TestRunDoctorReportsContainmentReady(t *testing.T) {
	stateDir, stop := startStatusDaemon(t)
	defer stop()
	fake := startFakePodman(t, nil)

	report, err := runDoctorJSON(t, "--state-dir", stateDir, "--sandbox-image", testSandboxImage)
	if err != nil {
		t.Fatalf("run(doctor) error = %v, want nil", err)
	}
	assertChecks(t, report,
		"platform=pass", "state-directory=pass", "workspace-registry=pass",
		"workspaces=pass", "daemon=pass", "containment=pass")
	if report.Result != "pass" {
		t.Fatalf("doctor result = %q, want pass", report.Result)
	}
	containment := findCheck(t, report, "containment")
	for _, want := range []string{"rootless Podman 5.2.0", "cgroups v2", "in local storage"} {
		if !strings.Contains(containment.Detail, want) {
			t.Errorf("containment detail = %q, want it to contain %q", containment.Detail, want)
		}
	}
	if slices.Contains(report.NotChecked, "containment") {
		t.Fatalf("doctor still lists containment as not checked: %q", report.NotChecked)
	}
	// Doctor diagnoses; it must run nothing that could pull, start, or change
	// a container.
	if calls, want := fakePodmanCalls(t, fake), []string{"info --format json", "image exists " + testSandboxImage}; !slices.Equal(calls, want) {
		t.Fatalf("podman calls = %q, want exactly %q", calls, want)
	}
}

func TestRunDoctorContainmentStatuses(t *testing.T) {
	type testCase struct {
		// fake runs the fake podman with these scenario files; when it is nil
		// no podman is on PATH at all.
		fake       map[string]string
		image      string
		wantStatus string
		wantDetail string
		wantHint   string
	}
	tests := map[string]testCase{
		"podman is not installed": {
			image:      testSandboxImage,
			wantStatus: "warn",
			wantDetail: "the podman command was not found",
			wantHint:   "Install rootless Podman",
		},
		"podman info output is invalid": {
			fake:       map[string]string{"info.json": "not json"},
			image:      testSandboxImage,
			wantStatus: "fail",
			wantDetail: "not a JSON object of the expected shape",
			wantHint:   "rootless Podman 4.0 or later",
		},
		"podman info fails": {
			fake:       map[string]string{"info-fails": ""},
			image:      testSandboxImage,
			wantStatus: "fail",
			wantDetail: "podman info failed",
			wantHint:   "Run `podman info`",
		},
		"podman runs as root": {
			fake:       map[string]string{"info.json": podmanInfoJSON("5.2.0", false, "v2", "cpu,memory,pids")},
			image:      testSandboxImage,
			wantStatus: "fail",
			wantDetail: "Podman runs as root, not rootless",
			wantHint:   "rootless Podman 4.0 or later",
		},
		"cgroups are v1": {
			fake:       map[string]string{"info.json": podmanInfoJSON("5.2.0", true, "v1", "cpu,memory,pids")},
			image:      testSandboxImage,
			wantStatus: "fail",
			wantDetail: "cgroups are not v2",
			wantHint:   "cgroups v2",
		},
		"a controller is not delegated": {
			fake:       map[string]string{"info.json": podmanInfoJSON("5.2.0", true, "v2", "cpu,memory")},
			image:      testSandboxImage,
			wantStatus: "fail",
			wantDetail: "cgroup controllers not delegated: pids",
			wantHint:   "cpu, memory, and pids",
		},
		"podman is too old": {
			fake:       map[string]string{"info.json": podmanInfoJSON("3.4.4", true, "v2", "cpu,memory,pids")},
			image:      testSandboxImage,
			wantStatus: "fail",
			wantDetail: "older than 4.0",
			wantHint:   "rootless Podman 4.0 or later",
		},
		"no image is configured": {
			fake:       map[string]string{},
			wantStatus: "warn",
			wantDetail: "no --sandbox-image was given",
			wantHint:   "--sandbox-image",
		},
		"the image is not pinned": {
			fake:       map[string]string{},
			image:      "alpine:3.20",
			wantStatus: "fail",
			wantDetail: "pinned by digest",
			wantHint:   "pinned by digest",
		},
		"the image is missing": {
			fake:       map[string]string{"image-missing": ""},
			image:      testSandboxImage,
			wantStatus: "fail",
			wantDetail: "not in local storage",
			wantHint:   "Pull the pinned image",
		},
		"the image query fails": {
			fake:       map[string]string{"image-fails": ""},
			image:      testSandboxImage,
			wantStatus: "fail",
			wantDetail: "podman image exists failed",
			wantHint:   "podman image exists",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			stateDir, stop := startStatusDaemon(t)
			defer stop()
			if test.fake != nil {
				startFakePodman(t, test.fake)
			}
			args := []string{"--state-dir", stateDir}
			if test.image != "" {
				args = append(args, "--sandbox-image", test.image)
			}
			report, err := runDoctorJSON(t, args...)
			if test.wantStatus == "fail" {
				requireCheckFailure(t, err)
			} else if err != nil {
				t.Fatalf("run(doctor) error = %v, want nil", err)
			}
			containment := findCheck(t, report, "containment")
			if containment.Status != test.wantStatus {
				t.Fatalf("containment status = %q, want %q (detail %q)", containment.Status, test.wantStatus, containment.Detail)
			}
			if !strings.Contains(containment.Detail, test.wantDetail) {
				t.Fatalf("containment detail = %q, want it to contain %q", containment.Detail, test.wantDetail)
			}
			if !strings.Contains(containment.Hint, test.wantHint) {
				t.Fatalf("containment hint = %q, want it to contain %q", containment.Hint, test.wantHint)
			}
			if report.Result != test.wantStatus {
				t.Fatalf("doctor result = %q, want %q", report.Result, test.wantStatus)
			}
		})
	}
}

func TestSecurity_DoctorContainmentIsReadOnly(t *testing.T) {
	requireLinux(t)
	stateDir := makePrivateStateDir(t)
	fake := startFakePodman(t, nil)
	before := stateDirEntries(t, stateDir)

	// The daemon check fails because Mio is not running; that is not what this
	// test is about.
	_, _ = runDoctorJSON(t, "--state-dir", stateDir, "--sandbox-image", testSandboxImage)

	allowed := map[string]bool{
		"info --format json":               true,
		"image exists " + testSandboxImage: true,
	}
	for _, call := range fakePodmanCalls(t, fake) {
		if !allowed[call] {
			t.Fatalf("doctor ran podman %q; only info and image exists are read-only", call)
		}
	}
	if after := stateDirEntries(t, stateDir); !slices.Equal(before, after) {
		t.Fatalf("doctor changed the state directory: before %q, after %q", before, after)
	}
}

func TestRunDoctorContainmentHelperIsBounded(t *testing.T) {
	older := containmentHelperTimeout
	containmentHelperTimeout = 200 * time.Millisecond
	t.Cleanup(func() { containmentHelperTimeout = older })
	startFakePodman(t, map[string]string{"slow": ""})
	stateDir := makePrivateStateDir(t)

	begin := time.Now()
	report, _ := runDoctorJSON(t, "--state-dir", stateDir, "--sandbox-image", testSandboxImage)
	elapsed := time.Since(begin)

	containment := findCheck(t, report, "containment")
	if containment.Status != "fail" || !strings.Contains(containment.Detail, "podman info failed") {
		t.Fatalf("containment = %+v, want a failing podman info after the deadline", containment)
	}
	// The fake sleeps for five seconds; doctor must not wait for it.
	if elapsed > 3*time.Second {
		t.Fatalf("doctor waited %s for a hung podman, want the helper deadline to bound it", elapsed)
	}
}

// podmanInfoJSON builds `podman info` output with the given facts.
func podmanInfoJSON(version string, rootless bool, cgroupVersion, controllers string) string {
	quoted := make([]string, 0, 3)
	for _, controller := range strings.Split(controllers, ",") {
		quoted = append(quoted, `"`+controller+`"`)
	}
	return fmt.Sprintf(
		`{"host":{"cgroupVersion":%q,"cgroupControllers":[%s],"security":{"rootless":%t}},"version":{"Version":%q}}`,
		cgroupVersion, strings.Join(quoted, ","), rootless, version)
}

// stateDirEntries lists a directory's entries, so a test can prove that
// doctor changed nothing.
func stateDirEntries(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%q) error = %v", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}
