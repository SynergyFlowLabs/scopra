package main

import (
	"context"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"slices"
	"strconv"
	"time"

	"github.com/SynergyFlowLabs/scopra/internal/localipc"
	"github.com/SynergyFlowLabs/scopra/packages/protocol"
)

// `scopra run` defaults and bounds (ADR-011). The limits are the command's
// defaults, not policy: Mio's own maximum and the containment still apply.
const (
	defaultRunTimeout     = 10 * time.Minute
	defaultRunMemoryBytes = 2 << 30
	defaultRunCPUs        = 2
	defaultRunPIDs        = 512
	defaultRunOutputBytes = 16 << 20
	defaultRunWorkDir     = "."
	defaultRunReason      = "local run by the workspace owner"
	// maxRunCPUs keeps the converted millicores within ADR-007's bound.
	maxRunCPUs    = 1000
	maxRunTimeout = 24 * time.Hour
	// placeholderWorkspaceID stands in for --path while the command, limits,
	// and reason are checked before Mio resolves the path. It has the
	// registry's format, so ADR-007 accepts the request it appears in.
	placeholderWorkspaceID = "00000000000000000000000000000000"
)

// errNoRunSeparator reports a command line without the "--" that separates
// `scopra run` options from the command.
var errNoRunSeparator = errors.New("run needs -- before the command")

// commandExitError carries a command's exit status to exitCode, which reports
// it without printing an error line. A command's own failure is not a Scopra
// error.
type commandExitError struct {
	code int
}

func (e *commandExitError) Error() string { return "exit status " + strconv.Itoa(e.code) }

// runOptions is a parsed `scopra run` command line. When pathSet is true, run
// holds the placeholder workspace ID until Mio resolves the path.
type runOptions struct {
	stateDir   string
	jsonOutput bool
	path       string
	pathSet    bool
	run        localipc.RunRequest
}

// runRun runs one command in Scopra Hollow through Mio (ADR-011).
func runRun(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	options, err := parseRunOptions(args, stderr)
	if err != nil {
		return err
	}
	if options.pathSet {
		path, err := absoluteWorkspacePath(options.path, stderr)
		if err != nil {
			return err
		}
		id, err := workspaceIDForPath(ctx, options.stateDir, path)
		if err != nil {
			return fmt.Errorf("run command: %w", err)
		}
		options.run.WorkspaceID = id
		if err := localipc.ValidateRun(options.run); err != nil {
			return failUsage(stderr, "invalid run: %v", err)
		}
	}
	return executeRun(ctx, options, stdout, stderr)
}

// executeRun sends the run to Mio and reports its output and outcome. stdout
// bytes go to stdout and stderr bytes to stderr unchanged, as for a native
// run; with --json, stdout carries exactly one JSON object per line instead,
// in arrival order: one "started", any number of "output", and one "result".
// Each line is that message's canonical payload plus a "type" field.
func executeRun(ctx context.Context, options runOptions, stdout, stderr io.Writer) error {
	jsonOutput := options.jsonOutput
	executionID := ""
	writeLine := func(kind string, payload []byte) error {
		if !jsonOutput {
			return nil
		}
		_, err := fmt.Fprintf(stdout, `{"type":%s,%s`+"\n", strconv.Quote(kind), payload[1:])
		return err
	}
	var writeErr error
	result, err := localipc.RunExecution(ctx, options.stateDir, options.run, localipc.RunEvents{
		Started: func(notice protocol.ExecutionStarted) {
			if !jsonOutput || writeErr != nil {
				return
			}
			executionID = notice.ExecutionID
			payload, err := notice.CanonicalJSON()
			if err == nil {
				writeErr = writeLine("started", payload)
			}
		},
		Output: func(stream protocol.OutputStream, data []byte) {
			if writeErr != nil {
				return
			}
			if !jsonOutput {
				writeErr = writeOutput(stdout, stderr, stream, data)
				return
			}
			payload := `{"executionId":` + strconv.Quote(executionID) +
				`,"stream":` + strconv.Quote(string(stream)) +
				`,"data":` + strconv.Quote(base64.RawURLEncoding.EncodeToString(data)) + `}`
			writeErr = writeLine("output", []byte(payload))
		},
	})
	if err != nil {
		return fmt.Errorf("run command: %w", err)
	}
	if jsonOutput && writeErr == nil {
		payload, encodeErr := result.CanonicalJSON()
		if encodeErr == nil {
			writeErr = writeLine("result", payload)
		}
	}
	if writeErr != nil {
		return fmt.Errorf("write run output: %w", writeErr)
	}
	return runOutcome(result)
}

// writeOutput sends one chunk to the stream it came from, unchanged.
func writeOutput(stdout, stderr io.Writer, stream protocol.OutputStream, data []byte) error {
	writer := stdout
	if stream == protocol.StreamStderr {
		writer = stderr
	}
	_, err := writer.Write(data)
	return err
}

