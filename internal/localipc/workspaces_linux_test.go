//go:build linux

package localipc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/SynergyFlowLabs/scopra/internal/workspace"
	"github.com/SynergyFlowLabs/scopra/internal/workspace/registry"
)

func TestWorkspaceCommandsRoundTrip(t *testing.T) {
	stateDir := startWorkspaceDaemon(t)
	base := t.TempDir()
	alpha := makeWorkspaceDir(t, base, "alpha")
	beta := makeWorkspaceDir(t, base, "beta")
	link := filepath.Join(base, "beta-link")
	if err := os.Symlink(beta, link); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}
	ctx := requestContext(t)

	added, err := AddWorkspace(ctx, stateDir, alpha)
	if err != nil {
		t.Fatalf("AddWorkspace(%q) error = %v", alpha, err)
	}
	if !validWorkspaceID(added.ID) || added.Path != alpha {
		t.Fatalf("AddWorkspace(%q) = %+v, want a new ID and the canonical path", alpha, added)
	}
	viaLink, err := AddWorkspace(ctx, stateDir, link)
	if err != nil {
		t.Fatalf("AddWorkspace(symlink) error = %v", err)
	}
	if viaLink.Path != beta {
		t.Fatalf("AddWorkspace(symlink) path = %q, want the canonical path %q", viaLink.Path, beta)
	}
	listed, err := ListWorkspaces(ctx, stateDir)
	if err != nil {
		t.Fatalf("ListWorkspaces() error = %v", err)
	}
	if want := []Workspace{added, viaLink}; !slices.Equal(listed, want) {
		t.Fatalf("ListWorkspaces() = %+v, want %+v sorted by path", listed, want)
	}

	removed, err := RemoveWorkspace(ctx, stateDir, added.ID)
	if err != nil {
		t.Fatalf("RemoveWorkspace() error = %v", err)
	}
	if removed != added {
		t.Fatalf("RemoveWorkspace() = %+v, want the removed registration %+v", removed, added)
	}
	listed, err = ListWorkspaces(ctx, stateDir)
	if err != nil {
		t.Fatalf("ListWorkspaces() after removal error = %v", err)
	}
	if want := []Workspace{viaLink}; !slices.Equal(listed, want) {
		t.Fatalf("ListWorkspaces() after removal = %+v, want %+v", listed, want)
	}
	if info, err := os.Lstat(alpha); err != nil || !info.IsDir() {
		t.Fatalf("RemoveWorkspace() touched the workspace directory (Lstat error = %v)", err)
	}
}

func TestWorkspaceErrorsReachTheClient(t *testing.T) {
	stateDir := startWorkspaceDaemon(t)
	base := t.TempDir()
	registered := makeWorkspaceDir(t, base, "registered")
	file := filepath.Join(base, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	insideState := makeWorkspaceDir(t, stateDir, "project")
	ctx := requestContext(t)
	if _, err := AddWorkspace(ctx, stateDir, registered); err != nil {
		t.Fatalf("AddWorkspace(%q) error = %v", registered, err)
	}
	// Mio shares this working directory, where "registered" names a real
	// workspace. It must still refuse to resolve a relative path.
	t.Chdir(base)

	add := func(path string) func() error {
		return func() error {
			_, err := AddWorkspace(ctx, stateDir, path)
			return err
		}
	}
	tests := []struct {
		name string
		call func() error
		want string
	}{
		{name: "relative path", call: add("registered"), want: CodeInvalidPath},
		{name: "missing directory", call: add(filepath.Join(base, "missing")), want: CodeUnresolvablePath},
		{name: "regular file", call: add(file), want: CodeNotDirectory},
		{name: "directory containing the state directory", call: add(filepath.Dir(stateDir)), want: CodeProtectedPath},
		{name: "directory inside the state directory", call: add(insideState), want: CodeProtectedPath},
		{name: "already registered", call: add(registered), want: CodeAlreadyRegistered},
		{name: "inside a registered workspace", call: add(makeWorkspaceDir(t, registered, "inner")), want: CodeOverlap},
		{name: "unknown ID", call: func() error {
			_, err := RemoveWorkspace(ctx, stateDir, strings.Repeat("0", 32))
			return err
		}, want: CodeNotFound},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.call()
			var replyErr *WorkspaceError
			if !errors.As(err, &replyErr) || replyErr.Code != test.want || replyErr.Message == "" {
				t.Fatalf("error = %v, want a *WorkspaceError with code %q and a message", err, test.want)
			}
			if text := err.Error(); !strings.Contains(text, test.want) || !strings.Contains(text, replyErr.Message) {
				t.Fatalf("Error() = %q, want the code and the message", text)
			}
		})
	}
}

