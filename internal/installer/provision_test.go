package installer

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func programSet(names ...string) func(string) bool {
	installed := make(map[string]bool, len(names))
	for _, name := range names {
		installed[name] = true
	}
	return func(name string) bool { return installed[name] }
}

func TestDetectProvisionChoosesTheDistributionManager(t *testing.T) {
	tests := []struct {
		name     string
		release  string
		programs func(string) bool
		root     bool
		want     [][]string
		family   string
	}{
		{
			name:     "fedora",
			release:  "NAME=Fedora\nID=fedora\nVERSION_ID=41\n",
			programs: programSet("dnf", "sudo"),
			want:     [][]string{{"sudo", "dnf", "install", "-y", "podman"}},
			family:   "fedora",
		},
		{
			name:     "rhel derivative resolves through ID_LIKE",
			release:  "NAME=\"Rocky Linux\"\nID=rocky\nID_LIKE=\"rhel centos fedora\"\n",
			programs: programSet("dnf", "sudo"),
			want:     [][]string{{"sudo", "dnf", "install", "-y", "podman"}},
			family:   "fedora",
		},
		{
			name:     "ubuntu gets the rootless helpers too",
			release:  "NAME=Ubuntu\nID=ubuntu\nID_LIKE=debian\n",
			programs: programSet("apt-get", "sudo"),
			want: [][]string{
				{"sudo", "apt-get", "update"},
				{"sudo", "apt-get", "install", "-y", "podman", "uidmap", "slirp4netns", "fuse-overlayfs"},
			},
			family: "debian",
		},
		{
			name:     "mint resolves through ubuntu",
			release:  "NAME=\"Linux Mint\"\nID=linuxmint\nID_LIKE=ubuntu\n",
			programs: programSet("apt-get", "sudo"),
			want: [][]string{
				{"sudo", "apt-get", "update"},
				{"sudo", "apt-get", "install", "-y", "podman", "uidmap", "slirp4netns", "fuse-overlayfs"},
			},
			family: "debian",
		},
		{
			name:     "arch",
			release:  "ID=arch\n",
			programs: programSet("pacman", "sudo"),
			want:     [][]string{{"sudo", "pacman", "-Sy", "--needed", "--noconfirm", "podman"}},
			family:   "arch",
		},
		{
			name:     "opensuse",
			release:  "ID=\"opensuse-tumbleweed\"\nID_LIKE=\"opensuse suse\"\n",
			programs: programSet("zypper", "sudo"),
			want:     [][]string{{"sudo", "zypper", "--non-interactive", "install", "podman"}},
			family:   "suse",
		},
		{
			name:     "alpine",
			release:  "ID=alpine\n",
			programs: programSet("apk", "sudo"),
			want:     [][]string{{"sudo", "apk", "add", "--no-cache", "podman"}},
			family:   "alpine",
		},
		{
			name:     "root needs no sudo",
			release:  "ID=fedora\n",
			programs: programSet("dnf"),
			root:     true,
			want:     [][]string{{"dnf", "install", "-y", "podman"}},
			family:   "fedora",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan, ok := DetectProvision([]byte(test.release), test.programs, test.root)
			if !ok {
				t.Fatalf("DetectProvision() = no plan, want one")
			}
			if plan.Family != test.family {
				t.Fatalf("family = %q, want %q", plan.Family, test.family)
			}
			if !reflect.DeepEqual(plan.Commands, test.want) {
				t.Fatalf("commands = %q, want %q", plan.Commands, test.want)
			}
		})
	}
}

func TestDetectProvisionRefusesWhenItCannotKnow(t *testing.T) {
	tests := []struct {
		name     string
		release  string
		programs func(string) bool
		root     bool
	}{
		{
			name:     "podman is already installed",
			release:  "ID=fedora\n",
			programs: programSet("podman", "dnf", "sudo"),
		},
		{
			name:     "unknown distribution",
			release:  "ID=plan9\n",
			programs: programSet("dnf", "sudo"),
		},
		{
			name:     "missing os-release",
			release:  "",
			programs: programSet("dnf", "sudo"),
		},
		{
			name:     "package manager is not installed",
			release:  "ID=fedora\n",
			programs: programSet("sudo"),
		},
		{
			name:     "no sudo and not root",
			release:  "ID=fedora\n",
			programs: programSet("dnf"),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if plan, ok := DetectProvision([]byte(test.release), test.programs, test.root); ok {
				t.Fatalf("DetectProvision() = %+v, want no plan", plan)
			}
		})
	}
}

func TestParseOSReleaseHandlesQuotesAndComments(t *testing.T) {
	values := parseOSRelease([]byte("# comment\n\nNAME=\"Fedora Linux\"\nID=fedora\nID_LIKE='rhel fedora'\nbroken line\n"))
	want := map[string]string{"NAME": "Fedora Linux", "ID": "fedora", "ID_LIKE": "rhel fedora"}
	if !reflect.DeepEqual(values, want) {
		t.Fatalf("parseOSRelease() = %v, want %v", values, want)
	}
}

