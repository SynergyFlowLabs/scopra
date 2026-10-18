package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/SynergyFlowLabs/scopra/internal/buildinfo"
	"github.com/SynergyFlowLabs/scopra/internal/hollow"
	"github.com/SynergyFlowLabs/scopra/internal/identity"
	"github.com/SynergyFlowLabs/scopra/internal/journal"
	"github.com/SynergyFlowLabs/scopra/internal/localipc"
	"github.com/SynergyFlowLabs/scopra/internal/presence"
	"github.com/SynergyFlowLabs/scopra/internal/workspace"
	"github.com/SynergyFlowLabs/scopra/internal/workspace/registry"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		stop()
	}()

	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		switch {
		case errors.Is(err, flag.ErrHelp):
			return
		case errors.Is(err, context.Canceled):
			os.Exit(130)
		default:
			var usageErr *usageError
			if errors.As(err, &usageErr) {
				os.Exit(2)
			}
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if ctx == nil {
		return localipc.ErrInvalidContext
	}
	config, err := parseFlags(args, stderr)
	if err != nil {
		return err
	}
	if config.showVersion {
		_, err := fmt.Fprintf(stdout, "scoprad %s\n", buildinfo.Version())
		return err
	}
	if err := ctx.Err(); err != nil {
		return nil
	}

	daemon, err := localipc.Listen(config.stateDir, buildinfo.Version())
	if err != nil {
		return fmt.Errorf("start Scopra Mio: %w", err)
	}
	// The daemon now holds the state-directory lock, which makes Mio the
	// registry's only writer (ADR-005). Mio refuses to serve if the registry
	// is unusable, and serves workspace commands from it otherwise (ADR-006).
	workspaces, validator, err := openWorkspaceRegistry(daemon.StateDir())
	if err == nil {
		err = daemon.EnableWorkspaces(workspaces)
	}
	if err != nil {
		return errors.Join(fmt.Errorf("start Scopra Mio: %w", err), daemon.Close())
	}
	// The run journal is opened before serving: Mio never runs anything it
	// cannot record (ADR-013). A journal it cannot open or validate prevents
	// it from starting, like an unusable registry.
	runJournal, err := journal.Open(daemon.StateDir())
	if err != nil {
		return errors.Join(fmt.Errorf("start Scopra Mio: %w", err), daemon.Close())
	}
	defer func() { _ = runJournal.Close() }()
	// Local execution is always enabled (ADR-011): when containment cannot be
	// offered, every run is answered with a typed refusal rather than silence.
	executor, warning := newExecutor(workspaces, validator, config.sandboxImage)
	if err := daemon.EnableExecutions(executor, runJournal); err != nil {
		return errors.Join(fmt.Errorf("start Scopra Mio: %w", err), daemon.Close())
	}
	if _, err := fmt.Fprintln(stderr, "scoprad: listening"); err != nil {
		_ = daemon.Close()
		return fmt.Errorf("report daemon startup: %w", err)
	}
	if warning != "" {
		if _, err := fmt.Fprintln(stderr, "scoprad: warning: "+warning); err != nil {
			_ = daemon.Close()
			return fmt.Errorf("report daemon startup: %w", err)
		}
	}
	startPresence(ctx, daemon.StateDir(), stderr)
	return daemon.Serve(ctx)
}

// daemonConfig is what scoprad's command line sets.
type daemonConfig struct {
	stateDir string
	// sandboxImage is the Hollow container image, pinned by digest, for the
	// runner (ADR-010 increment 3a-2). Empty means none is configured, so
	// every execution will be rejected with containment_unavailable.
	sandboxImage string
	showVersion  bool
}

// parseFlags parses and validates scoprad's command line. Every problem is a
// usage error found before Mio touches its state directory; a malformed
// --sandbox-image is one even together with --version.
func parseFlags(args []string, stderr io.Writer) (daemonConfig, error) {
	flags := flag.NewFlagSet("scoprad", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() { printUsage(stderr) }
	var config daemonConfig
	flags.StringVar(&config.stateDir, "state-dir", "", "Mio state directory")
	flags.StringVar(&config.sandboxImage, "sandbox-image", "", "Hollow container image, pinned by digest (ADR-010)")
	flags.BoolVar(&config.showVersion, "version", false, "print daemon version")
	if err := flags.Parse(args); err != nil {
		return daemonConfig{}, &usageError{err: err}
	}
	imageSet := false
	flags.Visit(func(f *flag.Flag) { imageSet = imageSet || f.Name == "sandbox-image" })
	if imageSet && hollow.ValidateImage(config.sandboxImage) != nil {
		return daemonConfig{}, failUsage(stderr, "--sandbox-image %q must be %s", config.sandboxImage, hollow.ImageRule)
	}
	if config.showVersion && flags.NArg() != 0 {
		return daemonConfig{}, failUsage(stderr, "--version takes no positional arguments")
	}
	if flags.NArg() != 0 {
		return daemonConfig{}, failUsage(stderr, "scoprad runs in the foreground and takes no positional arguments")
	}
	return config, nil
}

// newWorkspaceValidator returns the ADR-004 workspace validator that Mio uses.
// Besides the filesystem root and the user's home directories, it protects
// Mio's state directory, so that no workspace can contain Mio's socket, lock,
// or registry.
func newWorkspaceValidator(stateDir string) (*workspace.Validator, error) {
	return workspace.NewValidator(stateDir)
}

// openWorkspaceRegistry loads the workspace registry from Mio's state
// directory, which the caller must hold locked, and returns the validator
// that execution reuses. Recorded workspaces are revalidated before use, not
// here, so a missing workspace does not stop Mio.
func openWorkspaceRegistry(stateDir string) (*registry.Registry, *workspace.Validator, error) {
	validator, err := newWorkspaceValidator(stateDir)
	if err != nil {
		return nil, nil, fmt.Errorf("prepare workspace validation: %w", err)
	}
	workspaces, err := registry.Open(stateDir, validator)
	if err != nil {
		return nil, nil, fmt.Errorf("open workspace registry: %w", err)
	}
	return workspaces, validator, nil
}

func failUsage(writer io.Writer, format string, values ...any) error {
	message := fmt.Sprintf(format, values...)
	_, _ = fmt.Fprintf(writer, "scoprad: %s\n", message)
	printUsage(writer)
	return &usageError{err: errors.New(message)}
}

func printUsage(writer io.Writer) {
	_, _ = fmt.Fprintln(writer, "usage: scoprad [--state-dir PATH] [--sandbox-image NAME@sha256:DIGEST] [--version]")
}

type usageError struct {
	err error
}

func (e *usageError) Error() string { return e.err.Error() }

func (e *usageError) Unwrap() error { return e.err }
