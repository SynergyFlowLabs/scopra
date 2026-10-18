package installer

import (
	"errors"
	"strings"
	"testing"

	"github.com/SynergyFlowLabs/scopra/internal/tui"
)

func plainSheet() tui.StyleSheet {
	return tui.StyleSheet{Profile: tui.ProfileNone}
}

// screens returns one model per screen, so a single loop can check them all.
func screens(t *testing.T) map[string]*Model {
	t.Helper()
	location := modelAtLocation(t)
	image := modelAtImage(t)
	service := modelAtService(t)
	running := modelAtService(t)
	press(running, tui.KeyEnter)
	_ = running.Intent()
	done := modelAtService(t)
	press(done, tui.KeyEnter)
	done.Update(Message{Result: &Result{Dir: "/tmp/bin", Version: "v1.2.3", Service: true, ServiceActive: true}})
	failed := modelAtService(t)
	failed.Update(Message{Err: errTest})
	aborted := newTestModel()
	aborted.Screen = ScreenAborted
	checks := newTestModel()
	checks.Screen = ScreenChecks
	checks.checks = failChecks()
	checks.checksRunning = false
	return map[string]*Model{
		"welcome":  newTestModel(),
		"checks":   checks,
		"location": location,
		"image":    image,
		"service":  service,
		"running":  running,
		"done":     done,
		"failed":   failed,
		"aborted":  aborted,
	}
}

var errTest = &testError{"disk full"}

type testError struct{ message string }

func (e *testError) Error() string { return e.message }

func TestViewFitsEveryScreen(t *testing.T) {
	sizes := [][2]int{{56, 18}, {80, 24}, {120, 40}}
	for name, model := range screens(t) {
		for _, size := range sizes {
			rendered := model.View(size[0], size[1], plainSheet())
			lines := strings.Split(rendered, "\n")
			if len(lines) > size[1] {
				t.Fatalf("%s at %dx%d: %d lines", name, size[0], size[1], len(lines))
			}
			for index, line := range lines {
				if width := tui.StringWidth(line); width > size[0] {
					t.Fatalf("%s at %dx%d: line %d is %d cells wide: %q", name, size[0], size[1], index, width, line)
				}
				if strings.ContainsRune(line, 0x1b) {
					t.Fatalf("%s at %dx%d: plain output contains an escape: %q", name, size[0], size[1], line)
				}
			}
		}
	}
}

func TestViewAsksForABiggerTerminal(t *testing.T) {
	model := newTestModel()
	rendered := model.View(40, 10, plainSheet())
	if !strings.Contains(rendered, "too small") {
		t.Fatalf("rendered = %q", rendered)
	}
}

func TestViewShowsTheVersionAndTheStepRail(t *testing.T) {
	rendered := newTestModel().View(80, 24, plainSheet())
	if !strings.Contains(rendered, "v1.2.3") {
		t.Fatalf("version missing: %q", rendered)
	}
	for _, label := range []string{"Preflight", "Location", "Sandbox", "Install"} {
		if !strings.Contains(rendered, label) {
			t.Fatalf("rail label %q missing", label)
		}
	}
}

func TestViewNamesTheFailedCheckAndItsFix(t *testing.T) {
	model := newTestModel()
	model.Screen = ScreenChecks
	model.checks = failChecks()
	rendered := model.View(80, 24, plainSheet())
	for _, want := range []string{"Rootless Podman", "podman is not available", "Install Podman."} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("checks screen does not mention %q: %q", want, rendered)
		}
	}
}

func TestViewExplainsWhyTheServiceIsUnavailable(t *testing.T) {
	model := NewModel("v1.2.3", "/tmp/bin", "", false)
	model.Screen = ScreenService
	rendered := model.View(80, 24, plainSheet())
	if !strings.Contains(rendered, "scoprad was not found") {
		t.Fatalf("rendered = %q", rendered)
	}
}

func TestViewHidesTheCommandLineWhenTheServiceIsAvailable(t *testing.T) {
	model := NewModel("v1.2.3", "/tmp/bin", "", true)
	model.Screen = ScreenService
	rendered := model.View(80, 24, plainSheet())
	if strings.Contains(rendered, "scoprad was not found") {
		t.Fatalf("rendered = %q", rendered)
	}
	if !strings.Contains(rendered, "systemd user service") {
		t.Fatalf("rendered = %q", rendered)
	}
}

func TestDoneScreenShowsWarningsAndThePathHint(t *testing.T) {
	model := newTestModel()
	model.Screen = ScreenDone
	model.result = &Result{
		Dir:      "/tmp/bin",
		Version:  "v1.2.3",
		Service:  true,
		Warnings: []string{"No sandbox image was configured."},
	}
	model.PathNotice = `export PATH="/tmp/bin:$PATH"`
	rendered := model.View(90, 30, plainSheet())
	for _, want := range []string{"/tmp/bin", "No sandbox image", "export PATH", "workspace add"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("done screen does not mention %q: %q", want, rendered)
		}
	}
}

