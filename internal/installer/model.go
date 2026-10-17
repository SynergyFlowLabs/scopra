package installer

import (
	"strings"

	"github.com/SynergyFlowLabs/scopra/internal/tui"
)

// Screen is one page of the installer.
type Screen int

const (
	// ScreenWelcome introduces the installer.
	ScreenWelcome Screen = iota
	// ScreenChecks shows the preflight results while they are collected.
	ScreenChecks
	// ScreenLocation asks where to install.
	ScreenLocation
	// ScreenImage asks for the optional sandbox image.
	ScreenImage
	// ScreenService asks whether to install the systemd user unit.
	ScreenService
	// ScreenSetup offers to install a missing prerequisite with the
	// distribution's package manager.
	ScreenSetup
	// ScreenRunning shows install progress.
	ScreenRunning
	// ScreenDone summarizes a successful install.
	ScreenDone
	// ScreenFailed explains a failure.
	ScreenFailed
	// ScreenAborted reports that the user quit.
	ScreenAborted
)

// Intent is what the model wants the runner to do next.
type Intent int

const (
	// IntentNone means the model is waiting for input.
	IntentNone Intent = iota
	// IntentChecks means run the preflight.
	IntentChecks
	// IntentInstall means run the install with the chosen options.
	IntentInstall
	// IntentProvision means install the missing prerequisite.
	IntentProvision
	// IntentQuit means exit, with ExitCode.
	IntentQuit
)

// Message is what the runner feeds back into the model.
type Message struct {
	// Checks carries preflight results.
	Checks []Check
	// ChecksErr reports that the preflight itself could not run.
	ChecksErr error
	// Step is one finished install step.
	Step *Step
	// Result is the finished install.
	Result *Result
	// Err ends the install with a failure.
	Err error
	// PathNotice is a shell snippet for a directory that is not on PATH.
	PathNotice string
	// Provision is the plan for installing a missing prerequisite, or nil.
	Provision *Provision
	// ProvisionDone reports that a provisioning attempt finished, with
	// ProvisionErr set when it failed.
	ProvisionDone bool
	ProvisionErr  error
	// Tick advances the spinner.
	Tick bool
}

// Model is the installer's deterministic state machine. It owns no
// terminals, files, or processes: the runner performs work and feeds results
// back as messages, which is what makes every screen testable.
type Model struct {
	Version string
	// HasDaemon reports whether scoprad was found beside the installer.
	HasDaemon bool
	// Dir is where the binaries go; Image is the optional sandbox image.
	Dir   *Input
	Image *Input

	Screen Screen
	intent Intent
	// ExitCode is set when the intent is IntentQuit.
	ExitCode int

	checks []Check
	// checksRunning is true until the results arrive.
	checksRunning bool

	// serviceChoices are the two answers to the service question.
	serviceChoice int
	// installService is the answer, once chosen.
	installService bool
	// noService bypasses the service-choice screen when the CLI flag made
	// that opt-out explicit.
	noService bool

	steps  []Step
	result *Result
	err    error
	// tick advances on every message while work runs.
	tick int
	// PathNotice is a shell snippet shown when the install directory is not
	// on PATH. It is set by the runner.
	PathNotice string
	// setup is the plan the interface offers when a prerequisite is missing,
	// and setupErr is why the last attempt failed, if it did.
	setup    *Provision
	setupErr error
	// width and height are the last drawn size, kept for text layout.
	width  int
	height int
}

// NewModel returns the installer at its first screen. hasDaemon says whether
// scoprad was found beside the installer, which decides whether the service
// can be offered.
func NewModel(version, defaultDir, defaultImage string, hasDaemon bool) *Model {
	return &Model{
		Version:       version,
		HasDaemon:     hasDaemon,
		Dir:           NewInput("Install directory", defaultDir, "~/.local/bin"),
		Image:         NewInput("Sandbox image", defaultImage, "optional: image@sha256:…"),
		Screen:        ScreenWelcome,
		serviceChoice: 0,
	}
}

