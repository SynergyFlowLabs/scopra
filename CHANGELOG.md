# Changelog

All notable changes to this project are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Scopra is pre-alpha. Until `v0.1.0`, breaking changes may appear in any minor
version, and the wire protocol is versioned separately (see ADR-002 and ADR-009).

## [Unreleased]

### Added

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

- `localipc.WorkspaceError` is now `localipc.ReplyError`, since the logs reply
  uses the same typed error. The wire format is unchanged.
- `Daemon.EnableExecutions` now takes the executor and the run journal, and
  requires both.

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
