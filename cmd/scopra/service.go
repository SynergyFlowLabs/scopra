package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/SynergyFlowLabs/scopra/internal/hollow"
	"github.com/SynergyFlowLabs/scopra/internal/localipc"
)

const (
	// serviceUnitName is the systemd unit that runs Mio.
	serviceUnitName = "scoprad.service"
	// serviceMarker is the first line of every unit Scopra writes. It is how
	// install and uninstall recognize their own file.
	serviceMarker = "# Managed by scopra: `scopra service install` rewrites this file."
	// serviceHelperTimeout bounds each systemctl call.
	serviceHelperTimeout = 30 * time.Second
	// serviceSchemaVersion identifies the layout of `service status --json`.
	serviceSchemaVersion = 1
)

// errServiceUnsupported reports a platform without systemd user services.
var errServiceUnsupported = errors.New("systemd user services are supported on Linux only")

// serviceStatusJSON is the documented `scopra service status --json` shape.
type serviceStatusJSON struct {
	SchemaVersion int    `json:"schemaVersion"`
	UnitPath      string `json:"unitPath"`
	Installed     bool   `json:"installed"`
	Managed       bool   `json:"managed,omitempty"`
	Enabled       string `json:"enabled,omitempty"`
	Active        string `json:"active,omitempty"`
}

// runService dispatches `scopra service` (ADR-012).
func runService(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return failUsage(stderr, "service needs install, uninstall, or status")
	}
	switch args[0] {
	case "install":
		return runServiceInstall(ctx, args[1:], stdout, stderr)
	case "uninstall":
		return runServiceUninstall(ctx, args[1:], stdout, stderr)
	case "status":
		return runServiceStatus(ctx, args[1:], stdout, stderr)
	default:
		return failUsage(stderr, "unknown service command %q; use install, uninstall, or status", args[0])
	}
}

// runServiceInstall writes the unit and enables it. Every check that does not
// need the filesystem runs first, and a failed systemctl call restores the
// previous unit, so an install never leaves a half-applied change.
func runServiceInstall(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("scopra service install", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() { printUsage(stderr) }
	stateDir := flags.String("state-dir", "", "Mio state directory")
	image := flags.String("sandbox-image", "", "Hollow container image, pinned by digest (ADR-010)")
	scoprad := flags.String("scoprad", "", "path of the scoprad executable")
	force := flags.Bool("force", false, "replace a unit that Scopra did not write")
	if err := flags.Parse(args); err != nil {
		return &usageError{err: err}
	}
	if flags.NArg() != 0 {
		return failUsage(stderr, "service install takes no positional arguments")
	}
	if !servicePlatformSupported() {
		return errServiceUnsupported
	}
	if *image != "" {
		if err := hollow.ValidateImage(*image); err != nil {
			return failUsage(stderr, "--sandbox-image %q must be %s", *image, hollow.ImageRule)
		}
	}
	// Mio resolves its state directory the same way; the unit pins the result
	// so that systemd's environment cannot change it.
	resolvedStateDir, err := localipc.ResolveStateDir(*stateDir)
	if err != nil {
		return failUsage(stderr, "invalid state directory: %v", err)
	}
	systemctl, err := resolveSystemctl()
	if err != nil {
		return err
	}
	binary, err := resolveScoprad(*scoprad)
	if err != nil {
		return err
	}
	unitPath, err := serviceUnitPath()
	if err != nil {
		return err
	}
	unit, err := serviceUnitContent(binary, resolvedStateDir, *image)
	if err != nil {
		return err
	}
	if err := applyServiceUnit(ctx, unitPath, unit, systemctl, *force); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Installed %s.\nScopra Mio is enabled and running.\n", unitPath)
	if *image == "" {
		fmt.Fprintln(stdout, "No sandbox image was configured; Mio will refuse every run until you reinstall with --sandbox-image.")
	}
	return nil
}

// resolveSystemctl finds systemctl before anything is written, so that a host
// without systemd is refused up front.
func resolveSystemctl() (string, error) {
	path, err := exec.LookPath("systemctl")
	if err != nil {
		return "", fmt.Errorf("the systemctl command was not found; install systemd or add it to PATH: %w", err)
	}
	return path, nil
}

// resolveScoprad finds the daemon to run: an explicit path, then PATH, then
// the directory of the scopra executable.
func resolveScoprad(explicit string) (string, error) {
	if explicit != "" {
		path, err := filepath.Abs(explicit)
		if err != nil {
			return "", fmt.Errorf("resolve --scoprad: %w", err)
		}
		return checkScopradExecutable(path)
	}
	if path, err := exec.LookPath("scoprad"); err == nil {
		return checkScopradExecutable(path)
	}
	if executable, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(executable), "scoprad")
		if checked, err := checkScopradExecutable(candidate); err == nil {
			return checked, nil
		}
	}
	return "", errors.New("the scoprad executable was not found; pass --scoprad PATH")
}

