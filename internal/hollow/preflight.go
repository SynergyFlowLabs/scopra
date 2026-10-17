package hollow

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
)

const (
	// maxPodmanInfoBytes bounds the podman info output that Mio reads.
	maxPodmanInfoBytes = 1 << 20
	// minPodmanMajor and minPodmanMinor are the oldest Podman that has every
	// option ADR-010 uses (passthrough logging and --unsetenv-all).
	minPodmanMajor = 4
	minPodmanMinor = 0
	// maxVersionDigits bounds each version number, so parsing never
	// overflows.
	maxVersionDigits = 6
)

// requiredControllers are the cgroup controllers that ADR-010's limits need.
var requiredControllers = []string{"cpu", "memory", "pids"}

// ErrContainmentUnavailable reports that rootless containment cannot be used
// as ADR-010 requires. Mio reports it as containment_unavailable (ADR-009)
// and never falls back to running commands natively.
var ErrContainmentUnavailable = errors.New("hollow: rootless containment is unavailable")

// PodmanInfo holds the parts of `podman info --format json` output that the
// preflight check needs.
type PodmanInfo struct {
	// Version is Podman's version, such as "4.0.0".
	Version string
	// Rootless reports whether Podman runs without root.
	Rootless bool
	// CgroupVersion is "v1" or "v2".
	CgroupVersion string
	// CgroupControllers are the controllers that containers can use, which
	// for rootless Podman are those delegated to the user.
	CgroupControllers []string
}

// ParsePodmanInfo decodes the output of `podman info --format json`. Fields
// that preflight does not need are ignored, so that newer Podman versions can
// add them. Output over 1 MiB, anything other than one JSON object, and a
// missing or mistyped required field are errors that wrap
// ErrContainmentUnavailable, because Mio must then fail closed.
func ParsePodmanInfo(output []byte) (PodmanInfo, error) {
	if len(output) > maxPodmanInfoBytes {
		return PodmanInfo{}, fmt.Errorf("%w: podman info output exceeds 1 MiB", ErrContainmentUnavailable)
	}
	var raw struct {
		Host *struct {
			CgroupVersion     *string   `json:"cgroupVersion"`
			CgroupControllers *[]string `json:"cgroupControllers"`
			Security          *struct {
				Rootless *bool `json:"rootless"`
			} `json:"security"`
		} `json:"host"`
		Version *struct {
			Version *string `json:"Version"`
		} `json:"version"`
	}
	decoder := json.NewDecoder(bytes.NewReader(output))
	if err := decoder.Decode(&raw); err != nil {
		return PodmanInfo{}, fmt.Errorf("%w: podman info output is not a JSON object of the expected shape: %v", ErrContainmentUnavailable, err)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return PodmanInfo{}, fmt.Errorf("%w: podman info output has trailing data", ErrContainmentUnavailable)
	}
	var missing string
	switch {
	case raw.Host == nil:
		missing = "host"
	case raw.Host.Security == nil || raw.Host.Security.Rootless == nil:
		missing = "host.security.rootless"
	case raw.Host.CgroupVersion == nil:
		missing = "host.cgroupVersion"
	case raw.Host.CgroupControllers == nil:
		missing = "host.cgroupControllers"
	case raw.Version == nil || raw.Version.Version == nil:
		missing = "version.Version"
	}
	if missing != "" {
		return PodmanInfo{}, fmt.Errorf("%w: podman info output lacks %s", ErrContainmentUnavailable, missing)
	}
	return PodmanInfo{
		Version:           *raw.Version.Version,
		Rootless:          *raw.Host.Security.Rootless,
		CgroupVersion:     *raw.Host.CgroupVersion,
		CgroupControllers: *raw.Host.CgroupControllers,
	}, nil
}

// Check reports whether info meets ADR-010's requirements: Podman 4.0 or
// later, rootless, cgroups v2, and the cpu, memory, and pids controllers. The
// error wraps ErrContainmentUnavailable and names every unmet requirement.
func (info PodmanInfo) Check() error {
	var problems []string
	if !atLeastVersion(info.Version, minPodmanMajor, minPodmanMinor) {
		problems = append(problems, fmt.Sprintf("Podman %q is older than %d.%d or not a recognized version", info.Version, minPodmanMajor, minPodmanMinor))
	}
	if !info.Rootless {
		problems = append(problems, "Podman runs as root, not rootless")
	}
	if info.CgroupVersion != "v2" {
		problems = append(problems, fmt.Sprintf("the cgroup version is %q, not v2", info.CgroupVersion))
	}
	var missing []string
	for _, controller := range requiredControllers {
		if !slices.Contains(info.CgroupControllers, controller) {
			missing = append(missing, controller)
		}
	}
	if len(missing) > 0 {
		problems = append(problems, "cgroup controllers not delegated: "+strings.Join(missing, ", "))
	}
	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrContainmentUnavailable, strings.Join(problems, "; "))
}

// atLeastVersion reports whether version, written major.minor with optional
// further components or suffixes such as "4.9.4-dev", is at least
// minMajor.minMinor. The numbers are compared as integers. A version with any
// character outside letters, digits, and ".-+~_" fails closed, as does any
// other form.
func atLeastVersion(version string, minMajor, minMinor int) bool {
	for i := 0; i < len(version); i++ {
		char := version[i]
		if !(char >= '0' && char <= '9' || char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || strings.IndexByte(".-+~_", char) >= 0) {
			return false
		}
	}
	// Without a ".", the minor version is empty, which smallNumber rejects.
	majorText, rest, _ := strings.Cut(version, ".")
	minorText := rest
	if end := strings.IndexFunc(rest, func(char rune) bool { return char < '0' || char > '9' }); end >= 0 {
		minorText = rest[:end]
	}
	major, majorOK := smallNumber(majorText)
	minor, minorOK := smallNumber(minorText)
	if !majorOK || !minorOK {
		return false
	}
	return major > minMajor || major == minMajor && minor >= minMinor
}

// smallNumber parses 1 to maxVersionDigits decimal digits.
func smallNumber(text string) (int, bool) {
	if text == "" || len(text) > maxVersionDigits {
		return 0, false
	}
	number := 0
	for i := 0; i < len(text); i++ {
		if text[i] < '0' || text[i] > '9' {
			return 0, false
		}
		number = number*10 + int(text[i]-'0')
	}
	return number, true
}
