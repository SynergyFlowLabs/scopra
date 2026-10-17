package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/SynergyFlowLabs/scopra/internal/localipc"
	"github.com/SynergyFlowLabs/scopra/internal/workspace"
	"github.com/SynergyFlowLabs/scopra/internal/workspace/registry"
)

var registeredPattern = regexp.MustCompile(`^Registered workspace ([0-9a-f]{32}) at (.*)\n$`)

func TestRunWorkspaceAddListRemove(t *testing.T) {
	stateDir := startWorkspaceDaemon(t)
	project := makeWorkspaceDir(t, t.TempDir(), "project")

	stdout, stderr, err := runCLI(t, "workspace", "add", "--state-dir", stateDir, project)
	if err != nil || stderr != "" {
		t.Fatalf("workspace add error = %v; stderr = %q", err, stderr)
	}
	match := registeredPattern.FindStringSubmatch(stdout)
	if match == nil || match[2] != project {
		t.Fatalf("workspace add output = %q, want the new ID and %q", stdout, project)
	}
	id := match[1]

	stdout, stderr, err = runCLI(t, "workspace", "list", "--state-dir", stateDir)
	if err != nil || stderr != "" || stdout != id+"  "+project+"\n" {
		t.Fatalf("workspace list = %q, %v; stderr = %q; want one %q line", stdout, err, stderr, id+"  "+project)
	}

	stdout, stderr, err = runCLI(t, "workspace", "remove", "--state-dir", stateDir, id)
	want := "Removed workspace " + id + " (" + project + "); the directory was not changed.\n"
	if err != nil || stderr != "" || stdout != want {
		t.Fatalf("workspace remove = %q, %v; stderr = %q; want %q", stdout, err, stderr, want)
	}
	stdout, _, err = runCLI(t, "workspace", "list", "--state-dir", stateDir)
	if err != nil || stdout != "No workspaces are registered.\n" {
		t.Fatalf("workspace list after removal = %q, %v; want the empty message", stdout, err)
	}
	if info, err := os.Lstat(project); err != nil || !info.IsDir() {
		t.Fatalf("workspace remove touched the directory (Lstat error = %v)", err)
	}
}

func TestRunWorkspaceJSON(t *testing.T) {
	stateDir := startWorkspaceDaemon(t)
	project := makeWorkspaceDir(t, t.TempDir(), "project")

	stdout, _, err := runCLI(t, "workspace", "add", "--state-dir", stateDir, "--json", project)
	if err != nil {
		t.Fatalf("workspace add --json error = %v", err)
	}
	var added struct {
		Workspace localipc.Workspace `json:"workspace"`
	}
	if err := json.Unmarshal([]byte(stdout), &added); err != nil {
		t.Fatalf("workspace add --json output %q is not JSON: %v", stdout, err)
	}
	record := `{"id":"` + added.Workspace.ID + `","path":` + jsonQuote(project) + `}`
	if want := `{"schemaVersion":1,"workspace":` + record + "}\n"; stdout != want {
		t.Fatalf("workspace add --json = %q, want %q", stdout, want)
	}

	stdout, _, err = runCLI(t, "workspace", "list", "--state-dir", stateDir, "--json")
	if want := `{"schemaVersion":1,"workspaces":[` + record + "]}\n"; err != nil || stdout != want {
		t.Fatalf("workspace list --json = %q, %v; want %q", stdout, err, want)
	}
	stdout, _, err = runCLI(t, "workspace", "remove", "--state-dir", stateDir, "--json", added.Workspace.ID)
	if want := `{"schemaVersion":1,"workspace":` + record + "}\n"; err != nil || stdout != want {
		t.Fatalf("workspace remove --json = %q, %v; want %q", stdout, err, want)
	}
	stdout, _, err = runCLI(t, "workspace", "list", "--state-dir", stateDir, "--json")
	if want := `{"schemaVersion":1,"workspaces":[]}` + "\n"; err != nil || stdout != want {
		t.Fatalf("empty workspace list --json = %q, %v; want %q", stdout, err, want)
	}
}

