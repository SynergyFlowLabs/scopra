# Changelog

All notable changes to this project are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Scopra is pre-alpha. Until `v0.1.0`, breaking changes may appear in any minor
version, and the wire protocol is versioned separately (see ADR-002 and ADR-009).

## [Unreleased]

### Added

- **Cgroup preflight now matches the daemon's service context (ADR-017).** The
  installer and `scopra doctor` run `podman info` inside a transient systemd
  user service requesting `Delegate=cpu cpuset io memory pids`; the generated
  `scoprad.service` requests the same controllers. The installer no longer
  trusts a desktop terminal's less-delegated cgroup, and a no-service install
  additionally checks the current process context where `scoprad` would be
  started. The opt-in user-manager drop-in is re-probed in the same session;
  an already-installed fix is not offered again, and no unverified logout/login
  instruction is shown. Hollow's runtime containment check remains fail-closed.
- **The installer can set up a missing prerequisite (ADR-017).** When the
  preflight fails because rootless Podman is absent, the checks screen offers
  `f` to install it with the distribution's own package manager. A confirmation
  screen prints every argument vector first; `y` runs them, Enter, `n`, or
  Escape cancels, and `--yes` never provisions. The plan is derived from
  `/etc/os-release` and a fixed table — `dnf` on Fedora/RHEL and derivatives,
  `apt-get` plus `uidmap`, `slirp4netns`, and `fuse-overlayfs` on
  Debian/Ubuntu, `pacman` on Arch, `zypper` on openSUSE, `apk` on Alpine — and
  is refused outright on an unknown distribution, a missing package manager, or
  a non-root process without `sudo`. Commands are argument vectors, never shell
  strings. Before they run, the interface retires its key reader, leaves the
  alternate screen, and restores the terminal, so `sudo` prompts normally and
  the installer never sees the password. On success the preflight runs again and
  the install continues; on failure the output stays on the screen and the
  checks screen shows the error next to the manual fix. `scopra install --yes`
  prints the same commands but never runs them.

- **One-command interactive installer (ADR-016).**
  `curl -fsSL https://scopra-six.vercel.app/install.sh | sh` downloads the
  release archive for the machine, verifies its SHA-256 against the release's
  `checksums.txt`, and opens `scopra install`: a terminal interface that checks
  the host (Linux, systemd user session, a resolvable state directory, rootless
  Podman 4.0+ with cgroups v2), asks where to put the binaries, accepts an
  optional digest-pinned sandbox image, offers a systemd user service, copies
  both binaries atomically, and confirms the service with systemd instead of
  trusting the previous step. `--yes` runs the same install without the
  interface, and a run without a terminal and without `--yes` stops rather than
  installing silently. New `internal/tui` (standard library only) provides
  raw-mode input, ANSI key decoding, and a cell-grid renderer;
  `internal/installer` holds the state machine, preflight, and install.
  `scripts/release.sh` builds `linux/amd64` and `linux/arm64` archives with the
  version stamped into both binaries, plus `checksums.txt`; `--publish` copies
  them into the documentation site under `/releases/<tag>/` and rewrites
  `/releases/latest.txt`, so the script, the docs, and the verified bytes share
  one origin. The site build keeps `/releases/` when it rebuilds the rest of the
  site.
- **Documentation site (ADR-015).** <https://scopra-six.vercel.app> publishes the
  README, the PRD, the ADRs, the security documents, and the community files as a
  static site. It is generated from the repository's Markdown by `tools/docsite`
  (Go standard library only), committed to `apps/docs-site/public`, and served by
  Vercel with no build step, no JavaScript, and a strict content security policy.
  Links are rewritten at build time and an unresolvable link fails the build;
  `TestCommittedSiteIsUpToDate` fails when the committed site no longer matches
  the docs.
- **Run journal and `scopra logs` (ADR-013).** Mio records one bounded,
  metadata-only JSON line per answered run in `<state-dir>/runs.jsonl` —
  execution and workspace IDs, state, times, exit code, stdout/stderr byte
  counts, truncation flag, chunk count, and error code — and never the command,
  arguments, path, or output. The journal holds at most 1000 entries and 1 MiB,
  rotates atomically, is owner-only (`0600`), refuses symlinks, discards a torn
  final line, and refuses startup on any other corruption. A new read-only IPC
  command, `execution.logs`, and `scopra logs [--workspace ID] [--limit N]
  [--json]` expose it, newest first. A failed append stops later runs.
- **Systemd user service (ADR-012).** `scopra service install/uninstall/status`
  manages Mio as a systemd *user* service whose unit's `ExecStart` carries the
  daemon path, the state directory, and the pinned image. Installs are atomic,
  refuse foreign units, and roll back when systemd rejects the change.
