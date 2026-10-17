package hollow

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/SynergyFlowLabs/scopra/packages/protocol"
)

const (
	testImage     = "registry.example/scopra/runner@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	testWorkspace = "/home/alice/projects/api"
	testPath      = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
)

// fixtureRequest decodes a valid request from the shared Messenger v1
// reference payloads.
func fixtureRequest(t testing.TB, name string) protocol.ExecutionRequest {
	t.Helper()
	payload, err := os.ReadFile(filepath.Join("..", "..", "packages", "test-fixtures", "messenger", "v1", "valid", name+".json"))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	request, err := protocol.DecodeExecutionRequest(payload)
	if err != nil {
		t.Fatalf("decode fixture %s: %v", name, err)
	}
	return request
}

func testSpec(t testing.TB) RunSpec {
	t.Helper()
	return RunSpec{Request: fixtureRequest(t, "execution-request-adr007"), WorkspacePath: testWorkspace, Image: testImage}
}

func TestPodmanRunArgs_ADR007Example(t *testing.T) {
	spec := testSpec(t)
	args, err := PodmanRunArgs(spec)
	if err != nil {
		t.Fatalf("PodmanRunArgs() error = %v", err)
	}
	want := []string{
		"run",
		"--name=scopra-b4f47c942fc6499aea49d76df1f89c63",
		"--label=scopra.action-hash=b4f47c942fc6499aea49d76df1f89c630f8683bd69c98f292b9e03eed4dd44e2",
		"--pull=never",
		"--log-driver=passthrough",
		"--network=none",
		"--userns=keep-id",
		"--cap-drop=all",
		"--security-opt=no-new-privileges",
		"--security-opt=label=disable",
		"--read-only",
		"--tmpfs=/tmp:rw,exec,nosuid,nodev,mode=1777,size=2147483648",
		`--mount=type=bind,"src=/home/alice/projects/api",dst=/workspace,ro=true`,
		"--workdir=/workspace",
		"--unsetenv-all",
		"--env=PATH=" + testPath,
		"--env=HOME=/tmp",
		"--init",
		"--memory=2147483648b",
		"--memory-swap=2147483648b",
		"--cpus=2",
		"--pids-limit=512",
		"--timeout=610",
		`--entrypoint=["go"]`,
		testImage,
		"test", "./...",
	}
	if !slices.Equal(args, want) {
		t.Fatalf("PodmanRunArgs() =\n%q\nwant\n%q", args, want)
	}
	checkInvariants(t, spec, args)
}

func TestPodmanRunArgs_ADR008Vector2(t *testing.T) {
	spec := testSpec(t)
	spec.Request = fixtureRequest(t, "execution-request-adr008-vector2")
	args, err := PodmanRunArgs(spec)
	if err != nil {
		t.Fatalf("PodmanRunArgs() error = %v", err)
	}
	want := []string{
		"run",
		"--name=scopra-9df45c503097070244bdcf27d06fb6c6",
		"--label=scopra.action-hash=9df45c503097070244bdcf27d06fb6c66e9328ee908d234ae03f3831a5731533",
		"--pull=never",
		"--log-driver=passthrough",
		"--network=none",
		"--userns=keep-id",
		"--cap-drop=all",
		"--security-opt=no-new-privileges",
		"--security-opt=label=disable",
		"--read-only",
		"--tmpfs=/tmp:rw,exec,nosuid,nodev,mode=1777,size=1099511627776",
		`--mount=type=bind,"src=/home/alice/projects/api",dst=/workspace,ro=false`,
		"--workdir=/workspace/src/app",
		"--unsetenv-all",
		"--env=PATH=" + testPath,
		"--env=HOME=/tmp",
		"--init",
		"--memory=1099511627776b",
		"--memory-swap=1099511627776b",
		"--cpus=2",
		"--pids-limit=512",
		"--timeout=610",
		`--entrypoint=["./scripts/test.sh"]`,
		testImage,
		"tab\there", "new\nline", "\u0001", "\u007f", "\u2028", "quote\" back\\slash", "é", "😀", "",
	}
	if !slices.Equal(args, want) {
		t.Fatalf("PodmanRunArgs() =\n%q\nwant\n%q", args, want)
	}
	checkInvariants(t, spec, args)
}

