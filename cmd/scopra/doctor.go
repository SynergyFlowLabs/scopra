package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/SynergyFlowLabs/scopra/internal/buildinfo"
	"github.com/SynergyFlowLabs/scopra/internal/localipc"
)

// doctorSchemaVersion identifies the layout of `scopra doctor --json` output.
// Incompatible changes require a new version; checks and fields may be added.
const doctorSchemaVersion = 1

const (
	checkNamePlatform       = "platform"
	checkNameStateDirectory = "state-directory"
	checkNameDaemon         = "daemon"
)

type checkStatus string

const (
	checkPass checkStatus = "pass"
	checkWarn checkStatus = "warn"
	checkFail checkStatus = "fail"
	checkSkip checkStatus = "skip"
)

// errDoctorChecksFailed reports that at least one check failed after the
// report has been written.
var errDoctorChecksFailed = errors.New("scopra doctor found failing checks")

// notCheckedAreas names readiness areas that doctor deliberately does not
// assess because Scopra does not implement them yet.
var notCheckedAreas = []string{
	"enrollment", "relay", "workspaces", "policy", "containment", "execution",
}

type doctorCheck struct {
	Name   string      `json:"name"`
	Status checkStatus `json:"status"`
	Detail string      `json:"detail"`
	Hint   string      `json:"hint,omitempty"`
}

type doctorReport struct {
	SchemaVersion int           `json:"schemaVersion"`
	ScopraVersion string        `json:"scopraVersion"`
	StateDir      string        `json:"stateDir,omitempty"`
	Result        checkStatus   `json:"result"`
	Checks        []doctorCheck `json:"checks"`
	NotChecked    []string      `json:"notChecked"`
}

// platformInfo describes the platform being diagnosed. Its zero value is an
// unsupported platform.
type platformInfo struct {
	goos         string
	goarch       string
	ipcSupported bool
}

