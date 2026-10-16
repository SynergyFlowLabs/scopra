# Scopra Development Progress

**Started:** 2026-10-16 (Asia/Karachi)
**Current phase:** Milestone 1 — local secure execution foundation
**Current increment:** Mio single-daemon state-directory lock
**Status:** Implemented on `arena/c7a368c1-scopra`; local checks passed; Actions startup failure remains unresolved

This file is the running engineering log. Each increment records scope, verification, commit, and push status. Security-sensitive work remains incremental, testable, and fail-closed.

## Baseline

- The repository was an initial planning scaffold at kickoff: no Go source files or `go.mod` existed; CI only checked documentation and secret-file presence.
- Scopra remains the umbrella product. The Mercury-inspired component names are documented in `docs/product/naming.md`.
- The sandbox initially had no Go compiler (again in the Step 3 session). Each time, a temporary Go 1.26.9 toolchain was built from official `golang/go` source tags under `/tmp`; no toolchain artifacts are committed.
- Every GitHub Actions run in this repository so far has ended in `startup_failure` before any job was created. Examples: Step 2 runs `38854979363` (`90724b2`) and `38855061603` (`fbf97ed`), and the `main` merge run `38855765861`. The first commit's trivial `actions/checkout@v4` workflow failed the same way, which points to an organization- or account-level Actions policy or billing setting rather than workflow content (see Step 3). The workflow passes local `actionlint`, and GitHub gives no diagnostic. The cause is unresolved, and these runs are not code-test results.

## Working practices

- Installed and reviewed selected skills from skills.sh in `.agents/skills/` (project-local; CLI telemetry disabled): `go-project-layout`, `go-coding-standards`, `go-test-quality`, `go-cli`, `test-driven-development`, and `security-review`.
- The sources and content hashes are recorded in `skills-lock.json`.
- Keep the existing executable identifiers `scopra` and `scoprad` until a compatibility decision says otherwise.
- Go verification used Go 1.26.9 with `CGO_ENABLED=1` for the race detector. No external Go dependencies were added.

## Roadmap

1. **Protocol foundation** — envelope v1 structural validation is implemented and locally verified. Typed execution requests/results, canonical approval binding, cryptographic verification, replay storage, and compatibility/version guarantees remain pending.
2. **Local daemon and CLI** — the initial foreground Mio status service with an exclusive state-directory lock, Mariner `scopra status`, and a minimal read-only `scopra doctor` (platform, state-directory, and daemon checks) are implemented. Workspace CRUD, broader doctor checks, execution, service installation, and expanded CLI/IPC authorization remain pending.
3. **Policy and containment boundary** — implement default-deny policy and rootless containment before enabling execution; no native fallback.
4. **Structured execution and audit** — limits, cancellation, bounded output, and attributable events with adversarial tests.
5. **Enrollment, relay, approvals, and integrations** — proceed only after local foundations and security invariants are testable.

## Progress log

### 2026-10-16 — Kickoff and repository review

- Read the PRD, implementation plan, architecture, product, security, API, operations, and component documentation.
- Confirmed the project was pre-alpha with no implementation code.
- Discovered relevant Go, CLI, testing, TDD, and security-review skills from skills.sh; installed six selected project-local skills, reviewed their instructions, and recorded source hashes in `skills-lock.json`.

### 2026-10-16 — Naming and workflow setup

- Adopted the supplied component naming system and pushed it as `398b337` on `arena/0b81a55b-scopra`.
- Added this log and installed the selected skills; pushed as `ae6779d`.
- Added a single root Go module, ADR-001, ADR-002 for the envelope contract, pinned Go 1.26.9 CI, and project test/lint/bootstrap targets.
- Test-first checkpoint `cc91d35` was pushed before the protocol implementation. The local red run failed on the expected missing `Envelope`/`DecodeEnvelope` API.
- GitHub Actions runs `38848464758` (test checkpoint) and `38851033476` (implementation push) both ended in `startup_failure` with no jobs, so neither is treated as a code test result.

### 2026-10-16 — Step 1: Messenger envelope v1

