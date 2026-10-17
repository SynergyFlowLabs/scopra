package installer

import (
	"errors"
	"strings"
	"testing"

	"github.com/SynergyFlowLabs/scopra/internal/tui"
)

func passChecks() []Check {
	return []Check{
		{ID: "linux", Title: "Linux host", Status: StatusOK, Required: true},
		{ID: "systemd", Title: "systemd user session", Status: StatusOK, Required: true},
	}
}

func failChecks() []Check {
	return []Check{
		{ID: "podman", Title: "Rootless Podman 4.0+", Status: StatusFailed, Required: true, Detail: "podman is not available", Fix: "Install Podman."},
	}
}

func press(m *Model, keyType tui.KeyType) { m.Key(tui.Key{Type: keyType}) }

// begin presses enter on the welcome screen and consumes the intent it
// produces, which is what the runner does.
func begin(m *Model) {
	press(m, tui.KeyEnter)
	_ = m.Intent()
}
func typeRunes(m *Model, text string) {
	for _, r := range text {
		m.Key(tui.Key{Type: tui.KeyRune, Rune: r})
	}
}

// newTestModel returns a model with a short install path so the screens can
// be driven without touching the filesystem.
func newTestModel() *Model {
	return NewModel("v1.2.3", "/tmp/scopra-test/bin", "", true)
}

func TestWelcomeStartsTheChecks(t *testing.T) {
	model := newTestModel()
	if model.Screen != ScreenWelcome {
		t.Fatalf("screen = %v, want welcome", model.Screen)
	}
	press(model, tui.KeyEnter)
	if model.Screen != ScreenChecks {
		t.Fatalf("screen = %v, want checks", model.Screen)
	}
	if intent := model.Intent(); intent != IntentChecks {
		t.Fatalf("intent = %v, want checks", intent)
	}
	if intent := model.Intent(); intent != IntentNone {
		t.Fatalf("the intent must be consumed once, got %v", intent)
	}
	if !model.Running() {
		t.Fatal("the model should report that checks are running")
	}
}

func TestFailedChecksBlockTheInstall(t *testing.T) {
	model := newTestModel()
	begin(model)
	model.Update(Message{Checks: failChecks()})
	if model.Screen != ScreenChecks {
		t.Fatalf("screen = %v, want checks", model.Screen)
	}
	if model.Running() {
		t.Fatal("the model should not report work after the checks arrived")
	}
	press(model, tui.KeyEnter)
	if model.Screen != ScreenChecks {
		t.Fatalf("enter must not leave a failed preflight, screen = %v", model.Screen)
	}
	if intent := model.Intent(); intent != IntentNone {
		t.Fatalf("intent = %v, want none", intent)
	}
	if Ready(model.Checks()) {
		t.Fatal("the model must report the checks as not ready")
	}
}

func TestPassingChecksLeadToTheLocationScreen(t *testing.T) {
	model := newTestModel()
	begin(model)
	model.Update(Message{Checks: passChecks()})
	press(model, tui.KeyEnter)
	if model.Screen != ScreenLocation {
		t.Fatalf("screen = %v, want location", model.Screen)
	}
}

func TestChecksErrorEndsTheInstall(t *testing.T) {
	model := newTestModel()
	begin(model)
	model.Update(Message{ChecksErr: errors.New("cannot probe")})
	if model.Screen != ScreenFailed {
		t.Fatalf("screen = %v, want failed", model.Screen)
	}
	if !strings.Contains(model.Err().Error(), "cannot probe") {
		t.Fatalf("error = %v", model.Err())
	}
}

func TestLocationRejectsARelativeDirectory(t *testing.T) {
	model := modelAtLocation(t)
	model.Dir.SetValue("relative/bin")
	press(model, tui.KeyEnter)
	if model.Screen != ScreenLocation {
		t.Fatalf("screen = %v, want location", model.Screen)
	}
	if !strings.Contains(model.Dir.Error, "absolute") {
		t.Fatalf("input error = %q", model.Dir.Error)
	}
}

func TestLocationAcceptsAnAbsoluteDirectory(t *testing.T) {
	model := modelAtLocation(t)
	model.Dir.SetValue("/opt/scopra/bin")
	press(model, tui.KeyEnter)
	if model.Screen != ScreenImage {
		t.Fatalf("screen = %v, want image", model.Screen)
	}
	if model.Dir.Error != "" {
		t.Fatalf("input error = %q", model.Dir.Error)
	}
	if model.Options().Dir != "/opt/scopra/bin" {
		t.Fatalf("options dir = %q", model.Options().Dir)
	}
}

func TestImageMustBePinnedToADigest(t *testing.T) {
	model := modelAtImage(t)
	model.Image.SetValue("debian:latest")
	press(model, tui.KeyEnter)
	if model.Screen != ScreenImage {
		t.Fatalf("screen = %v, want image", model.Screen)
	}
	if !strings.Contains(model.Image.Error, "sha256") {
		t.Fatalf("image error = %q", model.Image.Error)
	}
	model.Image.SetValue("")
	press(model, tui.KeyEnter)
	if model.Screen != ScreenService {
		t.Fatalf("screen = %v, want service", model.Screen)
	}
}

