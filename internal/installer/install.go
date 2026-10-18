package installer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/SynergyFlowLabs/scopra/internal/hollow"
)

// Options describes an install. Paths are absolute; the caller resolves them.
type Options struct {
	// Version is what the binary reports, shown in the summary.
	Version string
	// Dir is where the executables are written.
	Dir string
	// SandboxImage is the digest-pinned image for the service, or empty.
	SandboxImage string
	// Service asks for a systemd user unit.
	Service bool
	// Scopra is the source executable; Scoprad is the daemon beside it.
	Scopra  string
	Scoprad string

	// Filesystem and process hooks, replaced in tests.
	MkdirAll func(path string, perm os.FileMode) error
	CopyFile func(source, target string) error
	Run      func(ctx context.Context, name string, args ...string) ([]byte, error)
}

// Result summarizes an install.
type Result struct {
	Dir     string
	Version string
	Service bool
	// ServiceActive is set when the service was installed and answered.
	ServiceActive bool
	// Warnings holds non-fatal notes for the summary screen.
	Warnings []string
}

// Step is one line of the install log.
type Step struct {
	Title  string
	Detail string
	Status Status
}

// serviceStatusJSON mirrors the fields the installer reads from
// `scopra service status --json`.
type serviceStatusJSON struct {
	Installed bool   `json:"installed"`
	Active    string `json:"active"`
}

// serviceTimeout bounds `scopra service install` and the status check; systemd
// can be slow on a cold boot.
const serviceTimeout = 60 * time.Second

// Plan lists the steps an install will take, so the interface can show them
// before any work starts.
func Plan(options Options) []Step {
	steps := []Step{
		{Title: "Create install directory"},
		{Title: "Install scopra"},
		{Title: "Install scoprad"},
	}
	if options.Service {
		steps = append(steps, Step{Title: "Install systemd user service"})
		steps = append(steps, Step{Title: "Verify the service"})
	}
	return steps
}

// Execute performs the install, reporting each step as it finishes. It stops
// at the first failure: a partial install is reported, never papered over.
func Execute(ctx context.Context, options Options, report func(Step)) (Result, error) {
	result := Result{Dir: options.Dir, Version: options.Version, Service: options.Service}
	if report == nil {
		report = func(Step) {}
	}
	mkdirAll := options.MkdirAll
	if mkdirAll == nil {
		mkdirAll = os.MkdirAll
	}
	copyFile := options.CopyFile
	if copyFile == nil {
		copyFile = copyExecutable
	}
	run := options.Run
	if run == nil {
		run = runCommand
	}

	// 1. The directory must exist and be writable by this user. Nothing is
	// written until both hold, so a failed install leaves no half-made tree.
	if err := mkdirAll(options.Dir, 0o755); err != nil {
		return result, fail(report, "Create install directory", err)
	}
	if err := checkWritable(options.Dir); err != nil {
		return result, fail(report, "Create install directory", err)
	}
	report(Step{Title: "Create install directory", Detail: options.Dir, Status: StatusOK})

	// 2. The Mariner binary.
	scopraPath := filepath.Join(options.Dir, "scopra")
	if sameFile(options.Scopra, scopraPath) {
		report(Step{Title: "Install scopra", Detail: scopraPath + " (already in place)", Status: StatusOK})
	} else {
		if err := copyFile(options.Scopra, scopraPath); err != nil {
			return result, fail(report, "Install scopra", err)
		}
		report(Step{Title: "Install scopra", Detail: scopraPath, Status: StatusOK})
	}

	// 3. The daemon. Without it a service would point at nothing, so a
	// requested service turns a missing daemon into a failure, not a warning.
	scopradPath := filepath.Join(options.Dir, "scoprad")
	switch {
	case options.Scoprad == "" && options.Service:
		return result, fail(report, "Install scoprad", errors.New("scoprad was not found beside the installer; pass --scoprad PATH"))
	case options.Scoprad == "":
		report(Step{Title: "Install scoprad", Detail: "not bundled with this download", Status: StatusWarn})
		result.Warnings = append(result.Warnings, "scoprad is not installed, so `scopra run` has no daemon.")
	case sameFile(options.Scoprad, scopradPath):
		report(Step{Title: "Install scoprad", Detail: scopradPath + " (already in place)", Status: StatusOK})
	default:
		if err := copyFile(options.Scoprad, scopradPath); err != nil {
			return result, fail(report, "Install scoprad", err)
		}
		report(Step{Title: "Install scoprad", Detail: scopradPath, Status: StatusOK})
	}

	if !options.Service {
		return result, nil
	}

	// 4. The unit. `service install` writes the unit, enables it, and starts
	// it; the installer passes only absolute paths and never a shell.
	args := []string{"service", "install", "--scoprad", scopradPath}
	if options.SandboxImage != "" {
		args = append(args, "--sandbox-image", options.SandboxImage)
	}
	serviceContext, cancel := context.WithTimeout(ctx, serviceTimeout)
	defer cancel()
	output, err := run(serviceContext, scopraPath, args...)
	if err != nil {
		return result, fail(report, "Install systemd user service", err)
	}
	report(Step{Title: "Install systemd user service", Detail: "scoprad.service enabled", Status: StatusOK})
	if strings.Contains(string(output), "No sandbox image was configured") {
		result.Warnings = append(result.Warnings, "No sandbox image was configured; Mio will refuse every run until you reinstall with --sandbox-image.")
	}

	// 5. Ask systemd back rather than trust the previous step.
	statusContext, statusCancel := context.WithTimeout(ctx, serviceTimeout)
	defer statusCancel()
	statusOutput, err := run(statusContext, scopraPath, "service", "status", "--json")
	if err != nil {
		return result, fail(report, "Verify the service", err)
	}
	var status serviceStatusJSON
	if err := json.Unmarshal(statusOutput, &status); err != nil {
		return result, fail(report, "Verify the service", fmt.Errorf("service status printed invalid JSON: %w", err))
	}
	if !status.Installed {
		return result, fail(report, "Verify the service", errors.New("the unit was written but systemd does not report it as installed"))
	}
	if status.Active != "active" {
		return result, fail(report, "Verify the service", fmt.Errorf("scoprad.service is %q, want active", status.Active))
	}
	result.ServiceActive = true
	report(Step{Title: "Verify the service", Detail: "scoprad.service is active", Status: StatusOK})
	return result, nil
}