// checkScopradExecutable requires an absolute path to an executable regular
// file, so a unit can never point at a directory or a non-program.
func checkScopradExecutable(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("find the scoprad executable: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("scoprad path %q is not a regular file", path)
	}
	if info.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("scoprad path %q is not executable", path)
	}
	return path, nil
}

// serviceUnitPath returns the systemd user unit path.
func serviceUnitPath() (string, error) {
	configHome, err := serviceConfigHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(configHome, "systemd", "user", serviceUnitName), nil
}

// serviceConfigHome follows the XDG Base Directory specification: an absolute
// $XDG_CONFIG_HOME, or $HOME/.config. A relative value fails closed.
func serviceConfigHome() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		if !filepath.IsAbs(dir) {
			return "", errors.New("$XDG_CONFIG_HOME must be an absolute path")
		}
		return filepath.Clean(dir), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate the user configuration directory: %w", err)
	}
	if !filepath.IsAbs(home) {
		return "", errors.New("the home directory must be an absolute path")
	}
	return filepath.Join(home, ".config"), nil
}

// serviceUnitContent builds the unit that runs scoprad (ADR-012). Every
// argument is escaped as systemd reads it.
func serviceUnitContent(scopradPath, stateDir, image string) (string, error) {
	binary, err := systemdExecArg(scopradPath)
	if err != nil {
		return "", err
	}
	stateArgument, err := systemdExecArg("--state-dir=" + stateDir)
	if err != nil {
		return "", err
	}
	execArgs := []string{binary, stateArgument}
	if image != "" {
		imageArgument, err := systemdExecArg("--sandbox-image=" + image)
		if err != nil {
			return "", err
		}
		execArgs = append(execArgs, imageArgument)
	}
	lines := []string{
		serviceMarker,
		"[Unit]",
		"Description=Scopra Mio local execution daemon",
		"Documentation=https://github.com/SynergyFlowLabs/scopra",
		"StartLimitIntervalSec=60",
		"StartLimitBurst=3",
		"",
		"[Service]",
		"Type=simple",
		"ExecStart=" + strings.Join(execArgs, " "),
		"Restart=on-failure",
		"RestartSec=2",
		"UMask=0077",
		"",
		"[Install]",
		"WantedBy=default.target",
	}
	return strings.Join(lines, "\n") + "\n", nil
}

// systemdExecArg escapes one ExecStart argument: percent and dollar signs are
// specifiers for systemd, so they are doubled first; anything systemd's
// word-splitting would treat specially is then double-quoted with backslash
// escapes. Control characters are refused.
func systemdExecArg(value string) (string, error) {
	if value == "" {
		return "", errors.New("a unit argument cannot be empty")
	}
	for _, char := range value {
		if unicode.IsControl(char) {
			return "", errors.New("a unit argument cannot contain control characters")
		}
	}
	escaped := strings.ReplaceAll(value, "%", "%%")
	escaped = strings.ReplaceAll(escaped, "$", "$$")
	if !strings.ContainsAny(escaped, " \t\"'\\") {
		return escaped, nil
	}
	var quoted strings.Builder
	quoted.WriteByte('"')
	for _, char := range escaped {
		if char == '\\' || char == '"' {
			quoted.WriteByte('\\')
		}
		quoted.WriteRune(char)
	}
	quoted.WriteByte('"')
	return quoted.String(), nil
}

