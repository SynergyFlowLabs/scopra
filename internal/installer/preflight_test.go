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
	return Environment{
		GOOS:     goos,
		StateDir: stateDir,
		Run: func(_ context.Context, name string, args ...string) ([]byte, error) {
			key := name + " " + strings.Join(args, " ")
			if answer, ok := answers[key]; ok {
				return []byte(answer), nil
			}
			return nil, errors.New("executable file not found in $PATH")
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
	if got := findCheck(t, checks, "podman").Detail; got != "podman 4.9.3, rootless, cgroups v2" {
		t.Fatalf("podman detail = %q", got)
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
