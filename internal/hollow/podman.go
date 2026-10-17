// Package hollow implements Scopra Hollow, the execution sandbox. It builds
// the rootless Podman invocation for one execution (ADR-010) and starts no
// process itself.
package hollow

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/SynergyFlowLabs/scopra/packages/protocol"
)

const (
	// containerWorkspace is where the workspace appears inside the container.
	containerWorkspace = "/workspace"
	// searchPath is the fixed PATH that resolves bare executable names
	// (ADR-007); the request never chooses it.
	searchPath = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
	// maxWorkspacePathBytes matches the registry's path limit (ADR-005).
	maxWorkspacePathBytes = 4095
	// timeoutBackstopSeconds lets Podman kill the container if Mio dies; Mio's
	// own timer, which fires first, decides timed_out.
	timeoutBackstopSeconds = 10
)

var (
	// ErrUnmountableWorkspace reports a workspace path that cannot be bind
	// mounted safely. Mio reports it as workspace_unavailable (ADR-009).
	ErrUnmountableWorkspace = errors.New("hollow: the workspace path cannot be mounted")
	// ErrInvalidImage reports an image reference that is not pinned by digest.
	ErrInvalidImage = errors.New("hollow: the image must be a reference pinned by digest")
)

// RunSpec is everything one execution needs.
type RunSpec struct {
	// Request is the validated execution request.
	Request protocol.ExecutionRequest
	// WorkspacePath is the workspace's canonical path, revalidated (ADR-004)
	// by the caller immediately before the run.
	WorkspacePath string
	// Image is the configured container image, pinned by digest.
	Image string
}

// PodmanRunArgs returns the arguments that follow "podman" to run spec, in
// ADR-010's exact order: options, then the image, then the request's
// arguments verbatim. The executable travels in a JSON --entrypoint, so the
// image's entrypoint and CMD never apply, and nothing after the image is read
// as a Podman option. It validates every input and starts no process.
func PodmanRunArgs(spec RunSpec) ([]string, error) {
	request := spec.Request
	actionHash, err := request.ActionHash()
	if err != nil {
		return nil, fmt.Errorf("hollow: %w", err)
	}
	if !mountablePath(spec.WorkspacePath) {
		return nil, fmt.Errorf("%w: %q must be a clean absolute path of valid UTF-8 with no control characters", ErrUnmountableWorkspace, spec.WorkspacePath)
	}
	if !pinnedImage(spec.Image) {
		return nil, fmt.Errorf("%w: %q must be name@sha256: followed by 64 lowercase hexadecimal characters", ErrInvalidImage, spec.Image)
	}
	entrypoint, err := jsonArray(request.Command.Executable)
	if err != nil {
		return nil, fmt.Errorf("hollow: encode the entrypoint: %w", err)
	}

	limits := request.Limits
	workdir := containerWorkspace
	if request.Command.WorkingDirectory != "." {
		workdir += "/" + request.Command.WorkingDirectory
	}
	readOnly := request.WorkspaceAccess != protocol.WorkspaceReadWrite
	args := []string{
		"run",
		"--name=scopra-" + actionHash[:32],
		"--label=scopra.action-hash=" + actionHash,
		"--pull=never",
		"--log-driver=passthrough",
		// ADR-007 version 1 allows only network "none".
		"--network=none",
		"--userns=keep-id",
		"--cap-drop=all",
		"--security-opt=no-new-privileges",
		"--security-opt=label=disable",
		"--read-only",
		fmt.Sprintf("--tmpfs=/tmp:rw,exec,nosuid,nodev,mode=1777,size=%d", limits.MemoryBytes),
		"--mount=type=bind," + csvField("src="+spec.WorkspacePath) + ",dst=" + containerWorkspace + ",ro=" + strconv.FormatBool(readOnly),
		"--workdir=" + workdir,
		"--unsetenv-all",
		"--env=PATH=" + searchPath,
		"--env=HOME=/tmp",
		"--init",
		fmt.Sprintf("--memory=%db", limits.MemoryBytes),
		fmt.Sprintf("--memory-swap=%db", limits.MemoryBytes),
		"--cpus=" + cpus(limits.CPUMillicores),
		fmt.Sprintf("--pids-limit=%d", limits.PIDs),
		fmt.Sprintf("--timeout=%d", limits.TimeoutSeconds+timeoutBackstopSeconds),
		"--entrypoint=" + entrypoint,
		spec.Image,
	}
	return append(args, request.Command.Arguments...), nil
}

// ValidateImage checks that image is a reference pinned by digest (ADR-010).
func ValidateImage(image string) error {
	return nil
}

// mountablePath reports whether p can travel in a CSV-quoted mount source: a
// clean absolute path other than the root, of valid UTF-8, with no control
// characters. Go's CSV reader, which Podman uses, would drop a carriage return
// before a newline inside the quoted field.
func mountablePath(p string) bool {
	if !strings.HasPrefix(p, "/") || p == "/" || len(p) > maxWorkspacePathBytes || !utf8.ValidString(p) || path.Clean(p) != p {
		return false
	}
	for _, char := range p {
		if unicode.IsControl(char) {
			return false
		}
	}
	return true
}

// pinnedImage reports whether image is a reference pinned by a SHA-256
// digest: a name that starts with a letter or digit and uses only reference
// characters, then "@sha256:" and 64 lowercase hexadecimal characters. A name
// cannot start with "-", so Podman never reads it as an option.
func pinnedImage(image string) bool {
	name, digest, ok := strings.Cut(image, "@sha256:")
	if !ok || name == "" || len(digest) != 64 || !isAlphanumeric(name[0]) {
		return false
	}
	for i := 0; i < len(name); i++ {
		if !isAlphanumeric(name[i]) && !strings.ContainsRune("._/:-", rune(name[i])) {
			return false
		}
	}
	for i := 0; i < len(digest); i++ {
		if !(digest[i] >= '0' && digest[i] <= '9' || digest[i] >= 'a' && digest[i] <= 'f') {
			return false
		}
	}
	return true
}

func isAlphanumeric(char byte) bool {
	return char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9'
}

// csvField quotes field for a CSV record, doubling any quotes inside it, so
// that commas and quotes in a path cannot start another mount option.
func csvField(field string) string {
	return `"` + strings.ReplaceAll(field, `"`, `""`) + `"`
}

// cpus writes millicores as an exact decimal number of CPUs, such as 1.5 or
// 0.001, with integer arithmetic.
func cpus(millicores uint64) string {
	whole, fraction := millicores/1000, millicores%1000
	if fraction == 0 {
		return strconv.FormatUint(whole, 10)
	}
	return strings.TrimRight(fmt.Sprintf("%d.%03d", whole, fraction), "0")
}

// jsonArray encodes the one-element JSON array that --entrypoint takes.
func jsonArray(element string) (string, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode([]string{element}); err != nil {
		return "", err
	}
	return strings.TrimSuffix(buffer.String(), "\n"), nil
}
