package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SynergyFlowLabs/scopra/internal/localipc"
	"github.com/SynergyFlowLabs/scopra/internal/workspace"
	"github.com/SynergyFlowLabs/scopra/internal/workspace/registry"
)

func TestRunHelpReturnsSuccessSentinel(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run(t.Context(), []string{"--help"}, &stdout, &stderr)
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("run(--help) error = %v, want flag.ErrHelp", err)
	}
	if !strings.Contains(stderr.String(), "usage: scoprad") {
		t.Fatalf("help output = %q, want usage", stderr.String())
	}
}

func TestRunVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := run(t.Context(), []string{"--version"}, &stdout, &stderr); err != nil {
		t.Fatalf("run(--version) error = %v", err)
	}
	if !strings.Contains(stdout.String(), "scoprad") || stdout.Len() == 0 {
		t.Fatalf("version output = %q, want scoprad version", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("version command wrote stderr: %q", stderr.String())
	}
}

func TestRunStopsAndRemovesSocketWhenContextIsCanceled(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("peer credential support is Linux-only in this increment")
	}
	stateDir := filepath.Join(t.TempDir(), "run")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var stdout bytes.Buffer
	stderr := &cancelOnWrite{cancel: cancel}

	if err := run(ctx, []string{"--state-dir", stateDir}, &stdout, stderr); err != nil {
		t.Fatalf("run() error = %v; stderr = %q", err, stderr.String())
	}
	if !strings.Contains(stderr.String(), "listening") {
		t.Fatalf("daemon startup did not report listener startup: %q", stderr.String())
	}
	if _, err := os.Lstat(filepath.Join(stateDir, "scoprad.sock")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("socket was not removed after shutdown (Lstat error = %v)", err)
	}
}

type cancelOnWrite struct {
	cancel context.CancelFunc
	buffer bytes.Buffer
}

func (w *cancelOnWrite) Write(data []byte) (int, error) {
	n, err := w.buffer.Write(data)
	w.cancel()
	return n, err
}

func (w *cancelOnWrite) String() string { return w.buffer.String() }

func TestRunRejectsUnexpectedArguments(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run(t.Context(), []string{"unexpected"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("run() error = nil, want usage error")
	}
}

func TestSecurity_RunRefusesUnusableWorkspaceRegistry(t *testing.T) {
	requireLinux(t)
	tests := []struct {
		name    string
		content func(stateDir string) string
		mode    os.FileMode
		want    error
	}{
		{
			name:    "unsupported version",
			content: func(string) string { return `{"version":2,"workspaces":[]}` },
			mode:    0o600,
			want:    registry.ErrCorruptRegistry,
		},
		{
			name:    "group-readable file",
			content: func(string) string { return `{"version":1,"workspaces":[]}` },
			mode:    0o640,
			want:    registry.ErrInsecureRegistry,
		},
		{
			// For example, a state directory moved into a registered workspace.
			name:    "registry inside a registered workspace",
			content: func(stateDir string) string { return registryWithWorkspace(filepath.Dir(stateDir)) },
			mode:    0o600,
			want:    registry.ErrCorruptRegistry,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stateDir := makeStateDir(t)
			file := filepath.Join(stateDir, "workspaces.json")
			content := tt.content(stateDir)
			writeFile(t, file, content, tt.mode)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var stdout bytes.Buffer
			stderr := &cancelOnWrite{cancel: cancel}

			err := run(ctx, []string{"--state-dir", stateDir}, &stdout, stderr)
			if !errors.Is(err, tt.want) {
				t.Fatalf("run() error = %v, want %v", err, tt.want)
			}
			if strings.Contains(stderr.String(), "listening") {
				t.Fatalf("run() reported listening with an unusable registry: %q", stderr.String())
			}
			requireUnchangedFile(t, file, content, tt.mode)
			if _, err := os.Lstat(filepath.Join(stateDir, "scoprad.sock")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("socket left behind after a refused start (Lstat error = %v)", err)
			}
			daemon, err := localipc.Listen(stateDir, "test-version")
			if err != nil {
				t.Fatalf("Listen() after a refused start error = %v, want the lock released", err)
			}
			_ = daemon.Close()
		})
	}
}

