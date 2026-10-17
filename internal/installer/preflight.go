package installer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/SynergyFlowLabs/scopra/internal/hollow"
	"github.com/SynergyFlowLabs/scopra/internal/localipc"
)

// Status is the outcome of a check or install step.
type Status int

const (
	// StatusPending means the work has not run yet.
	StatusPending Status = iota
	// StatusRunning means the work is in progress.
	StatusRunning
	// StatusOK means the check passed or the step finished.
	StatusOK
	// StatusWarn means the work finished with something worth reading, but
	// nothing is broken.
	StatusWarn
	// StatusFailed means the check or step failed.
	StatusFailed
	// StatusSkipped means the step did not apply to this run.
	StatusSkipped
)

// Symbol is the glyph shown next to a status.
func (s Status) Symbol() string {
	switch s {
	case StatusOK:
		return "✔"
	case StatusWarn:
		return "!"
	case StatusFailed:
		return "✖"
	case StatusRunning:
		return "•"
	case StatusSkipped:
		return "–"
	}
	return "·"
}

// Check is one preflight result.
type Check struct {
	ID       string
	Title    string
	Detail   string
	Fix      string
	Required bool
	Status   Status
}

// Failed reports whether the check blocks an install.
func (c Check) Failed() bool { return c.Required && c.Status == StatusFailed }

// Environment is everything preflight touches, named so tests can replace it.
type Environment struct {
	GOOS        string
	StateDir    string
	StateDirErr error
	Run         func(ctx context.Context, name string, args ...string) ([]byte, error)
	// Provision returns how to fix a failed prerequisite: install a missing
	// package, or delegate the cgroup controllers Podman needs. It returns
	// false when there is no plan the installer is willing to run.
	Provision func(ctx context.Context) (Provision, bool)
	// ProbePodmanInfo runs Podman's read-only info command in the cgroup
	// context Scopra's delegated systemd service will use. Tests may replace
	// it; production uses a short-lived transient user service.
	ProbePodmanInfo func(ctx context.Context) ([]byte, error)
	// ProbeCurrentPodmanInfo checks the caller's cgroup, for the explicit
	// no-service path where the user will start scoprad manually.
	ProbeCurrentPodmanInfo func(ctx context.Context) ([]byte, error)
}

// StateDir is the Mio state directory the installer reports and the service
// will use.
func (env Environment) StateDirectory() (string, error) {
	if env.StateDirErr != nil {
		return "", env.StateDirErr
	}
	if env.StateDir == "" {
		return "", fmt.Errorf("the state directory could not be resolved")
	}
	return env.StateDir, nil
}

// DefaultEnvironment returns the real environment.
func DefaultEnvironment() Environment {
	stateDir, err := localipc.ResolveStateDir("")
	return Environment{
		GOOS:                   runtime.GOOS,
		StateDir:               stateDir,
		StateDirErr:            err,
		Run:                    runCommand,
		Provision:              defaultProvision,
		ProbePodmanInfo:        defaultPodmanInfo,
		ProbeCurrentPodmanInfo: defaultDirectPodmanInfo,
	}
}

// defaultProvision decides how this machine's failed prerequisite can be fixed
// without guessing. It reads the distribution's own description before
// installing a package, and Podman's own report before touching systemd.
func defaultProvision(ctx context.Context) (Provision, bool) {
	root := os.Geteuid() == 0
	if !hasProgram("podman") {
		release, err := os.ReadFile("/etc/os-release")
		if err != nil {
			return Provision{}, false
		}
		return DetectProvision(release, hasProgram, root)
	}
	// Podman is installed. If the only thing missing is cgroup controller
	// delegation, the fix is a systemd drop-in rather than a package.
	output, err := defaultPodmanInfo(ctx)
	if err != nil {
		if errors.Is(err, errSystemdRunExecutableMissing) || errors.Is(err, errPodmanExecutableMissing) {
			return Provision{}, false
		}
		// The delegated probe is authoritative for a pass. A direct info
		// query is used only to decide whether the opt-in user@ drop-in
		// could address a missing controller; it never makes preflight pass.
		output, err = defaultDirectPodmanInfo(ctx)
		if err != nil {
			return Provision{}, false
		}
	}
	info, err := hollow.ParsePodmanInfo(output)
	if err != nil || !info.DelegationMissing() {
		return Provision{}, false
	}
	// The drop-in is already present. The delegated probe has tested the
	// context Scopra will use, so repeating the same system-level commands
	// cannot repair a remaining failure.
	if DelegationConfigured(DelegateDropInPath) {
		return Provision{}, false
	}
	stateDir, err := localipc.ResolveStateDir("")
	if err != nil || stateDir == "" {
		return Provision{}, false
	}
	return DetectDelegationFix(root, hasProgram, filepath.Join(stateDir, "installer"))
}

