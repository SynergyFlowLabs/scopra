package installer

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestManualInstallRefusesAnUndelegatedCurrentContext(t *testing.T) {
	model := newTestModel()
	model.installService = false
	created := false
	runner := &Runner{
		Model: model,
		Base: Options{
			Scoprad: "/tmp/scoprad",
			MkdirAll: func(string, os.FileMode) error {
				created = true
				return nil
			},
		},
		Env: Environment{
			ProbeCurrentPodmanInfo: func(context.Context) ([]byte, error) {
				return []byte(`{"host":{"cgroupVersion":"v2","cgroupControllers":["memory","pids"],"security":{"rootless":true}},"version":{"Version":"5.7.0"}}`), nil
			},
		},
	}
	messages := make(chan Message, 2)
	runner.runInstall(context.Background(), messages)

	step := <-messages
	if step.Step == nil || step.Step.Title != "Verify manual daemon context" || step.Step.Status != StatusFailed {
		t.Fatalf("first message = %#v, want a failed current-context check", step)
	}
	result := <-messages
	if result.Err == nil || !strings.Contains(result.Err.Error(), "cgroup controllers not delegated: cpu") || !strings.Contains(result.Err.Error(), "choose Scopra's delegated systemd service") {
		t.Fatalf("result error = %v, want a fail-closed service-context explanation", result.Err)
	}
	if created {
		t.Fatal("installer wrote files before the manual daemon context passed")
	}
}

func TestManualInstallDoesNotClaimReadinessWhenCurrentProbeFails(t *testing.T) {
	model := newTestModel()
	runner := &Runner{
		Model: model,
		Base:  Options{Scoprad: "/tmp/scoprad"},
		Env: Environment{
			ProbeCurrentPodmanInfo: func(context.Context) ([]byte, error) {
				return nil, errors.New("systemd-run unavailable")
			},
		},
	}
	messages := make(chan Message, 2)
	runner.runInstall(context.Background(), messages)

	step := <-messages
	if step.Step == nil || step.Step.Status != StatusFailed {
		t.Fatalf("manual context step = %#v, want failed", step)
	}
	result := <-messages
	if result.Err == nil || !strings.Contains(result.Err.Error(), "current cgroup") {
		t.Fatalf("result error = %v, want current-context failure", result.Err)
	}
}