func TestSecurity_PodmanRunArgsRejectsUnsafeInputs(t *testing.T) {
	digest := strings.Repeat("a", 64)
	tests := []struct {
		name    string
		change  func(*RunSpec)
		wantErr error
	}{
		{"invalid request", func(s *RunSpec) { s.Request.Command.Shell = true }, protocol.ErrInvalidExecutionRequest},
		{"zero request", func(s *RunSpec) { s.Request = protocol.ExecutionRequest{} }, protocol.ErrInvalidExecutionRequest},
		{"empty workspace path", func(s *RunSpec) { s.WorkspacePath = "" }, ErrUnmountableWorkspace},
		{"relative workspace path", func(s *RunSpec) { s.WorkspacePath = "home/alice/api" }, ErrUnmountableWorkspace},
		{"filesystem root", func(s *RunSpec) { s.WorkspacePath = "/" }, ErrUnmountableWorkspace},
		{"unclean workspace path", func(s *RunSpec) { s.WorkspacePath = "/home/alice/../bob" }, ErrUnmountableWorkspace},
		{"trailing slash", func(s *RunSpec) { s.WorkspacePath = "/home/alice/api/" }, ErrUnmountableWorkspace},
		{"newline in the workspace path", func(s *RunSpec) { s.WorkspacePath = "/home/alice/a\nb" }, ErrUnmountableWorkspace},
		{"carriage return in the workspace path", func(s *RunSpec) { s.WorkspacePath = "/home/alice/a\rb" }, ErrUnmountableWorkspace},
		{"NUL in the workspace path", func(s *RunSpec) { s.WorkspacePath = "/home/alice/a\x00b" }, ErrUnmountableWorkspace},
		{"DEL in the workspace path", func(s *RunSpec) { s.WorkspacePath = "/home/alice/a\x7fb" }, ErrUnmountableWorkspace},
		{"invalid UTF-8 in the workspace path", func(s *RunSpec) { s.WorkspacePath = "/home/alice/\xff" }, ErrUnmountableWorkspace},
		{"workspace path over 4095 bytes", func(s *RunSpec) { s.WorkspacePath = "/" + strings.Repeat("a", 4095) }, ErrUnmountableWorkspace},
		{"empty image", func(s *RunSpec) { s.Image = "" }, ErrInvalidImage},
		{"image without a digest", func(s *RunSpec) { s.Image = "registry.example/scopra/runner:1.0" }, ErrInvalidImage},
		{"short digest", func(s *RunSpec) { s.Image = "runner@sha256:" + digest[:63] }, ErrInvalidImage},
		{"long digest", func(s *RunSpec) { s.Image = "runner@sha256:" + digest + "a" }, ErrInvalidImage},
		{"uppercase digest", func(s *RunSpec) { s.Image = "runner@sha256:" + strings.ToUpper(digest) }, ErrInvalidImage},
		{"another digest algorithm", func(s *RunSpec) { s.Image = "runner@sha512:" + digest }, ErrInvalidImage},
		{"image that looks like an option", func(s *RunSpec) { s.Image = "--privileged@sha256:" + digest }, ErrInvalidImage},
		{"image with a space", func(s *RunSpec) { s.Image = "run ner@sha256:" + digest }, ErrInvalidImage},
		{"image without a name", func(s *RunSpec) { s.Image = "@sha256:" + digest }, ErrInvalidImage},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			spec := testSpec(t)
			test.change(&spec)
			args, err := PodmanRunArgs(spec)
			if !errors.Is(err, test.wantErr) || args != nil {
				t.Fatalf("PodmanRunArgs() = %q, %v; want nil and %v", args, err, test.wantErr)
			}
		})
	}
	for _, image := range []string{
		"runner@sha256:" + digest,
		"registry.example:5000/team/runner:1.2@sha256:" + digest,
		"Registry.Example/runner_x.y-z@sha256:" + digest,
	} {
		spec := testSpec(t)
		spec.Image = image
		if _, err := PodmanRunArgs(spec); err != nil {
			t.Errorf("PodmanRunArgs() with image %q error = %v, want acceptance", image, err)
		}
	}
}

