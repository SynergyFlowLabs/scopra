package main

import (
	"context"
	"encoding/json"
	"errors"
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

var (
	// errWorkspacePathNotRegistered reports that no registration matches the
	// path given to `workspace remove --path`.
	errWorkspacePathNotRegistered = errors.New("no registered workspace matches the path")
	// errAmbiguousWorkspacePath reports that the path equals one registration
	// but resolves to another.
	errAmbiguousWorkspacePath = errors.New("the path matches two registered workspaces")
)

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
	path, err := absoluteWorkspacePath(options.args[0], stderr)
	if err != nil {
		return err
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
	var id string
	switch {
	case options.pathSet && len(options.args) != 0:
		return failUsage(stderr, "workspace remove takes an ID or --path PATH, not both")
	case options.pathSet:
		path, err := absoluteWorkspacePath(options.path, stderr)
		if err != nil {
			return err
		}
		if id, err = workspaceIDForPath(ctx, options.stateDir, path); err != nil {
			return fmt.Errorf("remove workspace: %w", err)
		}
	case len(options.args) != 1:
		return failUsage(stderr, "workspace remove takes exactly one ID, after any flags, or --path PATH")
	default:
		id = options.args[0]
		if !localipc.ValidWorkspaceID(id) {
			return failUsage(stderr,
				"invalid workspace ID %q: expected 32 lowercase hexadecimal characters, as shown by scopra workspace list", id)
		}
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

// absoluteWorkspacePath checks a workspace PATH argument and makes it
// absolute. Mio never resolves relative paths, so the working directory is
// prefixed without cleaning: ".." must be resolved after any symlinks, as the
// kernel would for this process.
func absoluteWorkspacePath(path string, stderr io.Writer) (string, error) {
	if path == "" || !utf8.ValidString(path) {
		return "", failUsage(stderr, "workspace path %q must be non-empty and valid UTF-8", path)
	}
	if !filepath.IsAbs(path) {
		workingDir, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("make the workspace path absolute: %w", err)
		}
		path = workingDir + string(filepath.Separator) + path
	}
	if len(path) > registry.MaxPathBytes {
		return "", failUsage(stderr, "workspace path is longer than %d bytes", registry.MaxPathBytes)
	}
	return path, nil
}

// workspaceIDForPath finds the registration that path selects, using the
// existing list request. path selects a registration whose path equals it
// exactly, which still works after the directory is gone, or the one it now
// resolves to through symlinks. If the two disagree, it refuses to guess.
func workspaceIDForPath(ctx context.Context, stateDir, path string) (string, error) {
	workspaces, err := localipc.ListWorkspaces(ctx, stateDir)
	if err != nil {
		return "", err
	}
	exact, exactFound := findWorkspace(workspaces, path)
	var resolved localipc.Workspace
	resolvedFound := false
	if canonical, err := filepath.EvalSymlinks(path); err == nil {
		resolved, resolvedFound = findWorkspace(workspaces, canonical)
	}
	switch {
	case exactFound && resolvedFound && exact.ID != resolved.ID:
		return "", fmt.Errorf("%w: %q is registered as workspace %s but now resolves to workspace %s (%q); remove it by ID",
			errAmbiguousWorkspacePath, path, exact.ID, resolved.ID, resolved.Path)
	case exactFound:
		return exact.ID, nil
	case resolvedFound:
		return resolved.ID, nil
	default:
		return "", fmt.Errorf("%w: %q; if its directory is gone, give the path exactly as scopra workspace list shows it, "+
			"or remove it by ID", errWorkspacePathNotRegistered, path)
	}
}

func findWorkspace(workspaces []localipc.Workspace, path string) (localipc.Workspace, bool) {
	for _, registered := range workspaces {
		if registered.Path == path {
			return registered, true
		}
	}
	return localipc.Workspace{}, false
}

type workspaceOptions struct {
	stateDir   string
	jsonOutput bool
	// path holds `remove --path`, and pathSet whether it was given; the
	// other subcommands reject the flag.
	path    string
	pathSet bool
	args    []string
}

// parseWorkspaceFlags parses the flags that every workspace subcommand takes.
// Like the rest of scopra, it stops at the first positional argument.
func parseWorkspaceFlags(name string, args []string, stderr io.Writer) (workspaceOptions, error) {
	flags := flag.NewFlagSet("scopra workspace "+name, flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() { printUsage(stderr) }
	stateDir := flags.String("state-dir", "", "Mio state directory")
	jsonOutput := flags.Bool("json", false, "write the result as JSON")
	var options workspaceOptions
	if name == "remove" {
		flags.Func("path", "remove the registration for this directory instead of an ID", func(value string) error {
			options.path, options.pathSet = value, true
			return nil
		})
	}
	if err := flags.Parse(args); err != nil {
		return workspaceOptions{}, &usageError{err: err}
	}
	options.stateDir, options.jsonOutput, options.args = *stateDir, *jsonOutput, flags.Args()
	return options, nil
}

// writeJSON writes value as a single line of JSON.
func writeJSON(writer io.Writer, value any) error {
	return json.NewEncoder(writer).Encode(value)
}
