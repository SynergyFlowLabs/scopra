package installer

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// installScriptPath is the published bootstrap script.
func installScriptPath() string {
	return filepath.Join("..", "..", "apps", "docs-site", "static", "install.sh")
}

func readInstallScript(t *testing.T) string {
	t.Helper()
	content, err := os.ReadFile(installScriptPath())
	if err != nil {
		t.Fatalf("read install.sh: %v", err)
	}
	return string(content)
}

func TestInstallScriptIsPOSIXShell(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh is not available")
	}
	command := exec.Command("sh", "-n", installScriptPath())
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("sh -n: %v\n%s", err, output)
	}
}

func TestInstallScriptFailsClosedByConstruction(t *testing.T) {
	script := readInstallScript(t)
	for _, forbidden := range []string{"sudo", "eval ", "curl -sSL | sh", "chmod +s"} {
		if strings.Contains(codeOnly(script), forbidden) {
			t.Fatalf("install.sh must not contain %q", forbidden)
		}
	}
	for _, required := range []string{"set -eu", "sha256sum", "checksum mismatch", "mktemp -d", "trap cleanup EXIT", "SCOPRA_VERSION"} {
		if !strings.Contains(script, required) {
			t.Fatalf("install.sh must contain %q", required)
		}
	}
	if strings.Contains(script, "latest/download/checksums.txt") {
		t.Fatal("install.sh must build the release URL from one base, not hard-code a version")
	}
}

// codeOnly drops comment lines, so a comment that promises not to use sudo is
// not mistaken for code that does.
func codeOnly(script string) string {
	var out strings.Builder
	for _, line := range strings.Split(script, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		out.WriteString(line)
		out.WriteString("\n")
	}
	return out.String()
}

func TestInstallScriptRefusesMacOS(t *testing.T) {
	workdir := t.TempDir()
	script := writeTestScript(t, workdir, "file://"+filepath.Join(workdir, "releases"))
	marker := filepath.Join(workdir, "ran")
	installFakeUname(t, workdir, "Darwin", "x86_64")
	command := exec.Command("sh", script)
	command.Env = []string{"PATH=" + filepath.Join(workdir, "bin") + ":" + os.Getenv("PATH"), "MARKER=" + marker}
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("macOS must be refused, output: %s", output)
	}
	if !strings.Contains(string(output), "runs on Linux") {
		t.Fatalf("output = %s", output)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("nothing may run after the platform check")
	}
}

func TestInstallScriptDownloadsVerifiesAndRuns(t *testing.T) {
	if _, err := exec.LookPath("tar"); err != nil {
		t.Skip("tar is not available")
	}
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl is not available")
	}
	workdir := t.TempDir()
	marker := filepath.Join(workdir, "ran.txt")
	release := filepath.Join(workdir, "releases", "latest", "download")
	writeFakeRelease(t, release, "linux", "amd64", true)
	installFakeUname(t, workdir, "Linux", "x86_64")
	script := writeTestScript(t, workdir, "file://"+filepath.Join(workdir, "releases"))

	command := exec.Command("sh", script, "--yes")
	command.Env = []string{
		"PATH=" + filepath.Join(workdir, "bin") + ":" + os.Getenv("PATH"),
		"MARKER=" + marker,
	}
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("install.sh: %v\n%s", err, output)
	}
	content, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("the installer did not run: %v\n%s", err, output)
	}
	if got := strings.TrimSpace(string(content)); got != "install --yes" {
		t.Fatalf("the installer ran with %q, want \"install --yes\"", got)
	}
	if leftover, _ := filepath.Glob(filepath.Join(os.TempDir(), "scopra-install.*")); len(leftover) > 0 {
		// Only a warning: other tests may run in parallel.
		t.Logf("temporary directories left behind: %v", leftover)
	}
}

func TestInstallScriptRejectsABadChecksum(t *testing.T) {
	if _, err := exec.LookPath("tar"); err != nil {
		t.Skip("tar is not available")
	}
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl is not available")
	}
	workdir := t.TempDir()
	release := filepath.Join(workdir, "releases", "latest", "download")
	writeFakeRelease(t, release, "linux", "amd64", false)
	installFakeUname(t, workdir, "Linux", "x86_64")
	script := writeTestScript(t, workdir, "file://"+filepath.Join(workdir, "releases"))
	marker := filepath.Join(workdir, "ran")
	command := exec.Command("sh", script)
	command.Env = []string{
		"PATH=" + filepath.Join(workdir, "bin") + ":" + os.Getenv("PATH"),
		"MARKER=" + marker,
	}
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("a corrupt archive must fail, output: %s", output)
	}
	if !strings.Contains(string(output), "checksum mismatch") {
		t.Fatalf("output = %s", output)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("the installer must not run after a checksum mismatch")
	}
}

