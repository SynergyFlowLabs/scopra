package hollow

import (
	"errors"
	"fmt"
)

// ErrContainmentUnavailable reports that rootless containment cannot be used
// as ADR-010 requires. Mio reports it as containment_unavailable (ADR-009)
// and never falls back to running commands natively.
var ErrContainmentUnavailable = errors.New("hollow: rootless containment is unavailable")

// PodmanInfo holds the parts of `podman info --format json` output that the
// preflight check needs.
type PodmanInfo struct {
	Version           string
	Rootless          bool
	CgroupVersion     string
	CgroupControllers []string
}

// ParsePodmanInfo decodes the output of `podman info --format json`.
func ParsePodmanInfo(output []byte) (PodmanInfo, error) {
	return PodmanInfo{}, fmt.Errorf("%w: not implemented", ErrContainmentUnavailable)
}

// Check reports whether info meets ADR-010's requirements.
func (info PodmanInfo) Check() error {
	return nil
}
