# ADR-016: One-Command Interactive Installer

**Status:** Accepted
**Date:** 2026-10-17

## Context

Installing Scopra by hand means four things a reader has to get right: pick the
release archive for the architecture, verify it, put `scopra` and `scoprad` on
`PATH`, and decide whether to write a systemd user unit. Each is easy to get
wrong, and the consequences are quiet — a `scoprad` beside the wrong `scopra`, an
unverified download, a unit pointing at a binary that does not exist.

The project also has real prerequisites that a caller must understand before
anything runs: rootless Podman 4.0 or later, cgroup v2 delegation, a systemd user
session, and a resolvable state directory. Today the only guidance is prose in
the README and `docs/operations/installation.md`, and the only script,
`scripts/bootstrap.sh`, is a developer bootstrap that requires Go and downloads
modules. Neither is an install experience for someone who just wants the tool.

Two constraints shape the answer:

- the project ships **one dependency-free Go toolchain** and two binaries; adding
  a Node or Python installer would add a second language to own;
- the install writes files and enables a service, so it must be **explicit,
  reviewable, and fail-closed**. Silent installs, `sudo`, and `curl | sudo sh`
  are out.

A plain `read`-driven shell prompt could ask the questions, but it cannot show
live progress, a checklist that updates while Podman is queried, a cursor in a
text field, or a resize-safe layout — and it is awkward to test. A TUI compiled
from the same module can be tested without a terminal at all.

## Decision

**Two layers: a POSIX `install.sh` that only downloads and verifies, and a Go TUI
(`scopra install`) that does the installing.**

### `install.sh` — bootstrap only

Published at `https://scopra-six.vercel.app/install.sh`, so the command a user
copies is:

```console
$ curl -fsSL https://scopra-six.vercel.app/install.sh | sh
```

It is POSIX `sh` (dash, ash, busybox), `set -eu`, and does exactly this:

1. refuses any platform other than Linux, with the reason, before downloading;
2. maps `uname -m` to `amd64` or `arm64`;
3. reads `/releases/latest.txt` for the current tag (or uses `SCOPRA_VERSION`);
4. downloads `/releases/<tag>/checksums.txt` and
   `/releases/<tag>/scopra_linux_<arch>.tar.gz` with `curl -f` or `wget`;
5. requires **exactly one** checksum line naming the archive, of the right shape,
   and compares it with `sha256sum` or `shasum -a 256`;
6. unpacks into a `mktemp -d` directory that a trap removes on exit;
7. executes `scopra install "$@"` and returns its status.

It never uses `sudo`, never evaluates downloaded text, never installs anything
itself, and never writes outside the temporary directory. Because it passes its
arguments through, `sh -s -- --yes --dir /opt/bin` works for scripted installs.

**The release archives are served by the site itself**, under
`/releases/<tag>/`, with a one-line `/releases/latest.txt` naming the current
tag. The script, its documentation, and the archives it downloads therefore
share one origin, one TLS certificate, and one deployment: there is no second
host to keep in step, and no redirect or API call stands between the copy-paste
command and the bytes it verifies. GitHub Releases would have worked, but it
adds a second distribution channel with its own upload step, asset limits, and
redirect chain while the site build already has to publish the script.

`scripts/release.sh vX.Y.Z --publish` is what keeps the two layers in sync: it
builds the archives, then copies them and `checksums.txt` into
`apps/docs-site/public/releases/<tag>/` and rewrites `latest.txt`. The archives
are committed with the site, so a deployment is a fixed, reviewable set of bytes
— `go test ./tools/docsite` regenerates the site and fails when the committed
output differs. Publishing is a separate flag so that a local build for testing
never moves the `latest` pointer by accident. The site build (`tools/docsite`)
preserves `public/releases/` when it rebuilds the rest of the site.

### `scopra install` — the interactive installer

A new `internal/tui` package (standard library only) provides raw-mode input on
Linux, ANSI key decoding, a cell-grid frame renderer, and styled text. A new
`internal/installer` package holds the state machine, the screens, the preflight,
and the install itself. `cmd/scopra/install.go` wires them together.

- **The terminal is opened separately from stdin.** `curl … | sh` gives the
  script's standard input to the shell, so the installer falls back to
  `/dev/tty`. Without that, the one-command install could not be interactive at
  all.
- **Keys are decoded, not assumed.** CSI and SS3 sequences, Alt-modified runes,
  Shift-Tab, and a lone Escape are decoded; an idle incomplete sequence is
  discarded rather than guessed at, so a stray bracket cannot drive the
  interface. Control bytes from a paste are dropped before they reach a field.