func TestWorkspaceErrorsReportForeignOwnership(t *testing.T) {
	info, err := os.Stat("/usr")
	if err != nil || os.Geteuid() == 0 || info.Sys().(*syscall.Stat_t).Uid != 0 {
		t.Skip("needs a root-owned /usr and a non-root test user")
	}
	stateDir := startWorkspaceDaemon(t)

	_, err = AddWorkspace(requestContext(t), stateDir, "/usr")
	var replyErr *WorkspaceError
	if !errors.As(err, &replyErr) || replyErr.Code != CodeNotOwned {
		t.Fatalf("AddWorkspace(/usr) error = %v, want code %q", err, CodeNotOwned)
	}
}

func TestWorkspaceErrorsFromRegistryFailures(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{err: fmt.Errorf("%w (limit 256)", registry.ErrRegistryFull), want: CodeRegistryFull},
		{err: errors.New("replace registry file: no space left on device"), want: CodeInternal},
	}
	for _, test := range tests {
		t.Run(test.want, func(t *testing.T) {
			stateDir, daemon := listenForTest(t)
			if err := daemon.EnableWorkspaces(failingRegistry{err: test.err}); err != nil {
				t.Fatalf("EnableWorkspaces() error = %v", err)
			}
			serveInBackground(t, daemon)

			_, err := AddWorkspace(requestContext(t), stateDir, "/srv/project")
			var replyErr *WorkspaceError
			if !errors.As(err, &replyErr) || replyErr.Code != test.want || !strings.Contains(replyErr.Message, test.err.Error()) {
				t.Fatalf("AddWorkspace() error = %v, want code %q carrying %q", err, test.want, test.err)
			}
		})
	}
}

func TestListWorkspacesFollowsPagesOverTheSocket(t *testing.T) {
	stateDir := startWorkspaceDaemon(t)
	base := t.TempDir()
	ctx := requestContext(t)
	// Names of control characters escape to six bytes each in JSON, so two of
	// these nearly 4 KiB paths fill most of a frame.
	chain := slices.Repeat([]string{strings.Repeat("\x01", 255)}, 15)
	var want []Workspace
	for _, name := range []string{"a", "b", "c"} {
		path := filepath.Join(append([]string{base, name}, chain...)...)
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatalf("MkdirAll(long path) error = %v", err)
		}
		added, err := AddWorkspace(ctx, stateDir, path)
		if err != nil {
			t.Fatalf("AddWorkspace(long path) error = %v", err)
		}
		want = append(want, added)
	}

	page, next, err := listWorkspacesPage(ctx, stateDir, "")
	if err != nil {
		t.Fatalf("listWorkspacesPage() error = %v", err)
	}
	if next == "" || len(page) == 0 || len(page) >= len(want) {
		t.Fatalf("first page has %d of %d records and next cursor %q, want a partial page", len(page), len(want), next)
	}
	listed, err := ListWorkspaces(ctx, stateDir)
	if err != nil {
		t.Fatalf("ListWorkspaces() error = %v", err)
	}
	slices.SortFunc(want, func(a, b Workspace) int { return strings.Compare(a.Path, b.Path) })
	if !slices.Equal(listed, want) {
		t.Fatalf("ListWorkspaces() returned %d records, want all %d sorted by path", len(listed), len(want))
	}
}

