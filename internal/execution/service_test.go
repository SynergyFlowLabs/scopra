package execution

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/SynergyFlowLabs/scopra/internal/hollow"
	"github.com/SynergyFlowLabs/scopra/internal/workspace"
	"github.com/SynergyFlowLabs/scopra/internal/workspace/registry"
	"github.com/SynergyFlowLabs/scopra/packages/protocol"
)

const testWorkspaceID = "0123456789abcdef0123456789abcdef"

// testRecord is the registration that a fake registry returns.
var testRecord = registry.Record{
	ID: testWorkspaceID,
	Workspace: workspace.Workspace{
		Path:        "/home/dev/project",
		Fingerprint: workspace.Fingerprint{Device: 1, Inode: 2},
	},
}

// testRequest returns a valid ADR-007 request as Mio builds one for a local
// run (ADR-011).
func testRequest(t *testing.T) protocol.ExecutionRequest {
	t.Helper()
	request := protocol.ExecutionRequest{
		ExecutionID: "local-0123456789abcdef0123456789abcdef",
		Principals:  protocol.Principals{UserID: "local:1000", AgentID: "local", ClientID: "local-socket"},
		Resources:   protocol.Resources{MachineID: "local", WorkspaceID: testWorkspaceID},
		GrantID:     "local",
		SessionID:   "local",
		Command: protocol.Command{
			Executable:       "go",
			Arguments:        []string{"test", "./..."},
			WorkingDirectory: ".",
		},
		WorkspaceAccess: protocol.WorkspaceReadOnly,
		Network:         protocol.NetworkNone,
		Limits: protocol.Limits{
			TimeoutSeconds: 600,
			CPUMillicores:  2000,
			MemoryBytes:    2 << 30,
			PIDs:           512,
			OutputBytes:    16 << 20,
		},
		Reason:    "local run by the workspace owner",
		CreatedAt: time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC),
		ExpiresAt: time.Date(2026, 10, 17, 13, 0, 0, 0, time.UTC),
		Nonce:     "AAAAAAAAAAAAAAAAAAAAAA",
	}
	if err := request.Validate(); err != nil {
		t.Fatalf("the test request is invalid: %v", err)
	}
	return request
}

// succeededResult returns a valid succeeded result for request, which the
// fake runner returns.
func succeededResult(t *testing.T, request protocol.ExecutionRequest) protocol.ExecutionResult {
	t.Helper()
	hash, err := request.ActionHash()
	if err != nil {
		t.Fatalf("ActionHash() error = %v", err)
	}
	return protocol.ExecutionResult{
		ExecutionID: request.ExecutionID,
		ActionHash:  hash,
		State:       protocol.StateSucceeded,
		StartedAt:   time.Date(2026, 10, 17, 12, 0, 1, 250_000_000, time.UTC),
		EndedAt:     time.Date(2026, 10, 17, 12, 0, 2, 500_000_000, time.UTC),
		Output:      protocol.OutputSummary{StdoutBytes: 6, Chunks: 1},
	}
}

func startNotice(t *testing.T, request protocol.ExecutionRequest) protocol.ExecutionStarted {
	t.Helper()
	hash, err := request.ActionHash()
	if err != nil {
		t.Fatalf("ActionHash() error = %v", err)
	}
	return protocol.ExecutionStarted{
		ExecutionID: request.ExecutionID,
		ActionHash:  hash,
		StartedAt:   time.Date(2026, 10, 17, 12, 0, 1, 250_000_000, time.UTC),
	}
}

