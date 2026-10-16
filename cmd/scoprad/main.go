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
	if _, err := fmt.Fprintln(stderr, "scoprad: listening"); err != nil {
		_ = daemon.Close()
		return fmt.Errorf("report daemon startup: %w", err)
	}
	return daemon.Serve(ctx)
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
