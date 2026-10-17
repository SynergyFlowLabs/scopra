package installer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/SynergyFlowLabs/scopra/internal/tui"
)

// Runner drives the interface: it draws frames, forwards keys to the model,
// and performs the work the model asks for.
type Runner struct {
	Terminal *tui.Terminal
	Model    *Model
	// Base carries the values the flags supplied; the interface may override
	// the directory, image, and service answers.
	Base Options
	Env  Environment
}

// spinnerInterval is how often the progress indicator advances.
const spinnerInterval = 90 * time.Millisecond

// Run draws the interface until the model asks to quit, and returns the exit
// status the process should use.
func (r *Runner) Run(ctx context.Context) (int, error) {
	if r.Terminal == nil || r.Model == nil {
		return 1, fmt.Errorf("installer: the interface is not configured")
	}
	if err := r.Terminal.Enter(); err != nil {
		return 1, err
	}
	defer r.Terminal.Exit()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// The event stream is restarted around a child process that owns the
	// terminal, so it lives in a variable rather than a fixed channel.
	streamCtx, streamCancel := context.WithCancel(ctx)
	events := r.Terminal.Events(streamCtx)
	defer func() {
		if streamCancel != nil {
			streamCancel()
		}
	}()

	// stopEvents retires the reader before the terminal is handed to a child;
	// otherwise both the child and the reader would race for keystrokes.
	stopEvents := func() {
		if streamCancel == nil {
			return
		}
		streamCancel()
		r.Terminal.Pause()
		streamCancel = nil
		deadline := time.NewTimer(2 * time.Second)
		defer deadline.Stop()
		for {
			select {
			case _, open := <-events:
				if !open {
					return
				}
			case <-deadline.C:
				return
			}
		}
	}
	startEvents := func() {
		streamCtx, streamCancel = context.WithCancel(ctx)
		events = r.Terminal.Events(streamCtx)
	}

	messages := make(chan Message, 32)

	width, height := r.Terminal.Size()
	lastWidth, lastHeight := -1, -1
	draw := func() {
		if width != lastWidth || height != lastHeight {
			r.Terminal.Write("\x1b[2J")
			lastWidth, lastHeight = width, height
		}
		sheet := tui.StyleSheet{Profile: r.Terminal.Profile()}
		r.Terminal.Write(r.Model.View(width, height, sheet))
	}

	ticker := time.NewTicker(spinnerInterval)
	defer ticker.Stop()

	draw()
	for {
		select {
		case <-ctx.Done():
			return 130, nil
		case event, ok := <-events:
			if !ok {
				return r.Model.ExitCode, nil
			}
			if event.Resize {
				width, height = r.Terminal.Size()
				draw()
				continue
			}
			r.Model.Key(event.Key)
			if intent := r.Model.Intent(); intent != IntentNone {
				switch intent {
				case IntentQuit:
					return r.Model.ExitCode, nil
				case IntentChecks:
					go r.runChecks(ctx, messages)
				case IntentInstall:
					go r.runInstall(ctx, messages)
				case IntentProvision:
					plan := r.Model.Setup()
					if plan != nil {
						stopEvents()
						// The child owns the terminal: sudo has to be able to
						// prompt, and the interface must not read the
						// password.
						r.Terminal.Suspend()
						in, out := r.Terminal.Devices()
						setupErr := RunProvision(ctx, *plan, Stdio{In: in, Out: out, Err: out})
						if err := r.Terminal.Resume(); err != nil {
							return 1, err
						}
						r.Terminal.Unpause()
						startEvents()
						width, height = r.Terminal.Size()
						lastWidth, lastHeight = -1, -1
						messages <- Message{ProvisionDone: true, ProvisionErr: setupErr}
						go r.runChecks(ctx, messages)
					}
				}
			}
			draw()
		case message := <-messages:
			r.Model.Update(message)
			width, height = r.Terminal.Size()
			draw()
		case <-ticker.C:
			if r.Model.Running() {
				r.Model.Update(Message{Tick: true})
				draw()
			}
		}
	}
}

// runChecks performs the preflight off the drawing path. When Podman is the
// blocker and the distribution is one the installer knows, the message carries
// the plan the interface offers.
func (r *Runner) runChecks(ctx context.Context, messages chan<- Message) {
	checks := r.Env.Checks(ctx)
	message := Message{Checks: checks}
	if missingPodman(checks) && r.Env.Provision != nil {
		if plan, ok := r.Env.Provision(); ok {
			message.Provision = &plan
		}
	}
	messages <- message
}

// missingPodman reports whether the Podman check is what blocks the install.
func missingPodman(checks []Check) bool {
	for _, check := range checks {
		if check.ID == "podman" && check.Failed() {
			return true
		}
	}
	return false
}

// runInstall performs the install off the drawing path, forwarding each step.
func (r *Runner) runInstall(ctx context.Context, messages chan<- Message) {
	options := r.Base
	options.Version = r.Model.Version
	options.Dir = r.Model.Dir.Trimmed()
	options.SandboxImage = r.Model.Image.Trimmed()
	options.Service = r.Model.installService
	// The model's plan is the truth; Execute reports the same titles.
	result, err := Execute(ctx, options, func(finished Step) {
		copied := finished
		messages <- Message{Step: &copied}
	})
	notice := ""
	if err == nil && !onPath(options.Dir) {
		notice = fmt.Sprintf("Your shell does not find scopra yet. Add this to ~/.profile or ~/.zshrc, then open a new terminal:\n\nexport PATH=\"%s:$PATH\"", options.Dir)
	}
	messages <- Message{Result: &result, Err: err, PathNotice: notice}
}

// onPath reports whether dir is one of the directories in $PATH.
func onPath(dir string) bool {
	cleaned := filepath.Clean(dir)
	for _, entry := range filepath.SplitList(os.Getenv("PATH")) {
		if entry == "" {
			continue
		}
		if filepath.Clean(entry) == cleaned {
			return true
		}
	}
	return false
}

// Summary renders a plain-text result for non-interactive runs.
func Summary(result Result) string {
	var out strings.Builder
	fmt.Fprintf(&out, "Installed scopra %s in %s\n", result.Version, result.Dir)
	if result.Service {
		fmt.Fprintf(&out, "scoprad.service is installed and active\n")
	}
	for _, warning := range result.Warnings {
		fmt.Fprintf(&out, "Warning: %s\n", warning)
	}
	return out.String()
}
