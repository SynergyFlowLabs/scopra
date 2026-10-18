package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/SynergyFlowLabs/scopra/internal/journal"
	"github.com/SynergyFlowLabs/scopra/internal/localipc"
	"github.com/SynergyFlowLabs/scopra/packages/protocol"
)

// logsSchemaVersion identifies the layout of `scopra logs --json` output. An
// incompatible change needs a new version.
const logsSchemaVersion = 1

// logsOutput is the one object `scopra logs --json` writes.
type logsOutput struct {
	SchemaVersion int             `json:"schemaVersion"`
	Entries       []journal.Entry `json:"entries"`
}

// logsOptions is a parsed `scopra logs` command line.
type logsOptions struct {
	stateDir   string
	jsonOutput bool
	filter     localipc.LogFilter
}

// runLogs lists the runs Mio recorded (ADR-013). It is read-only, and every
// value it can check is checked before the socket is touched.
func runLogs(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	options, err := parseLogsOptions(args, stderr)
	if err != nil {
		return err
	}
	entries, err := localipc.Logs(ctx, options.stateDir, options.filter)
	if err != nil {
		return fmt.Errorf("read the run journal: %w", err)
	}
	if options.jsonOutput {
		if entries == nil {
			entries = []journal.Entry{}
		}
		return writeJSON(stdout, logsOutput{SchemaVersion: logsSchemaVersion, Entries: entries})
	}
	if len(entries) == 0 {
		if options.filter.WorkspaceID != "" {
			_, err := fmt.Fprintf(stdout, "No runs have been recorded for workspace %s.\n", options.filter.WorkspaceID)
			return err
		}
		_, err := fmt.Fprintln(stdout, "No runs have been recorded.")
		return err
	}
	var text strings.Builder
	for _, entry := range entries {
		text.WriteString(formatLogEntry(entry))
		text.WriteByte('\n')
	}
	_, err = io.WriteString(stdout, text.String())
	return err
}

// parseLogsOptions parses and checks `scopra logs`'s command line.
func parseLogsOptions(args []string, stderr io.Writer) (logsOptions, error) {
	flags := flag.NewFlagSet("scopra logs", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() { printUsage(stderr) }
	var options logsOptions
	workspaceID := ""
	flags.StringVar(&options.stateDir, "state-dir", "", "Mio state directory")
	flags.BoolVar(&options.jsonOutput, "json", false, "write the entries as one JSON object")
	flags.StringVar(&workspaceID, "workspace", "", "only entries for this workspace ID")
	limit := flags.Int("limit", localipc.DefaultLogLimit, "how many entries to show")
	if err := flags.Parse(args); err != nil {
		return logsOptions{}, &usageError{err: err}
	}
	switch {
	case flags.NArg() != 0:
		return logsOptions{}, failUsage(stderr, "logs takes no positional arguments")
	case *limit < 1 || *limit > localipc.MaxLogLimit:
		return logsOptions{}, failUsage(stderr, "logs --limit must be 1 to %d", localipc.MaxLogLimit)
	case workspaceID != "" && !localipc.ValidWorkspaceID(workspaceID):
		return logsOptions{}, failUsage(stderr,
			"invalid workspace ID %q: expected 32 lowercase hexadecimal characters, as shown by scopra workspace list", workspaceID)
	}
	options.filter = localipc.LogFilter{WorkspaceID: workspaceID, Limit: *limit}
	return options, nil
}

// formatLogEntry renders one entry as a single line with fixed fields. The
// IDs and states are constrained, but every field is escaped anyway.
func formatLogEntry(entry journal.Entry) string {
	detail := "exit -"
	switch entry.State {
	case protocol.StateSucceeded, protocol.StateFailed:
		detail = "exit " + strconv.Itoa(entry.ExitCode)
	case protocol.StateRejected, protocol.StateErrored:
		detail = "error " + entry.ErrorCode
	}
	duration := "duration -"
	if !entry.StartedAt.IsZero() && !entry.EndedAt.IsZero() {
		duration = "duration " + entry.Duration.String()
	}
	var line strings.Builder
	line.WriteString(entry.RecordedAt.UTC().Format("2006-01-02T15:04:05.000Z"))
	line.WriteString("  ")
	line.WriteString(string(entry.State))
	line.WriteString("  ")
	line.WriteString(detail)
	line.WriteString("  ")
	line.WriteString(duration)
	line.WriteString("  stdout ")
	line.WriteString(strconv.FormatUint(entry.StdoutBytes, 10))
	line.WriteString(" B  stderr ")
	line.WriteString(strconv.FormatUint(entry.StderrBytes, 10))
	line.WriteString(" B")
	if entry.Truncated {
		line.WriteString("  truncated")
	}
	line.WriteString("  workspace ")
	line.WriteString(entry.WorkspaceID)
	line.WriteString("  execution ")
	line.WriteString(entry.ExecutionID)
	return terminalSafe(line.String())
}