func TestServiceChoiceDecidesThePlan(t *testing.T) {
	model := modelAtService(t)
	if model.serviceChoice != 0 {
		t.Fatalf("default choice = %d, want the service", model.serviceChoice)
	}
	press(model, tui.KeyUp)
	if model.serviceChoice != 1 {
		t.Fatalf("after up, choice = %d", model.serviceChoice)
	}
	press(model, tui.KeyEnter)
	if model.installService {
		t.Fatal("the second choice must not install a service")
	}
	if model.Screen != ScreenRunning {
		t.Fatalf("screen = %v, want running", model.Screen)
	}
	if intent := model.Intent(); intent != IntentInstall {
		t.Fatalf("intent = %v, want install", intent)
	}
	if len(model.Steps()) != 3 {
		t.Fatalf("steps = %#v, want the three binary steps", model.Steps())
	}
}

func TestServiceIsNotOfferedWithoutADaemon(t *testing.T) {
	model := NewModel("v1.2.3", "/tmp/scopra-test/bin", "", false)
	model.Screen = ScreenService
	press(model, tui.KeyEnter)
	if model.installService {
		t.Fatal("a service cannot be installed without scoprad")
	}
	if options := model.Options(); options.Service {
		t.Fatalf("options = %#v", options)
	}
}

func TestEscapeGoesBack(t *testing.T) {
	model := modelAtImage(t)
	press(model, tui.KeyEscape)
	if model.Screen != ScreenLocation {
		t.Fatalf("screen = %v, want location", model.Screen)
	}
	press(model, tui.KeyEscape)
	if model.Screen != ScreenWelcome {
		t.Fatalf("screen = %v, want welcome", model.Screen)
	}
}

func TestInterruptAbortsAndQuits(t *testing.T) {
	model := newTestModel()
	model.Key(tui.Key{Type: tui.KeyInterrupt})
	if model.Screen != ScreenAborted {
		t.Fatalf("screen = %v, want aborted", model.Screen)
	}
	if intent := model.Intent(); intent != IntentQuit {
		t.Fatalf("intent = %v, want quit", intent)
	}
	if model.ExitCode != 130 {
		t.Fatalf("exit code = %d, want 130", model.ExitCode)
	}
	press(model, tui.KeyEnter)
	if model.ExitCode != 130 {
		t.Fatalf("exit code after enter = %d", model.ExitCode)
	}
}

func TestQuitFromTheWelcomeScreen(t *testing.T) {
	model := newTestModel()
	model.Key(tui.Key{Type: tui.KeyRune, Rune: 'q'})
	if intent := model.Intent(); intent != IntentQuit {
		t.Fatalf("intent = %v, want quit", intent)
	}
	if model.ExitCode != 130 {
		t.Fatalf("exit code = %d, want 130", model.ExitCode)
	}
}

func TestStepsReplaceThePlanInPlace(t *testing.T) {
	model := modelAtService(t)
	press(model, tui.KeyEnter)
	model.Update(Message{Step: &Step{Title: "Create install directory", Detail: "/tmp/bin", Status: StatusOK}})
	steps := model.Steps()
	if len(steps) != len(Plan(model.Options())) {
		t.Fatalf("steps = %d, want the plan length", len(steps))
	}
	if steps[0].Status != StatusOK || steps[0].Detail != "/tmp/bin" {
		t.Fatalf("first step = %#v", steps[0])
	}
	if steps[1].Status != StatusRunning {
		t.Fatalf("second step = %#v, want running", steps[1])
	}
}

func TestDoneAndFailureScreens(t *testing.T) {
	model := modelAtService(t)
	press(model, tui.KeyEnter)
	model.Update(Message{Result: &Result{Dir: "/tmp/bin", Version: "v1.2.3", Service: false}})
	if model.Screen != ScreenDone {
		t.Fatalf("screen = %v, want done", model.Screen)
	}
	press(model, tui.KeyEnter)
	if model.ExitCode != 0 || model.Intent() != IntentQuit {
		t.Fatalf("exit = %d, intent = %v", model.ExitCode, model.Intent())
	}

	failed := modelAtService(t)
	press(failed, tui.KeyEnter)
	failed.Update(Message{Err: errors.New("disk full")})
	if failed.Screen != ScreenFailed {
		t.Fatalf("screen = %v, want failed", failed.Screen)
	}
	press(failed, tui.KeyEnter)
	if failed.ExitCode != 1 || failed.Intent() != IntentQuit {
		t.Fatalf("exit = %d, intent = %v", failed.ExitCode, failed.Intent())
	}
}

func TestTicksAdvanceTheSpinner(t *testing.T) {
	model := newTestModel()
	before := model.spinner()
	model.Update(Message{Tick: true})
	if model.spinner() == before {
		t.Fatal("a tick must advance the spinner")
	}
}

func TestRunningIsTrueOnlyWhileWorkRuns(t *testing.T) {
	model := newTestModel()
	if model.Running() {
		t.Fatal("nothing runs on the welcome screen")
	}
	begin(model)
	if !model.Running() {
		t.Fatal("the checks are running")
	}
	model.Update(Message{Checks: passChecks()})
	if model.Running() {
		t.Fatal("the checks finished")
	}
}

func modelAtLocation(t *testing.T) *Model {
	t.Helper()
	model := newTestModel()
	model.Screen = ScreenLocation
	return model
}

func modelAtImage(t *testing.T) *Model {
	t.Helper()
	model := modelAtLocation(t)
	press(model, tui.KeyEnter)
	if model.Screen != ScreenImage {
		t.Fatalf("screen = %v, want image", model.Screen)
	}
	return model
}

func modelAtService(t *testing.T) *Model {
	t.Helper()
	model := modelAtImage(t)
	press(model, tui.KeyEnter)
	if model.Screen != ScreenService {
		t.Fatalf("screen = %v, want service", model.Screen)
	}
	return model
}
