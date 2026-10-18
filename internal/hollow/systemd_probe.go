package hollow

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
)

// CommandRunner runs an exact argument vector and returns standard output.
// It is injected so callers can bound execution and tests can verify that no
// shell is involved.
type CommandRunner func(context.Context, string, ...string) ([]byte, error)

const delegatedControllerList = "cpu cpuset io memory pids"

// RunDelegatedPodmanInfo runs Podman's read-only host probe in a transient
// systemd user service with the same cgroup delegation Scopra's daemon needs.
// Podman's cgroupControllers field describes the cgroup of the Podman process;
// running it directly from a desktop terminal can report fewer controllers
// than the service that will execute Scopra requests.
func RunDelegatedPodmanInfo(ctx context.Context, run CommandRunner, systemdRun, podman string) ([]byte, error) {
	if ctx == nil {
		return nil, errors.New("hollow: delegated Podman probe needs a context")
	}
	if run == nil {
		return nil, errors.New("hollow: delegated Podman probe needs a command runner")
	}
	if !filepath.IsAbs(systemdRun) {
		return nil, fmt.Errorf("hollow: systemd-run path %q is not absolute", systemdRun)
	}
	if !filepath.IsAbs(podman) {
		return nil, fmt.Errorf("hollow: podman path %q is not absolute", podman)
	}

	return run(ctx, systemdRun,
		"--user",
		"--quiet",
		"--pipe",
		"--wait",
		"--collect",
		"--property=Delegate="+delegatedControllerList,
		"--",
		podman,
		"info",
		"--format",
		"json",
	)
}