// SetNoService makes the explicit --no-service flag skip the service-choice
// screen; the runner still verifies the manual daemon context when scoprad is bundled.
func (m *Model) SetNoService() {
	m.noService = true
	m.installService = false
}

// Intent reports what the runner should do next and clears it.
func (m *Model) Intent() Intent {
	intent := m.intent
	m.intent = IntentNone
	return intent
}

// Checks returns the preflight results.
func (m *Model) Checks() []Check { return m.checks }

// Steps returns the install log.
func (m *Model) Steps() []Step { return m.steps }

// Result returns the finished install, or nil.
func (m *Model) Result() *Result { return m.result }

// Err returns the failure, or nil.
func (m *Model) Err() error { return m.err }

// Options returns the choices the user made.
func (m *Model) Options() Options {
	return Options{
		Version:      m.Version,
		Dir:          m.Dir.Trimmed(),
		SandboxImage: m.Image.Trimmed(),
		Service:      m.installService,
	}
}

// Update applies one message and returns the model.
func (m *Model) Update(message Message) {
	switch {
	case message.ProvisionDone:
		m.setupErr = message.ProvisionErr
		m.Screen = ScreenChecks
		m.checksRunning = true
		m.checks = nil
		return
	case message.Tick:
		m.tick++
		return
	case message.Checks != nil || message.ChecksErr != nil:
		m.checks = message.Checks
		m.setup = message.Provision
		m.checksRunning = false
		if message.ChecksErr != nil {
			m.err = message.ChecksErr
			m.Screen = ScreenFailed
			return
		}
		// The screen stays put: passing checks are worth reading before the
		// installer moves on, and a failure has to be seen.
		return
	case message.Step != nil:
		m.applyStep(*message.Step)
		return
	case message.Result != nil || message.Err != nil:
		m.result = message.Result
		m.err = message.Err
		m.PathNotice = message.PathNotice
		if message.Err != nil {
			m.Screen = ScreenFailed
			return
		}
		m.Screen = ScreenDone
		return
	}
}

// applyStep records a finished step in place, so the plan the interface showed
// and the log it ends with stay the same list.
func (m *Model) applyStep(step Step) {
	for index := range m.steps {
		if m.steps[index].Title != step.Title {
			continue
		}
		m.steps[index] = step
		if step.Status == StatusOK && index+1 < len(m.steps) && m.steps[index+1].Status == StatusPending {
			m.steps[index+1].Status = StatusRunning
		}
		return
	}
	m.steps = append(m.steps, step)
}

// Key applies a key press. It returns true when the key was consumed.
func (m *Model) Key(key tui.Key) bool {
	if key.Type == tui.KeyInterrupt {
		m.Screen = ScreenAborted
		m.ExitCode = 130
		m.intent = IntentQuit
		return true
	}
	switch m.Screen {
	case ScreenWelcome:
		return m.keyWelcome(key)
	case ScreenChecks:
		return m.keyChecks(key)
	case ScreenSetup:
		return m.keySetup(key)
	case ScreenLocation:
		return m.keyLocation(key)
	case ScreenImage:
		return m.keyImage(key)
	case ScreenService:
		return m.keyService(key)
	case ScreenRunning:
		return m.keyRunning(key)
	case ScreenDone:
		if key.Type == tui.KeyEnter || key.Is('q') {
			m.ExitCode = 0
			m.intent = IntentQuit
			return true
		}
	case ScreenFailed:
		if key.Type == tui.KeyEnter || key.Is('q') {
			m.ExitCode = 1
			m.intent = IntentQuit
			return true
		}
	case ScreenAborted:
		if key.Type == tui.KeyEnter {
			m.exit(130)
			return true
		}
	}
	return false
}

func (m *Model) keyWelcome(key tui.Key) bool {
	switch {
	case key.Type == tui.KeyEnter, key.Is(' '):
		m.startChecks()
		return true
	case key.Is('q'), key.Type == tui.KeyEscape:
		m.exit(130)
		return true
	}
	return false
}

func (m *Model) startChecks() {
	m.Screen = ScreenChecks
	m.checksRunning = true
	m.checks = nil
	m.intent = IntentChecks
}