// defaultPodmanInfo runs Podman's read-only host probe in a transient systemd
// user service with explicit controller delegation. This mirrors the
// execution context of scoprad.service instead of the terminal's app scope.
func defaultPodmanInfo(ctx context.Context) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	podman, err := exec.LookPath("podman")
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errPodmanExecutableMissing, err)
	}
	podman, err = filepath.Abs(podman)
	if err != nil {
		return nil, err
	}
	systemdRun, err := exec.LookPath("systemd-run")
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errSystemdRunExecutableMissing, err)
	}
	systemdRun, err = filepath.Abs(systemdRun)
	if err != nil {
		return nil, err
	}
	probeCtx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	return hollow.RunDelegatedPodmanInfo(probeCtx, runCommand, systemdRun, podman)
}

// defaultDirectPodmanInfo runs a bounded probe in the caller's cgroup. It can
// classify a provisioning failure and verifies the explicit no-service path;
// its result is never accepted as proof that a delegated service is ready.
func defaultDirectPodmanInfo(ctx context.Context) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	podman, err := exec.LookPath("podman")
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errPodmanExecutableMissing, err)
	}
	podman, err = filepath.Abs(podman)
	if err != nil {
		return nil, err
	}
	probeCtx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	return runCommand(probeCtx, podman, "info", "--format", "json")
}

// hasProgram reports whether name is on PATH. It is a variable so tests can
// describe a machine without touching this one.
var hasProgram = func(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// commandTimeout bounds every preflight command. A hung Podman must not hang
// the installer.
const commandTimeout = 10 * time.Second

var (
	errPodmanExecutableMissing     = errors.New("podman executable not found")
	errSystemdRunExecutableMissing = errors.New("systemd-run executable not found")
)

// maxCommandOutput bounds the output the installer keeps from a command.
const maxCommandOutput = 1 << 20

func runCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	command.Stdout = &limitedWriter{buffer: &stdout, limit: maxCommandOutput}
	command.Stderr = &limitedWriter{buffer: &stderr, limit: maxCommandOutput}
	command.Stdin = nil
	if err := command.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = strings.TrimSpace(stdout.String())
		}
		if message != "" {
			return nil, fmt.Errorf("%v: %s", err, firstLine(message))
		}
		return nil, err
	}
	return stdout.Bytes(), nil
}

// limitedWriter keeps memory bounded no matter what a child prints.
type limitedWriter struct {
	buffer *bytes.Buffer
	limit  int
}

func (w *limitedWriter) Write(data []byte) (int, error) {
	if remaining := w.limit - w.buffer.Len(); remaining > 0 {
		if len(data) > remaining {
			data = data[:remaining]
		}
		_, _ = w.buffer.Write(data)
	}
	return len(data), nil
}

func firstLine(text string) string {
	if index := strings.IndexByte(text, '\n'); index >= 0 {
		text = text[:index]
	}
	return strings.TrimSpace(text)
}

// Checks runs the preflight in the order the installer shows it. It never
// panics on a missing tool and never assumes a check passed.
func (env Environment) Checks(ctx context.Context) []Check {
	checks := []Check{env.linuxCheck(), env.systemdCheck(ctx), env.stateDirCheck(), env.podmanCheck(ctx)}
	return checks
}

func (env Environment) linuxCheck() Check {
	check := Check{ID: "linux", Title: "Linux host", Required: true, Status: StatusOK}
	check.Detail = "running on " + env.GOOS
	if env.GOOS != "linux" {
		check.Status = StatusFailed
		check.Detail = "this build targets " + env.GOOS
		check.Fix = "Install Scopra on Linux. Mio's containment relies on Linux namespaces and cgroups."
	}
	return check
}

func (env Environment) systemdCheck(ctx context.Context) Check {
	check := Check{ID: "systemd", Title: "systemd user session", Required: true}
	if _, err := env.Run(ctx, "systemctl", "--user", "show-environment"); err != nil {
		check.Status = StatusFailed
		check.Detail = "systemctl --user is not answering"
		check.Fix = "Log in to a systemd user session, or start one with `systemctl --user start` inside a login shell."
		return check
	}
	check.Status = StatusOK
	check.Detail = "systemctl --user answers"
	return check
}