func TestListWorkspacesSortsByPath(t *testing.T) {
	stateDir, daemon := listenForTest(t)
	// IDs in the opposite order to paths: Mio pages by ID, the client sorts by path.
	records := []registry.Record{
		{ID: strings.Repeat("b", 32), Workspace: workspace.Workspace{Path: "/srv/a"}},
		{ID: strings.Repeat("a", 32), Workspace: workspace.Workspace{Path: "/srv/b"}},
	}
	if err := daemon.EnableWorkspaces(fixedRegistry{records: records}); err != nil {
		t.Fatalf("EnableWorkspaces() error = %v", err)
	}
	serveInBackground(t, daemon)

	listed, err := ListWorkspaces(requestContext(t), stateDir)
	if err != nil {
		t.Fatalf("ListWorkspaces() error = %v", err)
	}
	want := []Workspace{{ID: strings.Repeat("b", 32), Path: "/srv/a"}, {ID: strings.Repeat("a", 32), Path: "/srv/b"}}
	if !slices.Equal(listed, want) {
		t.Fatalf("ListWorkspaces() = %+v, want %+v", listed, want)
	}
}

func TestSecurity_ListWorkspacesStopsAnEndlessListing(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "run")
	if err := os.Mkdir(stateDir, 0o700); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	socketPath := filepath.Join(stateDir, "scoprad.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socketPath, Net: "unix"})
	if err != nil {
		t.Fatalf("ListenUnix() error = %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	if err := os.Chmod(socketPath, 0o600); err != nil {
		t.Fatalf("Chmod(socket) error = %v", err)
	}
	// A misbehaving daemon that answers every page with a valid, advancing
	// page and always another cursor.
	var pages atomic.Int64
	go func() {
		for {
			connection, err := listener.AcceptUnix()
			if err != nil {
				return
			}
			encoded, err := readFrame(connection)
			if message, decodeErr := decodeRequest(encoded); err == nil && decodeErr == nil {
				page := pages.Add(1)
				id := fmt.Sprintf("%032x", page)
				reply, _ := encodeListReply(message.RequestID, []Workspace{{ID: id, Path: fmt.Sprintf("/srv/%d", page)}}, id)
				_ = writeFrame(connection, reply)
			}
			_ = connection.Close()
		}
	}()

	listed, err := ListWorkspaces(requestContext(t), stateDir)
	if !errors.Is(err, ErrInvalidMessage) {
		t.Fatalf("ListWorkspaces() = %d records, %v; want ErrInvalidMessage", len(listed), err)
	}
	if got := pages.Load(); got != registry.MaxWorkspaces+1 {
		t.Fatalf("client requested %d pages, want it to stop after %d records", got, registry.MaxWorkspaces+1)
	}
}

func TestSecurity_WorkspaceCommandsNeedAnEnabledRegistry(t *testing.T) {
	stateDir, daemon := listenForTest(t)
	serveInBackground(t, daemon)
	workspaceDir := makeWorkspaceDir(t, t.TempDir(), "project")
	ctx := requestContext(t)

	_, err := AddWorkspace(ctx, stateDir, workspaceDir)
	var replyErr *WorkspaceError
	if err == nil || errors.As(err, &replyErr) {
		t.Fatalf("AddWorkspace() without a registry error = %v, want the connection closed without a reply", err)
	}
	if _, err := os.Lstat(filepath.Join(stateDir, "workspaces.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a registry file appeared (Lstat error = %v)", err)
	}
	if _, err := Status(ctx, stateDir); err != nil {
		t.Fatalf("Status() after a refused workspace command error = %v", err)
	}
}

func TestSecurity_EnableWorkspacesOnlyBeforeServe(t *testing.T) {
	stateDir, daemon := listenForTest(t)
	if err := daemon.EnableWorkspaces(nil); err == nil {
		t.Fatal("EnableWorkspaces(nil) error = nil, want an error")
	}
	serveInBackground(t, daemon)
	if _, err := Status(requestContext(t), stateDir); err != nil {
		t.Fatalf("Status() error = %v", err)
	}

	if err := daemon.EnableWorkspaces(failingRegistry{}); !errors.Is(err, ErrDaemonAlreadyServing) {
		t.Fatalf("EnableWorkspaces() while serving error = %v, want ErrDaemonAlreadyServing", err)
	}
}

func TestSecurity_DaemonDropsMalformedWorkspaceRequests(t *testing.T) {
	stateDir := startWorkspaceDaemon(t)
	workspaceDir := makeWorkspaceDir(t, t.TempDir(), "project")
	payloads := []string{
		requestPrefix + `"command":"workspace.add","path":"` + workspaceDir + "\xff" + `"}`,
		requestPrefix + `"command":"workspace.add","path":"` + workspaceDir + `","id":"` + testWorkspaceID + `"}`,
	}
	for _, payload := range payloads {
		connection, err := net.DialTimeout("unix", filepath.Join(stateDir, "scoprad.sock"), time.Second)
		if err != nil {
			t.Fatalf("DialTimeout() error = %v", err)
		}
		if err := writeFrame(connection, []byte(payload)); err != nil {
			t.Fatalf("writeFrame() error = %v", err)
		}
		if reply, err := readFrame(connection); err == nil {
			t.Fatalf("Mio replied %s to a malformed workspace request", reply)
		}
		_ = connection.Close()
	}
	listed, err := ListWorkspaces(requestContext(t), stateDir)
	if err != nil || len(listed) != 0 {
		t.Fatalf("ListWorkspaces() = %+v, %v; want no registrations", listed, err)
	}
}

// startWorkspaceDaemon serves Mio's IPC with a real registry in a new state
// directory, as scoprad does, and returns the state directory.
func startWorkspaceDaemon(t *testing.T) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	stateDir, daemon := listenForTest(t)
	validator, err := workspace.NewValidator(stateDir)
	if err != nil {
		t.Fatalf("NewValidator() error = %v", err)
	}
	workspaces, err := registry.Open(stateDir, validator)
	if err != nil {
		t.Fatalf("registry.Open() error = %v", err)
	}
	if err := daemon.EnableWorkspaces(workspaces); err != nil {
		t.Fatalf("EnableWorkspaces() error = %v", err)
	}
	serveInBackground(t, daemon)
	return stateDir
}