func (m *Model) keyChecks(key tui.Key) bool {
	switch {
	case key.Is('q'):
		m.exit(130)
		return true
	case key.Is('f') && m.setup != nil && !m.checksRunning:
		m.Screen = ScreenSetup
		return true
	case key.Type == tui.KeyEnter && !m.checksRunning:
		if !Ready(m.checks) {
			return false
		}
		m.Screen = ScreenLocation
		m.Dir.Error = ""
		return true
	case key.Type == tui.KeyEscape:
		m.exit(130)
		return true
	}
	return false
}

// keySetup asks before anything privileged runs. Only an explicit "y" starts
// the commands; Enter cancels, because confirming a package-manager run should
// take a deliberate key.
func (m *Model) keySetup(key tui.Key) bool {
	switch {
	case key.Is('y') && m.setup != nil:
		m.setupErr = nil
		// Show the spinner rather than the stale results while the commands
		// and the re-check run. The plan stays: the runner reads it as the
		// intent is applied.
		m.checks = nil
		m.checksRunning = true
		m.Screen = ScreenChecks
		m.intent = IntentProvision
		return true
	case key.Type == tui.KeyEnter, key.Is('n'), key.Type == tui.KeyEscape:
		m.Screen = ScreenChecks
		return true
	case key.Is('q'):
		m.exit(130)
		return true
	}
	return false
}

func (m *Model) keyLocation(key tui.Key) bool {
	switch key.Type {
	case tui.KeyEnter:
		dir := m.Dir.Trimmed()
		if err := ValidateDir(dir); err != nil {
			m.Dir.Error = err.Error()
			return true
		}
		m.Dir.Error = ""
		m.Image.Error = ""
		m.Screen = ScreenImage
		return true
	case tui.KeyEscape:
		m.Screen = ScreenWelcome
		return true
	}
	m.Dir.Handle(key)
	return true
}

func (m *Model) keyImage(key tui.Key) bool {
	switch key.Type {
	case tui.KeyEnter:
		if err := ValidateImage(m.Image.Trimmed()); err != nil {
			m.Image.Error = strings.TrimPrefix(err.Error(), "hollow: ")
			return true
		}
		m.Image.Error = ""
		if m.noService {
			m.installService = false
			m.beginInstall()
			return true
		}
		m.serviceChoice = 0
		m.Screen = ScreenService
		return true
	case tui.KeyEscape:
		m.Screen = ScreenLocation
		return true
	}
	m.Image.Handle(key)
	return true
}

func (m *Model) keyService(key tui.Key) bool {
	switch key.Type {
	case tui.KeyUp, tui.KeyDown, tui.KeyTab, tui.KeyShiftTab:
		m.serviceChoice = 1 - m.serviceChoice
		return true
	case tui.KeyEnter:
		m.installService = m.serviceChoice == 0 && m.HasDaemon
		m.beginInstall()
		return true
	case tui.KeyEscape:
		m.Screen = ScreenImage
		return true
	}
	return true
}

func (m *Model) beginInstall() {
	m.steps = Plan(m.Options())
	for index := range m.steps {
		m.steps[index].Status = StatusPending
	}
	m.Screen = ScreenRunning
	m.intent = IntentInstall
}

func (m *Model) keyRunning(tui.Key) bool {
	// The install cannot be interrupted safely once a unit is being written,
	// so quit is offered only before it starts. Ctrl-C is handled above.
	return false
}

func (m *Model) exit(code int) {
	m.Screen = ScreenAborted
	m.ExitCode = code
	m.intent = IntentQuit
}

// Running reports whether work is in flight.
// Setup returns the plan the interface is offering, or nil.
func (m *Model) Setup() *Provision { return m.setup }

// SetupErr returns why the last provisioning attempt failed, or nil.
func (m *Model) SetupErr() error { return m.setupErr }

func (m *Model) Running() bool {
	return m.Screen == ScreenChecks && m.checksRunning || m.Screen == ScreenRunning && m.result == nil && m.err == nil
}