func TestInstallScriptHonoursThePinnedVersion(t *testing.T) {
	if _, err := exec.LookPath("tar"); err != nil {
		t.Skip("tar is not available")
	}
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl is not available")
	}
	workdir := t.TempDir()
	release := filepath.Join(workdir, "releases", "download", "v1.2.3")
	writeFakeRelease(t, release, "linux", "amd64", true)
	installFakeUname(t, workdir, "Linux", "x86_64")
	script := writeTestScript(t, workdir, "file://"+filepath.Join(workdir, "releases"))
	command := exec.Command("sh", script)
	command.Env = []string{
		"PATH=" + filepath.Join(workdir, "bin") + ":" + os.Getenv("PATH"),
		"MARKER=" + filepath.Join(workdir, "ran"),
		"SCOPRA_VERSION=v1.2.3",
	}
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("install.sh with a pinned version: %v\n%s", err, output)
	}
}

// writeTestScript copies install.sh and points its release base at a local
// directory. The replacement is deliberately narrow: one assignment line.
func writeTestScript(t *testing.T, workdir, base string) string {
	t.Helper()
	script := readInstallScript(t)
	lines := strings.Split(script, "\n")
	replaced := false
	for index, line := range lines {
		if strings.HasPrefix(line, "releases=") {
			lines[index] = "releases=" + quoteForShell(base)
			replaced = true
			break
		}
	}
	if !replaced {
		t.Fatal("install.sh no longer has a single releases= line to point at a test release")
	}
	target := filepath.Join(workdir, "install.sh")
	if err := os.WriteFile(target, []byte(strings.Join(lines, "\n")), 0o755); err != nil {
		t.Fatal(err)
	}
	return target
}

func quoteForShell(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// installFakeUname puts a uname that always reports the given platform first
// on PATH, so the test does not depend on the host.
func installFakeUname(t *testing.T, workdir, osName, machine string) {
	t.Helper()
	bin := filepath.Join(workdir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\ncase \"$1\" in\n-s) echo " + osName + " ;;\n-m) echo " + machine + " ;;\n*) echo " + osName + " ;;\nesac\n"
	if err := os.WriteFile(filepath.Join(bin, "uname"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

// writeFakeRelease writes checksums.txt and the archive for linux/amd64. When
// honest is false the archive is corrupted after the checksum is recorded.
func writeFakeRelease(t *testing.T, directory, osName, arch string, honest bool) {
	t.Helper()
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	archive := "scopra_" + osName + "_" + arch + ".tar.gz"
	content := buildArchive(t, map[string]string{
		"scopra":  "#!/bin/sh\nprintf '%s' \"$*\" > \"${MARKER:?}\"\n",
		"scoprad": "#!/bin/sh\nexit 0\n",
	})
	if !honest {
		content = append(content, byte('x'))
	}
	path := filepath.Join(directory, archive)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(content)
	if !honest {
		digest = sha256.Sum256(content[:len(content)-1])
	}
	checksums := hex.EncodeToString(digest[:]) + "  " + archive + "\n"
	if err := os.WriteFile(filepath.Join(directory, "checksums.txt"), []byte(checksums), 0o644); err != nil {
		t.Fatal(err)
	}
}

// buildArchive returns a gzipped tar with the given files at mode 0755.
func buildArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gzipWriter)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	// Deterministic order keeps the test independent of map iteration.
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			if names[j] < names[i] {
				names[i], names[j] = names[j], names[i]
			}
		}
	}
	for _, name := range names {
		content := files[name]
		if err := tarWriter.WriteHeader(&tar.Header{
			Name:     name,
			Mode:     0o755,
			Size:     int64(len(content)),
			Typeflag: tar.TypeReg,
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := tarWriter.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

// TestArchiveNamesMatchTheHost documents that the fake release above uses the
// same naming scheme the release script writes.
func TestArchiveNamesMatchTheHost(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the release naming scheme is defined for Linux")
	}
	script := readInstallScript(t)
	if !strings.Contains(script, `archive="scopra_${os}_${arch}.tar.gz"`) {
		t.Fatal("install.sh and the release script disagree about archive names")
	}
}