// runOutcome maps a terminal result to `scopra run`'s exit status (ADR-011):
// the command's own status, 124 for a timeout, 130 for cancellation, 137 for
// an out-of-memory kill, and an error, which exitCode reports as 1, for a
// rejection or a failure to start.
func runOutcome(result protocol.ExecutionResult) error {
	switch result.State {
	case protocol.StateSucceeded:
		return nil
	case protocol.StateFailed:
		return &commandExitError{code: result.ExitCode}
	case protocol.StateTimedOut:
		return &commandExitError{code: 124}
	case protocol.StateCancelled:
		return &commandExitError{code: 130}
	case protocol.StateOutOfMemory:
		return &commandExitError{code: 137}
	case protocol.StateRejected, protocol.StateErrored:
		return fmt.Errorf("%s: %s", result.Error.Code, result.Error.Message)
	default:
		return fmt.Errorf("Mio reported an unknown execution state %q", result.State)
	}
}

// parseRunOptions parses and checks `scopra run`'s command line. Everything it
// can check without Mio is a usage error, so an invalid command never reaches
// the socket. The command follows "--", so arguments that look like options
// stay arguments.
func parseRunOptions(args []string, stderr io.Writer) (runOptions, error) {
	flags := flag.NewFlagSet("scopra run", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() { printUsage(stderr) }
	var options runOptions
	workspaceID := ""
	flags.StringVar(&options.stateDir, "state-dir", "", "Mio state directory")
	flags.BoolVar(&options.jsonOutput, "json", false, "write the start notice, output frames, and result as JSON, one object per line")
	flags.StringVar(&workspaceID, "workspace", "", "registered workspace ID")
	flags.Func("path", "run in the workspace registered at this directory", func(value string) error {
		options.path, options.pathSet = value, true
		return nil
	})
	write := flags.Bool("write", false, "mount the workspace read-write")
	workDir := flags.String("workdir", defaultRunWorkDir, "workspace-relative working directory")
	timeout := flags.Duration("timeout", defaultRunTimeout, "run time limit, a whole number of seconds")
	memory := flags.Uint64("memory", defaultRunMemoryBytes, "memory limit in bytes")
	cpus := flags.Int("cpus", defaultRunCPUs, "CPU limit, a whole number")
	pids := flags.Uint64("pids", defaultRunPIDs, "process limit")
	outputLimit := flags.Uint64("output-limit", defaultRunOutputBytes, "stdout and stderr limit in bytes")
	reason := flags.String("reason", defaultRunReason, "why the command runs; it grants nothing")

	separator := slices.Index(args, "--")
	flagArgs := args
	if separator >= 0 {
		flagArgs = args[:separator]
	}
	if err := flags.Parse(flagArgs); err != nil {
		return runOptions{}, &usageError{err: err}
	}
	switch {
	case separator < 0:
		return runOptions{}, failUsage(stderr, "%v, so options and arguments cannot be confused", errNoRunSeparator)
	case flags.NArg() != 0:
		return runOptions{}, failUsage(stderr, "run takes no arguments before --")
	case options.pathSet && workspaceID != "":
		return runOptions{}, failUsage(stderr, "run takes --workspace ID or --path PATH, not both")
	case !options.pathSet && workspaceID == "":
		return runOptions{}, failUsage(stderr, "run needs --workspace ID or --path PATH")
	case workspaceID != "" && !localipc.ValidWorkspaceID(workspaceID):
		return runOptions{}, failUsage(stderr,
			"invalid workspace ID %q: expected 32 lowercase hexadecimal characters, as shown by scopra workspace list", workspaceID)
	}

	command := args[separator+1:]
	if len(command) == 0 {
		return runOptions{}, failUsage(stderr, "run needs EXECUTABLE after --")
	}
	// A sliced array is never nil, so an executable without arguments
	// encodes as ADR-007's empty array.
	arguments := command[1:]
	timeoutSeconds, err := wholeSeconds(*timeout, "timeout")
	if err != nil {
		return runOptions{}, failUsage(stderr, "%v", err)
	}
	if *cpus < 1 || *cpus > maxRunCPUs {
		return runOptions{}, failUsage(stderr, "--cpus must be 1 to %d", maxRunCPUs)
	}
	access := protocol.WorkspaceReadOnly
	if *write {
		access = protocol.WorkspaceReadWrite
	}
	id := workspaceID
	if options.pathSet {
		id = placeholderWorkspaceID
	}
	run := localipc.RunRequest{
		WorkspaceID: id,
		Command: protocol.Command{
			Executable:       command[0],
			Arguments:        arguments,
			WorkingDirectory: *workDir,
		},
		WorkspaceAccess: access,
		Limits: protocol.Limits{
			TimeoutSeconds: timeoutSeconds,
			CPUMillicores:  uint64(*cpus) * 1000,
			MemoryBytes:    *memory,
			PIDs:           *pids,
			OutputBytes:    *outputLimit,
		},
		Reason: *reason,
	}
	if err := localipc.ValidateRun(run); err != nil {
		return runOptions{}, failUsage(stderr, "invalid run: %v", err)
	}
	options.run = run
	return options, nil
}

// wholeSeconds converts a duration to the whole seconds that ADR-007 carries,
// refusing a value it cannot state exactly or that is outside its bounds.
func wholeSeconds(duration time.Duration, name string) (uint64, error) {
	switch {
	case duration < time.Second:
		return 0, fmt.Errorf("--%s must be at least 1s", name)
	case duration > maxRunTimeout:
		return 0, fmt.Errorf("--%s must be at most %s", name, maxRunTimeout)
	case duration%time.Second != 0:
		return 0, fmt.Errorf("--%s must be a whole number of seconds", name)
	}
	return uint64(duration / time.Second), nil
}