func TestRunWorkspaceAddResolvesRelativePathsPhysically(t *testing.T) {
	stateDir := startWorkspaceDaemon(t)
	base := makeWorkspaceDir(t, t.TempDir())
	inside := makeWorkspaceDir(t, base, "real", "inside")
	sibling := makeWorkspaceDir(t, base, "real", "sibling")
	link := filepath.Join(base, "link")
	if err := os.Symlink(inside, link); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}
	// Through the symlink, ".." is base/real for the kernel. Cleaning the
	// path lexically would name base/sibling instead, which does not exist.
	t.Chdir(link)

	for path, want := range map[string]string{"../sibling": sibling, ".": inside} {
		stdout, _, err := runCLI(t, "workspace", "add", "--state-dir", stateDir, path)
		if match := registeredPattern.FindStringSubmatch(stdout); err != nil || match == nil || match[2] != want {
			t.Fatalf("workspace add %q = %q, %v; want %q registered", path, stdout, err, want)
		}
	}
}

func TestRunWorkspaceRemoveByPath(t *testing.T) {
	tests := []struct {
		name string
		// prepare changes the file system if needed and returns the --path
		// argument that should select project.
		prepare func(t *testing.T, base, project string) string
		json    bool
	}{
		{name: "exact path", prepare: func(t *testing.T, base, project string) string { return project }},
		{name: "relative path", prepare: func(t *testing.T, base, project string) string {
			t.Chdir(base)
			return "project"
		}},
		{name: "through a symlink", prepare: func(t *testing.T, base, project string) string {
			link := filepath.Join(base, "link")
			if err := os.Symlink(project, link); err != nil {
				t.Fatalf("Symlink() error = %v", err)
			}
			return link
		}},
		{name: "existing directory with a trailing slash", prepare: func(t *testing.T, base, project string) string {
			return project + "/"
		}},
		{name: "directory no longer exists", prepare: func(t *testing.T, base, project string) string {
			if err := os.Remove(project); err != nil {
				t.Fatalf("Remove(project) error = %v", err)
			}
			return project
		}},
		{name: "json output", prepare: func(t *testing.T, base, project string) string { return project }, json: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stateDir := startWorkspaceDaemon(t)
			base := makeWorkspaceDir(t, t.TempDir())
			project := makeWorkspaceDir(t, base, "project")
			other := makeWorkspaceDir(t, base, "other")
			id := addWorkspace(t, stateDir, project)
			otherID := addWorkspace(t, stateDir, other)
			path := test.prepare(t, base, project)

			args := []string{"workspace", "remove", "--state-dir", stateDir}
			want := "Removed workspace " + id + " (" + project + "); the directory was not changed.\n"
			if test.json {
				args = append(args, "--json")
				want = `{"schemaVersion":1,"workspace":{"id":"` + id + `","path":` + jsonQuote(project) + "}}\n"
			}
			stdout, stderr, err := runCLI(t, append(args, "--path", path)...)
			if err != nil || stderr != "" || stdout != want {
				t.Fatalf("workspace remove --path %q = %q, %v; stderr = %q; want %q", path, stdout, err, stderr, want)
			}
			requireRegistered(t, stateDir, otherID+"  "+other)
			if test.name != "directory no longer exists" {
				if info, err := os.Lstat(project); err != nil || !info.IsDir() {
					t.Fatalf("workspace remove touched the directory (Lstat error = %v)", err)
				}
			}
		})
	}
}