// stateDirCheck proves the state directory resolves before anything is
// installed, because a service that cannot open it refuses to start.
func (env Environment) stateDirCheck() Check {
	check := Check{ID: "state-dir", Title: "State directory", Required: true}
	dir, err := env.StateDirectory()
	if err != nil {
		check.Status = StatusFailed
		check.Detail = err.Error()
		check.Fix = "Set $XDG_STATE_HOME to an absolute path, or make $HOME absolute."
		return check
	}
	check.Status = StatusOK
	check.Detail = dir
	return check
}

func (env Environment) podmanInfo(ctx context.Context) ([]byte, error) {
	if env.ProbePodmanInfo == nil {
		return nil, errors.New("installer: no delegated Podman probe is configured")
	}
	return env.ProbePodmanInfo(ctx)
}

func (env Environment) podmanCheck(ctx context.Context) Check {
	output, err := env.podmanInfo(ctx)
	missing := errors.Is(err, errPodmanExecutableMissing)
	return podmanInfoCheck(output, err, true, missing)
}

// PodmanCurrentContextCheck verifies the cgroup of a manually launched daemon.
// It must not be substituted for the delegated-service probe used by a
// systemd-managed install.
func (env Environment) PodmanCurrentContextCheck(ctx context.Context) Check {
	if env.ProbeCurrentPodmanInfo == nil {
		return podmanInfoCheck(nil, errors.New("installer: no current-context Podman probe is configured"), false, false)
	}
	output, err := env.ProbeCurrentPodmanInfo(ctx)
	missing := errors.Is(err, errPodmanExecutableMissing) || errors.Is(err, exec.ErrNotFound)
	return podmanInfoCheck(output, err, false, missing)
}

func podmanInfoCheck(output []byte, err error, delegated, missing bool) Check {
	check := Check{ID: "podman", Title: "Rootless Podman 4.0+", Required: true}
	if err != nil {
		check.Status = StatusFailed
		switch {
		case missing:
			check.Detail = "podman is not available"
			check.Fix = "Install Podman 4.0 or later, then run `podman info` once to create its configuration."
		case errors.Is(err, errSystemdRunExecutableMissing):
			check.Detail = "systemd-run is not available"
			check.Fix = "Install systemd-run or add it to PATH, then run the installer again."
		case delegated:
			check.Detail = "the delegated systemd service probe could not run"
			check.Fix = "Check that `systemd-run --user` can start a transient service with cgroup delegation, then run the installer again."
		default:
			check.Detail = "the Podman probe in the current process context could not run"
			check.Fix = "Check that `podman info --format json` succeeds in the context where you will start scoprad."
		}
		return check
	}
	info, err := hollow.ParsePodmanInfo(output)
	if err != nil {
		check.Status = StatusFailed
		check.Detail = "podman info could not be read"
		check.Fix = "Check that `podman info --format json` prints one JSON object."
		return check
	}
	if err := info.Check(); err != nil {
		check.Status = StatusFailed
		check.Detail = containmentDetail(err)
		if delegated {
			check.Fix = "Run rootless Podman 4.0+ with cgroup v2 delegation; see ADR-010 for the exact requirements."
		} else {
			check.Fix = "Choose Scopra's systemd user service, which requests cgroup delegation, or start scoprad from a context with the required controllers."
		}
		return check
	}
	check.Status = StatusOK
	contextDetail := "(delegated service)"
	if !delegated {
		contextDetail = "(current process context)"
	}
	check.Detail = "podman " + info.Version + ", rootless, cgroups " + info.CgroupVersion + " " + contextDetail
	return check
}

// containmentDetail drops the sentinel prefix so the screen names the unmet
// requirement rather than repeating that containment is unavailable.
func containmentDetail(err error) string {
	message := err.Error()
	if prefix := hollow.ErrContainmentUnavailable.Error() + ": "; strings.HasPrefix(message, prefix) {
		return strings.TrimPrefix(message, prefix)
	}
	return message
}

// Ready reports whether every required check passed.
func Ready(checks []Check) bool {
	for _, check := range checks {
		if check.Failed() {
			return false
		}
	}
	return true
}

// FailureReason returns the first required failure, for an error message.
func FailureReason(checks []Check) string {
	for _, check := range checks {
		if check.Failed() {
			return check.Title + ": " + check.Detail
		}
	}
	return ""
}
