package main

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"

	"github.com/SynergyFlowLabs/scopra/internal/hollow"
)

// containmentHelperTimeout bounds each podman call that doctor makes, so a
// hung podman cannot stall the report.
var containmentHelperTimeout = 10 * time.Second

// diagnoseContainment checks whether this host can offer ADR-010's rootless
// containment and, when sandboxImage is set, whether that image is present.
// It is read-only: it runs only `podman info --format json` and, for a pinned
// image, `podman image exists`. It never pulls, starts, or removes anything,
// and it contacts Mio only through the existing daemon check.
//
// The statuses keep a healthy host quiet and a broken execution setup
// visible: a missing podman or image is a warning, because status and
// workspaces still work; a podman that exists but cannot meet ADR-010, or an
// image that cannot run, is a failure.
func diagnoseContainment(ctx context.Context, sandboxImage string) doctorCheck {
	podman, err := exec.LookPath("podman")
	if err != nil {
		return doctorCheck{
			Name:   checkNameContainment,
			Status: checkWarn,
			Detail: "the podman command was not found; Mio starts but refuses every run",
			Hint: "Install rootless Podman 4.0 or later (ADR-010). Mio resolves podman once at startup, " +
				"so restart scoprad after installing it.",
		}
	}

	output, err := runPodmanHelper(ctx, podman, "info", "--format", "json")
	if err != nil {
		return doctorCheck{
			Name:   checkNameContainment,
			Status: checkFail,
			Detail: "podman info failed: " + err.Error(),
			Hint:   "Run `podman info` yourself; Mio refuses every run when its preflight check fails.",
		}
	}
	info, err := hollow.ParsePodmanInfo(output)
	if err == nil {
		err = info.Check()
	}
	if err != nil {
		return doctorCheck{
			Name:   checkNameContainment,
			Status: checkFail,
			Detail: err.Error(),
			Hint: "Mio requires rootless Podman 4.0 or later, cgroups v2, and the cpu, memory, and pids " +
				"controllers delegated to your user.",
		}
	}
	host := fmt.Sprintf("rootless Podman %s with cgroups v2 and the cpu, memory, and pids controllers delegated", info.Version)

	if sandboxImage == "" {
		return doctorCheck{
			Name:   checkNameContainment,
			Status: checkWarn,
			Detail: host + "; no --sandbox-image was given, so the image was not checked",
			Hint:   "Pass the same --sandbox-image that scoprad uses. Mio starts without one but refuses every run.",
		}
	}
	if err := hollow.ValidateImage(sandboxImage); err != nil {
		return doctorCheck{
			Name:   checkNameContainment,
			Status: checkFail,
			Detail: err.Error(),
			Hint: "Pass an image pinned by digest, as ADR-010 requires; scoprad also refuses to start " +
				"with any other form.",
		}
	}

	_, err = runPodmanHelper(ctx, podman, "image", "exists", sandboxImage)
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return doctorCheck{
			Name:   checkNameContainment,
			Status: checkPass,
			Detail: host + "; the sandbox image is in local storage",
		}
	case errors.As(err, &exitErr) && exitErr.ExitCode() == 1:
		return doctorCheck{
			Name:   checkNameContainment,
			Status: checkFail,
			Detail: "the sandbox image is not in local storage, and Mio never pulls images",
			Hint:   "Pull the pinned image on this host first, then start scoprad with the same --sandbox-image.",
		}
	default:
		return doctorCheck{
			Name:   checkNameContainment,
			Status: checkFail,
			Detail: "podman image exists failed: " + err.Error(),
			Hint:   "Run `podman image exists IMAGE` yourself; Mio refuses every run when its preflight check fails.",
		}
	}
}

// runPodmanHelper runs one read-only podman query under the helper deadline.
func runPodmanHelper(ctx context.Context, podman string, args ...string) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, containmentHelperTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, podman, args...)
	// Once the deadline kills podman, stop waiting for its output.
	command.WaitDelay = time.Second
	return command.Output()
}