func TestRunWorkspaceRemoveByPathRequiresOneMatch(t *testing.T) {
	tests := []struct {
		name        string
		prepare     func(t *testing.T, base, a, b string) string
		wantErr     error
		wantMessage []string
	}{
		{name: "unregistered directory", prepare: func(t *testing.T, base, a, b string) string {
			return makeWorkspaceDir(t, base, "unregistered")
		}, wantErr: errWorkspacePathNotRegistered, wantMessage: []string{"unregistered", "as scopra workspace list shows it", "by ID"}},
		{name: "missing directory with a trailing slash", prepare: func(t *testing.T, base, a, b string) string {
			// No lexical cleaning: the path no longer resolves and differs
			// from the registered one, so it matches nothing.
			if err := os.Remove(a); err != nil {
				t.Fatalf("Remove(a) error = %v", err)
			}
			return a + "/"
		}, wantErr: errWorkspacePathNotRegistered},
		{name: "control characters", prepare: func(t *testing.T, base, a, b string) string {
			return filepath.Join(base, "x\x1b[2J\nforged")
		}, wantErr: errWorkspacePathNotRegistered, wantMessage: []string{`x\x1b[2J\nforged`}},
		{name: "registered path now resolves to another workspace", prepare: func(t *testing.T, base, a, b string) string {
			if err := os.Remove(a); err != nil {
				t.Fatalf("Remove(a) error = %v", err)
			}
			if err := os.Symlink(b, a); err != nil {
				t.Fatalf("Symlink() error = %v", err)
			}
			return a
		}, wantErr: errAmbiguousWorkspacePath, wantMessage: []string{"remove it by ID"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stateDir := startWorkspaceDaemon(t)
			base := makeWorkspaceDir(t, t.TempDir())
			a := makeWorkspaceDir(t, base, "a")
			b := makeWorkspaceDir(t, base, "b")
			idA := addWorkspace(t, stateDir, a)
			idB := addWorkspace(t, stateDir, b)
			path := test.prepare(t, base, a, b)

			stdout, _, err := runCLI(t, "workspace", "remove", "--state-dir", stateDir, "--path", path)
			var stderr bytes.Buffer
			if code := exitCode(err, &stderr); code != 1 || !errors.Is(err, test.wantErr) || stdout != "" {
				t.Fatalf("workspace remove --path %q: exit code %d, error %v, stdout %q; want 1 and %v", path, code, err, stdout, test.wantErr)
			}
			message := stderr.String()
			if errors.Is(test.wantErr, errAmbiguousWorkspacePath) {
				test.wantMessage = append(test.wantMessage, idA, idB)
			}
			for _, want := range test.wantMessage {
				if !strings.Contains(message, want) {
					t.Fatalf("stderr = %q, want it to contain %q", message, want)
				}
			}
			if strings.ContainsRune(message, '\x1b') || strings.Contains(message, "\nforged") {
				t.Fatalf("stderr contains raw control characters: %q", message)
			}
			requireRegistered(t, stateDir, idA+"  "+a, idB+"  "+b)
		})
	}
}

func TestRunWorkspaceReportsMioErrors(t *testing.T) {
	stateDir := startWorkspaceDaemon(t)
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "missing directory", args: []string{"add", filepath.Join(t.TempDir(), "missing")}, want: localipc.CodeUnresolvablePath},
		{name: "directory containing the state directory", args: []string{"add", filepath.Dir(stateDir)}, want: localipc.CodeProtectedPath},
		{name: "unknown ID", args: []string{"remove", strings.Repeat("0", 32)}, want: localipc.CodeNotFound},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			args := append([]string{"workspace", test.args[0], "--state-dir", stateDir}, test.args[1:]...)
			stdout, _, err := runCLI(t, args...)
			var stderr bytes.Buffer
			if code := exitCode(err, &stderr); code != 1 || stdout != "" {
				t.Fatalf("exit code = %d, stdout = %q; want 1 and no output", code, stdout)
			}
			if !strings.Contains(stderr.String(), test.want) {
				t.Fatalf("error output = %q, want it to name Mio's code %q", stderr.String(), test.want)
			}
		})
	}
}

func TestRunWorkspaceReportsUnavailableMio(t *testing.T) {
	requireLinux(t)
	stateDir := makePrivateStateDir(t)

	for _, args := range [][]string{
		{"workspace", "list", "--state-dir", stateDir},
		{"workspace", "remove", "--state-dir", stateDir, "--path", "/srv/a"},
	} {
		_, _, err := runCLI(t, args...)
		var stderr bytes.Buffer
		if code := exitCode(err, &stderr); code != 1 || !errors.Is(err, localipc.ErrDaemonUnavailable) {
			t.Fatalf("run(%q) without Mio: exit code %d, error %v; want 1 and ErrDaemonUnavailable", args, code, err)
		}
	}
}