- **Every check is the same rule Mio applies.** The preflight runs
  `podman info --format json` and hands it to `hollow.ParsePodmanInfo` and
  `PodmanInfo.Check`, so version, rootless mode, cgroup version, and delegated
  controllers are judged once, in one place. A failing required check stops the
  install; there is no "install anyway".
- **Nothing is silent.** With no terminal and no `--yes`, the installer exits
  with an explanation instead of installing. The summary states what changed.
- **The install is small and repeatable.** Create the directory, prove it is
  writable, copy each binary through a temporary file and a rename, then call the
  existing `scopra service install` (never a second implementation of the unit)
  and confirm with `scopra service status --json`. Any failure stops the
  sequence and is shown with the step that failed.
- **No terminal state leaks.** Alternate screen, hidden cursor, and disabled line
  wrapping are restored on exit, interrupt, and quit; `Ctrl-C` exits 130.

Non-interactive installs are the same core with a line-oriented reporter, so
there is one implementation of the install and two front-ends.

### Releases and the version stamp

`scripts/release.sh vX.Y.Z [--publish]` cross-compiles `linux/amd64` and
`linux/arm64` with `CGO_ENABLED=0` and `-trimpath`, stamps the tag into both
binaries with `-X …/internal/buildinfo.VersionOverride`, and writes one archive
per platform plus `checksums.txt`. Suffixing `--publish` copies them to
`apps/docs-site/public/releases/<tag>/` and points `latest.txt` at the tag, which
is what `install.sh` reads. `scopra version` reports the stamp; an invalid stamp
is ignored, so a typo cannot produce a lying version.

The archives live in the repository rather than in a binary store: two
platforms, about six megabytes, and a handful of releases is small enough for
Git to carry, and it makes what users download byte-for-byte the same thing the
site test compares against. A release is therefore a commit plus a deployment,
which is a workflow the project already has.

> **Amended 2026-10-17 (ADR-017).** The install itself still never uses `sudo`
> and still changes only the install directory and the user unit. A missing
> prerequisite is the one exception, and it is opt-in: the interface prints the
> distribution's exact command, waits for `y`, and hands the terminal to the
> child so `sudo` can prompt. `--yes` never provisions. Read "never uses `sudo`"
> below as "the install never does; a prerequisite is offered, not assumed".

## Consequences

- A user copies one line and gets a checklist, a choice of directory, an image
  field, a service question, live progress, and a summary — with nothing changed
  until they confirm.
- The installer is testable without a terminal: the model is a pure state
  machine, the frame renderer is asserted cell by cell, and `install.sh` is
  exercised end to end against a fake release, including a corrupted archive.
- Two artifacts must stay in sync: `install.sh` and the release naming scheme,
  and the installer and `scopra service install`. Tests pin the naming scheme and
  the installer calls the command rather than duplicating it.
- **The checksum is integrity, not authorship.** `checksums.txt` travels beside
  the archive over the same TLS connection. A signature is not implemented; the
  documentation says so plainly rather than implying more.
- macOS and Windows are refused before anything is downloaded, because Mio's
  containment is Linux-only. Shipping archives that could not run would be worse
  than a clear refusal.
- The TUI is owned code: roughly a thousand lines including tests, which is the
  price of the no-dependency rule.

## Alternatives considered

- **A shell-only interactive installer.** Rejected: arrow keys, a text cursor,
  resize handling, and a live checklist are not portable across `dash`, `ash`,
  and busybox, and the result could only be tested by driving a pty.
- **A third-party TUI library (Bubble Tea, tcell, termbox).** Rejected: the
  project has no external Go dependencies, and the subset needed here — keys,
  frames, styles, raw mode — is small enough to own and test.
- **Installing with `sudo` into `/usr/local/bin`.** Rejected: Mio is a per-user
  daemon with a per-user state directory, socket, and unit. A root install would
  create a second, mismatched set of paths and permissions.
- **Auto-detecting everything and installing without questions.** Rejected: the
  sandbox image is a security decision, and installing a service that starts at
  login without asking changes the machine's behaviour. The preflight may pass
  silently; the decisions may not.
- **Skipping preflight when Podman is absent and installing anyway.** Rejected:
  it would produce an install that cannot run, and the project's rule is to fail
  closed with a reason.
- **Publishing the installer as a second binary beside `scopra`.** Rejected: one
  more artifact to build, sign, and explain; `scopra install --yes` already
  covers scripted installs.
- **A signed installer script or a Sigstore-signed release.** Not rejected on
  principle — deferred. It needs key management and a signing step that this
  repository does not have yet; the documentation states the current boundary.