func runDoctor(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("scopra doctor", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() { printUsage(stderr) }
	stateDir := flags.String("state-dir", "", "Mio state directory")
	jsonOutput := flags.Bool("json", false, "write the report as JSON")
	if err := flags.Parse(args); err != nil {
		return &usageError{err: err}
	}
	if flags.NArg() != 0 {
		return failUsage(stderr, "doctor takes no positional arguments")
	}

	report := diagnose(ctx, *stateDir, platformInfo{
		goos:         runtime.GOOS,
		goarch:       runtime.GOARCH,
		ipcSupported: localipc.PlatformSupported(),
	})
	if err := ctx.Err(); err != nil {
		return err
	}

	var err error
	if *jsonOutput {
		err = json.NewEncoder(stdout).Encode(report)
	} else {
		err = writeDoctorText(stdout, report)
	}
	if err != nil {
		return fmt.Errorf("write doctor report: %w", err)
	}
	if report.Result == checkFail {
		return errDoctorChecksFailed
	}
	return nil
}

// diagnose runs the read-only checks in order; a failed check skips the checks
// that depend on it. Nothing is created, repaired, removed, or started.
func diagnose(ctx context.Context, stateDir string, platform platformInfo) doctorReport {
	report := doctorReport{
		SchemaVersion: doctorSchemaVersion,
		ScopraVersion: buildinfo.Version(),
		NotChecked:    slices.Clone(notCheckedAreas),
	}

	platformCheck := diagnosePlatform(platform)
	stateDirCheck := skippedCheck(checkNameStateDirectory, checkNamePlatform)
	daemonCheck := skippedCheck(checkNameDaemon, checkNamePlatform)
	if platformCheck.Status != checkFail {
		report.StateDir, stateDirCheck = diagnoseStateDir(stateDir)
		daemonCheck = skippedCheck(checkNameDaemon, checkNameStateDirectory)
		if stateDirCheck.Status != checkFail {
			daemonCheck = diagnoseDaemon(ctx, report.StateDir)
		}
	}

	report.Checks = []doctorCheck{platformCheck, stateDirCheck, daemonCheck}
	report.Result = overallResult(report.Checks)
	return report
}

func diagnosePlatform(platform platformInfo) doctorCheck {
	name := platform.goos + "/" + platform.goarch
	if !platform.ipcSupported {
		return doctorCheck{
			Name:   checkNamePlatform,
			Status: checkFail,
			Detail: "authenticated local IPC is unavailable on " + name + "; Mio fails closed",
			Hint:   "Run Scopra Mio and Mariner on Linux; other platforms are not supported yet.",
		}
	}
	return doctorCheck{
		Name:   checkNamePlatform,
		Status: checkPass,
		Detail: name + " supports authenticated local IPC (same-user peer credentials)",
	}
}

// diagnoseStateDir resolves and inspects the state directory without creating
// or repairing it, returning the resolved path when resolution succeeds.
func diagnoseStateDir(stateDir string) (string, doctorCheck) {
	resolved, err := localipc.ResolveStateDir(stateDir)
	if err != nil {
		return "", doctorCheck{
			Name:   checkNameStateDirectory,
			Status: checkFail,
			Detail: err.Error(),
			Hint:   "Set XDG_CACHE_HOME or HOME, or pass the same --state-dir to scopra and scoprad.",
		}
	}

	err = localipc.CheckStateDir(resolved)
	switch {
	case err == nil:
		return resolved, doctorCheck{
			Name:   checkNameStateDirectory,
			Status: checkPass,
			Detail: "exists and is private to the current user",
		}
	case errors.Is(err, os.ErrNotExist):
		return resolved, doctorCheck{
			Name:   checkNameStateDirectory,
			Status: checkWarn,
			Detail: "does not exist yet",
			Hint:   "scoprad creates it with owner-only permissions when it starts.",
		}
	case errors.Is(err, localipc.ErrSocketPathTooLong):
		return resolved, doctorCheck{
			Name:   checkNameStateDirectory,
			Status: checkFail,
			Detail: err.Error(),
			Hint:   "Choose a shorter --state-dir and pass the same path to scoprad.",
		}
	case errors.Is(err, localipc.ErrInsecureStateDir):
		return resolved, doctorCheck{
			Name:   checkNameStateDirectory,
			Status: checkFail,
			Detail: err.Error(),
			Hint: "The directory must be owned by you with mode 0700, and no ancestor may be " +
				"writable by others unless it is a root-owned sticky directory. " +
				"Fix the permissions yourself or choose another --state-dir.",
		}
	default:
		return resolved, doctorCheck{
			Name:   checkNameStateDirectory,
			Status: checkFail,
			Detail: err.Error(),
			Hint:   "Inspect the directory or choose another --state-dir.",
		}
	}
}

// diagnoseDaemon sends Mio the existing authenticated, read-only status
// request. It never starts Mio or removes a stale socket.
func diagnoseDaemon(ctx context.Context, stateDir string) doctorCheck {
	response, err := localipc.Status(ctx, stateDir)
	switch {
	case err == nil:
		return doctorCheck{
			Name:   checkNameDaemon,
			Status: checkPass,
			Detail: fmt.Sprintf(
				"Scopra Mio's local IPC endpoint responds (daemon version %s)",
				response.DaemonVersion,
			),
		}
	case errors.Is(err, localipc.ErrDaemonUnavailable):
		return doctorCheck{
			Name:   checkNameDaemon,
			Status: checkFail,
			Detail: "Scopra Mio is not running (its socket is missing or refuses connections)",
			Hint:   "Start Mio in the foreground with `scoprad` (pass the same --state-dir, if any).",
		}
	case errors.Is(err, localipc.ErrInsecureSocket):
		return doctorCheck{
			Name:   checkNameDaemon,
			Status: checkFail,
			Detail: "the Mio socket has an unsafe type, owner, or mode, so it was not contacted",
			Hint: "scoprad will not replace it. Confirm that no trusted Mio uses it, " +
				"remove it, then start scoprad.",
		}
	case errors.Is(err, localipc.ErrInvalidMessage),
		errors.Is(err, localipc.ErrInvalidFrame),
		errors.Is(err, localipc.ErrFrameTooLarge):
		return doctorCheck{
			Name:   checkNameDaemon,
			Status: checkFail,
			Detail: "Scopra Mio did not return a valid status response: " + err.Error(),
			Hint:   "Use the same version of scopra and scoprad, then restart scoprad.",
		}
	default:
		return doctorCheck{
			Name:   checkNameDaemon,
			Status: checkFail,
			Detail: "cannot query Scopra Mio: " + err.Error(),
			Hint:   "Restart scoprad as the current user, then run scopra doctor again.",
		}
	}
}

func skippedCheck(name, failedPrerequisite string) doctorCheck {
	return doctorCheck{
		Name:   name,
		Status: checkSkip,
		Detail: "not run because the " + failedPrerequisite + " check failed",
	}
}

func overallResult(checks []doctorCheck) checkStatus {
	result := checkPass
	for _, check := range checks {
		switch check.Status {
		case checkFail:
			return checkFail
		case checkWarn:
			result = checkWarn
		}
	}
	return result
}

func writeDoctorText(writer io.Writer, report doctorReport) error {
	var text strings.Builder
	fmt.Fprintf(&text, "Scopra doctor (scopra %s)\n", terminalSafe(report.ScopraVersion))
	if report.StateDir != "" {
		fmt.Fprintf(&text, "State directory: %s\n", terminalSafe(report.StateDir))
	}
	for _, check := range report.Checks {
		fmt.Fprintf(&text, "[%s] %s: %s\n", check.Status, check.Name, terminalSafe(check.Detail))
		if check.Hint != "" {
			fmt.Fprintf(&text, "       hint: %s\n", terminalSafe(check.Hint))
		}
	}
	fmt.Fprintf(&text, "Result: %s\n", report.Result)
	fmt.Fprintf(&text, "Not checked (not implemented yet): %s.\n",
		strings.Join(report.NotChecked, ", "))
	text.WriteString("A passing result does not mean Scopra can authorize or execute commands.\n")
	_, err := io.WriteString(writer, text.String())
	return err
}

// terminalSafe escapes control and other non-printable characters so that
// paths and error text cannot inject terminal escape sequences or lines.
func terminalSafe(text string) string {
	var escaped strings.Builder
	for _, character := range text {
		if unicode.IsPrint(character) {
			escaped.WriteRune(character)
			continue
		}
		quoted := strconv.QuoteRune(character)
		escaped.WriteString(quoted[1 : len(quoted)-1])
	}
	return escaped.String()
}