- **MIT license and no pricing model (ADR-014).** The project is free and
  open-source under the MIT License, with no paid tiers, subscriptions, usage
  billing, plan limits, or feature gates. `NOTICE` records third-party material.
- **Community documentation.** A restructured README, `CONTRIBUTING.md`,
  `CODE_OF_CONDUCT.md` (Contributor Covenant 2.1), and a full `SECURITY.md`
  with private reporting, disclosure timelines, scope, and safe harbor.

### Changed

- **The install command is the first thing on the site.** The landing page leads
  with the one-liner and its requirements, and an "Install, every way" section
  near the bottom spells out the variants: scripted, pinned version, custom
  directory, no service, pinned image, verifying the archive by hand, and
  building from source. The README gained the same reference as "Detailed
  install commands".
- **Releases are served by the documentation site.** `install.sh` reads
  `/releases/latest.txt` and downloads from `/releases/<tag>/` instead of
  `github.com/.../releases/download/...`. GitHub releases stay as a human-facing
  mirror only; nothing in the install path calls the GitHub API.
- `localipc.WorkspaceError` is now `localipc.ReplyError`, since the logs reply
  uses the same typed error. The wire format is unchanged.
- `Daemon.EnableExecutions` now takes the executor and the run journal, and
  requires both.

### Fixed

- **The installer looped on a cgroup delegation fix it had already applied.**
  Alpha.7 stopped repeating the commands when the drop-in was present, but
  guessed that a new login was required. Diagnostics from the affected host
  disproved that explanation: `cpu` was available at the user-manager boundary,
  while the terminal's `app.slice` exposed only `memory` and `pids`. The
  cgroup-preflight correction above probes the actual delegated service context
  and removes the relogin heuristic; it also checks the current process context
  before accepting a no-service install. It is included in
  `v0.1.0-alpha.8`.
- **A scripted install printed a command that could not run.** `scopra install
  --yes` described the delegation drop-in with a `cp` from the state directory,
  but only the interactive path staged that file, so the command failed with
  "No such file or directory". The scripted path now stages it before printing
  the commands and keeps it, since the commands are for a person to run; the
  interactive path still stages, runs, and removes it. Shipped in
  `v0.1.0-alpha.6`.
- **The installer's terminal dropped every key press.** Raw mode was configured
  with a read timeout, and a quiet tenth of a second arrives as a zero-length
  read that Go reports as `io.EOF`; the reader treated it as the end of input,
  exited, and left the interface running with no keyboard. Reads now block
  until a byte arrives, and a separate idle timer decides when a pending escape
  sequence is complete, so a lone `Escape` still registers and arrow keys split
  across reads still decode. `internal/tui` gained terminal event tests covering
  a quiet terminal, a lone escape, a split sequence, end of input, and
  cancellation.

## [0.0.0] - 2026-10-17

First tagged checkpoint of the pre-alpha foundation.

### Added

- **Protocol packages.** Messenger v1 envelope validation (ADR-002), strict
  execution request decoding (ADR-007) and its canonical form and action hash
  (ADR-008), and every execution lifecycle message: started, output, result,
  and cancel (ADR-009). Reference payloads live in `packages/test-fixtures`.
- **Local daemon (`scoprad`).** Persistent per-user state directory
  (`$XDG_STATE_HOME/scopra`), exclusive state-directory lock, graceful shutdown,
  and strict same-UID Unix-socket IPC with bounded frames (ADR-003).
- **Read-only status.** `scopra status` and a `status` IPC command.
- **Workspaces.** Canonical path validation and revalidation (ADR-004),
  owner-only registry storage with atomic writes (ADR-005), and
  `scopra workspace add/list/remove` over IPC (ADR-006).
- **Diagnostics.** `scopra doctor` checks the platform, state directory,
  workspace registry, registered workspaces, Mio's endpoint, and containment
  readiness, read-only.
- **Hollow sandbox.** The exact rootless Podman argument vector, fail-closed
  preflight, and a runner with timeout, cancellation, and leftover cleanup
  (ADR-010), tested against a fake Podman.
- **Local execution.** `execution.run` and `scopra run`, streaming the start
  notice, output, and result, with the command's own exit status and JSON output
  (ADR-011).
- Six ADRs (001–006), a threat model, a risk register, a security architecture,
  a product requirements document, and an implementation plan.

[Unreleased]: https://github.com/SynergyFlowLabs/scopra/compare/v0.0.0...HEAD
[0.0.0]: https://github.com/SynergyFlowLabs/scopra/releases/tag/v0.0.0
