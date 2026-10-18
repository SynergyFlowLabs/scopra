package installer

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// goodPodmanInfo is the smallest `podman info --format json` that satisfies
// ADR-010: Podman 4.0 or later, rootless, cgroups v2 with delegated
// controllers.
const goodPodmanInfo = `{
  "host": {
    "arch": "amd64",
    "cgroupVersion": "v2",
    "cgroupControllers": ["cpu", "memory", "pids", "io"],
    "security": {"rootless": true}
  },
  "version": {"Version": "4.9.3"}
}`

// fakeEnvironment returns an environment whose commands answer from a table.
// A missing entry is an error, like a command that is not installed.
func fakeEnvironment(goos, stateDir string, answers map[string]string) Environment {
	run := func(_ context.Context, name string, args ...string) ([]byte, error) {
		key := name + " " + strings.Join(args, " ")
		if answer, ok := answers[key]; ok {
			return []byte(answer), nil
		}
		if strings.HasPrefix(key, "podman info") {
			return nil, errPodmanExecutableMissing
		}
		return nil, errors.New("executable file not found in $PATH")
	}
	return Environment{
		GOOS:     goos,
		StateDir: stateDir,
		Run:      run,
		ProbePodmanInfo: func(ctx context.Context) ([]byte, error) {
			return run(ctx, "podman", "info", "--format", "json")
		},
	}
}

func defaultAnswers() map[string]string {
	return map[string]string{
		"systemctl --user show-environment": "PATH=/usr/bin\n",
		"podman info --format json":         goodPodmanInfo,
	}
}

func findCheck(t *testing.T, checks []Check, id string) Check {
	t.Helper()
	for _, check := range checks {
		if check.ID == id {
			return check
		}
	}
	t.Fatalf("check %q not found in %#v", id, checks)
	return Check{}
}

func TestChecksPassOnAPreparedLinuxHost(t *testing.T) {
	env := fakeEnvironment("linux", "/home/user/.local/state/scopra", defaultAnswers())
	checks := env.Checks(context.Background())
	if len(checks) != 4 {
		t.Fatalf("got %d checks, want 4", len(checks))
	}
	for _, check := range checks {
		if check.Status != StatusOK {
			t.Fatalf("check %q: got %v (%s), want ok", check.ID, check.Status, check.Detail)
		}
	}
	if !Ready(checks) {
		t.Fatal("a prepared host must be ready")
	}
	if got := findCheck(t, checks, "podman").Detail; got != "podman 4.9.3, rootless, cgroups v2 (delegated service)" {
		t.Fatalf("podman detail = %q", got)
	}
}

func TestChecksUseTheDelegatedPodmanProbe(t *testing.T) {
	answers := defaultAnswers()
	answers["podman info --format json"] = `{"not":"the delegated probe"}`
	env := fakeEnvironment("linux", "/home/user/.local/state/scopra", answers)
	calls := 0
	env.ProbePodmanInfo = func(context.Context) ([]byte, error) {
		calls++
		return []byte(goodPodmanInfo), nil
	}

	check := findCheck(t, env.Checks(context.Background()), "podman")
	if check.Status != StatusOK {
		t.Fatalf("Podman check = %#v, want the delegated probe to pass", check)
	}
	if calls != 1 {
		t.Fatalf("delegated Podman probe calls = %d, want 1", calls)
	}
	if got, want := check.Detail, "podman 4.9.3, rootless, cgroups v2 (delegated service)"; got != want {
		t.Fatalf("Podman detail = %q, want %q", got, want)
	}
}

func TestMissingSystemdRunIsNotReportedAsMissingPodman(t *testing.T) {
	check := podmanInfoCheck(nil, errSystemdRunExecutableMissing, true, false)
	if !check.Failed() || check.Detail != "systemd-run is not available" {
		t.Fatalf("check = %#v, want an explicit systemd-run failure", check)
	}
	if strings.Contains(strings.ToLower(check.Fix), "install podman") {
		t.Fatalf("fix = %q, must not suggest reinstalling Podman", check.Fix)
	}
}

