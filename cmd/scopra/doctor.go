package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/SynergyFlowLabs/scopra/internal/buildinfo"
	"github.com/SynergyFlowLabs/scopra/internal/localipc"
	"github.com/SynergyFlowLabs/scopra/internal/workspace"
	"github.com/SynergyFlowLabs/scopra/internal/workspace/registry"
)

// doctorSchemaVersion identifies the layout of `scopra doctor --json` output.
// Incompatible changes require a new version; checks and fields may be added.
const doctorSchemaVersion = 1

const (
	checkNamePlatform          = "platform"
	checkNameStateDirectory    = "state-directory"
	checkNameWorkspaceRegistry = "workspace-registry"
	checkNameWorkspaces        = "workspaces"
	checkNameDaemon            = "daemon"
	checkNameContainment       = "containment"
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
// assess because Scopra does not implement them yet. Containment is no longer
// listed: the containment check assesses the host and the configured image,
// read-only, although nothing runs a container.
var notCheckedAreas = []string{
	"enrollment", "relay", "policy", "execution",
}

// maxStaleWorkspacesShown bounds how many stale registrations the workspaces
// check names; the rest are only counted.
const maxStaleWorkspacesShown = 5

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
	sandboxImage := flags.String("sandbox-image", "", "Hollow container image to check, pinned by digest (ADR-010)")
	jsonOutput := flags.Bool("json", false, "write the report as JSON")
	if err := flags.Parse(args); err != nil {
		return &usageError{err: err}
	}
	if flags.NArg() != 0 {
		return failUsage(stderr, "doctor takes no positional arguments")
	}

	report := diagnose(ctx, *stateDir, *sandboxImage, platformInfo{
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
func diagnose(ctx context.Context, stateDir, sandboxImage string, platform platformInfo) doctorReport {
	report := doctorReport{
		SchemaVersion: doctorSchemaVersion,
		ScopraVersion: buildinfo.Version(),
		NotChecked:    slices.Clone(notCheckedAreas),
	}

	platformCheck := diagnosePlatform(platform)
	stateDirCheck := skippedCheck(checkNameStateDirectory, checkNamePlatform)
	registryCheck := skippedCheck(checkNameWorkspaceRegistry, checkNamePlatform)
	workspacesCheck := skippedCheck(checkNameWorkspaces, checkNamePlatform)
	daemonCheck := skippedCheck(checkNameDaemon, checkNamePlatform)
	containmentCheck := skippedCheck(checkNameContainment, checkNamePlatform)
	if platformCheck.Status != checkFail {
		containmentCheck = diagnoseContainment(ctx, sandboxImage)
		report.StateDir, stateDirCheck = diagnoseStateDir(stateDir)
		registryCheck = skippedCheck(checkNameWorkspaceRegistry, checkNameStateDirectory)
		workspacesCheck = skippedCheck(checkNameWorkspaces, checkNameStateDirectory)
		daemonCheck = skippedCheck(checkNameDaemon, checkNameStateDirectory)
		if stateDirCheck.Status != checkFail {
			var records []registry.Record
			registryCheck, records = diagnoseWorkspaceRegistry(report.StateDir)
			workspacesCheck = skippedCheck(checkNameWorkspaces, checkNameWorkspaceRegistry)
			blockedBy := ""
			if registryCheck.Status == checkFail {
				blockedBy = checkNameWorkspaceRegistry // scoprad refuses to start with a bad registry
			} else {
				workspacesCheck = diagnoseWorkspaces(report.StateDir, records)
			}
			daemonCheck = diagnoseDaemon(ctx, report.StateDir, blockedBy)
		}
	}

	report.Checks = []doctorCheck{platformCheck, stateDirCheck, registryCheck, workspacesCheck, daemonCheck, containmentCheck}
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
			Hint:   "Set XDG_STATE_HOME or HOME to an absolute path, or pass the same --state-dir to scopra and scoprad.",
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

// diagnoseWorkspaceRegistry validates Mio's workspace registry without
// changing it, and returns the registrations so that diagnoseWorkspaces
// revalidates what this check read. A failing check returns none.
func diagnoseWorkspaceRegistry(stateDir string) (doctorCheck, []registry.Record) {
	records, err := registry.Check(stateDir)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return doctorCheck{
			Name:   checkNameWorkspaceRegistry,
			Status: checkPass,
			Detail: "no workspaces are registered; the state directory does not exist yet",
		}, nil
	case err == nil && len(records) == 0:
		return doctorCheck{Name: checkNameWorkspaceRegistry, Status: checkPass, Detail: "no workspaces are registered"}, nil
	case err == nil && len(records) == 1:
		return doctorCheck{Name: checkNameWorkspaceRegistry, Status: checkPass, Detail: "1 workspace is registered"}, records
	case err == nil:
		return doctorCheck{
			Name:   checkNameWorkspaceRegistry,
			Status: checkPass,
			Detail: fmt.Sprintf("%d workspaces are registered", len(records)),
		}, records
	default:
		return registryFailure(filepath.Join(stateDir, registry.FileName), err), nil
	}
}

// registryFailure describes a registry that scoprad would refuse to load.
func registryFailure(file string, err error) doctorCheck {
	const removal = " Removing the file unregisters every workspace but changes no directory."
	switch {
	case errors.Is(err, registry.ErrInsecureRegistry):
		return doctorCheck{
			Name:   checkNameWorkspaceRegistry,
			Status: checkFail,
			Detail: err.Error(),
			Hint:   "scoprad will not start until " + file + " is a regular file owned by you with mode 0600." + removal,
		}
	case errors.Is(err, registry.ErrCorruptRegistry):
		return doctorCheck{
			Name:   checkNameWorkspaceRegistry,
			Status: checkFail,
			Detail: err.Error(),
			Hint:   "scoprad will not start until " + file + " is valid; restore it from a backup if you have one." + removal,
		}
	default:
		return doctorCheck{
			Name:   checkNameWorkspaceRegistry,
			Status: checkFail,
			Detail: "cannot read the workspace registry: " + err.Error(),
			Hint:   "scoprad will not start until " + file + " can be read; check its permissions." + removal,
		}
	}
}

// diagnoseWorkspaces revalidates each registration under ADR-004 with the
// validator configuration that Mio uses, so a workspace that was deleted,
// replaced, or moved is reported before anything tries to use it. Like
// Revalidate it only inspects metadata: no registry or workspace changes.
func diagnoseWorkspaces(stateDir string, records []registry.Record) doctorCheck {
	if len(records) == 0 {
		return doctorCheck{Name: checkNameWorkspaces, Status: checkPass, Detail: "no workspaces are registered"}
	}
	validator, err := workspace.NewValidator(stateDir)
	if err != nil {
		return doctorCheck{
			Name:   checkNameWorkspaces,
			Status: checkFail,
			Detail: "cannot prepare workspace validation: " + err.Error(),
			Hint:   "scoprad prepares the same validation at startup and will not start either; set HOME to your absolute home directory.",
		}
	}
	var stale []string
	for _, record := range records {
		if err := validator.Revalidate(record.Workspace); err != nil {
			stale = append(stale, record.ID+" "+record.Path+" ("+staleReason(err)+")")
		}
	}
	switch {
	case len(stale) == 0 && len(records) == 1:
		return doctorCheck{Name: checkNameWorkspaces, Status: checkPass, Detail: "the registered workspace still qualifies"}
	case len(stale) == 0:
		return doctorCheck{
			Name:   checkNameWorkspaces,
			Status: checkPass,
			Detail: fmt.Sprintf("all %d registered workspaces still qualify", len(records)),
		}
	}
	verb := "qualify"
	if len(stale) == 1 {
		verb = "qualifies"
	}
	listed := stale[:min(len(stale), maxStaleWorkspacesShown)]
	detail := fmt.Sprintf("%d of %d registered workspaces no longer %s: %s",
		len(stale), len(records), verb, strings.Join(listed, "; "))
	if more := len(stale) - len(listed); more > 0 {
		detail += fmt.Sprintf("; and %d more", more)
	}
	return doctorCheck{
		Name:   checkNameWorkspaces,
		Status: checkWarn,
		Detail: detail,
		Hint: "Scopra revalidates a workspace before using it, so these would be refused. Restore each directory, " +
			"or run `scopra workspace remove ID`, which does not change the directory.",
	}
}

// staleReason summarizes why a registration no longer qualifies.
func staleReason(err error) string {
	switch {
	case errors.Is(err, os.ErrNotExist):
		return "missing"
	case errors.Is(err, workspace.ErrChanged):
		return "changed since it was registered"
	case errors.Is(err, workspace.ErrNotDirectory):
		return "not a directory"
	case errors.Is(err, workspace.ErrNotOwned):
		return "not owned by you"
	case errors.Is(err, workspace.ErrProtectedPath):
		return "is or contains a protected location"
	default:
		return err.Error()
	}
}

// diagnoseDaemon sends Mio the existing authenticated, read-only status
// request. It never starts Mio or removes a stale socket. blockedBy names an
// earlier failed check that would stop scoprad from starting, or is empty; if
// Mio is not running, the hint then points there before suggesting scoprad.
func diagnoseDaemon(ctx context.Context, stateDir, blockedBy string) doctorCheck {
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
		hint := "Start Mio in the foreground with `scoprad` (pass the same --state-dir, if any)."
		if blockedBy != "" {
			hint = "scoprad will not start until the problem reported by the " + blockedBy +
				" check is fixed; then start Mio in the foreground with `scoprad` (pass the same --state-dir, if any)."
		}
		return doctorCheck{
			Name:   checkNameDaemon,
			Status: checkFail,
			Detail: "Scopra Mio is not running (its socket is missing or refuses connections)",
			Hint:   hint,
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
	text.WriteString("A passing result does not mean Scopra has authorized anything or run a command.\n")
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
