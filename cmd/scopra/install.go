package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/SynergyFlowLabs/scopra/internal/buildinfo"
	"github.com/SynergyFlowLabs/scopra/internal/hollow"
	"github.com/SynergyFlowLabs/scopra/internal/installer"
)

// runInstall dispatches `scopra install`. The command is the second half of
// the bootstrap in install.sh: the script downloads and verifies the release,
// and this command moves the binaries into place and offers the service.
func runInstall(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("scopra install", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() { printUsage(stderr) }
	dir := flags.String("dir", "", "install directory (default: $XDG_BIN_HOME or ~/.local/bin)")
	image := flags.String("sandbox-image", "", "Hollow container image, pinned by digest (ADR-010)")
	scoprad := flags.String("scoprad", "", "path of the scoprad executable")
	noService := flags.Bool("no-service", false, "install only the command line, with no systemd unit")
	plain := flags.Bool("yes", false, "run without the interactive interface, using the flags as answers")
	if err := flags.Parse(args); err != nil {
		return &usageError{err: err}
	}
	if flags.NArg() != 0 {
		return failUsage(stderr, "install takes no positional arguments")
	}

	installDir, err := installer.DefaultDir(os.LookupEnv, os.UserHomeDir)
	if err != nil {
		return err
	}
	if *dir != "" {
		installDir = filepath.Clean(*dir)
	}
	if err := installer.ValidateDir(installDir); err != nil {
		return failUsage(stderr, "%v", err)
	}
	if *image != "" {
		if err := hollow.ValidateImage(*image); err != nil {
			return failUsage(stderr, "--sandbox-image %q must be %s", *image, hollow.ImageRule)
		}
	}

	source, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate the running executable: %w", err)
	}
	source, err = filepath.Abs(source)
	if err != nil {
		return fmt.Errorf("resolve the running executable: %w", err)
	}
	daemon := ""
	if *scoprad != "" {
		daemon, err = resolveScoprad(*scoprad)
		if err != nil {
			return failUsage(stderr, "%v", err)
		}
	} else if found, err := resolveScoprad(""); err == nil {
		daemon = found
	}

	options := installer.Options{
		Version:      buildinfo.Version(),
		Dir:          installDir,
		SandboxImage: *image,
		Service:      !*noService,
		Scopra:       source,
		Scoprad:      daemon,
	}

	if *plain {
		return runInstallPlain(ctx, options, stdout, stderr)
	}
	terminal, err := installer.OpenTerminal()
	if err != nil {
		// Without a terminal there is nobody to confirm anything, so the
		// installer says what it needs instead of installing silently.
		return fmt.Errorf("the interactive installer needs a terminal (%v); re-run with --yes for a scripted install", err)
	}
	model := installer.NewModel(buildinfo.Version(), installDir, *image, daemon != "")
	runner := &installer.Runner{
		Terminal: terminal,
		Model:    model,
		Base:     options,
		Env:      installer.DefaultEnvironment(),
	}
	code, runErr := runner.Run(ctx)
	if runErr != nil {
		return runErr
	}
	if code != 0 {
		return &commandExitError{code: code}
	}
	return nil
}

// runInstallPlain performs the install and writes a line per step.
func runInstallPlain(ctx context.Context, options installer.Options, stdout, stderr io.Writer) error {
	env := installer.DefaultEnvironment()
	checks := env.Checks(ctx)
	for _, check := range checks {
		if _, err := fmt.Fprintf(stdout, "%s %s", check.Status.Symbol(), check.Title); err != nil {
			return err
		}
		if check.Detail != "" {
			if _, err := fmt.Fprintf(stdout, ": %s", check.Detail); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(stdout); err != nil {
			return err
		}
		if check.Failed() && check.Fix != "" {
			if _, err := fmt.Fprintf(stdout, "  fix: %s\n", check.Fix); err != nil {
				return err
			}
		}
	}
	if !installer.Ready(checks) {
		if env.Provision != nil {
			if plan, ok := env.Provision(ctx); ok {
				_, _ = fmt.Fprintln(stdout, "\nto fix it now, run:")
				for _, line := range plan.CommandLines() {
					_, _ = fmt.Fprintf(stdout, "  %s\n", line)
				}
				if plan.NeedsRelogin {
					_, _ = fmt.Fprintln(stdout, "then log out and back in, and run the installer again")
				}
			}
		}
		return errors.New("preflight failed: " + installer.FailureReason(checks))
	}
	result, err := installer.Execute(ctx, options, func(step installer.Step) {
		_, _ = fmt.Fprintf(stdout, "%s %s\n", step.Status.Symbol(), step.Title)
		if step.Detail != "" {
			_, _ = fmt.Fprintf(stdout, "  %s\n", step.Detail)
		}
	})
	if err != nil {
		return err
	}
	_, err = fmt.Fprint(stdout, installer.Summary(result))
	return err
}