func TestService_Execute_RunsTheRecordedWorkspaceInHollow(t *testing.T) {
	request := testRequest(t)
	want := succeededResult(t, request)
	store := &fakeRegistry{record: testRecord}
	validator := &fakeValidator{}
	var gotEvents []string
	runner := &fakeRunner{run: func(_ context.Context, _ protocol.ExecutionRequest, _ string, events hollow.Events) (protocol.ExecutionResult, error) {
		events.Started(startNotice(t, request))
		events.Output(protocol.ExecutionOutput{
			ExecutionID: request.ExecutionID,
			Sequence:    1,
			Stream:      protocol.StreamStdout,
			Data:        []byte("hello\n"),
		})
		return want, nil
	}}
	service := &Service{Workspaces: store, Validator: validator, Runner: runner}

	result, err := service.Execute(t.Context(), request, Events{
		Started: func(protocol.ExecutionStarted) { gotEvents = append(gotEvents, "started") },
		Output: func(chunk protocol.ExecutionOutput) {
			gotEvents = append(gotEvents, fmt.Sprintf("%s:%d", chunk.Stream, len(chunk.Data)))
		},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !reflect.DeepEqual(result, want) {
		t.Fatalf("Execute() = %+v, want Hollow's result %+v", result, want)
	}
	if err := result.ValidateAgainst(request); err != nil {
		t.Fatalf("the result does not belong to the request: %v", err)
	}
	if got := strings.Join(gotEvents, "|"); got != "started|stdout:6" {
		t.Fatalf("events = %s, want the runner's events in order", got)
	}
	if len(runner.paths) != 1 || runner.paths[0] != testRecord.Path {
		t.Fatalf("Runner.Run() paths = %q, want only %q", runner.paths, testRecord.Path)
	}
	if len(runner.requests) != 1 || !reflect.DeepEqual(runner.requests[0], request) {
		t.Fatalf("Runner.Run() request = %+v, want the request unchanged", runner.requests)
	}
	if len(validator.checked) != 1 || validator.checked[0] != testRecord.Workspace {
		t.Fatalf("Revalidate() checked %+v, want the recorded workspace once", validator.checked)
	}
}

func TestService_Execute_RejectsAnUnknownWorkspace(t *testing.T) {
	request := testRequest(t)
	store := &fakeRegistry{err: fmt.Errorf("%w: %q", registry.ErrNotFound, testWorkspaceID)}
	runner := &fakeRunner{}
	service := &Service{Workspaces: store, Validator: &fakeValidator{}, Runner: runner}

	result, err := service.Execute(t.Context(), request, Events{})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.State != protocol.StateRejected ||
		result.Error.Code != protocol.ErrorCodeWorkspaceUnavailable ||
		result.Error.Message != "the workspace is not registered" {
		t.Fatalf("Execute() = %+v, want a workspace_unavailable rejection", result)
	}
	if err := result.ValidateAgainst(request); err != nil {
		t.Fatalf("the rejection is not a valid result: %v", err)
	}
	if len(runner.paths) != 0 {
		t.Fatalf("Runner.Run() ran with %q, want no run", runner.paths)
	}
}

func TestService_Execute_RejectsAWorkspaceThatFailedRevalidation(t *testing.T) {
	request := testRequest(t)
	store := &fakeRegistry{record: testRecord}
	validator := &fakeValidator{err: fmt.Errorf("%w: the directory was replaced", workspace.ErrChanged)}
	runner := &fakeRunner{}
	service := &Service{Workspaces: store, Validator: validator, Runner: runner}

	result, err := service.Execute(t.Context(), request, Events{})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.State != protocol.StateRejected ||
		result.Error.Code != protocol.ErrorCodeWorkspaceUnavailable ||
		result.Error.Message != "the workspace failed revalidation" {
		t.Fatalf("Execute() = %+v, want a workspace_unavailable rejection", result)
	}
	if err := result.ValidateAgainst(request); err != nil {
		t.Fatalf("the rejection is not a valid result: %v", err)
	}
	if len(runner.paths) != 0 {
		t.Fatalf("Runner.Run() ran with %q, want no run", runner.paths)
	}
}

func TestService_Execute_RejectsWhenContainmentIsUnavailable(t *testing.T) {
	request := testRequest(t)
	service := &Service{
		Workspaces:  &fakeRegistry{record: testRecord},
		Validator:   &fakeValidator{},
		Unavailable: "no sandbox image is configured",
	}

	result, err := service.Execute(t.Context(), request, Events{})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.State != protocol.StateRejected ||
		result.Error.Code != protocol.ErrorCodeContainmentUnavailable ||
		result.Error.Message != "no sandbox image is configured" {
		t.Fatalf("Execute() = %+v, want a containment_unavailable rejection with the reason", result)
	}
	if err := result.ValidateAgainst(request); err != nil {
		t.Fatalf("the rejection is not a valid result: %v", err)
	}
}

func TestService_Execute_RejectsWithoutARunnerOrReason(t *testing.T) {
	request := testRequest(t)
	service := &Service{Workspaces: &fakeRegistry{record: testRecord}, Validator: &fakeValidator{}}

	result, err := service.Execute(t.Context(), request, Events{})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.State != protocol.StateRejected ||
		result.Error.Code != protocol.ErrorCodeContainmentUnavailable ||
		result.Error.Message != "containment is unavailable" {
		t.Fatalf("Execute() = %+v, want the generic containment_unavailable rejection", result)
	}
}

func TestSecurity_Service_ChecksTheWorkspaceBeforeContainment(t *testing.T) {
	request := testRequest(t)
	service := &Service{
		Workspaces:  &fakeRegistry{err: registry.ErrNotFound},
		Validator:   &fakeValidator{},
		Unavailable: "podman was not found",
	}

	result, err := service.Execute(t.Context(), request, Events{})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Error.Code != protocol.ErrorCodeWorkspaceUnavailable {
		t.Fatalf("Execute() error code = %q, want workspace_unavailable first", result.Error.Code)
	}
}

func TestService_Execute_ReportsARunnerFailure(t *testing.T) {
	request := testRequest(t)
	wantErr := errors.New("podman failed")
	runner := &fakeRunner{run: func(context.Context, protocol.ExecutionRequest, string, hollow.Events) (protocol.ExecutionResult, error) {
		return protocol.ExecutionResult{}, wantErr
	}}
	service := &Service{Workspaces: &fakeRegistry{record: testRecord}, Validator: &fakeValidator{}, Runner: runner}

	result, err := service.Execute(t.Context(), request, Events{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Execute() error = %v, want the runner's error", err)
	}
	if result != (protocol.ExecutionResult{}) {
		t.Fatalf("Execute() = %+v with an error, want no invented result", result)
	}
}

func TestService_Execute_PassesTheContextToHollow(t *testing.T) {
	type contextKey struct{}
	request := testRequest(t)
	ctx := context.WithValue(t.Context(), contextKey{}, "sentinel")
	seen := false
	runner := &fakeRunner{run: func(runCtx context.Context, _ protocol.ExecutionRequest, _ string, _ hollow.Events) (protocol.ExecutionResult, error) {
		seen = runCtx.Value(contextKey{}) == "sentinel"
		return succeededResult(t, request), nil
	}}
	service := &Service{Workspaces: &fakeRegistry{record: testRecord}, Validator: &fakeValidator{}, Runner: runner}

	if _, err := service.Execute(ctx, request, Events{}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !seen {
		t.Fatal("Hollow did not receive the caller's context")
	}
}

func TestSecurity_Service_RequiresItsDependencies(t *testing.T) {
	request := testRequest(t)
	tests := map[string]struct {
		service *Service
		ctx     context.Context
	}{
		"nil service":   {service: nil, ctx: context.Background()},
		"nil context":   {service: &Service{Workspaces: &fakeRegistry{}, Validator: &fakeValidator{}}, ctx: nil},
		"nil registry":  {service: &Service{Validator: &fakeValidator{}}, ctx: context.Background()},
		"nil validator": {service: &Service{Workspaces: &fakeRegistry{}}, ctx: context.Background()},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := test.service.Execute(test.ctx, request, Events{}); !errors.Is(err, ErrInvalidService) {
				t.Fatalf("Execute() error = %v, want ErrInvalidService", err)
			}
		})
	}
}

// fakeRegistry is a workspace registry with one scripted answer.
type fakeRegistry struct {
	record registry.Record
	err    error
	ids    []string
}

func (f *fakeRegistry) Get(id string) (registry.Record, error) {
	f.ids = append(f.ids, id)
	if f.err != nil {
		return registry.Record{}, f.err
	}
	return f.record, nil
}

// fakeValidator records the workspaces it checks and returns one scripted
// error.
type fakeValidator struct {
	err     error
	checked []workspace.Workspace
}

func (f *fakeValidator) Revalidate(recorded workspace.Workspace) error {
	f.checked = append(f.checked, recorded)
	return f.err
}

// fakeRunner records the runs it receives and runs run.
type fakeRunner struct {
	run      func(context.Context, protocol.ExecutionRequest, string, hollow.Events) (protocol.ExecutionResult, error)
	paths    []string
	requests []protocol.ExecutionRequest
}

func (f *fakeRunner) Run(ctx context.Context, request protocol.ExecutionRequest, path string, events hollow.Events) (protocol.ExecutionResult, error) {
	f.paths = append(f.paths, path)
	f.requests = append(f.requests, request)
	return f.run(ctx, request, path, events)
}

func TestSecurity_Service_RefusesWhenContainmentIsMarkedUnavailable(t *testing.T) {
	request := testRequest(t)
	runner := &fakeRunner{run: func(context.Context, protocol.ExecutionRequest, string, hollow.Events) (protocol.ExecutionResult, error) {
		t.Fatal("Runner.Run() was called although containment is unavailable")
		return protocol.ExecutionResult{}, nil
	}}
	service := &Service{
		Workspaces:  &fakeRegistry{record: testRecord},
		Validator:   &fakeValidator{},
		Runner:      runner,
		Unavailable: "leftover containers could not be removed",
	}

	result, err := service.Execute(t.Context(), request, Events{})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.State != protocol.StateRejected ||
		result.Error.Code != protocol.ErrorCodeContainmentUnavailable ||
		result.Error.Message != "leftover containers could not be removed" {
		t.Fatalf("Execute() = %+v, want the unavailable reason to win", result)
	}
}

func TestSecurity_Service_RevalidatesBeforeCheckingContainment(t *testing.T) {
	request := testRequest(t)
	validator := &fakeValidator{err: fmt.Errorf("%w: the directory was replaced", workspace.ErrChanged)}
	service := &Service{
		Workspaces:  &fakeRegistry{record: testRecord},
		Validator:   validator,
		Unavailable: "podman was not found",
	}

	result, err := service.Execute(t.Context(), request, Events{})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Error.Code != protocol.ErrorCodeWorkspaceUnavailable {
		t.Fatalf("Execute() error code = %q, want workspace_unavailable first", result.Error.Code)
	}
	if len(validator.checked) != 1 {
		t.Fatalf("Revalidate() ran %d times, want once", len(validator.checked))
	}
}

func TestService_Execute_LooksUpTheRequestsWorkspaceID(t *testing.T) {
	request := testRequest(t)
	store := &fakeRegistry{record: testRecord}
	runner := &fakeRunner{run: func(context.Context, protocol.ExecutionRequest, string, hollow.Events) (protocol.ExecutionResult, error) {
		return succeededResult(t, request), nil
	}}
	service := &Service{Workspaces: store, Validator: &fakeValidator{}, Runner: runner}

	if _, err := service.Execute(t.Context(), request, Events{}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(store.ids) != 1 || store.ids[0] != request.Resources.WorkspaceID {
		t.Fatalf("registry lookups = %q, want the request's workspace ID", store.ids)
	}
}

func TestSecurity_RejectNeverInventsAResultForAnInvalidRequest(t *testing.T) {
	result, err := reject(protocol.ExecutionRequest{}, protocol.ErrorCodeWorkspaceUnavailable, "the workspace is not registered")
	if err == nil {
		t.Fatalf("reject() = %+v, want an error for an invalid request", result)
	}
	if result != (protocol.ExecutionResult{}) {
		t.Fatalf("reject() = %+v with an error, want no result", result)
	}
}
