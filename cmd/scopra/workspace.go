package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/SynergyFlowLabs/scopra/internal/localipc"
	"github.com/SynergyFlowLabs/scopra/internal/workspace/registry"
)

// workspaceSchemaVersion identifies the layout of `scopra workspace --json`
// output. Incompatible changes require a new version; fields may be added.
const workspaceSchemaVersion = 1

type workspaceRecordOutput struct {
	SchemaVersion int                `json:"schemaVersion"`
	Workspace     localipc.Workspace `json:"workspace"`
}

type workspaceListOutput struct {
	SchemaVersion int                  `json:"schemaVersion"`
	Workspaces    []localipc.Workspace `json:"workspaces"`
}

// runWorkspace manages Mio's workspace registrations over local IPC
// (ADR-006). Registering a workspace authorizes nothing by itself.
func runWorkspace(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return failUsage(stderr, "workspace requires a subcommand: add, list, or remove")
	}
	switch args[0] {
	case "add":
		return runWorkspaceAdd(ctx, args[1:], stdout, stderr)
	case "list":
		return runWorkspaceList(ctx, args[1:], stdout, stderr)
	case "remove":
		return runWorkspaceRemove(ctx, args[1:], stdout, stderr)
	case "--help", "-h", "help":
		printUsage(stderr)
		return flag.ErrHelp
	default:
		return failUsage(stderr, "unknown workspace subcommand %q", args[0])
	}
}

func runWorkspaceAdd(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	options, err := parseWorkspaceFlags("add", args, stderr)
	if err != nil {
		return err
	}
	if len(options.args) != 1 {
		return failUsage(stderr, "workspace add takes exactly one PATH, after any flags")
	}
	path := options.args[0]
	if path == "" || !utf8.ValidString(path) {
		return failUsage(stderr, "workspace path %q must be non-empty and valid UTF-8", path)
	}
	if !filepath.IsAbs(path) {
		// Mio never resolves relative paths. Prefix the working directory
		// without cleaning, so that Mio resolves ".." after any symlinks, as
		// the kernel would for this process.
		workingDir, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("make the workspace path absolute: %w", err)
		}
		path = workingDir + string(filepath.Separator) + path
	}
	if len(path) > registry.MaxPathBytes {
		return failUsage(stderr, "workspace path is longer than %d bytes", registry.MaxPathBytes)
	}

	added, err := localipc.AddWorkspace(ctx, options.stateDir, path)
	if err != nil {
		return fmt.Errorf("register workspace: %w", err)
	}
	if options.jsonOutput {
		return writeJSON(stdout, workspaceRecordOutput{SchemaVersion: workspaceSchemaVersion, Workspace: added})
	}
	_, err = fmt.Fprintf(stdout, "Registered workspace %s at %s\n", added.ID, terminalSafe(added.Path))
	return err
}

func runWorkspaceList(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	options, err := parseWorkspaceFlags("list", args, stderr)
	if err != nil {
		return err
	}
	if len(options.args) != 0 {
		return failUsage(stderr, "workspace list takes no positional arguments")
	}

	workspaces, err := localipc.ListWorkspaces(ctx, options.stateDir)
	if err != nil {
		return fmt.Errorf("list workspaces: %w", err)
	}
	if options.jsonOutput {
		return writeJSON(stdout, workspaceListOutput{SchemaVersion: workspaceSchemaVersion, Workspaces: workspaces})
	}
	if len(workspaces) == 0 {
		_, err := fmt.Fprintln(stdout, "No workspaces are registered.")
		return err
	}
	var text strings.Builder
	for _, registered := range workspaces {
		fmt.Fprintf(&text, "%s  %s\n", registered.ID, terminalSafe(registered.Path))
	}
	_, err = io.WriteString(stdout, text.String())
	return err
}

func runWorkspaceRemove(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	options, err := parseWorkspaceFlags("remove", args, stderr)
	if err != nil {
		return err
	}
	if len(options.args) != 1 {
		return failUsage(stderr, "workspace remove takes exactly one ID, after any flags")
	}
	id := options.args[0]
	if !localipc.ValidWorkspaceID(id) {
		return failUsage(stderr,
			"invalid workspace ID %q: expected 32 lowercase hexadecimal characters, as shown by scopra workspace list", id)
	}

	removed, err := localipc.RemoveWorkspace(ctx, options.stateDir, id)
	if err != nil {
		return fmt.Errorf("remove workspace: %w", err)
	}
	if options.jsonOutput {
		return writeJSON(stdout, workspaceRecordOutput{SchemaVersion: workspaceSchemaVersion, Workspace: removed})
	}
	_, err = fmt.Fprintf(stdout, "Removed workspace %s (%s); the directory was not changed.\n",
		removed.ID, terminalSafe(removed.Path))
	return err
}

type workspaceOptions struct {
	stateDir   string
	jsonOutput bool
	args       []string
}

// parseWorkspaceFlags parses the flags that every workspace subcommand takes.
// Like the rest of scopra, it stops at the first positional argument.
func parseWorkspaceFlags(name string, args []string, stderr io.Writer) (workspaceOptions, error) {
	flags := flag.NewFlagSet("scopra workspace "+name, flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() { printUsage(stderr) }
	stateDir := flags.String("state-dir", "", "Mio state directory")
	jsonOutput := flags.Bool("json", false, "write the result as JSON")
	if err := flags.Parse(args); err != nil {
		return workspaceOptions{}, &usageError{err: err}
	}
	return workspaceOptions{stateDir: *stateDir, jsonOutput: *jsonOutput, args: flags.Args()}, nil
}

// writeJSON writes value as a single line of JSON.
func writeJSON(writer io.Writer, value any) error {
	return json.NewEncoder(writer).Encode(value)
}
