package hollow

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRunDelegatedPodmanInfoUsesAnExplicitTransientService(t *testing.T) {
	var gotName string
	var gotArgs []string
	wantOutput := []byte(`{"host":{},"version":{}}`)
	got, err := RunDelegatedPodmanInfo(context.Background(), func(_ context.Context, name string, args ...string) ([]byte, error) {
		gotName = name
		gotArgs = args
		return wantOutput, nil
	}, "/usr/bin/systemd-run", "/usr/bin/podman")
	if err != nil {
		t.Fatalf("RunDelegatedPodmanInfo() error = %v", err)
	}
	if string(got) != string(wantOutput) {
		t.Fatalf("output = %q, want %q", got, wantOutput)
	}
	if gotName != "/usr/bin/systemd-run" {
		t.Fatalf("command = %q, want absolute systemd-run path", gotName)
	}
	wantArgs := []string{
		"--user", "--quiet", "--pipe", "--wait", "--collect",
		"--property=Delegate=cpu cpuset io memory pids",
		"--", "/usr/bin/podman", "info", "--format", "json",
	}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("args = %#v, want %#v", gotArgs, wantArgs)
	}
}

func TestRunDelegatedPodmanInfoFailsClosedWithoutAbsoluteCommands(t *testing.T) {
	run := func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("runner called for invalid command paths")
		return nil, nil
	}
	cases := []struct {
		name       string
		systemdRun string
		podman     string
	}{
		{name: "relative systemd-run", systemdRun: "systemd-run", podman: "/usr/bin/podman"},
		{name: "relative podman", systemdRun: "/usr/bin/systemd-run", podman: "podman"},
		{name: "missing systemd-run", podman: "/usr/bin/podman"},
		{name: "missing podman", systemdRun: "/usr/bin/systemd-run"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if _, err := RunDelegatedPodmanInfo(context.Background(), run, test.systemdRun, test.podman); err == nil {
				t.Fatal("RunDelegatedPodmanInfo() error = nil, want refusal")
			}
		})
	}
}

func TestRunDelegatedPodmanInfoPropagatesCommandFailure(t *testing.T) {
	wantErr := errors.New("systemd-run: Controller cpu is not available")
	_, err := RunDelegatedPodmanInfo(context.Background(), func(context.Context, string, ...string) ([]byte, error) {
		return nil, wantErr
	}, "/usr/bin/systemd-run", "/usr/bin/podman")
	if !errors.Is(err, wantErr) {
		t.Fatalf("RunDelegatedPodmanInfo() error = %v, want %v", err, wantErr)
	}
}

func TestRunDelegatedPodmanInfoDoesNotAcceptRelativePathsByCleaning(t *testing.T) {
	badPath := filepath.Join(".", "usr", "bin", "podman")
	if filepath.IsAbs(badPath) {
		t.Fatalf("test path %q unexpectedly absolute", badPath)
	}
	if _, err := RunDelegatedPodmanInfo(context.Background(), func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("runner called for relative podman path")
		return nil, nil
	}, "/usr/bin/systemd-run", badPath); err == nil {
		t.Fatal("RunDelegatedPodmanInfo() error = nil, want refusal")
	}
}