func TestViewDrawsTheInputCursor(t *testing.T) {
	model := modelAtLocation(t)
	frame := tui.NewFrame(80, 24)
	model.drawField(frame, 2, 4, 60, model.Dir, true)
	found := false
	for y := 0; y < 24; y++ {
		for x := 0; x < 80; x++ {
			if frame.Cell(x, y).Style == styleCursor {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("the cursor cell was not drawn")
	}
}

func TestViewSpinsWhileTheChecksRun(t *testing.T) {
	model := newTestModel()
	model.Screen = ScreenChecks
	model.checksRunning = true
	first := model.View(80, 24, plainSheet())
	if !strings.Contains(first, "Checking your system") {
		t.Fatalf("rendered = %q", first)
	}
	model.Update(Message{Tick: true})
	second := model.View(80, 24, plainSheet())
	if first == second {
		t.Fatal("the spinner must visibly change")
	}
}

func TestViewUsesColorWhenTheProfileAllowsIt(t *testing.T) {
	rendered := newTestModel().View(80, 24, tui.StyleSheet{Profile: tui.Profile256})
	if !strings.ContainsRune(rendered, 0x1b) {
		t.Fatal("the color profile must emit escape sequences")
	}
	if !strings.Contains(rendered, "scopra") {
		t.Fatalf("rendered = %q", rendered)
	}
}

func TestChecksScreenOffersTheSetupKey(t *testing.T) {
	model := newTestModel()
	begin(model)
	plan := Provision{Family: "fedora", Manager: "dnf", Packages: []string{"podman"},
		Commands: [][]string{{"sudo", "dnf", "install", "-y", "podman"}}}
	model.Update(Message{Checks: failChecks(), Provision: &plan})
	view := model.View(100, 30, tui.StyleSheet{})
	if !strings.Contains(view, "Press f and the installer can fix this for you.") {
		t.Fatalf("checks screen does not offer the setup key:\n%s", view)
	}
	if !strings.Contains(view, "f fix this") {
		t.Fatalf("footer does not name the setup key:\n%s", view)
	}
}

func TestSetupScreenShowsEveryCommand(t *testing.T) {
	model := newTestModel()
	begin(model)
	plan := Provision{Family: "debian", Manager: "apt-get", Packages: []string{"podman"},
		Summary: "Scopra needs rootless Podman 4.0+, and this machine does not have it yet.",
		Commands: [][]string{
			{"sudo", "apt-get", "update"},
			{"sudo", "apt-get", "install", "-y", "podman", "uidmap"},
		}}
	model.Update(Message{Checks: failChecks(), Provision: &plan})
	model.Key(tui.Key{Type: tui.KeyRune, Rune: 'f'})
	model.Update(Message{ProvisionDone: true, ProvisionErr: errors.New("sudo: a password is required")})
	model.Screen = ScreenSetup
	view := model.View(100, 30, tui.StyleSheet{})
	for _, want := range []string{
		"Scopra needs rootless Podman 4.0+",
		"sudo apt-get update",
		"sudo apt-get install -y podman uidmap",
		"y install now",
		"The last attempt failed: sudo: a password is required",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("setup screen is missing %q:\n%s", want, view)
		}
	}
}

func TestChecksScreenExplainsADelegatedServiceProbeFailure(t *testing.T) {
	model := newTestModel()
	begin(model)
	checks := failChecks()
	checks[0].Detail = "the delegated systemd service probe could not run"
	checks[0].Fix = "Check that systemd-run --user can start a transient service with cgroup delegation."
	model.Update(Message{Checks: checks})
	view := model.View(100, 30, tui.StyleSheet{})
	for _, want := range []string{
		"delegated systemd service probe could not run",
		"systemd-run --user can start a transient service",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("checks screen is missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "Log out, log back in") || strings.Contains(view, "Press f") {
		t.Fatalf("checks screen offers an unverified login/repeated-fix remedy:\n%s", view)
	}
}

func TestChecksScreenReportsAFailedFix(t *testing.T) {
	model := newTestModel()
	begin(model)
	plan := Provision{Title: "Install Podman", Commands: [][]string{{"sudo", "apt-get", "update"}}}
	model.Update(Message{Checks: failChecks(), Provision: &plan, ProvisionDone: true})
	model.Update(Message{ProvisionDone: true, ProvisionErr: errors.New("sudo: a password is required")})
	model.Update(Message{Checks: failChecks(), Provision: &plan})
	view := model.View(100, 30, tui.StyleSheet{})
	if !strings.Contains(view, "Press f and the installer can fix this") {
		t.Fatalf("checks screen does not offer another attempt:\n%s", view)
	}
}

func TestServiceScreenExplainsManualContextValidation(t *testing.T) {
	model := modelAtService(t)
	view := model.View(100, 30, tui.StyleSheet{})
	if !strings.Contains(view, "manual launch; current cgroup is checked") {
		t.Fatalf("service screen does not explain the no-service context check:\n%s", view)
	}
}

func TestSetupScreenDescribesACgroupFix(t *testing.T) {
	model := newTestModel()
	begin(model)
	plan := Provision{
		Title:                  "Delegate cgroup controllers",
		Summary:                "Podman is installed, but Scopra could not confirm the user manager's delegation of cpu, memory, and pids.",
		Note:                   "This writes a systemd drop-in for the user manager and reloads systemd.",
		VerifyDelegationDropIn: true,
		Commands: [][]string{
			{"sudo", "mkdir", "-p", "/etc/systemd/system/user@.service.d"},
			{"sudo", "cp", "/home/u/.local/state/scopra/installer/99-scopra-delegate.conf", "/etc/systemd/system/user@.service.d/99-scopra-delegate.conf"},
		},
	}
	model.Update(Message{Checks: failChecks(), Provision: &plan})
	model.Key(tui.Key{Type: tui.KeyRune, Rune: 'f'})
	view := model.View(120, 30, tui.StyleSheet{})
	for _, want := range []string{
		"could not confirm the user manager's delegation",
		"sudo mkdir -p /etc/systemd/system/user@.service.d",
		"sudo cp /home/u/.local/state/scopra/installer/99-scopra-delegate.conf",
		"This writes a systemd drop-in",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("setup screen is missing %q:\n%s", want, view)
		}
	}
}