func TestRunStartsWithMissingOrValidWorkspaceRegistry(t *testing.T) {
	requireLinux(t)
	tests := []struct {
		name    string
		content string // empty means no registry file
	}{
		{name: "no registry file"},
		// Workspaces are revalidated before use, so a recorded workspace that
		// no longer exists must not stop Mio from starting.
		{name: "registry naming a missing workspace", content: registryWithWorkspace("/nonexistent/scopra-workspace")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stateDir := makeStateDir(t)
			file := filepath.Join(stateDir, "workspaces.json")
			if tt.content != "" {
				writeFile(t, file, tt.content, 0o600)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var stdout bytes.Buffer
			stderr := &cancelOnWrite{cancel: cancel}

			if err := run(ctx, []string{"--state-dir", stateDir}, &stdout, stderr); err != nil {
				t.Fatalf("run() error = %v; stderr = %q", err, stderr.String())
			}
			if !strings.Contains(stderr.String(), "listening") {
				t.Fatalf("daemon startup did not report listener startup: %q", stderr.String())
			}
			if tt.content != "" {
				requireUnchangedFile(t, file, tt.content, 0o600)
			} else if _, err := os.Lstat(file); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("run() created a registry file (Lstat error = %v)", err)
			}
		})
	}
}

func TestRunServesWorkspaceCommands(t *testing.T) {
	requireLinux(t)
	stateDir := makeStateDir(t)
	workspaceDir := makeStateDir(t) // any owner-only directory outside the state directory
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	stderr := &signalOnWrite{written: make(chan struct{})}
	done := make(chan error, 1)
	go func() { done <- run(ctx, []string{"--state-dir", stateDir}, io.Discard, stderr) }()
	select {
	case <-stderr.written:
	case err := <-done:
		t.Fatalf("run() returned before serving: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("scoprad did not report startup")
	}
	if !strings.Contains(stderr.String(), "listening") {
		t.Fatalf("daemon startup did not report listener startup: %q", stderr.String())
	}

	requestCtx, requestCancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer requestCancel()
	added, err := localipc.AddWorkspace(requestCtx, stateDir, workspaceDir)
	if err != nil {
		t.Fatalf("AddWorkspace() error = %v", err)
	}
	listed, err := localipc.ListWorkspaces(requestCtx, stateDir)
	if err != nil {
		t.Fatalf("ListWorkspaces() error = %v", err)
	}
	if len(listed) != 1 || listed[0] != added || added.Path != workspaceDir {
		t.Fatalf("ListWorkspaces() = %+v, want only %q", listed, workspaceDir)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run() after cancellation error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("scoprad did not stop after cancellation")
	}
}

// signalOnWrite records output and closes written after the first write.
type signalOnWrite struct {
	written chan struct{}
	once    sync.Once
	mu      sync.Mutex
	buffer  bytes.Buffer
}

func (w *signalOnWrite) Write(data []byte) (int, error) {
	w.mu.Lock()
	n, err := w.buffer.Write(data)
	w.mu.Unlock()
	w.once.Do(func() { close(w.written) })
	return n, err
}

func (w *signalOnWrite) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buffer.String()
}

func TestWorkspaceValidatorProtectsStateDirectory(t *testing.T) {
	requireLinux(t)
	stateDir := makeStateDir(t)

	validator, err := newWorkspaceValidator(stateDir)
	if err != nil {
		t.Fatalf("newWorkspaceValidator() error = %v", err)
	}
	// The parent directory contains Mio's socket, lock, and registry.
	if _, err := validator.Inspect(filepath.Dir(stateDir)); !errors.Is(err, workspace.ErrProtectedPath) {
		t.Fatalf("Inspect(parent of the state directory) error = %v, want ErrProtectedPath", err)
	}
}

func requireLinux(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("peer credential support is Linux-only in this increment")
	}
}

// makeStateDir returns a new owner-only state directory with symlinks resolved.
func makeStateDir(t *testing.T) string {
	t.Helper()
	stateDir := filepath.Join(t.TempDir(), "run")
	if err := os.Mkdir(stateDir, 0o700); err != nil {
		t.Fatalf("Mkdir(state dir) error = %v", err)
	}
	resolved, err := filepath.EvalSymlinks(stateDir)
	if err != nil {
		t.Fatalf("EvalSymlinks(state dir) error = %v", err)
	}
	return resolved
}

func registryWithWorkspace(path string) string {
	return `{"version":1,"workspaces":[{"id":"0123456789abcdef0123456789abcdef","path":"` + path +
		`","fingerprint":{"device":1,"inode":2}}]}`
}

func writeFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("Chmod(%q) error = %v", path, err)
	}
}

func requireUnchangedFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("Lstat(%q) error = %v", path, err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != mode {
		t.Fatalf("%s mode = %v, want a regular file with mode %v", path, info.Mode(), mode)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	if string(data) != content {
		t.Fatalf("%s changed to %q, want %q", path, data, content)
	}
}