func TestPodmanRunArgs_QuotesTheMountSourceAsCSV(t *testing.T) {
	for _, workspace := range []string{
		"/home/alice/a,b",
		`/home/alice/a"b`,
		`/home/alice/""`,
		"/home/alice/a:b",
		"/home/alice/a=b",
		"/home/alice/a b",
		"/home/alice/ü/😀",
		`/home/alice/x",dst=/etc,ro=false,"src=/`,
	} {
		t.Run(workspace, func(t *testing.T) {
			spec := testSpec(t)
			spec.WorkspacePath = workspace
			args, err := PodmanRunArgs(spec)
			if err != nil {
				t.Fatalf("PodmanRunArgs() error = %v", err)
			}
			checkInvariants(t, spec, args)
		})
	}
}

func TestPodmanRunArgs_MapsLimitsExactly(t *testing.T) {
	for millicores, want := range map[uint64]string{
		1: "0.001", 10: "0.01", 999: "0.999", 1000: "1", 1001: "1.001", 1250: "1.25", 1500: "1.5", 2000: "2", 1000000: "1000",
	} {
		spec := testSpec(t)
		spec.Request.Limits.CPUMillicores = millicores
		args, err := PodmanRunArgs(spec)
		if err != nil {
			t.Fatalf("PodmanRunArgs() with %d millicores error = %v", millicores, err)
		}
		if !slices.Contains(args, "--cpus="+want) {
			t.Errorf("PodmanRunArgs() with %d millicores = %q, want --cpus=%s", millicores, args, want)
		}
	}
	spec := testSpec(t)
	spec.Request.Limits = protocol.Limits{TimeoutSeconds: 86400, CPUMillicores: 1, MemoryBytes: 1 << 20, PIDs: 65536, OutputBytes: 1}
	args, err := PodmanRunArgs(spec)
	if err != nil {
		t.Fatalf("PodmanRunArgs() error = %v", err)
	}
	for _, want := range []string{
		"--timeout=86410", "--memory=1048576b", "--memory-swap=1048576b", "--pids-limit=65536",
		"--tmpfs=/tmp:rw,exec,nosuid,nodev,mode=1777,size=1048576",
	} {
		if !slices.Contains(args, want) {
			t.Errorf("PodmanRunArgs() = %q, want it to contain %s", args, want)
		}
	}
}

func TestSecurity_PodmanRunArgsKeepsArgumentsAfterTheImage(t *testing.T) {
	spec := testSpec(t)
	spec.Request.Command.Arguments = []string{"--privileged", "-v", "/:/host", "--network=host", "--entrypoint=sh", testImage}
	args, err := PodmanRunArgs(spec)
	if err != nil {
		t.Fatalf("PodmanRunArgs() error = %v", err)
	}
	checkInvariants(t, spec, args)
}

func FuzzPodmanRunArgs(f *testing.F) {
	f.Add("go", "./...", ".", testWorkspace, false)
	f.Add("./scripts/test.sh", "--privileged", "src/app", `/home/alice/a",b`, true)
	f.Add("/usr/bin/env", "-v", "a/b", "/home/alice/a\nb", false)
	f.Fuzz(func(t *testing.T, executable, argument, workingDirectory, workspacePath string, readWrite bool) {
		spec := testSpec(t)
		spec.Request.Command.Executable = executable
		spec.Request.Command.Arguments = []string{argument}
		spec.Request.Command.WorkingDirectory = workingDirectory
		spec.Request.WorkspaceAccess = protocol.WorkspaceReadOnly
		if readWrite {
			spec.Request.WorkspaceAccess = protocol.WorkspaceReadWrite
		}
		spec.WorkspacePath = workspacePath
		args, err := PodmanRunArgs(spec)
		if err != nil {
			if !errors.Is(err, protocol.ErrInvalidExecutionRequest) && !errors.Is(err, ErrUnmountableWorkspace) {
				t.Fatalf("PodmanRunArgs() error %v is not a request or workspace error", err)
			}
			return
		}
		checkInvariants(t, spec, args)
	})
}

