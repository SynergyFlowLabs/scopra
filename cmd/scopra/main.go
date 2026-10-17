package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/SynergyFlowLabs/scopra/internal/buildinfo"
	"github.com/SynergyFlowLabs/scopra/internal/localipc"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		stop()
	}()

	if code := exitCode(run(ctx, os.Args[1:], os.Stdout, os.Stderr), os.Stderr); code != 0 {
		stop()
		os.Exit(code)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if ctx == nil {
		return localipc.ErrInvalidContext
	}
	if len(args) == 0 {
		return failUsage(stderr, "command required")
	}

	switch args[0] {
	case "status":
		return runStatus(ctx, args[1:], stdout, stderr)
	case "doctor":
		return runDoctor(ctx, args[1:], stdout, stderr)
	case "workspace":
		return runWorkspace(ctx, args[1:], stdout, stderr)
	case "run":
		return runRun(ctx, args[1:], stdout, stderr)
	case "logs":
		return runLogs(ctx, args[1:], stdout, stderr)
	case "service":
		return runService(ctx, args[1:], stdout, stderr)
	case "install":
		return runInstall(ctx, args[1:], stdout, stderr)
	case "version", "--version":
		if len(args) != 1 {
			return failUsage(stderr, "version takes no arguments")
		}
		_, err := fmt.Fprintf(stdout, "scopra %s\n", buildinfo.Version())
		return err
	case "--help", "-h", "help":
		printUsage(stderr)
		return flag.ErrHelp
	default:
		return failUsage(stderr, "unknown command %q", args[0])
	}
}

func runStatus(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("scopra status", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() { printUsage(stderr) }
	stateDir := flags.String("state-dir", "", "Mio state directory")
	jsonOutput := flags.Bool("json", false, "write status as JSON")
	if err := flags.Parse(args); err != nil {
		return &usageError{err: err}
	}
	if flags.NArg() != 0 {
		return failUsage(stderr, "status takes no positional arguments")
	}

	response, err := localipc.Status(ctx, *stateDir)
	if err != nil {
		return fmt.Errorf("query Scopra Mio status: %w", err)
	}
	if *jsonOutput {
		encoder := json.NewEncoder(stdout)
		return encoder.Encode(response)
	}
	_, err = fmt.Fprintf(stdout, "Scopra Mio is %s (version %s)\n", response.Status, response.DaemonVersion)
	return err
}

// exitCode reports err on stderr unless it was already reported, and returns
// the exit status: 0 for success and help, 2 for usage errors (reported with
// the usage text), a command's own status (or 124, 130, or 137) for a run's
// outcome, 130 when interrupted, and 1 otherwise. Error text is escaped,
// because it can include paths and messages from Mio.
func exitCode(err error, stderr io.Writer) int {
	switch {
	case err == nil, errors.Is(err, flag.ErrHelp):
		return 0
	case errors.Is(err, context.Canceled):
		return 130
	}
	var statusErr *commandExitError
	if errors.As(err, &statusErr) {
		return statusErr.code
	}
	var usageErr *usageError
	if errors.As(err, &usageErr) {
		return 2
	}
	_, _ = fmt.Fprintln(stderr, "error:", terminalSafe(err.Error()))
	return 1
}

func failUsage(writer io.Writer, format string, values ...any) error {
	message := fmt.Sprintf(format, values...)
	_, _ = fmt.Fprintf(writer, "scopra: %s\n", message)
	printUsage(writer)
	return &usageError{err: errors.New(message)}
}

func printUsage(writer io.Writer) {
	_, _ = fmt.Fprint(writer, `usage: scopra <command> [flags]

commands:
  status [--state-dir PATH] [--json]               query Scopra Mio's local IPC status
  doctor [--state-dir PATH] [--json]               run read-only local diagnostics
  workspace add [--state-dir PATH] [--json] PATH   register a workspace directory with Mio
  workspace list [--state-dir PATH] [--json]       list registered workspaces
  workspace remove [--state-dir PATH] [--json] (ID | --path PATH)
                                                   remove a registration; the directory is not changed
  run [--state-dir PATH] [--json] (--workspace ID | --path PATH) [--write]
      [--workdir DIR] [--timeout DURATION] [--memory BYTES] [--cpus N]
      [--pids N] [--output-limit BYTES] [--reason TEXT] -- EXECUTABLE [ARGUMENT...]
                                                   run a command in the workspace, inside Scopra Hollow
  logs [--state-dir PATH] [--workspace ID] [--limit N] [--json]
                                                   list recorded runs from Mio's journal
  service install [--state-dir PATH] [--sandbox-image IMAGE] [--scoprad PATH] [--force]
                                                   install Scopra Mio as a systemd user service
  service uninstall                                stop and remove the systemd user service
  service status [--json]                          report the unit and its systemd state
  install [--dir PATH] [--sandbox-image IMAGE] [--scoprad PATH]
      [--no-service] [--yes]                       install Scopra interactively, or plainly with --yes
  version                                          print the scopra version

Flags go before PATH or ID.
`)
}

type usageError struct {
	err error
}

func (e *usageError) Error() string { return e.err.Error() }

func (e *usageError) Unwrap() error { return e.err }