// applyServiceUnit checks the directory, writes the unit atomically, and
// applies it with systemd. On a systemctl failure the previous file content
// (or its absence) is restored before the error is returned.
func applyServiceUnit(ctx context.Context, unitPath, content, systemctl string, force bool) error {
	dir := filepath.Dir(unitPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create the systemd user directory: %w", err)
	}
	if err := checkServiceDir(dir); err != nil {
		return err
	}
	previous, existed, err := readServiceUnit(unitPath)
	if err != nil {
		return err
	}
	if existed && !force && !isManagedServiceUnit(previous) {
		return fmt.Errorf("%s was not written by Scopra; remove it yourself or pass --force", unitPath)
	}
	if err := writeServiceUnit(unitPath, content); err != nil {
		return err
	}
	if err := runSystemctl(ctx, systemctl, "daemon-reload"); err != nil {
		restoreServiceUnit(unitPath, previous, existed)
		_ = runSystemctl(ctx, systemctl, "daemon-reload")
		return fmt.Errorf("reload systemd after writing %s: %w", unitPath, err)
	}
	if err := runSystemctl(ctx, systemctl, "enable", "--now", serviceUnitName); err != nil {
		restoreServiceUnit(unitPath, previous, existed)
		_ = runSystemctl(ctx, systemctl, "daemon-reload")
		return fmt.Errorf("enable and start %s: %w", serviceUnitName, err)
	}
	return nil
}

// readServiceUnit reads an existing unit. Anything at that path that is not a
// regular file, including a symlink, is refused.
func readServiceUnit(path string) (string, bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("inspect %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return "", false, fmt.Errorf("%s is not a regular file", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false, fmt.Errorf("read %s: %w", path, err)
	}
	return string(data), true, nil
}

// writeServiceUnit replaces path atomically with an owner-only file.
func writeServiceUnit(path, content string) error {
	dir := filepath.Dir(path)
	temp, err := os.CreateTemp(dir, ".scoprad.service-")
	if err != nil {
		return fmt.Errorf("create a temporary unit: %w", err)
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return fmt.Errorf("secure the temporary unit: %w", err)
	}
	if _, err := temp.WriteString(content); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write the temporary unit: %w", err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return fmt.Errorf("sync the temporary unit: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close the temporary unit: %w", err)
	}
	if err := os.Rename(tempName, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}

// restoreServiceUnit puts the previous unit back, or removes the new one.
func restoreServiceUnit(path, previous string, existed bool) {
	if existed {
		_ = writeServiceUnit(path, previous)
		return
	}
	_ = os.Remove(path)
}

// isManagedServiceUnit reports whether Scopra wrote content.
func isManagedServiceUnit(content string) bool {
	return strings.HasPrefix(content, serviceMarker+"\n")
}

// checkServiceDir requires dir and its ancestors to be real directories that
// other users cannot write to, with the final directory owned by this user.
// A root-owned sticky directory, such as /tmp, is safe.
func checkServiceDir(dir string) error {
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("resolve the systemd user directory: %w", err)
	}
	root := filepath.VolumeName(absolute) + string(filepath.Separator)
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return fmt.Errorf("inspect %s: %w", root, err)
	}
	if !safeServiceAncestor(rootInfo) {
		return fmt.Errorf("%s is not a real directory or is writable by other users", root)
	}
	current := root
	for _, part := range strings.Split(strings.TrimPrefix(absolute, root), string(filepath.Separator)) {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return fmt.Errorf("inspect %s: %w", current, err)
		}
		if !safeServiceAncestor(info) {
			return fmt.Errorf("%s is not a real directory or is writable by other users", current)
		}
		if current == absolute && !ownedServiceDir(info) {
			return fmt.Errorf("%s is not owned by the current user", current)
		}
	}
	return nil
}

// runSystemctl runs one fixed systemctl --user argument vector.
func runSystemctl(ctx context.Context, systemctl string, args ...string) error {
	_, err := runSystemctlOutput(ctx, systemctl, args...)
	return err
}

// runSystemctlOutput runs systemctl and returns its standard output, folding
// systemctl's own standard error into the error.
func runSystemctlOutput(ctx context.Context, systemctl string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, serviceHelperTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, systemctl, append([]string{"--user"}, args...)...)
	command.WaitDelay = time.Second
	output, err := command.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			if message := strings.TrimSpace(string(exitErr.Stderr)); message != "" {
				return output, fmt.Errorf("%w: %s", err, terminalSafe(message))
			}
		}
		return output, err
	}
	return output, nil
}