- Implemented a bounded JSON envelope decoder with unknown envelope-field rejection, duplicate-key rejection at every JSON depth, a closed message-type set, opaque identifier checks, canonical 128-bit-or-longer base64url nonces, and freshness validation via `Envelope.ValidateAt` (five-minute maximum lifetime, 30-second future-clock tolerance, and strict expiry), and exact-payload SHA-256 verification.
- Added focused rejection tests and a fuzz target. Structural validation is explicitly not signature verification, replay protection, authorization, or typed payload validation; all remain required before dispatch.
- Security review covered untrusted JSON parsing, digest handling, authorization separation, and the surrounding architecture. No high-confidence exploitable issue was identified in this structural-only slice; the omitted trust/authorization checks are documented as mandatory follow-up work.
- Verification passed locally: `make bootstrap`, `make lint`, `make test`, `make security-test`, `go vet ./...`, and `go test -cover ./...` (89.4% statement coverage). A 10-second fuzz run completed 644,563 executions; the final 5-second rerun completed 81,360 executions, with no crash or finding.
- Implementation and documentation were committed as `8a58cc4` (`feat(protocol): add bounded Messenger envelope validation`) and pushed to `arena/0b81a55b-scopra`.

### 2026-10-16 — Step 2: Mio and Mariner status IPC (merged in PR #1)

- Accepted ADR-003 for the local status-only IPC boundary. Wrote tests before implementation and confirmed the expected red build for missing frame, message, daemon, and CLI APIs. A later strict-JSON review added case-alias rejection tests first; they failed against Go's case-insensitive struct-field decoding and passed after exact top-level key validation was added.
- Added foreground `scoprad`, `scopra status` (human and JSON output), `scopra version`, and build-info version reporting. Status value `running` means the local IPC endpoint responds; it does not imply execution readiness. Socket binding prevents a second daemon; stale sockets are removed only after a failed probe and inode recheck; shutdown removes only the socket inode Mio created.
- Added four-byte big-endian framing capped at 64 KiB; strict versioned JSON with exact field names, unknown/duplicate fields, trailing JSON, invalid request IDs, and unknown commands rejected; a 16-client concurrency cap; and two-second I/O deadlines.
- On Linux, both ends verify same-effective-UID `SO_PEERCRED`; the state path is checked against symlinks and unsafe writable ancestors, with mode `0700`, and the socket is `0600`. Darwin and Windows cross-compilation passed; unsupported non-Linux runtime paths fail closed. Same-UID processes are not isolated from each other.
- This endpoint is read-only and exposes status only. It does not register workspaces, authorize actions, connect to the cloud, or execute commands. These remain mandatory future increments.
- Security review covered untrusted JSON, filesystem paths and permissions, peer authentication, fail-closed errors, resource limits, and the IPC trust boundary. No high-confidence exploitable issue remains in this status-only slice; the same-UID limitation and all missing execution authorization are documented.
- Local verification passed: `make bootstrap`, `make lint`, `make test`, `make security-test`, `go test -race -count=1 -timeout 60s ./...`, and `git diff --check`. Coverage: local IPC 75.5%, build info 100%, CLI 53.8%, daemon 55.3%. The final 10-second IPC decoder fuzz run completed 89,260 executions without a crash or failure.
- The repository tree was regenerated. Commit `90724b2` (`feat(local): add Mio status IPC foundation`) and progress commit `fbf97ed` were pushed to `arena/0b81a55b-scopra`. Their Actions runs (`38854979363`, `38855061603`) ended in `startup_failure` before jobs/logs; `actionlint` passes locally, but GitHub provides no diagnostic.

### 2026-10-16 — Step 3: Read-only `scopra doctor`

