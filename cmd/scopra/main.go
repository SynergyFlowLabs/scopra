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
	if len(args) == 0 {
		return failUsage(stderr, "command required")
	}

	switch args[0] {
	case "status":
		return runStatus(ctx, args[1:], stdout, stderr)
	case "doctor":
		return runDoctor(ctx, args[1:], stdout, stderr)
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

// exitCode will report err and return the exit status.
// Skeleton for the Step 10 tests: not implemented yet.
func exitCode(err error, stderr io.Writer) int { return -1 }

func failUsage(writer io.Writer, format string, values ...any) error {
	message := fmt.Sprintf(format, values...)
	_, _ = fmt.Fprintf(writer, "scopra: %s\n", message)
	printUsage(writer)
	return &usageError{err: errors.New(message)}
}

func printUsage(writer io.Writer) {
	_, _ = fmt.Fprint(writer, `usage: scopra <command> [flags]

commands:
  status [--state-dir PATH] [--json]  query Scopra Mio's local IPC status
  doctor [--state-dir PATH] [--json]  run read-only local diagnostics
  version                             print the scopra version
`)
}

type usageError struct {
	err error
}

func (e *usageError) Error() string { return e.err.Error() }

func (e *usageError) Unwrap() error { return e.err }
