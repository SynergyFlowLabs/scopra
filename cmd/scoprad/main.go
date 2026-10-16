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
	"github.com/SynergyFlowLabs/scopra/internal/localipc"
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
	flags := flag.NewFlagSet("scoprad", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() { printUsage(stderr) }
	stateDir := flags.String("state-dir", "", "Mio state directory")
	showVersion := flags.Bool("version", false, "print daemon version")
	if err := flags.Parse(args); err != nil {
		return &usageError{err: err}
	}
	if *showVersion {
		if flags.NArg() != 0 {
			return failUsage(stderr, "--version takes no positional arguments")
		}
		_, err := fmt.Fprintf(stdout, "scoprad %s\n", buildinfo.Version())
		return err
	}
	if flags.NArg() != 0 {
		return failUsage(stderr, "scoprad runs in the foreground and takes no positional arguments")
	}
	if err := ctx.Err(); err != nil {
		return nil
	}

	daemon, err := localipc.Listen(*stateDir, buildinfo.Version())
	if err != nil {
		return fmt.Errorf("start Scopra Mio: %w", err)
	}
	// The daemon now holds the state-directory lock, which makes Mio the
	// registry's only writer (ADR-005). Mio refuses to serve if the registry
	// is unusable, and serves workspace commands from it otherwise (ADR-006).
	workspaces, err := openWorkspaceRegistry(daemon.StateDir())
	if err == nil {
		err = daemon.EnableWorkspaces(workspaces)
	}
	if err != nil {
		return errors.Join(fmt.Errorf("start Scopra Mio: %w", err), daemon.Close())
	}
	if _, err := fmt.Fprintln(stderr, "scoprad: listening"); err != nil {
		_ = daemon.Close()
		return fmt.Errorf("report daemon startup: %w", err)
	}
	return daemon.Serve(ctx)
}

// newWorkspaceValidator returns the ADR-004 workspace validator that Mio uses.
// Besides the filesystem root and the user's home directories, it protects
// Mio's state directory, so that no workspace can contain Mio's socket, lock,
// or registry.
func newWorkspaceValidator(stateDir string) (*workspace.Validator, error) {
	return workspace.NewValidator(stateDir)
}

// openWorkspaceRegistry loads the workspace registry from Mio's state
// directory, which the caller must hold locked. Recorded workspaces are
// revalidated before use, not here, so a missing workspace does not stop Mio.
func openWorkspaceRegistry(stateDir string) (*registry.Registry, error) {
	validator, err := newWorkspaceValidator(stateDir)
	if err != nil {
		return nil, fmt.Errorf("prepare workspace validation: %w", err)
	}
	workspaces, err := registry.Open(stateDir, validator)
	if err != nil {
		return nil, fmt.Errorf("open workspace registry: %w", err)
	}
	return workspaces, nil
}

func failUsage(writer io.Writer, format string, values ...any) error {
	message := fmt.Sprintf(format, values...)
	_, _ = fmt.Fprintf(writer, "scoprad: %s\n", message)
	printUsage(writer)
	return &usageError{err: errors.New(message)}
}

func printUsage(writer io.Writer) {
	_, _ = fmt.Fprintln(writer, "usage: scoprad [--state-dir PATH] [--version]")
}

type usageError struct {
	err error
}

func (e *usageError) Error() string { return e.err.Error() }

func (e *usageError) Unwrap() error { return e.err }
