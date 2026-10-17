package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SynergyFlowLabs/scopra/internal/installer"
)

const installTestPodmanInfo = `{"host":{"cgroupVersion":"v2","cgroupControllers":["cpu","memory","pids"],"security":{"rootless":true}},"version":{"Version":"5.7.0"}}`

func installTestEnvironment(currentInfo string) installer.Environment {
	return installer.Environment{
		GOOS:     "linux",
		StateDir: "/state",
		Run: func(_ context.Context, name string, args ...string) ([]byte, error) {
			if name == "systemctl" && strings.Join(args, " ") == "--user show-environment" {
				return []byte("PATH=/usr/bin\n"), nil
			}
			return nil, errors.New("unexpected command")
		},
		ProbePodmanInfo: func(context.Context) ([]byte, error) {
			return []byte(installTestPodmanInfo), nil
		},
		ProbeCurrentPodmanInfo: func(context.Context) ([]byte, error) {
			return []byte(currentInfo), nil
		},
	}
}

func writeInstallSource(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("executable"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestPlainNoServiceInstallRefusesAnUndelegatedDaemonContext(t *testing.T) {
	sourceDir := t.TempDir()
	scopra := filepath.Join(sourceDir, "scopra")
	scoprad := filepath.Join(sourceDir, "scoprad")
	writeInstallSource(t, scopra)
	writeInstallSource(t, scoprad)

	output := `{"host":{"cgroupVersion":"v2","cgroupControllers":["memory","pids"],"security":{"rootless":true}},"version":{"Version":"5.7.0"}}`
	dest := filepath.Join(t.TempDir(), "bin")
	var stdout, stderr bytes.Buffer
	err := runInstallPlainWithEnvironment(context.Background(), installer.Options{
		Version: "test",
		Dir:     dest,
		Scopra:  scopra,
		Scoprad: scoprad,
		Service: false,
	}, &stdout, &stderr, installTestEnvironment(output))
	if err == nil || !strings.Contains(err.Error(), "cgroup controllers not delegated: cpu") {
		t.Fatalf("install error = %v, want an undelegated current-context failure", err)
	}
	if !strings.Contains(stdout.String(), "Podman in the current process context") || !strings.Contains(stdout.String(), "(delegated service)") {
		t.Fatalf("install output = %q, want both contexts visible", stdout.String())
	}
	if _, err := os.Stat(dest); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("install directory exists after a failed current-context check: %v", err)
	}
}

func TestPlainNoServiceInstallPassesWhenBothContextsAreReady(t *testing.T) {
	sourceDir := t.TempDir()
	scopra := filepath.Join(sourceDir, "scopra")
	scoprad := filepath.Join(sourceDir, "scoprad")
	writeInstallSource(t, scopra)
	writeInstallSource(t, scoprad)
	dest := filepath.Join(t.TempDir(), "bin")

	var stdout, stderr bytes.Buffer
	err := runInstallPlainWithEnvironment(context.Background(), installer.Options{
		Version: "test",
		Dir:     dest,
		Scopra:  scopra,
		Scoprad: scoprad,
		Service: false,
	}, &stdout, &stderr, installTestEnvironment(installTestPodmanInfo))
	if err != nil {
		t.Fatalf("install error = %v; stdout = %q; stderr = %q", err, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "Podman in the current process context: podman 5.7.0") {
		t.Fatalf("install output = %q, want the current-context check", stdout.String())
	}
	for _, name := range []string{"scopra", "scoprad"} {
		if _, err := os.Stat(filepath.Join(dest, name)); err != nil {
			t.Fatalf("installed %s: %v", name, err)
		}
	}
}
