package installer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// Provision is a way to install Scopra's missing prerequisite with the
// distribution's own package manager. Every command is an argument vector, not
// a shell string, so nothing that comes from /etc/os-release or a terminal can
// be re-parsed as shell syntax. The installer runs these only after the user
// confirms them on a screen that prints every command.
type Provision struct {
	// Family names the distribution family, for the interface: "fedora",
	// "debian", "arch", "suse", or "alpine".
	Family string
	// Manager is the package manager executable, for example "dnf".
	Manager string
	// Packages are the packages the plan installs.
	Packages []string
	// Commands are the exact argument vectors, sudo included when needed.
	Commands [][]string
}

// CommandLines renders the plan for display, one line per command.
func (p Provision) CommandLines() []string {
	lines := make([]string, 0, len(p.Commands))
	for _, argv := range p.Commands {
		lines = append(lines, strings.Join(argv, " "))
	}
	return lines
}

// DetectProvision reads /etc/os-release and returns how to install Podman on
// this machine. It returns false when Podman is already installed, when the
// distribution is not one it knows, or when it would have to elevate privileges
// without sudo being available: an unknown system gets instructions, never a
// guess.
func DetectProvision(osRelease []byte, hasProgram func(string) bool, root bool) (Provision, bool) {
	if hasProgram("podman") {
		return Provision{}, false
	}
	release := parseOSRelease(osRelease)
	family, manager, packages, ok := provisionFamily(release["ID"], release["ID_LIKE"])
	if !ok {
		return Provision{}, false
	}
	if !hasProgram(manager) {
		return Provision{}, false
	}
	prefix := []string{}
	if !root {
		if !hasProgram("sudo") {
			return Provision{}, false
		}
		prefix = []string{"sudo"}
	}
	return Provision{
		Family:   family,
		Manager:  manager,
		Packages: packages,
		Commands: provisionCommands(prefix, manager, packages),
	}, true
}

// parseOSRelease reads the KEY=value format of /etc/os-release. Values may be
// quoted; comments and unknown lines are ignored.
func parseOSRelease(data []byte) map[string]string {
	values := make(map[string]string)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 {
			quote := value[0]
			if (quote == '"' || quote == '\'') && value[len(value)-1] == quote {
				value = value[1 : len(value)-1]
			}
		}
		values[strings.TrimSpace(key)] = value
	}
	return values
}

// provisionFamily maps a distribution to a package manager and the packages
// needed for rootless Podman. ID comes first, then each entry of ID_LIKE, so a
// derivative such as Linux Mint resolves through "ubuntu" and "debian".
func provisionFamily(id, idLike string) (family, manager string, packages []string, ok bool) {
	candidates := append([]string{strings.ToLower(id)}, strings.Fields(strings.ToLower(idLike))...)
	for _, candidate := range candidates {
		switch candidate {
		case "fedora", "rhel", "centos", "rocky", "almalinux", "ol":
			return "fedora", "dnf", []string{"podman"}, true
		case "debian", "ubuntu":
			// uidmap, slirp4netns, and fuse-overlayfs are what rootless
			// Podman needs on these distributions; without them `podman
			// info` fails or the container cannot start.
			return "debian", "apt-get",
				[]string{"podman", "uidmap", "slirp4netns", "fuse-overlayfs"}, true
		case "arch", "archlinux":
			return "arch", "pacman", []string{"podman"}, true
		case "opensuse", "opensuse-leap", "opensuse-tumbleweed", "suse", "sles", "sled":
			return "suse", "zypper", []string{"podman"}, true
		case "alpine":
			return "alpine", "apk", []string{"podman"}, true
		}
	}
	return "", "", nil, false
}

// provisionCommands builds the argument vectors each package manager needs.
// They install from the distribution's own repositories and never from a URL
// the installer supplies.
func provisionCommands(prefix []string, manager string, packages []string) [][]string {
	with := func(args ...string) []string {
		argv := make([]string, 0, len(prefix)+1+len(args))
		argv = append(argv, prefix...)
		argv = append(argv, manager)
		return append(argv, args...)
	}
	switch manager {
	case "dnf":
		return [][]string{with(append([]string{"install", "-y"}, packages...)...)}
	case "apt-get":
		return [][]string{
			with("update"),
			with(append([]string{"install", "-y"}, packages...)...),
		}
	case "pacman":
		return [][]string{with(append([]string{"-Sy", "--needed", "--noconfirm"}, packages...)...)}
	case "zypper":
		return [][]string{with(append([]string{"--non-interactive", "install"}, packages...)...)}
	case "apk":
		return [][]string{with(append([]string{"add", "--no-cache"}, packages...)...)}
	}
	return nil
}

// Stdio is where a provision command reads and writes. The runner passes the
// terminal itself, because sudo has to be able to prompt for a password.
type Stdio struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
}

// RunProvision runs the plan's commands in order and stops at the first
// failure. The caller restores the terminal first: the child owns it while this
// runs, which is what lets sudo prompt normally.
func RunProvision(ctx context.Context, plan Provision, stdio Stdio) error {
	if len(plan.Commands) == 0 {
		return errors.New("nothing to install")
	}
	for _, argv := range plan.Commands {
		if len(argv) == 0 {
			continue
		}
		command := exec.CommandContext(ctx, argv[0], argv[1:]...)
		command.Stdin = stdio.In
		command.Stdout = stdio.Out
		command.Stderr = stdio.Err
		if err := command.Run(); err != nil {
			return fmt.Errorf("%s: %w", strings.Join(argv, " "), err)
		}
	}
	return nil
}
