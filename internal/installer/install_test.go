package installer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeRunner struct {
	// calls records every command, in order.
	calls []string
	// answers maps a command to its output; a missing entry fails.
	answers map[string]string
	// failures maps a command to an error.
	failures map[string]error
}

func (f *fakeRunner) run(_ context.Context, name string, args ...string) ([]byte, error) {
	key := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, key)
	if err, ok := f.failures[key]; ok {
		return nil, err
	}
	if answer, ok := f.answers[key]; ok {
		return []byte(answer), nil
	}
	return nil, errors.New("not found")
}

// prepareSources writes stand-in executables and returns their paths.
func prepareSources(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	scopra := filepath.Join(dir, "scopra")
	scoprad := filepath.Join(dir, "scoprad")
	for _, path := range []string{scopra, scoprad} {
		if err := os.WriteFile(path, []byte("#!/bin/sh\necho "+filepath.Base(path)+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return scopra, scoprad
}

func TestExecuteInstallsBothBinaries(t *testing.T) {
	scopra, scoprad := prepareSources(t)
	dir := filepath.Join(t.TempDir(), "bin")
	runner := &fakeRunner{}
	options := Options{
		Version: "v9.9.9",
		Dir:     dir,
		Scopra:  scopra,
		Scoprad: scoprad,
		Run:     runner.run,
	}
	var steps []Step
	result, err := Execute(context.Background(), options, func(step Step) { steps = append(steps, step) })
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Dir != dir || result.Service {
		t.Fatalf("result = %#v", result)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("no service was requested, but ran %v", runner.calls)
	}
	for _, name := range []string{"scopra", "scoprad"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if info.Mode().Perm() != 0o755 {
			t.Fatalf("%s mode = %v, want 0755", name, info.Mode().Perm())
		}
		content, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || !strings.Contains(string(content), name) {
			t.Fatalf("%s content = %q, %v", name, content, err)
		}
	}
	if len(steps) != 3 {
		t.Fatalf("got %d steps, want 3: %#v", len(steps), steps)
	}
	for _, step := range steps {
		if step.Status != StatusOK {
			t.Fatalf("step %q: %v", step.Title, step.Status)
		}
	}
	// The directory is created with the user's own permissions.
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		t.Fatal("the install directory is not a directory")
	}
}

func TestExecuteFailsClosedWithoutADaemon(t *testing.T) {
	scopra, _ := prepareSources(t)
	dir := t.TempDir()
	_, err := Execute(context.Background(), Options{
		Dir:     dir,
		Scopra:  scopra,
		Service: true,
		Run:     (&fakeRunner{}).run,
	}, nil)
	if err == nil {
		t.Fatal("a requested service without scoprad must fail")
	}
	if !strings.Contains(err.Error(), "Install scoprad") {
		t.Fatalf("error = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "scoprad")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("scoprad should not exist: %v", statErr)
	}
}

func TestExecuteWarnsWhenTheDaemonIsNotBundled(t *testing.T) {
	scopra, _ := prepareSources(t)
	dir := t.TempDir()
	var steps []Step
	result, err := Execute(context.Background(), Options{
		Dir:    dir,
		Scopra: scopra,
		Run:    (&fakeRunner{}).run,
	}, func(step Step) { steps = append(steps, step) })
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "scoprad is not installed") {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
	if steps[2].Status != StatusWarn {
		t.Fatalf("scoprad step = %#v", steps[2])
	}
}

func TestExecuteInstallsAndVerifiesTheService(t *testing.T) {
	scopra := filepath.Join(t.TempDir(), "scopra")
	if err := os.WriteFile(scopra, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	installed := filepath.Join(dir, "scopra")
	scoprad := filepath.Join(dir, "scoprad")
	runner := &fakeRunner{answers: map[string]string{
		installed + " service install --scoprad " + scoprad + " --sandbox-image debian@sha256:" + strings.Repeat("a", 64): "Installed unit.\n",
		installed + " service status --json": `{"schemaVersion":1,"installed":true,"enabled":"enabled","active":"active"}`,
	}}
	steps := []Step{}
	result, err := Execute(context.Background(), Options{
		Dir:          dir,
		Scopra:       scopra,
		Scoprad:      scopra, // copies to dir/scoprad
		Service:      true,
		SandboxImage: "debian@sha256:" + strings.Repeat("a", 64),
		Run:          runner.run,
	}, func(step Step) { steps = append(steps, step) })
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.ServiceActive || !result.Service {
		t.Fatalf("result = %#v", result)
	}
	if len(steps) != 5 || steps[4].Status != StatusOK {
		t.Fatalf("steps = %#v", steps)
	}
	if len(runner.calls) != 2 {
		t.Fatalf("calls = %v", runner.calls)
	}
	if !strings.HasPrefix(runner.calls[0], installed+" service install") {
		t.Fatalf("first call = %q, want the installed scanner to install the unit", runner.calls[0])
	}
}

func TestExecuteStopsWhenTheServiceFails(t *testing.T) {
	scopra, scoprad := prepareSources(t)
	dir := t.TempDir()
	installed := filepath.Join(dir, "scopra")
	runner := &fakeRunner{failures: map[string]error{
		installed + " service install --scoprad " + filepath.Join(dir, "scoprad"): errors.New("systemd said no"),
	}}
	var last Step
	_, err := Execute(context.Background(), Options{
		Dir:     dir,
		Scopra:  scopra,
		Scoprad: scoprad,
		Service: true,
		Run:     runner.run,
	}, func(step Step) { last = step })
	if err == nil {
		t.Fatal("a failed service install must fail the install")
	}
	if last.Title != "Install systemd user service" || last.Status != StatusFailed {
		t.Fatalf("last step = %#v", last)
	}
	if !strings.Contains(err.Error(), "systemd said no") {
		t.Fatalf("error = %v", err)
	}
}

func TestExecuteFailsWhenTheServiceIsNotActive(t *testing.T) {
	scopra, scoprad := prepareSources(t)
	dir := t.TempDir()
	installed := filepath.Join(dir, "scopra")
	runner := &fakeRunner{answers: map[string]string{
		installed + " service install --scoprad " + filepath.Join(dir, "scoprad"): "Installed unit.\n",
		installed + " service status --json":                                      `{"installed":true,"active":"inactive"}`,
	}}
	_, err := Execute(context.Background(), Options{
		Dir: dir, Scopra: scopra, Scoprad: scoprad, Service: true, Run: runner.run,
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "want active") {
		t.Fatalf("error = %v", err)
	}
}

func TestExecuteFailsWhenServiceStatusIsNotJSON(t *testing.T) {
	scopra, scoprad := prepareSources(t)
	dir := t.TempDir()
	installed := filepath.Join(dir, "scopra")
	runner := &fakeRunner{answers: map[string]string{
		installed + " service install --scoprad " + filepath.Join(dir, "scoprad"): "Installed unit.\n",
		installed + " service status --json":                                      "Installed unit.\n",
	}}
	_, err := Execute(context.Background(), Options{
		Dir: dir, Scopra: scopra, Scoprad: scoprad, Service: true, Run: runner.run,
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "invalid JSON") {
		t.Fatalf("error = %v", err)
	}
}

func TestExecuteRepeatsSafely(t *testing.T) {
	scopra, scoprad := prepareSources(t)
	dir := t.TempDir()
	options := Options{Dir: dir, Scopra: scopra, Scoprad: scoprad}
	for attempt := 0; attempt < 2; attempt++ {
		if _, err := Execute(context.Background(), options, nil); err != nil {
			t.Fatalf("attempt %d: %v", attempt+1, err)
		}
	}
}

func TestExecuteDoesNotCopyTheRunningBinaryOntoItself(t *testing.T) {
	scopra, scoprad := prepareSources(t)
	dir := filepath.Dir(scopra)
	var steps []Step
	result, err := Execute(context.Background(), Options{
		Dir: dir, Scopra: scopra, Scoprad: scoprad,
	}, func(step Step) { steps = append(steps, step) })
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(steps[1].Detail, "already in place") {
		t.Fatalf("scopra step = %#v", steps[1])
	}
	if result.Dir != dir {
		t.Fatalf("dir = %q", result.Dir)
	}
}

func TestValidateDir(t *testing.T) {
	cases := []struct {
		dir  string
		want string
	}{
		{"/home/user/.local/bin", ""},
		{"", "cannot be empty"},
		{"relative/bin", "absolute"},
		{"/", "filesystem root"},
	}
	for _, testCase := range cases {
		err := ValidateDir(testCase.dir)
		if testCase.want == "" {
			if err != nil {
				t.Fatalf("ValidateDir(%q) = %v", testCase.dir, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), testCase.want) {
			t.Fatalf("ValidateDir(%q) = %v, want %q", testCase.dir, err, testCase.want)
		}
	}
}

func TestValidateImage(t *testing.T) {
	if err := ValidateImage(""); err != nil {
		t.Fatalf("an empty image is allowed: %v", err)
	}
	if err := ValidateImage("debian@sha256:" + strings.Repeat("a", 64)); err != nil {
		t.Fatalf("a pinned image must pass: %v", err)
	}
	if err := ValidateImage("debian:latest"); err == nil {
		t.Fatal("an unpinned image must be refused")
	}
}

func TestDefaultDir(t *testing.T) {
	lookup := func(values map[string]string) func(string) (string, bool) {
		return func(key string) (string, bool) {
			value, ok := values[key]
			return value, ok
		}
	}
	home := func() (string, error) { return "/home/user", nil }
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"xdg", map[string]string{"XDG_BIN_HOME": "/opt/bin"}, "/opt/bin"},
		{"relative xdg ignored", map[string]string{"XDG_BIN_HOME": "relative"}, "/home/user/.local/bin"},
		{"fallback", nil, "/home/user/.local/bin"},
	}
	for _, testCase := range cases {
		got, err := DefaultDir(lookup(testCase.env), home)
		if err != nil {
			t.Fatalf("%s: %v", testCase.name, err)
		}
		if got != testCase.want {
			t.Fatalf("%s: got %q, want %q", testCase.name, got, testCase.want)
		}
	}
	if _, err := DefaultDir(nil, func() (string, error) { return "", errors.New("no home") }); err == nil {
		t.Fatal("a missing home directory must fail")
	}
}

func TestSummaryNamesTheDirectoryAndWarnings(t *testing.T) {
	summary := Summary(Result{Dir: "/opt/bin", Version: "v1.2.3", Service: true, Warnings: []string{"no image"}})
	for _, want := range []string{"/opt/bin", "v1.2.3", "scoprad.service", "no image"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary %q does not mention %q", summary, want)
		}
	}
}
