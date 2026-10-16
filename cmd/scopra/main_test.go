package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/SynergyFlowLabs/scopra/internal/localipc"
)

func TestRunStatusJSON(t *testing.T) {
	stateDir, stop := startStatusDaemon(t)
	defer stop()

	var stdout, stderr bytes.Buffer
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	if err := run(ctx, []string{"status", "--state-dir", stateDir, "--json"}, &stdout, &stderr); err != nil {
		t.Fatalf("run() error = %v; stderr = %q", err, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("run() wrote unexpected stderr: %q", stderr.String())
	}

	var response localipc.StatusResponse
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("status output is not JSON: %v; output = %q", err, stdout.String())
	}
	if response.Status != "running" || response.DaemonVersion != "test-version" {
		t.Fatalf("status response = %+v, want running test-version", response)
	}
}

func TestRunStatusHumanReadable(t *testing.T) {
	stateDir, stop := startStatusDaemon(t)
	defer stop()

	var stdout, stderr bytes.Buffer
	if err := run(t.Context(), []string{"status", "--state-dir", stateDir}, &stdout, &stderr); err != nil {
		t.Fatalf("run() error = %v; stderr = %q", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "running") || !strings.Contains(stdout.String(), "test-version") {
		t.Fatalf("human status = %q, want running state and version", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("run() wrote unexpected stderr: %q", stderr.String())
	}
}

func TestRunRejectsUnknownCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run(t.Context(), []string{"execute"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("run() error = nil, want unknown-command error")
	}
	if stdout.Len() != 0 {
		t.Fatalf("run() wrote unexpected stdout: %q", stdout.String())
	}
}

func TestRunRejectsUnknownFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run(t.Context(), []string{"status", "--unknown"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("run() error = nil, want unknown-flag error")
	}
}

func TestRunHelpReturnsSuccessSentinel(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run(t.Context(), []string{"--help"}, &stdout, &stderr)
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("run(--help) error = %v, want flag.ErrHelp", err)
	}
	if !strings.Contains(stderr.String(), "usage: scopra") {
		t.Fatalf("help output = %q, want usage", stderr.String())
	}
}

func startStatusDaemon(t *testing.T) (string, func()) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("peer credential support is Linux-only in this increment")
	}
	stateDir := filepath.Join(t.TempDir(), "run")
	daemon, err := localipc.Listen(stateDir, "test-version")
	if err != nil {
		t.Fatalf("localipc.Listen() error = %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- daemon.Serve(ctx) }()
	return stateDir, func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("daemon Serve() error = %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Error("daemon did not stop")
		}
	}
}