func TestProvisionCommandLinesAreShellFree(t *testing.T) {
	plan, ok := DetectProvision([]byte("ID=fedora\n"), programSet("dnf", "sudo"), false)
	if !ok {
		t.Fatal("DetectProvision() = no plan, want one")
	}
	lines := plan.CommandLines()
	if len(lines) != 1 || lines[0] != "sudo dnf install -y podman" {
		t.Fatalf("CommandLines() = %q", lines)
	}
	for _, argv := range plan.Commands {
		if argv[0] == "sh" || argv[0] == "bash" {
			t.Fatalf("command %q runs a shell", argv)
		}
	}
}

func TestRunProvisionRunsNothingWithoutCommands(t *testing.T) {
	// A plan with no commands must not look like success.
	if err := RunProvision(context.Background(), Provision{}, Stdio{}); err == nil {
		t.Fatal("RunProvision() with an empty plan = nil, want an error")
	}
}

func TestRunProvisionStopsAtTheFirstFailure(t *testing.T) {
	plan := Provision{Commands: [][]string{
		{"/bin/sh", "-c", "exit 0"},
		{"/bin/sh", "-c", "exit 3"},
		{"/bin/sh", "-c", "exit 0"},
	}}
	var output strings.Builder
	err := RunProvision(context.Background(), plan, Stdio{Out: &output, Err: &output})
	if err == nil {
		t.Fatal("RunProvision() = nil, want the failing command's error")
	}
	if !strings.Contains(err.Error(), "exit status 3") {
		t.Fatalf("error = %v, want it to name the failing command", err)
	}
}

func TestRunProvisionReportsSuccess(t *testing.T) {
	plan := Provision{Commands: [][]string{
		{"/bin/sh", "-c", "printf one"},
		{"/bin/sh", "-c", "printf two"},
	}}
	var output strings.Builder
	if err := RunProvision(context.Background(), plan, Stdio{Out: &output, Err: &output}); err != nil {
		t.Fatalf("RunProvision() error = %v", err)
	}
	if output.String() != "onetwo" {
		t.Fatalf("output = %q, want the child's output", output.String())
	}
}

func TestDetectDelegationFixBuildsTheDropIn(t *testing.T) {
	plan, ok := DetectDelegationFix(false, programSet("systemctl", "sudo"), "/home/u/.local/state/scopra/installer")
	if !ok {
		t.Fatal("DetectDelegationFix() = no plan, want one")
	}
	if plan.Title != "Delegate cgroup controllers" || !plan.NeedsRelogin {
		t.Fatalf("plan = %+v, want the delegation fix that needs a new login", plan)
	}
	if len(plan.Files) != 1 {
		t.Fatalf("files = %+v, want one staged drop-in", plan.Files)
	}
	file := plan.Files[0]
	if file.Path != "/home/u/.local/state/scopra/installer/99-scopra-delegate.conf" || file.Mode != 0o644 {
		t.Fatalf("staged file = %+v", file)
	}
	if !strings.Contains(file.Content, "Delegate=cpu cpuset io memory pids") || !strings.HasPrefix(file.Content, "[Service]") {
		t.Fatalf("drop-in content = %q", file.Content)
	}
	want := [][]string{
		{"sudo", "mkdir", "-p", "/etc/systemd/system/user@.service.d"},
		{"sudo", "cp", file.Path, "/etc/systemd/system/user@.service.d/99-scopra-delegate.conf"},
		{"sudo", "chmod", "0644", "/etc/systemd/system/user@.service.d/99-scopra-delegate.conf"},
		{"sudo", "systemctl", "daemon-reload"},
	}
	if !reflect.DeepEqual(plan.Commands, want) {
		t.Fatalf("commands = %q, want %q", plan.Commands, want)
	}
}

func TestDetectDelegationFixRefusesWhenItCannotBeDone(t *testing.T) {
	tests := []struct {
		name     string
		programs func(string) bool
		root     bool
		staging  string
	}{
		{name: "no staging directory", programs: programSet("systemctl", "sudo"), staging: ""},
		{name: "no systemctl", programs: programSet("sudo"), staging: "/tmp/stage"},
		{name: "no sudo and not root", programs: programSet("systemctl"), staging: "/tmp/stage"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if plan, ok := DetectDelegationFix(test.root, test.programs, test.staging); ok {
				t.Fatalf("DetectDelegationFix() = %+v, want no plan", plan)
			}
		})
	}
}

func TestRunProvisionStagesFilesAndRemovesThem(t *testing.T) {
	dir := t.TempDir()
	staged := filepath.Join(dir, "installer", "99-scopra-delegate.conf")
	plan := Provision{
		Files: []ProvisionFile{{Path: staged, Mode: 0o640, Content: "[Service]\nDelegate=pids\n"}},
		Commands: [][]string{
			{"/bin/sh", "-c", "test -f \"$1\" && cat \"$1\"", "sh", staged},
		},
	}
	var output strings.Builder
	if err := RunProvision(context.Background(), plan, Stdio{Out: &output, Err: &output}); err != nil {
		t.Fatalf("RunProvision() error = %v", err)
	}
	if !strings.Contains(output.String(), "Delegate=pids") {
		t.Fatalf("the command did not see the staged file: %q", output.String())
	}
	if _, err := os.Stat(staged); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the staged file survived the run: %v", err)
	}
}