// forbiddenPrefixes are options that ADR-010 never passes.
var forbiddenPrefixes = []string{
	"--privileged", "--cap-add", "--device", "--volume", "--pid", "--ipc", "--uts", "--group-add",
	"--env-host", "--env-file", "--secret", "--interactive", "--tty", "--rm", "--publish", "--add-host",
}

// checkInvariants verifies ADR-010's shape for any accepted spec.
func checkInvariants(t *testing.T, spec RunSpec, args []string) {
	t.Helper()
	arguments := spec.Request.Command.Arguments
	image := len(args) - len(arguments) - 1
	if image < 1 || args[0] != "run" || args[image] != spec.Image || !slices.Equal(args[image+1:], arguments) {
		t.Fatalf("args = %q; want run, options, the image, then exactly the arguments %q", args, arguments)
	}
	counts := map[string]int{}
	var securityOpts, envs []string
	var mount, entrypoint string
	for _, option := range args[1:image] {
		if !strings.HasPrefix(option, "--") {
			t.Fatalf("option %q does not start with --", option)
		}
		name, value, _ := strings.Cut(option, "=")
		counts[name]++
		for _, forbidden := range forbiddenPrefixes {
			if name == forbidden {
				t.Fatalf("forbidden option %q in %q", option, args)
			}
		}
		switch name {
		case "--security-opt":
			securityOpts = append(securityOpts, value)
		case "--env":
			envs = append(envs, value)
		case "--mount":
			mount = value
		case "--entrypoint":
			entrypoint = value
		}
	}
	for _, once := range []string{"--name", "--label", "--pull", "--log-driver", "--network", "--userns", "--cap-drop",
		"--read-only", "--tmpfs", "--mount", "--workdir", "--unsetenv-all", "--init", "--memory", "--memory-swap",
		"--cpus", "--pids-limit", "--timeout", "--entrypoint"} {
		if counts[once] != 1 {
			t.Fatalf("%s appears %d times in %q, want once", once, counts[once], args)
		}
	}
	for _, exact := range []string{"--pull=never", "--log-driver=passthrough", "--network=none", "--userns=keep-id", "--cap-drop=all"} {
		if !slices.Contains(args[1:image], exact) {
			t.Fatalf("args = %q, want %s", args, exact)
		}
	}
	if !slices.Equal(securityOpts, []string{"no-new-privileges", "label=disable"}) {
		t.Fatalf("security options = %q, want exactly no-new-privileges and label=disable", securityOpts)
	}
	if !slices.Equal(envs, []string{"PATH=" + testPath, "HOME=/tmp"}) {
		t.Fatalf("environment = %q, want exactly the fixed PATH and HOME", envs)
	}
	// Parse the mount the way Podman does: CSV fields, each split at its first "=".
	fields, err := csv.NewReader(strings.NewReader(mount)).ReadAll()
	if err != nil || len(fields) != 1 || len(fields[0]) != 4 {
		t.Fatalf("mount %q parses as %q, %v; want one record of four fields", mount, fields, err)
	}
	readOnly := "true"
	if spec.Request.WorkspaceAccess == protocol.WorkspaceReadWrite {
		readOnly = "false"
	}
	if want := []string{"type=bind", "src=" + spec.WorkspacePath, "dst=/workspace", "ro=" + readOnly}; !slices.Equal(fields[0], want) {
		t.Fatalf("mount fields = %q, want %q", fields[0], want)
	}
	var command []string
	if err := json.Unmarshal([]byte(entrypoint), &command); err != nil || !slices.Equal(command, []string{spec.Request.Command.Executable}) {
		t.Fatalf("entrypoint %q decodes to %q, %v; want exactly [%q]", entrypoint, command, err, spec.Request.Command.Executable)
	}
}
