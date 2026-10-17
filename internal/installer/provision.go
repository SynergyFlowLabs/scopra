package installer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Provision is a way to install Scopra's missing prerequisite with the
// distribution's own package manager. Every command is an argument vector, not
// a shell string, so nothing that comes from /etc/os-release or a terminal can
// be re-parsed as shell syntax. The installer runs these only after the user
// confirms them on a screen that prints every command.
type Provision struct {
	// Family names the distribution family, for the interface: "fedora",
	// "debian", "arch", "suse", or "alpine". It is empty for a plan that
	// fixes a configuration rather than installing a package.
	Family string
	// Manager is the package manager executable, for example "dnf".
	Manager string
	// Packages are the packages the plan installs.
	Packages []string
	// Title names the fix: "Install Podman", "Delegate cgroup controllers".
	Title string
	// Summary explains why the machine needs the fix, in one sentence.
	Summary string
	// Note tells the user what the commands do to the system.
	Note string
	// NeedsRelogin marks a fix that systemd only applies to the next session.
	NeedsRelogin bool
	// Files are staged before the commands run. The commands copy them into
	// place, so every path in them is the path that appears on the screen.
	Files []ProvisionFile
	// Commands are the exact argument vectors, sudo included when needed.
	Commands [][]string
}

// ProvisionFile is a file the plan stages with the user's own permissions, for
// a command to install. Staging keeps the privileged commands argument-only:
// nothing is written to a system path by a shell.
type ProvisionFile struct {
	Path    string
	Mode    os.FileMode
	Content string
}

// delegateDropIn gives the user manager every cgroup controller Scopra's limits
// need. systemd's own user@.service delegates only memory and pids, so a
// rootless Podman session has no cpu controller and cannot enforce limits.
const delegateDropIn = `[Service]
Delegate=cpu cpuset io memory pids
`

// delegateDir is where systemd reads drop-ins for the user manager.
const delegateDir = "/etc/systemd/system/user@.service.d"

// delegateName sorts after distribution drop-ins, so this one wins.
const delegateName = "99-scopra-delegate.conf"

// DetectDelegationFix returns the commands that delegate the cgroup controllers
// Podman needs to the user manager, or false when the installer cannot do it
// safely: without systemd, without root or sudo, or without somewhere to stage
// the drop-in.
func DetectDelegationFix(root bool, hasProgram func(string) bool, stagingDir string) (Provision, bool) {
	if stagingDir == "" || !hasProgram("systemctl") {
		return Provision{}, false
	}
	prefix := []string{}
	if !root {
		if !hasProgram("sudo") {
			return Provision{}, false
		}
		prefix = []string{"sudo"}
	}
	staged := filepath.Join(stagingDir, delegateName)
	destination := filepath.Join(delegateDir, delegateName)
	with := func(name string, args ...string) []string {
		argv := make([]string, 0, len(prefix)+1+len(args))
		argv = append(argv, prefix...)
		argv = append(argv, name)
		return append(argv, args...)
	}
	return Provision{
		Title:        "Delegate cgroup controllers",
		Summary:      "Podman is installed, but your user session is not delegated the cgroup controllers Scopra's limits need: cpu, memory, and pids.",
		Note:         "This writes a systemd drop-in for the user manager and reloads systemd. sudo asks for your password; Scopra never stores it. systemd applies the change the next time you log in.",
		NeedsRelogin: true,
		Files: []ProvisionFile{{
			Path:    staged,
			Mode:    0o644,
			Content: delegateDropIn,
		}},
		Commands: [][]string{
			with("mkdir", "-p", delegateDir),
			with("cp", staged, destination),
			with("chmod", "0644", destination),
			with("systemctl", "daemon-reload"),
		},
	}, true
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
		Title:    "Install Podman",
		Summary:  "Scopra needs rootless Podman 4.0+, and this machine does not have it yet.",
		Note:     "They install from your distribution's repositories. sudo asks for your password; Scopra never stores it, and nothing else on the system changes.",
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
		return errors.New("nothing to run")
	}
	staged := make([]string, 0, len(plan.Files))
	defer func() {
		for _, path := range staged {
			_ = os.Remove(path)
		}
	}()
	for _, file := range plan.Files {
		if err := os.MkdirAll(filepath.Dir(file.Path), 0o700); err != nil {
			return fmt.Errorf("stage %s: %w", file.Path, err)
		}
		if err := os.WriteFile(file.Path, []byte(file.Content), file.Mode); err != nil {
			return fmt.Errorf("stage %s: %w", file.Path, err)
		}
		// WriteFile is subject to the umask; the copy must carry the mode the
		// plan prints.
		if err := os.Chmod(file.Path, file.Mode); err != nil {
			return fmt.Errorf("stage %s: %w", file.Path, err)
		}
		staged = append(staged, file.Path)
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