func TestRunWorkspaceUsageErrors(t *testing.T) {
	// No Mio runs here: contacting one would fail with a different error.
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Chdir(t.TempDir())
	tests := []struct {
		name string
		args []string
	}{
		{name: "no subcommand", args: []string{"workspace"}},
		{name: "unknown subcommand", args: []string{"workspace", "rename"}},
		{name: "unknown flag", args: []string{"workspace", "list", "--verbose"}},
		{name: "add without a path", args: []string{"workspace", "add"}},
		{name: "add with two paths", args: []string{"workspace", "add", "/srv/a", "/srv/b"}},
		{name: "add with a flag after the path", args: []string{"workspace", "add", "/srv/a", "--json"}},
		{name: "add with invalid UTF-8", args: []string{"workspace", "add", "/srv/\xff"}},
		{name: "add with a path over 4095 bytes", args: []string{"workspace", "add", "/" + strings.Repeat("a", registry.MaxPathBytes)}},
		{name: "add with a relative path that becomes too long", args: []string{"workspace", "add", strings.Repeat("a", registry.MaxPathBytes-1)}},
		{name: "list with an argument", args: []string{"workspace", "list", "extra"}},
		{name: "remove without an ID", args: []string{"workspace", "remove"}},
		{name: "remove with a malformed ID", args: []string{"workspace", "remove", "not-an-id"}},
		{name: "remove with an uppercase ID", args: []string{"workspace", "remove", strings.Repeat("A", 32)}},
		{name: "remove with an ID and --path", args: []string{"workspace", "remove", "--path", "/srv/a", strings.Repeat("a", 32)}},
		{name: "remove with an empty --path", args: []string{"workspace", "remove", "--path", ""}},
		{name: "remove with --path but no value", args: []string{"workspace", "remove", "--path"}},
		{name: "remove with --path in invalid UTF-8", args: []string{"workspace", "remove", "--path", "/srv/\xff"}},
		{name: "remove with --path over 4095 bytes", args: []string{"workspace", "remove", "--path", "/" + strings.Repeat("a", registry.MaxPathBytes)}},
		{name: "remove with a relative --path that becomes too long", args: []string{"workspace", "remove", "--path", strings.Repeat("a", registry.MaxPathBytes-1)}},
		{name: "add with --path", args: []string{"workspace", "add", "--path", "/srv/a"}},
		{name: "list with --path", args: []string{"workspace", "list", "--path", "/srv/a"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stdout, stderr, err := runCLI(t, test.args...)
			var usageErr *usageError
			if !errors.As(err, &usageErr) {
				t.Fatalf("run(%q) error = %v, want a usage error", test.args, err)
			}
			if stdout != "" || !strings.Contains(stderr, "usage: scopra") {
				t.Fatalf("run(%q) stdout = %q, stderr = %q; want usage on stderr only", test.args, stdout, stderr)
			}
			if strings.Contains(stderr, "unknown command") {
				t.Fatalf("run(%q) stderr = %q, want workspace to be a known command", test.args, stderr)
			}
		})
	}
}

func TestRunWorkspaceHelp(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"workspace", "--help"}, {"workspace", "add", "--help"}} {
		_, stderr, err := runCLI(t, args...)
		if !errors.Is(err, flag.ErrHelp) || !strings.Contains(stderr, "workspace add") || !strings.Contains(stderr, "(ID | --path PATH)") {
			t.Fatalf("run(%q) = %v, stderr %q; want flag.ErrHelp and usage naming the workspace commands", args, err, stderr)
		}
	}
}

func TestSecurity_RunWorkspaceEscapesTerminalOutput(t *testing.T) {
	stateDir := startWorkspaceDaemon(t)
	project := makeWorkspaceDir(t, t.TempDir(), "escape\x1b[31m")

	stdout, _, err := runCLI(t, "workspace", "add", "--state-dir", stateDir, project)
	if err != nil {
		t.Fatalf("workspace add error = %v", err)
	}
	listed, _, err := runCLI(t, "workspace", "list", "--state-dir", stateDir)
	if err != nil {
		t.Fatalf("workspace list error = %v", err)
	}
	_, _, err = runCLI(t, "workspace", "add", "--state-dir", stateDir, project+"\x1b]0;title\x07")
	var stderr bytes.Buffer
	exitCode(err, &stderr)
	for name, output := range map[string]string{"add": stdout, "list": listed, "error": stderr.String()} {
		if strings.ContainsAny(output, "\x1b\x07") || !strings.Contains(output, `escape\x1b[31m`) {
			t.Fatalf("%s output = %q, want control characters escaped", name, output)
		}
	}
}