func TestNoServiceCheckFailsClosedWithoutAnExplicitCurrentProbe(t *testing.T) {
	env := fakeEnvironment("linux", "/state", defaultAnswers())
	check := env.PodmanCurrentContextCheck(context.Background())
	if !check.Failed() || !strings.Contains(check.Detail, "current process context") {
		t.Fatalf("manual context check = %#v, want a fail-closed missing-probe result", check)
	}
}

func TestChecksFailClosedWithoutAnExplicitDelegatedProbe(t *testing.T) {
	podmanRunCalled := false
	env := Environment{
		GOOS:     "linux",
		StateDir: "/state",
		Run: func(_ context.Context, name string, args ...string) ([]byte, error) {
			if name == "podman" {
				podmanRunCalled = true
				return []byte(goodPodmanInfo), nil
			}
			return []byte("PATH=/usr/bin\n"), nil
		},
	}

	check := findCheck(t, env.Checks(context.Background()), "podman")
	if !check.Failed() || !strings.Contains(check.Detail, "delegated systemd service") {
		t.Fatalf("Podman check = %#v, want fail-closed delegated-probe failure", check)
	}
	if podmanRunCalled {
		t.Fatal("preflight used the generic direct runner as a delegated-service probe")
	}
}

func TestNoServiceProbeUsesTheCurrentProcessContext(t *testing.T) {
	answers := defaultAnswers()
	answers["podman info --format json"] = `{"host":{"cgroupVersion":"v2","cgroupControllers":["memory","pids"],"security":{"rootless":true}},"version":{"Version":"5.7.0"}}`
	env := fakeEnvironment("linux", "/state", answers)
	env.ProbePodmanInfo = func(context.Context) ([]byte, error) {
		return []byte(goodPodmanInfo), nil
	}
	env.ProbeCurrentPodmanInfo = func(context.Context) ([]byte, error) {
		return []byte(answers["podman info --format json"]), nil
	}

	serviceCheck := findCheck(t, env.Checks(context.Background()), "podman")
	if serviceCheck.Status != StatusOK || !strings.Contains(serviceCheck.Detail, "delegated service") {
		t.Fatalf("delegated service check = %#v, want a pass in the service context", serviceCheck)
	}
	manualCheck := env.PodmanCurrentContextCheck(context.Background())
	if !manualCheck.Failed() || !strings.Contains(manualCheck.Detail, "cpu") {
		t.Fatalf("manual daemon check = %#v, want failure naming cpu", manualCheck)
	}
	if !strings.Contains(manualCheck.Fix, "systemd user service") {
		t.Fatalf("manual daemon fix = %q, want the delegated service option", manualCheck.Fix)
	}
}

func TestChecksDoNotCallATerminalScopeFailurePodmanUnavailable(t *testing.T) {
	answers := defaultAnswers()
	answers["podman info --format json"] = `{"host":{"cgroupVersion":"v2","cgroupControllers":["memory","pids"],"security":{"rootless":true}},"version":{"Version":"5.7.0"}}`
	env := fakeEnvironment("linux", "/state", answers)
	env.ProbePodmanInfo = func(context.Context) ([]byte, error) {
		return nil, errors.New("systemd-run failed")
	}

	check := findCheck(t, env.Checks(context.Background()), "podman")
	if check.Status != StatusFailed {
		t.Fatalf("status = %v, want failed", check.Status)
	}
	if !strings.Contains(check.Detail, "delegated systemd service") {
		t.Fatalf("detail = %q, want delegated-service diagnosis", check.Detail)
	}
	if strings.Contains(strings.ToLower(check.Fix), "install podman") {
		t.Fatalf("fix = %q, must not suggest reinstalling Podman", check.Fix)
	}
}

func TestChecksRefuseAnotherPlatform(t *testing.T) {
	env := fakeEnvironment("darwin", "/Users/x/.local/state/scopra", defaultAnswers())
	checks := env.Checks(context.Background())
	check := findCheck(t, checks, "linux")
	if check.Status != StatusFailed || !check.Required {
		t.Fatalf("linux check = %#v", check)
	}
	if !strings.Contains(check.Fix, "Linux") {
		t.Fatalf("linux fix = %q", check.Fix)
	}
	if Ready(checks) {
		t.Fatal("a non-Linux host must not be ready")
	}
	if got := FailureReason(checks); !strings.Contains(got, "Linux host") {
		t.Fatalf("failure reason = %q", got)
	}
}