// fail reports a failed step and wraps the error with the step title so the
// caller never has to guess what broke.
func fail(report func(Step), title string, err error) error {
	report(Step{Title: title, Detail: err.Error(), Status: StatusFailed})
	return fmt.Errorf("%s: %w", title, err)
}

// copyExecutable writes source to target with mode 0755 through a temporary
// file and a rename, so a reader never sees a half-written binary.
func copyExecutable(source, target string) error {
	if source == "" {
		return errors.New("no source executable")
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%q is not a regular file", source)
	}
	if info.Size() > maxBinaryBytes {
		return fmt.Errorf("%q is larger than %d bytes", source, maxBinaryBytes)
	}
	dir := filepath.Dir(target)
	temporary, err := os.CreateTemp(dir, ".scopra-install-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	cleanup := func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
	}
	written, err := io.Copy(temporary, io.LimitReader(input, maxBinaryBytes))
	if err != nil {
		cleanup()
		return err
	}
	if written != info.Size() {
		cleanup()
		return fmt.Errorf("%q changed while it was copied", source)
	}
	if err := temporary.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := temporary.Chmod(0o755); err != nil {
		cleanup()
		return err
	}
	if err := temporary.Close(); err != nil {
		_ = os.Remove(temporaryPath)
		return err
	}
	if err := os.Rename(temporaryPath, target); err != nil {
		_ = os.Remove(temporaryPath)
		return err
	}
	return nil
}

// maxBinaryBytes bounds the executables the installer will copy.
const maxBinaryBytes = 256 << 20

// checkWritable proves the directory accepts a new file, which is the property
// the install actually needs.
func checkWritable(dir string) error {
	probe, err := os.CreateTemp(dir, ".scopra-write-*")
	if err != nil {
		return fmt.Errorf("the install directory is not writable: %w", err)
	}
	name := probe.Name()
	_ = probe.Close()
	if err := os.Remove(name); err != nil {
		return fmt.Errorf("the install directory is not writable: %w", err)
	}
	return nil
}

// sameFile reports whether two paths name the same file, so an install into
// the running binary's directory does not copy a file onto itself.
func sameFile(first, second string) bool {
	if first == "" || second == "" {
		return false
	}
	firstInfo, err := os.Stat(first)
	if err != nil {
		return false
	}
	secondInfo, err := os.Stat(second)
	if err != nil {
		return false
	}
	return os.SameFile(firstInfo, secondInfo)
}

// ValidateDir checks an install directory without creating it.
func ValidateDir(dir string) error {
	if dir == "" {
		return errors.New("the install directory cannot be empty")
	}
	if !filepath.IsAbs(dir) {
		return errors.New("the install directory must be an absolute path")
	}
	cleaned := filepath.Clean(dir)
	if cleaned == "/" {
		return errors.New("refusing to install into the filesystem root")
	}
	return nil
}

// ValidateImage checks the optional sandbox image with the same rule Mio uses.
func ValidateImage(image string) error {
	if image == "" {
		return nil
	}
	return hollow.ValidateImage(image)
}

// DefaultDir returns the install directory the installer proposes:
// $XDG_BIN_HOME when it is absolute, otherwise ~/.local/bin.
func DefaultDir(lookupEnv func(string) (string, bool), homeDir func() (string, error)) (string, error) {
	if lookupEnv != nil {
		if dir, ok := lookupEnv("XDG_BIN_HOME"); ok && dir != "" && filepath.IsAbs(dir) {
			return filepath.Clean(dir), nil
		}
	}
	home, err := homeDir()
	if err != nil {
		return "", fmt.Errorf("locate the home directory: %w", err)
	}
	return filepath.Join(home, ".local", "bin"), nil
}