func listenForTest(t *testing.T) (string, *Daemon) {
	t.Helper()
	daemon, err := Listen(filepath.Join(t.TempDir(), "run"), "test-version")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	t.Cleanup(func() { _ = daemon.Close() })
	return daemon.StateDir(), daemon
}

func serveInBackground(t *testing.T, daemon *Daemon) {
	t.Helper()
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
	})
}

func requestContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
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

// fixedRegistry lists fixed records and supports nothing else.
type fixedRegistry struct{ records []registry.Record }

func (r fixedRegistry) Add(string) (registry.Record, error) {
	return registry.Record{}, errors.New("unsupported")
}
func (r fixedRegistry) Get(string) (registry.Record, error) {
	return registry.Record{}, errors.New("unsupported")
}
func (r fixedRegistry) Remove(string) error     { return errors.New("unsupported") }
func (r fixedRegistry) List() []registry.Record { return slices.Clone(r.records) }

// failingRegistry fails every operation with err.
type failingRegistry struct{ err error }

func (r failingRegistry) Add(string) (registry.Record, error) { return registry.Record{}, r.err }
func (r failingRegistry) Get(string) (registry.Record, error) { return registry.Record{}, r.err }
func (r failingRegistry) Remove(string) error                 { return r.err }
func (r failingRegistry) List() []registry.Record             { return nil }