func TestChecksFailWhenSystemdIsMissing(t *testing.T) {
	answers := defaultAnswers()
	delete(answers, "systemctl --user show-environment")
	env := fakeEnvironment("linux", "/state", answers)
	check := findCheck(t, env.Checks(context.Background()), "systemd")
	if check.Status != StatusFailed {
		t.Fatalf("status = %v, want failed", check.Status)
	}
	if !strings.Contains(check.Fix, "systemd user session") {
		t.Fatalf("fix = %q", check.Fix)
	}
}

func TestChecksFailWhenPodmanIsMissing(t *testing.T) {
	answers := defaultAnswers()
	delete(answers, "podman info --format json")
	env := fakeEnvironment("linux", "/state", answers)
	check := findCheck(t, env.Checks(context.Background()), "podman")
	if check.Status != StatusFailed {
		t.Fatalf("status = %v, want failed", check.Status)
	}
	if !strings.Contains(check.Fix, "Podman 4.0") {
		t.Fatalf("fix = %q", check.Fix)
	}
}

func TestChecksNameTheUnmetPodmanRequirement(t *testing.T) {
	cases := map[string]string{
		"old version": `{"host":{"cgroupVersion":"v2","cgroupControllers":["cpu","memory","pids"],"security":{"rootless":true}},"version":{"Version":"3.4.2"}}`,
		"rootful":     `{"host":{"cgroupVersion":"v2","cgroupControllers":["cpu","memory","pids"],"security":{"rootless":false}},"version":{"Version":"4.9.3"}}`,
		"cgroups v1":  `{"host":{"cgroupVersion":"v1","cgroupControllers":["cpu","memory","pids"],"security":{"rootless":true}},"version":{"Version":"4.9.3"}}`,
		"no pids":     `{"host":{"cgroupVersion":"v2","cgroupControllers":["cpu","memory"],"security":{"rootless":true}},"version":{"Version":"4.9.3"}}`,
	}
	wants := map[string]string{
		"old version": "older than 4.0",
		"rootful":     "runs as root",
		"cgroups v1":  "cgroups are not v2",
		"no pids":     "cgroup controllers not delegated: pids",
	}
	for name, info := range cases {
		t.Run(name, func(t *testing.T) {
			answers := defaultAnswers()
			answers["podman info --format json"] = info
			env := fakeEnvironment("linux", "/state", answers)
			check := findCheck(t, env.Checks(context.Background()), "podman")
			if check.Status != StatusFailed {
				t.Fatalf("status = %v, want failed", check.Status)
			}
			if !strings.Contains(check.Detail, wants[name]) {
				t.Fatalf("detail = %q, want it to mention %q", check.Detail, wants[name])
			}
		})
	}
}

func TestChecksFailWhenPodmanInfoIsNotJSON(t *testing.T) {
	answers := defaultAnswers()
	answers["podman info --format json"] = "not json"
	env := fakeEnvironment("linux", "/state", answers)
	check := findCheck(t, env.Checks(context.Background()), "podman")
	if check.Status != StatusFailed || !strings.Contains(check.Fix, "podman info") {
		t.Fatalf("check = %#v", check)
	}
}

func TestChecksFailWhenTheStateDirectoryDoesNotResolve(t *testing.T) {
	env := fakeEnvironment("linux", "", defaultAnswers())
	env.StateDirErr = errors.New("$XDG_STATE_HOME must be absolute")
	check := findCheck(t, env.Checks(context.Background()), "state-dir")
	if check.Status != StatusFailed {
		t.Fatalf("status = %v, want failed", check.Status)
	}
	if check.Detail != "$XDG_STATE_HOME must be absolute" {
		t.Fatalf("detail = %q", check.Detail)
	}
}

func TestStatusSymbols(t *testing.T) {
	cases := map[Status]string{
		StatusOK:      "✔",
		StatusWarn:    "!",
		StatusFailed:  "✖",
		StatusRunning: "•",
		StatusPending: "·",
		StatusSkipped: "–",
	}
	for status, want := range cases {
		if got := status.Symbol(); got != want {
			t.Fatalf("status %v: got %q, want %q", status, got, want)
		}
	}
}