- New session branch `arena/c7a368c1-scopra` starts from `main` at `1aac79f` (PR #1 merge). The previous branch `arena/0b81a55b-scopra` is merged and is not reused.
- Go was absent from the new sandbox, and GitHub release assets were unreachable. Go 1.26.9 was rebuilt from the official `golang/go` `go1.26.9` tag (commit `2ae494e`); the PyPI `go-bin` 1.26.6 wheel (SHA-256 checked against PyPI metadata) served only as the bootstrap compiler. The toolchain, `GOPATH`, and build cache live under `/tmp`; none are committed. Unmodified `main` passed `make bootstrap`, `make lint`, `make test`, `make security-test`, the race suite, and `git diff --check`, with coverage matching Step 2.
- CI: every Actions run in this private, organization-owned repository fails with `startup_failure` and zero jobs. This includes run `38855765861` for `main` at `1aac79f`, and run `38835626416` for the first commit, whose workflow was a trivial `actions/checkout@v4` job. The API reports the failed runs' workflow path as `BuildFailed`. Both pinned action SHAs in `ci.yml` resolve to their `v7.0.0` tags. Because the original trivial workflow failed the same way, the likely cause is an organization- or account-level Actions policy or billing setting; this integration cannot read those settings (HTTP 403). The failure is unresolved and is not a code-test result.
- Selection: `doctor` is the smallest pending local-lifecycle/Mariner item (roadmap item 2; plan Week 1 "Mariner doctor spike"). It reuses the existing status IPC and needs no wire or security-boundary change, so no ADR is planned. Workspace CRUD would add state mutation and a new IPC command, so it stays out of scope. The CI problem is outside the code and does not block this increment.

#### Acceptance checklist (agreed before implementation)

- [x] `scopra doctor [--state-dir PATH] [--json]` runs three checks in a fixed order: `platform`, `state-directory`, then `daemon`. Each check reports `pass`, `warn`, `fail`, or `skip`, with a detail. Warnings and failures include a remediation hint.
- [x] `platform` passes on Linux and fails closed elsewhere; when it fails, the remaining checks are skipped.
- [x] `state-directory` resolves the same path as `scoprad` and `scopra status`. It passes only for a private directory: owner-only `0700`, with no symlinks, no unsafe writable ancestors, and a socket path within the length limit. It warns if the directory does not exist yet and fails if the path is unresolvable, insecure, or too long. A failure skips `daemon`.
- [x] `daemon` reuses the existing authenticated `status` query. It passes only when Mio's IPC endpoint responds, and reports Mio's version. It fails with a remediation hint when Mio is not running, its socket is unsafe, or its response is invalid.
- [x] Doctor is read-only. It never creates or changes permissions on the state directory, removes stale sockets, starts Mio, or contacts an unsafe socket. It sends only the existing `status` request; IPC is unchanged.
- [x] A human-readable report goes to stdout with control characters escaped. `--json` writes one versioned object (`schemaVersion: 1`). Exit codes are 0 when no check fails, 1 when any check fails, 2 for usage errors, and 130 on cancellation.
- [x] Output states that enrollment, relay, workspaces, policy, containment, and execution are not checked. A passing result does not imply execution readiness or authorization.

Non-goals: auto-starting Mio, repairs or `--fix`, checks for workspaces, policy, containment, cloud, or execution, service installation, version-compatibility enforcement, and IPC protocol changes.

#### Test-first record

- Before any implementation, added 14 doctor tests (`cmd/scopra/doctor_test.go`) and six localipc state-directory tests. `./cmd/scopra` was added to `make security-test` so the read-only `TestSecurity_Doctor*` cases run with the protocol and IPC security suites.
- Red run: the 13 black-box CLI tests failed at runtime with `unknown command "doctor"`, and the existing CLI tests still passed alongside them. The white-box unsupported-platform test then failed to compile on the missing `diagnose`/`platformInfo`. The localipc tests failed to compile on the missing `ResolveStateDir`/`CheckStateDir` API.

#### Implementation and verification record

- `internal/localipc` gained three exported read-only wrappers over existing private logic, so doctor checks exactly what `Listen` and `Status` enforce: `PlatformSupported`, `ResolveStateDir`, and `CheckStateDir`. `CheckStateDir` runs the non-creating `validateStateDir` and the socket-path length check. `Status`, the wire format, and the trust boundary are unchanged, so no ADR was needed.
- `cmd/scopra/doctor.go` implements the checks, the schema-v1 JSON, and a human-readable report. Dynamic text in that report passes through `terminalSafe`, which escapes non-printable runes. The CLI usage text now lists `status`, `doctor`, and `version` on separate lines.
- Green: all new and existing tests pass. A coverage review found two gaps against the agreed contract: the doctor branch for an overlong state directory, and `warn` aggregation. Their tests were written after the code, so each was confirmed to fail against a deliberate mutation before being kept. Mutation checks also confirmed that the tests catch:
  - cancellation being ignored;
  - terminal escaping being removed;
  - the daemon check running after a state-directory failure;
  - the platform check not failing closed.
  A sanity probe confirmed that the "never contacts an unsafe socket" assertion detects a real connection.
- Smoke test with built binaries on Linux:
  - Missing state directory: `warn`/`fail`, exit 1, nothing created.
  - Running Mio: all checks `pass`, exit 0.
  - After SIGTERM: daemon `fail`.
  - Stale socket after SIGKILL: doctor leaves the same inode in place, and a restarted `scoprad` replaces it.
  - Directory with mode `0755`: `fail`/`skip`, mode unchanged.
  - `doctor extra` and `doctor --fix`: exit 2. `doctor -h`: exit 0.
  - Default path with an isolated `HOME`: nothing created.
- Security review: covered path and environment input, the IPC trust boundary, filesystem side effects, terminal output, error disclosure, and resource bounds. No high-confidence exploitable issue was found. Doctor is read-only and inherits peer-UID authentication, strict decoding, and two-second dial and I/O bounds from `Status`. Paths and errors are shown only to the invoking user. The window between the directory check and `Status` is harmless because `Status` re-validates before it connects.
- Local verification passed:
  - `make bootstrap`, `make lint`, `make test`, and `make security-test` (which now includes `./cmd/scopra`);
  - `go test -race -count=1 -timeout 60s ./...`, plus ten repeated race runs of the CLI and IPC packages;
  - `git diff --check`;
  - build, vet, and test-compile for darwin/arm64, darwin/amd64, windows/amd64, and linux/arm64.
- Coverage: CLI 80.1% (up from 53.8%), local IPC 75.7%, daemon 55.3%, build info 100%, protocol 89.4%.
- Follow-ups, not part of this increment: containment-readiness checks once Hollow exists, optional scopra/scoprad version-compatibility reporting, and service-manager diagnostics once service installation lands.
- Pushes to `arena/c7a368c1-scopra`: test checkpoint `61544c8` (red by design) and implementation `2d3cf9b`. Their Actions runs, `38858082470` and `38858770092`, both ended in `startup_failure` with zero jobs and workflow path `BuildFailed`, matching the `main` run `38855765861`. They are not code-test results; the evidence for this increment is the local verification above.

### 2026-10-16 — Step 4: Mio single-daemon lock

- Selection: the plan's Mio checklist lists "lock". ADR-003 made binding the socket the single-daemon lock, but probe-and-recheck stale recovery cannot make "remove only if still stale" atomic.
- A scratch stress test, not committed, started eight `Listen` calls at once against a stale socket. More than one daemon started in 4–9 of every 300 trials. With real `scoprad` processes the window is narrow (0 of 40 trials).
- A deterministic variant exists: if the socket file alone is deleted while Mio runs (it lives under the cache directory), a second `scoprad` starts and the first keeps running unreachable.
- Serve also closes the daemon before draining in-flight handlers, so a lock released in `Close` would end while requests were still being served.
- A real single-instance guarantee is a prerequisite for the single-writer persistent state that workspace management needs. Doctor follow-ups and workspace work stay deferred.

#### Acceptance checklist (agreed before implementation)

- [x] `Listen` takes a non-blocking exclusive `flock` on `<state-dir>/scoprad.lock` after preparing the state directory and before inspecting, removing, or binding the socket.
- [x] While the lock is held, another `Listen` for the same state directory fails with `ErrDaemonAlreadyRunning` and leaves the socket path untouched, even if the socket is missing. Concurrent starts against a stale socket produce exactly one daemon, and every other start reports `ErrDaemonAlreadyRunning`.
- [x] The lock file is opened read-only, without following symlinks, blocking on FIFOs, or truncating. It is created as `0600`, is never written, and is close-on-exec. Only a regular file owned by the effective user, with no group or other access, is accepted. Anything else fails with `ErrInsecureStateDir` and is left unchanged; a symlink target is never created.
- [x] The daemon keeps the lock until it can no longer act. `Close` releases it when the daemon is not serving. While serving, it is released only after in-flight requests finish. Failed startups release it, and the lock file is kept after shutdown.
- [x] Stale sockets are still removed only after a failed probe and an inode recheck, now while holding the lock.
- [x] `CheckStateDir`, and therefore `scopra doctor`, stays read-only: it rejects an existing unsafe lock file but never creates, opens, or locks it.
- [x] ADR-003 is amended, and the READMEs, plan, and progress log are updated. The IPC wire format is unchanged.

Non-goals: PID files or lock contents, taking over or killing a running daemon, moving the default state directory (for example to `$XDG_RUNTIME_DIR`), detecting deletion of the whole state directory, doctor diagnostics for the lock holder, and service installation.

#### Test-first record

- Added `internal/localipc/lock_linux_test.go`, which runs only on Linux and covers nine behaviours. The lock tests use the literal on-disk name `scoprad.lock`, so they compiled against the existing code and fail at runtime.
- Red run, with six expected failures:
  - A second `Listen` succeeded after the first daemon's socket was unlinked.
  - In a concurrent start, a losing start failed with `bind: address already in use` instead of `ErrDaemonAlreadyRunning`.
  - `Listen` accepted a dangling symlink, a group-readable file, a directory, and a FIFO at the lock path.
  - No lock file was created.
  - A second `Listen` succeeded while the closing daemon was still serving a request.
  - `CheckStateDir` ignored unsafe lock files.
- Three guard tests passed before implementation, as expected: the lock is reacquired after `Close`, the lock is released after a failed start, and `CheckStateDir` does not create a lock file. They protect behaviour the new code could break and are checked against deliberate mutations after implementation. All other existing tests still passed, and darwin and windows `go vet` stayed clean.

#### Implementation and verification record

- New files:
  - `lock.go` (platform-neutral): the lock-file name, the validation shared by `Listen` and `CheckStateDir`, and `releaseLock`.
  - `lock_linux.go`: opens with `O_RDONLY|O_CREATE|O_NOFOLLOW|O_NONBLOCK`, validates the opened inode with `fstat`, then calls `flock(LOCK_EX|LOCK_NB)`.
  - `lock_other.go`: a stub that fails closed with `ErrUnsupportedPlatform`.
- Changes in `socket.go`:
  - `Listen` locks before stale-socket recovery and releases the lock on every failed start.
  - `Close` releases the lock only if `Serve` never started. `Serve` releases it after its handlers drain.
  - `CheckStateDir` adds a `Lstat`-only lock-file check.
- The IPC wire format is unchanged. ADR-003 is amended because it named socket binding as the lock.
- Green: all nine lock tests pass. Ten deliberate mutations were each caught:
  - `Close` never releases the lock.
  - A failed start leaks the lock.
  - `CheckStateDir` opens and creates the lock file.
  - The lock is released while requests are still in flight.
  - `Serve` never releases the lock.
  - `O_NOFOLLOW` is dropped.
  - `O_NONBLOCK` is dropped (the FIFO case then hangs until the five-second guard fires).
  - The group/other permission check is removed.
  - The directory occupant is not classified as insecure.
  - The lock is taken after stale-socket recovery (caught in 3 of 3 runs).
- During refactoring, the concurrent-start test gained an assertion that the winning daemon still owns its socket inode, so an orphaned winner cannot pass.
- Smoke test with built binaries:
  - A second `scoprad` exits 1 with `daemon is already running: another process holds the state directory lock`, including after the live socket file is deleted.
  - After a clean stop, the `0600` lock file remains and a restart works. Doctor passes while Mio holds the lock.
  - After SIGKILL, the kernel releases the lock and a restart recovers the stale socket.
  - A `0644` lock file makes `scoprad` refuse to start, and doctor reports `state-directory: fail`; the mode is left unchanged.
  - Starting eight real `scoprad` processes at once against a stale socket left exactly one reachable daemon in 40 of 40 trials. All 280 losing processes reported the same "already running" error.
- Security review: covered symlink, FIFO, directory, hard-link, and foreign-owner occupants; validating the opened inode rather than the path; lock lifetime across failure, `Close`, `Serve`, and crash; close-on-exec; and interaction with read-only doctor. No high-confidence exploitable issue was found. The documented limits are that a same-UID process can hold the lock to block Mio, and that deleting the whole state directory while Mio runs is undetected.
- Local verification passed:
  - `make bootstrap`, `make lint`, `make test`, and `make security-test`;
  - `go test -race -count=1 -timeout 60s ./...`, plus ten repeated race runs of the IPC, CLI, and daemon packages;
  - `git diff --check`;
  - build, vet, and test-compile for darwin/arm64, darwin/amd64, windows/amd64, and linux/arm64.
- Coverage: local IPC 76.9% (up from 75.7%), CLI 80.1%, daemon 55.3%, build info 100%, protocol 89.4%.
- Follow-ups, not part of this increment: detecting a deleted state directory (a liveness or self-check design), reporting the lock holder in doctor without taking the lock (for example via `/proc/locks`), and choosing a runtime-directory default such as `$XDG_RUNTIME_DIR`.