func TestExitCode(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		want       int
		wantStderr string
	}{
		{name: "success"},
		{name: "help", err: flag.ErrHelp},
		{name: "help through a usage error", err: &usageError{err: flag.ErrHelp}},
		{name: "usage error", err: &usageError{err: errors.New("bad argument")}, want: 2},
		{name: "interrupted", err: fmt.Errorf("read response: %w", context.Canceled), want: 130},
		{
			name:       "failure with control characters",
			err:        &localipc.WorkspaceError{Code: localipc.CodeInternal, Message: "disk\x1b[31m on\nfire"},
			want:       1,
			wantStderr: `error: localipc: Mio reported internal: disk\x1b[31m on\nfire` + "\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stderr bytes.Buffer
			if got := exitCode(test.err, &stderr); got != test.want || stderr.String() != test.wantStderr {
				t.Fatalf("exitCode() = %d, stderr %q; want %d, %q", got, stderr.String(), test.want, test.wantStderr)
			}
		})
	}
}

// startWorkspaceDaemon serves Mio's IPC with a real registry, as scoprad
// does, and returns the state directory.
func startWorkspaceDaemon(t *testing.T) string {
	t.Helper()
	requireLinux(t)
	t.Setenv("HOME", t.TempDir())
	daemon, err := localipc.Listen(filepath.Join(t.TempDir(), "run"), "test-version")
	if err != nil {
		t.Fatalf("localipc.Listen() error = %v", err)
	}
	validator, err := workspace.NewValidator(daemon.StateDir())
	if err != nil {
		t.Fatalf("NewValidator() error = %v", err)
	}
	workspaces, err := registry.Open(daemon.StateDir(), validator)
	if err != nil {
		t.Fatalf("registry.Open() error = %v", err)
	}
	if err := daemon.EnableWorkspaces(workspaces); err != nil {
		t.Fatalf("EnableWorkspaces() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- daemon.Serve(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("daemon did not stop")
		}
		_ = daemon.Close()
	})
	return daemon.StateDir()
}

func runCLI(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var stdout, stderr bytes.Buffer
	err := run(ctx, args, &stdout, &stderr)
	return stdout.String(), stderr.String(), err
}

// addWorkspace registers path through the CLI and returns its ID.
func addWorkspace(t *testing.T, stateDir, path string) string {
	t.Helper()
	stdout, stderr, err := runCLI(t, "workspace", "add", "--state-dir", stateDir, path)
	match := registeredPattern.FindStringSubmatch(stdout)
	if err != nil || stderr != "" || match == nil || match[2] != path {
		t.Fatalf("workspace add %q = %q, %v; stderr = %q", path, stdout, err, stderr)
	}
	return match[1]
}

// requireRegistered fails unless `scopra workspace list` prints exactly the
// given "ID  PATH" lines.
func requireRegistered(t *testing.T, stateDir string, lines ...string) {
	t.Helper()
	want := "No workspaces are registered.\n"
	if len(lines) > 0 {
		want = strings.Join(lines, "\n") + "\n"
	}
	stdout, stderr, err := runCLI(t, "workspace", "list", "--state-dir", stateDir)
	if err != nil || stderr != "" || stdout != want {
		t.Fatalf("workspace list = %q, %v; stderr = %q; want %q", stdout, err, stderr, want)
	}
}

// makeWorkspaceDir creates an owner-only directory and returns its canonical path.
func makeWorkspaceDir(t *testing.T, parent string, names ...string) string {
	t.Helper()
	path := filepath.Join(append([]string{parent}, names...)...)
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", path, err)
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q) error = %v", path, err)
	}
	return canonical
}

func jsonQuote(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