// systemctlQuery asks systemd a question whose non-zero exit is an answer,
// such as `is-enabled` or `is-active`, and returns the first line of output.
func systemctlQuery(ctx context.Context, systemctl, verb, fallback string) (string, error) {
	output, err := runSystemctlOutput(ctx, systemctl, verb, serviceUnitName)
	text := strings.TrimSpace(string(output))
	if first, _, found := strings.Cut(text, "\n"); found {
		text = first
	}
	if err != nil {
		var exitErr *exec.ExitError
		// systemctl answers with a non-zero status when the unit is disabled
		// or inactive; only a failure without an answer is an error.
		if !errors.As(err, &exitErr) || text == "" {
			return "", err
		}
	}
	if text == "" {
		text = fallback
	}
	return text, nil
}

// runServiceUninstall stops and disables Mio, removes the unit Scopra wrote,
// and reloads systemd. A unit Scopra did not write is never touched.
func runServiceUninstall(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("scopra service uninstall", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() { printUsage(stderr) }
	if err := flags.Parse(args); err != nil {
		return &usageError{err: err}
	}
	if flags.NArg() != 0 {
		return failUsage(stderr, "service uninstall takes no arguments")
	}
	if !servicePlatformSupported() {
		return errServiceUnsupported
	}
	unitPath, err := serviceUnitPath()
	if err != nil {
		return err
	}
	content, existed, err := readServiceUnit(unitPath)
	if err != nil {
		return err
	}
	if !existed {
		fmt.Fprintln(stdout, "Scopra Mio is not installed.")
		return nil
	}
	if !isManagedServiceUnit(content) {
		return fmt.Errorf("%s was not written by Scopra; remove it yourself if it is safe to do so", unitPath)
	}
	systemctl, err := resolveSystemctl()
	if err != nil {
		return err
	}
	if err := runSystemctl(ctx, systemctl, "disable", "--now", serviceUnitName); err != nil {
		return fmt.Errorf("stop and disable %s: %w", serviceUnitName, err)
	}
	if err := os.Remove(unitPath); err != nil {
		return fmt.Errorf("remove %s: %w", unitPath, err)
	}
	if err := runSystemctl(ctx, systemctl, "daemon-reload"); err != nil {
		return fmt.Errorf("reload systemd after removing %s: %w", unitPath, err)
	}
	fmt.Fprintf(stdout, "Removed %s; Scopra Mio is stopped and disabled.\n", unitPath)
	return nil
}

// runServiceStatus reports the unit and systemd's view of it, changing
// nothing. The only systemctl calls are the two read-only queries.
func runServiceStatus(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("scopra service status", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() { printUsage(stderr) }
	jsonOutput := flags.Bool("json", false, "write the status as JSON")
	if err := flags.Parse(args); err != nil {
		return &usageError{err: err}
	}
	if flags.NArg() != 0 {
		return failUsage(stderr, "service status takes no arguments")
	}
	if !servicePlatformSupported() {
		return errServiceUnsupported
	}
	unitPath, err := serviceUnitPath()
	if err != nil {
		return err
	}
	content, installed, err := readServiceUnit(unitPath)
	if err != nil {
		return err
	}
	report := serviceStatusJSON{SchemaVersion: serviceSchemaVersion, UnitPath: unitPath, Installed: installed}
	if installed {
		report.Managed = isManagedServiceUnit(content)
		systemctl, err := resolveSystemctl()
		if err != nil {
			return err
		}
		if report.Enabled, err = systemctlQuery(ctx, systemctl, "is-enabled", "disabled"); err != nil {
			return fmt.Errorf("query the unit's enabled state: %w", err)
		}
		if report.Active, err = systemctlQuery(ctx, systemctl, "is-active", "unknown"); err != nil {
			return fmt.Errorf("query the unit's active state: %w", err)
		}
	}
	if *jsonOutput {
		if err := json.NewEncoder(stdout).Encode(report); err != nil {
			return fmt.Errorf("write the service status: %w", err)
		}
		return nil
	}
	if !installed {
		fmt.Fprintf(stdout, "Scopra Mio service: not installed\nUnit: %s\n", unitPath)
		return nil
	}
	kind := "not managed by scopra"
	if report.Managed {
		kind = "managed by scopra"
	}
	fmt.Fprintf(stdout, "Scopra Mio service: installed (%s)\nUnit: %s\nEnabled: %s\nActive: %s\n",
		kind, unitPath, report.Enabled, report.Active)
	return nil
}
