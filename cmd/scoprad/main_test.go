package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
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
