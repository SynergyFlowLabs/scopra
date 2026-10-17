# Scopra Development Progress

**Started:** 2026-10-16 (Asia/Karachi)
**Current phase:** Milestone 1 — local secure execution foundation
**Current increment:** a generated documentation site deployed to Vercel (Step 35)
**Status:** Implemented on `arena/fe74ddf7-scopra`; local checks passed; CI is out of scope for now at the maintainer's request

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

1. **Protocol foundation** — envelope v1 structural validation, strict execution request v1 decoding (ADR-007), the canonical request and action hash (ADR-008), and every execution lifecycle message: results, start notices, output chunks, and cancellation (ADR-009) are implemented and locally verified. The plan's first Messenger task is complete, and reference payloads in `packages/test-fixtures/messenger/v1` pin the version 1 wire format. Approval records, compatibility tests across protocol versions, cryptographic verification, replay storage, and compatibility/version guarantees remain pending.
2. **Local daemon and CLI** — the initial foreground Mio status service with a persistent per-user state directory and an exclusive state-directory lock, Mariner `scopra status`, a minimal read-only `scopra doctor` (platform, state-directory, workspace-registry, registered-workspace revalidation, and daemon checks), and `scopra workspace add/list/remove` are implemented. Workspace validation (ADR-004) and registry storage (ADR-005) exist; Mio checks its registry at startup and serves add, list, and remove over local IPC (ADR-006). Local execution (`scopra run`, ADR-011, tested with fakes), `scopra service` (ADR-012), and the bounded metadata journal read by `scopra logs` (ADR-013) are implemented too. Broader doctor checks, output capture and retention, and expanded CLI/IPC authorization remain pending.
3. **Policy and containment boundary** — implement default-deny policy and rootless containment before enabling execution; no native fallback. ADR-010 decides the rootless Podman backend. Its first two increments, the exact `podman run` argument builder and the fail-closed preflight check in `internal/hollow`, and the `scoprad --sandbox-image` setting are implemented and verified. The runner against a fake Podman, with its timeout, cancellation, per-run preflight, and leftover cleanup (3a-2 and 3a-3), is also done, and Mio now wires it into `execution.run` (ADR-011 increment 2), so a local run executes through Hollow when the host has rootless Podman and a pinned image. Still to do: verification on a real host (3b) and the adversarial tests, both of which need rootless Podman.
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
  - `make lint` and `make security-test`;
  - `go test -race -count=1 -timeout 60s ./...`, plus ten repeated race runs of the IPC, CLI, and daemon packages;
  - `git diff --check`;
  - build, vet, and test-compile for darwin/arm64, darwin/amd64, windows/amd64, and linux/arm64.
- Coverage: local IPC 76.9% (up from 75.7%), CLI 80.1%, daemon 55.3%, build info 100%, protocol 89.4%.
- Follow-ups, not part of this increment: detecting a deleted state directory (a liveness or self-check design), reporting the lock holder in doctor without taking the lock (for example via `/proc/locks`), and choosing a runtime-directory default such as `$XDG_RUNTIME_DIR`.
- Pushes to `arena/c7a368c1-scopra`: test checkpoint `77ad4c8` (red by design) and implementation `ad0917a`. Their Actions runs, `38860207623` and `38860701486`, ended in `startup_failure` with zero jobs and workflow path `BuildFailed`, like every earlier run. They are not code-test results; the evidence for this increment is the local verification above.

### 2026-10-16 — Step 5: Workspace path validation

- Selection: the plan's first three "Workspace and containment" items are to canonicalize the workspace and record a repository fingerprint, to reject the filesystem root and the complete home directory, and to revalidate before execution. The workspace registry, IPC commands, and CLI all depend on these rules, so they come first as a standard-library-only package with no wiring.
- The PRD and plan do not define the "repository fingerprint". Version 1 uses the root directory's filesystem identity (device and inode numbers). That requires no repository contents, which are untrusted, and no `git` invocation, since repository configuration can make `git` run commands. ADR-004 records this choice and its limits; nothing is persisted yet, so it remains cheap to change.
- Facts about the environment: `filepath.EvalSymlinks` resolves `..` physically after symlinks, while `filepath.Clean` resolves it lexically, so paths must never be cleaned before they are resolved. The account database lists this user's home separately from `$HOME`.

#### Acceptance checklist (agreed before implementation)

- [x] New package `internal/workspace` using the standard library only. `NewValidator(protected ...string)` always protects the filesystem root and the current user's home directories, taken from both `$HOME` and the account database. It fails closed with `ErrHomeUnknown` if no absolute home directory is known, with `ErrInvalidPath` for a relative protected path, and with `ErrUnsupportedPlatform` outside Linux.
- [x] `Inspect(path)` accepts only absolute, valid UTF-8 paths and returns the canonical root: symlinks resolved, `..` resolved physically, and the result cleaned and valid UTF-8. A missing path or dangling symlink reports `os.ErrNotExist`. A non-directory, including a symlink to a file, reports `ErrNotDirectory`.
- [x] `Inspect` returns `ErrProtectedPath` for the filesystem root and for any directory that is or contains a home directory or another protected directory, such as Mio's state directory. This holds when the directory is reached through a symlink, when the protected path itself contains a symlink, and when the protected directory does not exist yet. The account home stays protected even if `$HOME` is overridden.
- [x] `Inspect` returns `ErrNotOwned` for a root that the effective user does not own.
- [x] `Inspect` records a version 1 fingerprint: the root directory's device and inode numbers.
- [x] `Revalidate(recorded)` re-applies every rule. It returns `ErrChanged` when the recorded path no longer resolves to itself (for example, an ancestor or the root became a symlink) or when the fingerprint differs (the directory was replaced). A deleted workspace reports `os.ErrNotExist`.
- [x] Validation is read-only: it reads filesystem metadata only and never creates, modifies, or removes anything, reads repository contents, or runs commands.
- [x] ADR-004 records the rules and the fingerprint's definition and limits. The plan, progress log, and repository tree are updated.

Non-goals: the workspace registry or any persistence, IPC commands, a `scopra workspace` CLI, read-only and read-write modes, denylists for sensitive subdirectories, content-based repository fingerprints such as a Git root commit, wiring revalidation into execution, and descriptor-based (`openat2`) path resolution.

#### Test-first record

- Added `internal/workspace/workspace_linux_test.go`: 17 white-box tests, which run only on Linux.
- First red: the build failed because the API was undefined (`undefined: Validator`).
- A declarations-only skeleton followed: exported types and sentinel errors, plus signatures that return "not implemented".
- Second red: 16 tests failed at runtime against the behaviour they specify, covering canonicalization, physical `..` after a symlink, invalid, missing, and non-directory paths, root, home and protected-path containment, ownership, revalidation, and validator construction.
- One guard test (`TestHomeDirectoriesIgnoresRelativeHOME`) passed trivially against the skeleton. It is checked against a deliberate mutation after implementation.

#### Implementation and verification record

- `workspace.go` contains the platform-neutral rules, home discovery, containment, and resolution of existing prefixes. `platform_linux.go` checks the directory and its ownership and builds the fingerprint from `Lstat`; `platform_other.go` fails closed. ADR-004 records the rules, the version 1 fingerprint, its limits, and the rejected alternatives.
- Green: all 17 tests pass under the race detector, and none are skipped (the account-home and `/usr` ownership tests ran). Package coverage is 93.1%. The uncovered lines are the non-Linux branch and error paths that only a race can reach.
- Thirteen deliberate mutations were each caught:
  - cleaning the path before resolving it (lexical `..`);
  - no explicit filesystem-root check;
  - trusting only `$HOME`;
  - rejecting only exact matches;
  - not resolving symlinks in protected paths;
  - requiring protected paths to exist;
  - revalidation ignoring path changes;
  - revalidation ignoring fingerprint changes;
  - no ownership check;
  - accepting a relative `$HOME` (the guard test);
  - not rechecking UTF-8 after resolution;
  - no directory check;
  - prefix containment without a separator boundary.
- The last mutation came from the security review: a shared-prefix sibling case was added to the protected-path test and confirmed to fail against a naive `strings.HasPrefix` before it was kept.
- Harness pitfall: the first run of "reject only exact matches" produced no test failures because the mutation left an unused import and the build failed. Rerun in a compile-safe form, it was caught by three tests. A mutation counts as caught only when tests fail, not when the build fails.
- Security review: covered input validation, physical versus lexical `..`, aliases for the root and home directory, `$HOME` overrides, prefix confusion, ownership, symlinks swapped after resolution (handled by `Lstat` in the metadata check), and resource bounds (`EvalSymlinks` link limits and kernel path limits). No high-confidence exploitable issue was found. The limits recorded in ADR-004 are validation-to-use races (callers must revalidate, and later bind by descriptor), mount aliases, inode reuse, and same-UID changes.
- Local verification passed:
  - `make bootstrap`, `make lint`, `make test`, and `make security-test` (which now includes `./internal/workspace`);
  - `go test -race -count=1 -timeout 60s ./...` and ten repeated race runs of the workspace package;
  - the workspace tests with `CGO_ENABLED=0`, which uses the pure-Go account database, as static release builds would;
  - `git diff --check`;
  - build, vet, and test-compile for darwin/arm64, darwin/amd64, windows/amd64, and linux/arm64.
- Follow-ups, not part of this increment: a workspace registry (persistence, IPC commands, and the `scopra workspace` CLI, which will need an ADR for the new wire commands), mount-aware alias checks, descriptor-based binding at execution time, and a decision on a content-based repository identity.
- Pushes to `arena/c7a368c1-scopra`: test checkpoint `44c0e4d` (red by design) and implementation `16913b6`. CI was not checked, at the maintainer's request; the evidence for this increment is the local verification above.

### 2026-10-16 — Step 6: Workspace registry storage, part 1

- Selection: the registry is split into two parts. Part 1, this step, stores registrations behind the ADR-004 rules. Part 2 will add Mio wiring, IPC commands, and the `scopra workspace` CLI, which change the IPC format and need their own ADR.
- Mio's state-directory lock is its only single-writer guarantee, so the library takes a directory that the caller must own exclusively; Mio will pass its locked state directory. Mio's default state directory is under the cache directory, so part 2 must settle on a persistent default before registration is exposed. Losing records fails safe, since lost records grant no access. Nothing is persisted in production yet.
- New registration rules: no duplicate directory, including the same fingerprint under a new path after a move; no overlapping workspaces, which keeps future read-only and read-write rules unambiguous; and no workspace that contains the registry itself.

#### Acceptance checklist (agreed before implementation)

- [x] New package `internal/workspace/registry`, using the standard library only. `Open(dir, validator)` loads `<dir>/workspaces.json`, or starts empty and creates nothing when the file is missing. `dir` must be a real directory owned by the effective user with no group or other access. Only a process that owns `dir` exclusively may open it (Mio, under its state-directory lock). Calls within a process are serialized, and other platforms fail closed.
- [x] `Add(path)` registers a workspace only after `Validator.Inspect` (ADR-004) succeeds, and records the canonical path, the fingerprint, and a random 128-bit ID. It rejects:
  - the same directory under the same or another canonical path, with `ErrAlreadyRegistered`;
  - overlap with an existing workspace, with `ErrOverlap`;
  - a workspace that contains the registry directory, with `workspace.ErrProtectedPath`;
  - a 257th workspace, with `ErrRegistryFull`.
- [x] `Remove(id)` deletes only the registration, never the workspace directory or its contents, and returns `ErrNotFound` for an unknown ID. `Get(id)` and `List()` return detached copies, and `List` is sorted by path.
- [x] Every change is persisted atomically: an owner-only temporary file is written in the same directory, synced, and renamed over `workspaces.json`, and then the directory is synced. A failed write leaves the previous file and the in-memory registry unchanged and leaves no temporary file behind.
- [x] The file is opened without following symlinks or blocking on FIFOs. It must be an owner-only regular file owned by the effective user; otherwise `Open` returns `ErrInsecureRegistry` and leaves the file untouched. The content is versioned JSON (`version: 1`), read up to a 1 MiB limit and decoded strictly. Invalid UTF-8, malformed JSON, unknown fields, trailing data, an unsupported version, malformed or duplicate IDs, non-canonical, duplicate, or overlapping paths, or too many records fail closed with `ErrCorruptRegistry`.
- [x] ADR-005 records the storage format, write protocol, registration rules, and limits. The plan, progress log, and repository tree are updated.

Non-goals: wiring into `scoprad`, IPC commands, the `scopra workspace` CLI, read-only and read-write modes, moving Mio's default state directory to a persistent location (to be decided before part 2), cleaning up temporary files left by crashes, and migration tooling.

#### Test-first record

- Added `internal/workspace/registry/registry_linux_test.go`: 16 white-box tests, which run only on Linux, including 18 corrupt-file cases and 5 unsafe occupants. The tests pin the version 1 format in both directions (as written and as hand-written) and cover the atomic write failing both before the temporary file exists and after it is written.
- First red: the build failed because the API was undefined (`undefined: Registry`).
- With a declarations-only skeleton, all 16 tests failed at runtime against the behaviour they specify.

#### Implementation and verification record

- `registry.go` contains the platform-neutral format, registration rules, atomic save, and strict decoding. `registry_linux.go` checks the directory and file (`O_NOFOLLOW|O_NONBLOCK`, then `fstat` on the opened file) and syncs the directory; `registry_other.go` fails closed. ADR-005 records the format, rules, write protocol, limits, and the open decision about the default location.
- Before implementation, three more corrupt-file cases were added and seen to fail: a missing workspace list, a `null` workspace list, and duplicate fingerprints. Each describes a file that Mio could not have written.
- Green: all 17 tests and their subtests pass under the race detector, with no skips; the read-only-directory case ran because the tests do not run as root. Package coverage is 92.9%. The uncovered lines are write, sync, and close failure branches that cannot be triggered without fault injection.
- Twenty-one deliberate mutations were each caught by real test failures; the harness reports build failures separately, and none occurred. The mutations covered:
  - skipping ADR-004 validation;
  - allowing a workspace to contain the registry;
  - detecting duplicates by path only;
  - no overlap check;
  - no maximum;
  - writing non-atomically, straight to the registry file;
  - adopting records after a failed write;
  - leaving the temporary file behind;
  - following symlinks;
  - blocking on FIFOs (caught by the five-second guard);
  - ignoring group and other permission bits;
  - accepting unknown fields, trailing data, or invalid UTF-8;
  - skipping load-time duplicate and overlap checks;
  - no read limit;
  - `List` returning the internal slice;
  - no mutex (caught by the race detector);
  - accepting a missing workspace list;
  - unsorted records;
  - `encode` writing files larger than the read limit.
- The last mutation came from the coverage review: `TestEncodeNeverWritesWhatOpenWouldReject` pins the guarantee that `Add` can never write a file that `Open` would reject.
- Security review: covered file and directory occupants (symlink, FIFO, directory, group-readable), validating the opened file rather than the path, temporary-file permissions and cleanup, rename atomicity and directory sync, single-writer assumptions, structural checks on load versus revalidation at use time, the read and write limits, and silent UTF-8 substitution. No high-confidence exploitable issue was found. ADR-005 records the remaining limits: the exclusive-owner precondition, no checks on the directory's ancestors, same-UID edits, and the cache-directory default.
- Local verification passed:
  - `make bootstrap`, `make lint`, `make test`, and `make security-test` (which now includes `./internal/workspace/...`);
  - `go test -race -count=1 -timeout 60s ./...` and ten repeated race runs of the workspace packages;
  - the workspace packages with `CGO_ENABLED=0`;
  - `git diff --check`;
  - build, vet, and test-compile for darwin/arm64, darwin/amd64, windows/amd64, and linux/arm64.
- Follow-ups for part 2: choose a persistent default state directory, wire the registry into Mio under its lock, add IPC commands and the `scopra workspace` CLI (with an ADR for the new wire commands and frame-size limits for `list`), and clean up stale temporary files.
- Pushes to `arena/c7a368c1-scopra`: test checkpoint `4926c24` (red by design) and implementation `edeac53`. CI was not checked, at the maintainer's request; the evidence for this increment is the local verification above.

### 2026-10-16 — Step 7: Persistent default state directory

- Selection: ADR-005 requires a persistent default before part 2 exposes registration. The current default, `<user-cache-dir>/scopra/run`, is a location that users and cleaning tools may delete. At the end of Step 6 the maintainer accepted the proposal to move the default to `~/.local/state/scopra`, keep `--state-dir`, and amend ADR-003. Wiring the registry into Mio stays in part 2.
- The socket, the lock, and (from part 2) the registry stay in one directory. The lock is Mio's single-writer guarantee for that state, so it must sit beside the state it protects. Moving only the socket to `$XDG_RUNTIME_DIR` would add a second location that both binaries must agree on.
- Facts: Go has no `os.UserStateDir`. `os.UserCacheDir` rejects a relative `XDG_CACHE_HOME` but does not check `HOME`. The repository has no tags or releases, and the old default held only the socket and the lock file, so there is nothing to migrate.

#### Acceptance checklist (agreed before implementation)

- [x] With no `--state-dir`, `scoprad`, `scopra status`, and `scopra doctor` all use `$XDG_STATE_HOME/scopra` when `XDG_STATE_HOME` is set, and `$HOME/.local/state/scopra` otherwise. The user's cache directory is never used, not even as a fallback.
- [x] Resolution fails closed instead of falling back or resolving against the working directory. A relative `XDG_STATE_HOME`, or a relative `HOME` when it is needed, returns `ErrInsecureStateDir`; having neither set returns an error. Doctor then reports `state-directory: fail`, skips `daemon`, and gives a hint that names `XDG_STATE_HOME` and `--state-dir`.
- [x] On first start with the default, `scoprad` creates each missing component (for example `~/.local`, `~/.local/state`, and `scopra`) as an owner-only `0700` directory under the existing state-directory checks, and serves there. `scopra status` with no flags reaches it, and nothing is created in the cache directory.
- [x] An explicit `--state-dir`, the state-directory and lock checks, the socket path limit, the doctor JSON schema, and the IPC wire format are unchanged. The existing tests guard them.
- [x] ADR-003 gets a second amendment covering the new location, the alternatives, and compatibility with older builds. ADR-005's open item points to it, and the READMEs, the doctor JSON example, the plan, and this log are updated.

Non-goals: wiring the registry into Mio; workspace IPC commands and CLI; moving or deleting anything at the old cache-directory location; detecting an older `scoprad` still serving the old default; a socket in `$XDG_RUNTIME_DIR`; native defaults for macOS and Windows, where Mio still fails closed; showing the default in usage text; and service installation.

#### Test-first record

- Changed and added tests, all Linux-only:
  - `internal/localipc`: `TestResolveStateDirDefaultsToXDGStateHome` replaces the cache-directory test. New tests cover the `$HOME/.local/state` fallback, fail-closed resolution (`TestSecurity_DefaultStateDirFailsClosedForUnusableEnvironment`: relative `XDG_STATE_HOME`, relative `HOME`, neither set), and an end-to-end check that `Listen("")` creates the default directory chain as `0700` directories and that `Status("")` reaches it there.
  - `cmd/scopra`: the doctor's default-directory test now expects `$XDG_STATE_HOME/scopra`. The unresolvable-directory test gains a relative `XDG_STATE_HOME` case and checks the hint.
- Every test sets `XDG_CACHE_HOME` to a usable decoy directory, so the old code resolves successfully and each test proves that the cache directory is never used as a fallback.
- Red run: six tests failed (five of them through subtests), each because the old code returned the decoy's `scopra/run`. All 60 other tests in the two packages passed, including the existing guards `TestDefaultStateDirIsAbsolute` and `TestResolveStateDirReturnsCleanAbsolutePath`.

#### Implementation and verification record

- `DefaultStateDir` in `internal/localipc/socket.go` now returns `$XDG_STATE_HOME/scopra`, or `$HOME/.local/state/scopra` using `os.UserHomeDir`, and rejects relative values with `ErrInsecureStateDir`. `Listen`, `Status`, `CheckStateDir`, and doctor already shared `resolveStateDir`, so the only other code change is the doctor hint. The directory checks, the lock, the socket, the wire format, and the doctor JSON schema are unchanged.
- ADR-003 has a second amendment, "persistent default state directory". It covers the location, failing closed on relative values, the rejected alternatives (a socket in `$XDG_RUNTIME_DIR`, a separate persistent directory, `$XDG_DATA_HOME`), compatibility with earlier builds, and the limits. ADR-005's open item and its separate-directory alternative now point to it, and the earlier inline "superseded" notes name the amendment they mean.
- Green: all six red tests pass under the race detector with no skips, and the two guards still pass. `DefaultStateDir` has 100% coverage.
- Ten deliberate mutations were each caught by real test failures, with no build failures:
  - ignoring `XDG_STATE_HOME`;
  - accepting a relative `XDG_STATE_HOME`;
  - ignoring a relative `XDG_STATE_HOME` and falling back to `HOME`, as the XDG specification suggests;
  - accepting a relative `HOME`;
  - falling back to the cache directory when `HOME` is missing;
  - a wrong subpath under `HOME`, and another under `XDG_STATE_HOME`;
  - creating intermediate directories as `0755`;
  - a doctor hint that omits `XDG_STATE_HOME`;
  - `Status` resolving a different default from `Listen`.
- Smoke test with built binaries in a fresh home directory, with an empty environment apart from `PATH` and the variables under test:
  - Before the first start, doctor reports the new path with `state-directory: warn` and creates nothing.
  - `scoprad` creates `~/.local`, `~/.local/state`, and `scopra` as `0700` directories, with a `0600` lock and socket, and creates no `.cache`. `scopra status` and doctor (pass, exit 0) find Mio with no flags. A second `scoprad` is refused. TERM exits 0, removes the socket, and keeps the lock, and a restart works.
  - With `XDG_STATE_HOME` set, Mio serves `$XDG_STATE_HOME/scopra`, and a CLI without the variable reports Mio unavailable.
  - A relative `XDG_STATE_HOME` makes `scoprad`, `scopra status`, and doctor fail closed with the new hint, and nothing is created in the working directory. With no `HOME` either, `scoprad` exits with `$HOME is not defined`. An explicit `--state-dir` still works with a broken environment.
  - An existing group-writable `~/.local` (`0775`) makes `scoprad` and doctor fail closed with the writable-ancestor message, and the directory is left unchanged. ADR-003 records this as a limit, since a `002` umask can produce it.
  - Harness pitfall: the first run backgrounded a shell function, so `$!` was the wrapper subshell. TERM killed the wrapper and orphaned `scoprad`, which kept answering and made one check look wrong. Rerun with the binary itself in the background, every result matched expectations; the orphans were then stopped and removed their sockets.
- Security review: covered relative values (the working directory could be inside a workspace), lexical `..` cleaning of environment values (every resulting component is still validated and symlinks are rejected), the permissions of created components, error text (it names variables but never echoes their values), compatibility with older binaries, and state directories shared between hosts. No high-confidence exploitable issue was found. The environment is controlled by the same user, which is outside the security boundary.
- Local verification passed:
  - `make lint` and `make security-test`;
  - `go test -race -count=1 -timeout 60s ./...` and ten repeated race runs of the IPC, CLI, and daemon packages;
  - the IPC and command packages with `CGO_ENABLED=0`;
  - `git diff --check`;
  - build, vet, and test-compile for darwin/arm64, darwin/amd64, windows/amd64, and linux/arm64.
- Coverage: local IPC 77.4% (up from 76.9%), CLI 80.1%, daemon 55.3%, build info 100%, workspace 93.1%, registry 92.9%, protocol 89.4%.
- Follow-ups, not part of this increment: registry part 2 (wiring into Mio under its lock, workspace IPC commands with a new ADR that includes frame-size limits for `list`, the `scopra workspace` CLI, and stale temporary-file cleanup); possibly relaxing the ancestor rule for user-private groups under a `002` umask; and native defaults for other platforms.
- Pushes to `arena/c7a368c1-scopra`: test checkpoint `4a48f65` (red by design), then the implementation commit that adds this record. CI was not checked, at the maintainer's request.

### 2026-10-16 — Step 8: Mio opens its workspace registry at startup

- Selection: at the end of Step 7 the maintainer accepted the proposal that registry part 2 start by wiring storage into Mio, with no wire change. Asked whether a corrupt registry should stop Mio or only disable workspace features, the maintainer continued with the recommendation: refuse to start. For now only a manual edit can produce a bad file, and Mio has no way yet to report a degraded state. Workspace IPC commands and the CLI follow in later steps.
- Wiring is in `cmd/scoprad`, which the `go-project-layout` skill reserves for wiring. `localipc` stays transport-only and gains a `Daemon.StateDir()` accessor, so the registry is opened in exactly the directory whose lock Mio holds.
- Reading the code before wiring found two gaps that matter once the state directory is the registry directory:
  - `Add` rejects a workspace that contains the registry directory, but not one inside it. Mio will keep other state there (for example, audit records), so agent-controlled files must never be inside it.
  - `Open` never checks records against its own directory, although ADR-005 says loaded content must satisfy the rules that `Add` enforces. A registry inside a registered workspace, for example after a state directory is moved, would let workspace processes edit their own authorization.
  Both are closed here, before any command can add workspaces.

#### Acceptance checklist (agreed before implementation)

- [x] After taking the state-directory lock and before reporting `listening`, `scoprad` opens the registry in the directory it locked (`Daemon.StateDir()`), with an ADR-004 validator that also protects the state directory.
- [x] If the registry cannot be opened (an unsafe file or directory, invalid content, or no usable validator), `scoprad` does not serve. It exits 1 with the error, removes the socket it bound, releases the lock, and leaves the registry file unchanged.
- [x] With no registry file, `scoprad` starts as before and creates none. With a valid file, it starts and leaves the file unchanged. Recorded workspaces are not revalidated at startup, so a missing workspace directory does not stop Mio.
- [x] The registry protects its own directory in both directions. `Add` rejects a workspace inside the registry directory, as it already rejects one containing it (`workspace.ErrProtectedPath`). `Open` rejects a file with such a record (`ErrCorruptRegistry`) and leaves it unchanged.
- [x] ADR-005 is amended for Mio integration and the two-way rule. The `scoprad` README, the plan, the architecture overview, the README status line, and this log are updated. The IPC wire format, `scopra status`, and doctor are unchanged.

Non-goals: workspace IPC commands and the `scopra workspace` CLI; keeping the loaded registry for use by later commands; revalidating recorded workspaces at startup; a doctor check for the registry; repairing or migrating registry files; cleaning up stale temporary files; and any change to the status response.

#### Test-first record

- New tests, all Linux-only:
  - `internal/workspace/registry`: `TestSecurity_AddRejectsWorkspaceInsideRegistry`, and `TestSecurity_OpenRejectsRecordOverlappingRegistry` with three cases: a record that contains, is, or lies inside the registry directory.
  - `internal/localipc`: `TestDaemonStateDirIsTheLockedDirectory`, which requires the absolute locked directory even when `Listen` gets a relative, unclean path.
  - `cmd/scoprad`: `TestSecurity_RunRefusesUnusableWorkspaceRegistry` covers an unsupported version, a group-readable file, and a registry inside a registered workspace. Each case checks the error, that `listening` was never reported, that the file is byte-for-byte unchanged with the same mode, that no socket is left, and that the lock was released. `TestWorkspaceValidatorProtectsStateDirectory` checks the validator wiring. The guard `TestRunStartsWithMissingOrValidWorkspaceRegistry` pins that a missing file is not created and that a valid file naming a missing workspace neither stops Mio nor changes.
- First red: `internal/localipc` and `cmd/scoprad` failed to build on exactly the two missing declarations (`Daemon.StateDir`, `newWorkspaceValidator`). The registry package built, and both new tests failed: `Add` accepted the inner directory, and `Open` accepted all three overlapping files, including a record naming the registry directory itself.
- With declarations-only skeletons, five tests failed at runtime (six subtests between them), each on the behaviour it specifies. The other 67 tests in the three packages passed, including the new guard.

#### Implementation and verification record

- `cmd/scoprad/main.go`: after `localipc.Listen`, `run` calls `openWorkspaceRegistry(daemon.StateDir())`, which builds Mio's validator (`newWorkspaceValidator`, protecting the state directory) and calls `registry.Open`. On failure, `run` returns the error joined with `daemon.Close()`, which removes the socket and releases the lock because `Serve` never started. The registry is discarded for now; workspace commands will keep it.
- `internal/localipc`: `Daemon.StateDir()` returns the absolute directory recorded by `Listen`. The wire format is unchanged.
- `internal/workspace/registry`: `Add` uses `overlaps` instead of `contains` against the registry directory. `Open` rejects any decoded record that overlaps the canonical registry directory with `ErrCorruptRegistry`. `decode` is unchanged and remains a pure format check.
- ADR-005 has a "Mio integration" amendment covering the two-way rule, the startup behaviour, the refuse-to-start choice and when to revisit it, the home-directory requirement, and the window in which the socket is bound before the registry is opened. The ADR index, the `scoprad` README, the README status line, the plan, and the architecture overview are updated. `make security-test` now includes `./cmd/scoprad`.
- Green: all tests pass under the race detector with no skips.
- Eleven deliberate mutations, each caught by real test failures with no build failures:
  - ignoring registry errors at startup;
  - leaking the socket and lock on a refused start;
  - `StateDir` reporting the parent directory;
  - a validator that does not protect the state directory;
  - `Add` rejecting only containing workspaces;
  - `Open` skipping the directory check;
  - `Open` checking only workspaces that contain the directory, and only workspaces inside it;
  - `Open` comparing with the non-canonical directory;
  - reporting `listening` before opening the registry;
  - `openWorkspaceRegistry` ignoring `Open` errors.
- The non-canonical-directory mutation first survived, because the test resolved symlinks before calling `Open`. Mio is unaffected, since `localipc` rejects symlinks anywhere in the state directory path, but `Open` allows symlinked ancestors. The overlap test now opens the registry through a symlinked ancestor and catches the mutation.
- Not exercised: a validator that cannot be prepared (`ErrHomeUnknown`). The account database always provides a home here, so the path is covered only by the workspace package's own tests and the shared error path.
- Smoke test with built binaries and a `--state-dir`, with an empty environment apart from `PATH` and `HOME`:
  - With no registry file, Mio starts, `scopra status` reaches it, and TERM leaves only `scoprad.lock`; no registry file is created.
  - Five bad registries each make `scoprad` exit 1 with a precise reason: an unsupported version, a world-readable file, a record containing the state directory, a record inside it, and a symlinked registry file. Each file stays byte-for-byte identical with the same mode, no socket is left, and later starts succeed, so the lock was released. During a refused start, doctor reports `state-directory: pass` and `daemon: fail`.
  - A valid registry naming a missing workspace starts normally and is left unchanged.
  - With no `HOME` at all, Mio starts using the account database.
- Security review: covered the lock-held invariant (the registry is opened only after `Listen`, in exactly the locked directory), fail-closed startup with full cleanup, the two-way directory rule and its canonical comparison, the validator's protection of the state directory for future revalidation, error text (paths are quoted with `%q`, so control characters are escaped), and the window in which the socket is bound before the registry is opened (no request is served). No high-confidence exploitable issue was found. A same-UID process can stop Mio by planting a bad registry, just as it can hold the lock; same-UID processes remain outside the boundary.
- Local verification passed:
  - `make lint` and `make security-test`;
  - `go test -race -count=1 -timeout 60s ./...` and ten repeated race runs of the daemon, IPC, and workspace packages;
  - the command and internal packages with `CGO_ENABLED=0`;
  - `git diff --check`;
  - build, vet, and test-compile for darwin/arm64, darwin/amd64, windows/amd64, and linux/arm64.
- Coverage: daemon 61.4% (up from 55.3%), registry 93.0% (up from 92.9%), local IPC 77.4%, CLI 80.1%, workspace 93.1%, build info 100%, protocol 89.4%.
- Environment: this session started in a fresh sandbox. The local branch had been re-created at `main` with the restored work uncommitted. A temporary index showed that the working tree was byte-identical to the pushed tip `46d2fe0`, so a mixed reset re-attached the branch to its pushed history without touching any file. The Go 1.26.9 toolchain was rebuilt from the pinned source, and the baseline passed before any change.
- Follow-ups, not part of this increment: workspace IPC commands with a new ADR (including how `list` fits within the 64 KiB frame limit), keeping the registry in Mio for those commands, the `scopra workspace` CLI, a read-only doctor check for the registry, and cleanup of stale temporary files.
- Pushes to `arena/c7a368c1-scopra`: test checkpoint `87433c6` (red by design), then the implementation commit that adds this record. CI was not checked, at the maintainer's request.

### 2026-10-16 — Step 9: Workspace commands over local IPC

- Selection: at the end of Step 8 the maintainer accepted the proposal to add workspace commands to Mio's local IPC, with a new ADR, before the `scopra workspace` CLI (Step 10). By replying "continue", the maintainer also accepted both proposed defaults:
  - Authorization: the same-UID socket is the user's administrative channel for registration.
  - Listing: paged with a cursor instead of raising the 64 KiB frame limit.
- Constraints from the code and docs:
  - **No error replies:** IPC v1 answers only `status` and closes the connection on any invalid request, so it has no way to say why a request failed. `add` can fail in many distinct ways, so well-formed workspace requests need typed error replies.
  - **Authorization:** ADR-003 says state-changing commands need more than same-UID IPC, and that new commands need compatibility and security tests before dispatch is enabled. The threat model routes agents through Caloris and Goldstone, never the local socket, and requires that agents cannot approve themselves. The new ADR therefore accepts same-UID only for registration, which must never arrive through Caloris, Goldstone, or Messenger. Approvals keep needing more, and the future sandbox must keep workspace processes away from the socket.
  - **Page size:** a registered path is at most 4095 bytes (Linux `PATH_MAX`) but can grow sixfold when escaped as JSON, so pages are sized by encoded bytes, not by count. `registry.Open` must also bound path length, or a hand-edited file could hold a record too large for any frame.
  - **Unresolvable paths:** when `workspace.Inspect` cannot resolve a path (missing, a file in the middle, permission denied), it returns a bare wrapped system error that cannot be told apart from a registry write failure. A new sentinel, `workspace.ErrUnresolvable`, makes it classifiable.
  - **Invalid UTF-8:** Go's JSON decoder silently replaces invalid UTF-8, which in a path could name a different directory, so whole request frames must be valid UTF-8.

#### Acceptance checklist (agreed before implementation)

- [x] Once its registry is open, Mio serves `workspace.add {path}`, `workspace.list {cursor}`, and `workspace.remove {id}` over IPC v1, each with an exact key set. `localipc` provides the client functions `AddWorkspace`, `ListWorkspaces` (which follows every page and returns records sorted by path), and `RemoveWorkspace`. Records carry only an ID and a canonical path.
- [x] A failed workspace operation returns an error reply. Its code comes from a closed set (`invalid_path`, `unresolvable_path`, `not_directory`, `protected_path`, `not_owned`, `already_registered`, `overlap`, `registry_full`, `not_found`, `internal`) and its message is bounded. Malformed requests still get no reply: invalid UTF-8, unknown or wrongly cased keys, bad IDs or cursors, empty or overlong paths, and unknown commands. A daemon without a registry treats workspace commands as unknown, and Mio never resolves a relative path.
- [x] Every list page fits in one 64 KiB frame and holds at least one record while any remain. Following `nextCursor` returns each record exactly once while the registry is unchanged, even with 4095-byte, escape-heavy paths. `registry.Open` rejects paths longer than 4095 bytes, and the client rejects pages that do not advance.
- [x] The status request and reply encodings are unchanged, so older clients keep working. Existing status, peer-credential, framing, lock, and doctor tests pass unchanged. `scoprad` serves the new commands end to end.
- [x] ADR-006 records the commands, the error replies, paging, compatibility, and authorization, and ADR-003 notes where ADR-006 extends and qualifies it. The ADR index, READMEs, plan, architecture overview, and this log are updated.

Non-goals: the `scopra workspace` CLI; approvals or any other authorization of actions; revalidation status in listings; renaming or updating workspaces; a new protocol version; longer timeouts for commands that change state; a doctor check for the registry; any Caloris, Goldstone, or Messenger path for workspaces.

#### Test-first record

- New tests:
  - `internal/localipc/workspaces_test.go` (all platforms): exact request encodings, including an unchanged status request; 21 malformed workspace requests; encoder-side field checks; worst-case paging (256 records with 4095-byte paths of characters that JSON escapes to six bytes); page edge cases; the error-code mapping; bounded, valid error messages; acceptance of future error codes; 17 invalid replies; and 8 list pages that do not advance.
  - `internal/localipc/workspaces_linux_test.go`: these run against a real registry over the real socket.
    - Round trip: add, including through a symlink; list sorted by path; remove, leaving the directory untouched.
    - Every error code reaching the client. This includes a relative path that names a registered workspace in Mio's working directory, which must still be refused.
    - Foreign ownership (`/usr`), and registry failures through a fake registry.
    - Paging with three real paths of nearly 4 KiB of control characters.
    - Security: commands without an enabled registry, `EnableWorkspaces` only before `Serve`, and malformed requests dropped with nothing registered.
  - Registry: a corrupt-file case for a path longer than 4095 bytes, and the round-trip test now uses exactly the longest storable path.
  - Workspace: `ErrUnresolvable` for missing and dangling paths and for a path through a regular file.
  - `cmd/scoprad`: `TestRunServesWorkspaceCommands`, end to end through `run`.
- First red: all four packages failed to build, on exactly the declarations this step introduces. These include `localipc.Workspace`, `WorkspaceError`, the error codes, `AddWorkspace`, `ListWorkspaces`, `RemoveWorkspace`, `Daemon.EnableWorkspaces`, the request fields, `registry.MaxPathBytes`, `registry.MaxWorkspaces`, and `workspace.ErrUnresolvable`.
- With declarations-only skeletons, 20 tests failed at runtime. The other 126 passed, and so did one table: the 21 malformed requests pass because today's decoder rejects every workspace command. That table is a guard paired with the encoding test, which fails, so the new decoder must accept valid requests and still reject every malformed one.

#### Implementation and verification record

- `internal/localipc`:
  - `messages.go`: `decodeRequest` reads `command` first, then requires exactly that command's keys and decodes each field strictly, rejecting `null` and wrong types. `checkJSONDocument` adds a whole-document UTF-8 check to the existing size, syntax, and duplicate-key checks. `encodeRequest` uses explicit per-command structs, so status bytes are unchanged.
  - `workspaces.go`: the client functions, `EnableWorkspaces`, the dispatcher, paging, the error mapping, and strict reply decoding. Nested objects get the same exact-key checks as the top level.
  - `socket.go`: `Status`'s connection logic became a shared `roundTrip` that validates the request before touching the filesystem and keeps status's error wording. Requests are dispatched through `reply`.
- `internal/workspace` gains `ErrUnresolvable`, wrapped together with the underlying error, so existing `os.ErrNotExist` checks still hold. `registry.Open` bounds paths at `MaxPathBytes` (4095), and `MaxWorkspaces` is exported. `scoprad` enables workspace dispatch with the registry it opened.
- Documentation: ADR-006 is new. ADR-003 notes where ADR-006 extends and qualifies it, and the risk register's R-01 now lists no access to Mio's state directory or socket. The ADR index, READMEs, plan, architecture overview, and tree are updated.
- Green: all tests pass under the race detector with no skips.
- Added after the first green run, each for a gap found by reasoning or mutation:
  - `TestListWorkspacesSortsByPath`, using a fixed registry whose ID order is the reverse of its path order. With random IDs, a missing client sort would only sometimes be caught.
  - An error message of 1000 invalid bytes, showing that the message must be made valid UTF-8 before it is truncated.
  - `TestWorkspacePagesFitFramesWhenRecordsFillThePage`, after the mutation that removes the budget margin survived. It sweeps path lengths so that some page fills its budget almost exactly.
  - `TestSecurity_ListWorkspacesStopsAnEndlessListing`, with a fake daemon that always answers with another valid page.
- 34 deliberate, compile-safe mutations, each caught by real test failures:
  - Protocol strictness: invalid UTF-8 accepted; `null` accepted as a string; per-command key sets skipped; no path bound; no cursor or ID validation; workspace fields on status; status encoding gaining a key.
  - Dispatch: an error reply instead of silence without a registry; `EnableWorkspaces` allowed after `Serve` or with `nil`; `scoprad` not enabling workspaces.
  - Paging: the cursor record included again; the next cursor dropped; the budget ignored; the budget margin removed.
  - Client: no sort by path; non-ascending pages, any next cursor, or non-strict nested objects accepted; error replies decoded with success keys; request ID, version, path form, or error code not checked; the listing unbounded.
  - Errors: unresolvable mapped to `invalid_path`; no truncation; truncation splitting characters; truncation before the message is made valid.
  - Remove: replying without the path; never removing.
  - Libraries: the registry path bound not enforced; `ErrUnresolvable` not wrapped.
  - Two mutations first failed to compile, because each left an import or variable unused. Rerun in compile-safe form, both were caught.
- Smoke test with built binaries and an independent Python client speaking the wire format:
  - `add` replies with the canonical path, including through a symlink. Error replies carried the codes `invalid_path` (relative path), `unresolvable_path`, `already_registered` (naming the existing ID), `protected_path` (the state directory), and `not_found`.
  - `list` and `remove` work, and registrations survive a restart in a `0600` file.
  - A real `0xFF` byte, an extra key, and an unknown command each got no reply. A JSON lone-surrogate escape decoded to U+FFFD and was reported, as ADR-006 states.
  - Compatibility with the binaries from before this step (`9e300e3`): the old `scopra status` works against the new `scoprad`, and the new `scopra status` against the old one. A workspace command to the old daemon gets the connection closed. Doctor passes.
  - Harness note: the first invalid-UTF-8 smoke check sent a literal backslash-`xff`, because bash does not expand escapes in double quotes. That tested invalid JSON, not invalid UTF-8. Rerun with a real byte, it got no reply and registered nothing.
- Security review covered:
  - authorization scope (same-UID registration on the local socket only, never through Caloris, Goldstone, or Messenger, and not acceptable for approvals);
  - the future sandbox requirement (no socket in workspaces), now recorded in R-01;
  - input bounds (frame, path, ID, cursor, message, and page);
  - silent UTF-8 replacement;
  - relative paths, which never reach a lookup;
  - client termination against a misbehaving daemon;
  - error text (bounded, valid UTF-8, to be escaped on display);
  - status compatibility.
  No high-confidence exploitable issue was found. Same-UID processes can register and remove workspaces, which ADR-006 records as a deliberate limit.
- Local verification passed:
  - `make lint` and `make security-test`;
  - `go test -race -count=1 -timeout 120s ./...` and ten repeated race runs of the IPC, daemon, and workspace packages;
  - the command and internal packages with `CGO_ENABLED=0`;
  - `git diff --check`;
  - build, vet, and test-compile for darwin/arm64, darwin/amd64, windows/amd64, and linux/arm64.
- Coverage: local IPC 82.4% (up from 77.4%), daemon 63.3% (up from 61.4%), registry 93.1% (up from 93.0%), workspace 93.1%, CLI 80.1%, build info 100%, protocol 89.4%. The worst-case paging test takes about 4 seconds under the race detector and 0.3 seconds without it.
- Follow-ups, not part of this increment:
  - the `scopra workspace add/list/remove` CLI (Step 10), which must make paths absolute before sending them and escape Mio's messages;
  - a read-only doctor check for the registry;
  - revalidation status in listings;
  - cleanup of stale temporary files;
  - a decision on approvals and user presence before any command that authorizes an action.
- Pushes to `arena/c7a368c1-scopra`: test checkpoint `f93a06f` (red by design), then the implementation commit that adds this record. CI was not checked, at the maintainer's request.

### 2026-10-16 — Step 10: `scopra workspace` CLI

- Selection: at the end of Step 9 the maintainer accepted the proposed defaults by replying "continue":
  - `add PATH`, where the CLI makes a relative path absolute and Mio never resolves relative paths;
  - `list` in plain text, with a versioned `--json` like doctor's;
  - `remove ID` without a confirmation prompt, because only the registration is removed;
  - exit codes 0, 1, and 2, with Mio's messages escaped before printing.
  `--json` is offered on all three subcommands so that scripts can capture the ID from `add`.
- Findings from the code:
  - `main` prints every error unescaped, but ADR-006 requires clients to escape Mio's messages, which can carry control characters from paths. Error reporting moves into a testable function that escapes all error text, reusing doctor's `terminalSafe`.
  - `filepath.Abs` cleans paths lexically, so from a symlinked working directory `../sibling` would name a different directory than the kernel resolves for every other program. The CLI only prefixes the working directory, and Mio's `EvalSymlinks` resolves `..` physically.
  - Go's `flag` package stops at the first positional argument, so flags must come before `PATH` or `ID`.

#### Acceptance checklist (agreed before implementation)

- [x] `scopra workspace add [--state-dir PATH] [--json] PATH` registers a directory through Mio. A relative PATH gets the working directory prefixed without lexical cleaning, and the CLI never resolves paths itself. Text output names the new ID and the canonical path; `--json` writes `{"schemaVersion":1,"workspace":{"id":...,"path":...}}`.
- [x] `scopra workspace list [--state-dir PATH] [--json]` prints every registration sorted by path, one `ID  path` line each, or a message when there are none. `--json` writes `{"schemaVersion":1,"workspaces":[...]}`, with an empty array when there are none.
- [x] `scopra workspace remove [--state-dir PATH] [--json] ID` removes a registration without a prompt and says that the directory was not changed. `--json` writes the removed record.
- [x] Exit status:
  - `0` on success;
  - `1` when Mio is unavailable or rejects the request, with the error naming Mio's code;
  - `2` for usage errors, detected before Mio is contacted: a missing or extra argument, an unknown subcommand, a malformed ID, or a path that is not valid UTF-8 or would be longer than 4095 bytes;
  - `130` when interrupted.
  Every error message, and every path in text output, has control characters escaped.
- [x] `scopra status` and `scopra doctor` behave as before, except that their error text is now escaped too. The README, usage text, plan, architecture overview, and this log are updated, and the wire protocol and Mio are unchanged.

Non-goals: removal by path; confirmation prompts; flags after positional arguments; a doctor check for the registry; revalidation status in listings; shell completion; any change to the IPC protocol or to Mio; and richer output, such as tables or colour.

#### Test-first record

- New tests in `cmd/scopra/workspace_test.go`. The Linux ones run against a real registry over the real socket.
  - The exact text formats for `add`, `list` (including the empty message), and `remove`, and that removal leaves the directory in place.
  - The exact JSON for all three subcommands, including `[]` for an empty list.
  - Physical resolution of `../sibling` and `.` from a working directory reached through a symlink. Lexical cleaning would name a directory that does not exist.
  - Mio's codes reaching the user with exit status 1: `unresolvable_path`, `protected_path`, and `not_found`. Mio being unavailable also gives exit status 1.
  - 13 usage errors that must be detected before Mio is contacted, without being reported as an unknown command.
  - Help for `scopra`, `scopra workspace`, and `scopra workspace add`.
  - Escaping of control characters in `add` and `list` output and in Mio's error text.
  - `exitCode` for success, help, usage errors, interruption, and an escaped failure.
- The usage-error table checks that `workspace` is not reported as an unknown command. Today's CLI already returns a usage error for any unknown command, so without that check the table would pass before any implementation.
- First red: the test package failed to build on exactly `exitCode`; the workspace commands are reached through `run`.
- With a declarations-only `exitCode` skeleton, all 9 new tests failed at runtime (22 subtests between them). The package's 21 existing tests passed.

#### Implementation and verification record

- `cmd/scopra/workspace.go`:
  - `runWorkspace` dispatches `add`, `list`, and `remove`, which share `parseWorkspaceFlags` (`--state-dir`, `--json`) and write single-line JSON with `workspaceSchemaVersion` 1.
  - `add` checks that the path is valid UTF-8, prefixes `os.Getwd()` to a relative path without cleaning it, and then checks the 4095-byte bound, all before contacting Mio.
  - `remove` checks the ID with the new `localipc.ValidWorkspaceID`.
  - Paths in text output pass through `terminalSafe`.
- `cmd/scopra/main.go`: `main` now exits through `exitCode`, which keeps the existing statuses (0, 2, 130, and 1) and escapes every error message. It also dispatches `workspace`, and the usage text lists the new commands and says that flags go before `PATH` or `ID`.
- No change to the wire protocol, to Mio, or to any ADR. The CLI README documents the commands, the output, the JSON shapes, the exit statuses, and the trust notes. The README status line, the `scoprad` README, the architecture overview, the plan, and the tree are updated. The plan's checkboxes stay unticked under its own rule, because execution-time revalidation does not exist yet.
- Green: all tests pass under the race detector with no skips.
- 20 deliberate, compile-safe mutations, each caught by real test failures:
  - Exit statuses: error text not escaped; interruption exiting 1; help through a usage error exiting 2; usage errors exiting 1.
  - Paths: relative paths cleaned lexically, or sent unchanged; no UTF-8 check; no length check.
  - Arguments: no ID check; `ValidWorkspaceID` accepting anything; `add` accepting extra arguments; `list` accepting arguments.
  - Output: `add` or `list` not escaped; `remove` without its note that the directory was not changed; `null` instead of `[]`; the wrong schema version.
  - Wiring: `workspace` not dispatched; workspace help reported as a usage error; usage text without the workspace commands.
- `main` cannot run under `go test`, so the smoke test checked it directly. A path with ESC and BEL reached stderr as `\x1b[31m\a`, with no raw control bytes.
- Smoke test with built binaries in a fresh home directory, using the default state directory:
  - Mio not running: exit 1 with `daemon is unavailable`.
  - `add` and `list` (text and single-line JSON) work. Run from a directory reached through a symlink, `add ../sibling` registered the physical `real/sibling`. `remove` reports that the directory was not changed, and removing again exits 1 with `not_found`.
  - A missing path (`unresolvable_path`) and a directory containing the state directory (`protected_path`) exit 1. A malformed ID, a missing subcommand, and a flag after `PATH` exit 2.
  - `scopra status` and `scopra doctor` are unchanged and pass.
  - Interruption (130) cannot practically be triggered against commands that finish in milliseconds; `TestExitCode` covers it.
- Security review covered:
  - escaping of every error message and of every path in text output;
  - paths made absolute without lexical cleaning, so the CLI cannot redirect `..` across symlinks;
  - validation of arguments before any contact with Mio;
  - removal never touching directories;
  - single-line JSON;
  - the unchanged ADR-006 trust model: any same-user process can use these commands, and registration authorizes nothing.
  No high-confidence exploitable issue was found.
- Local verification passed:
  - `make lint` and `make security-test`;
  - `go test -race -count=1 -timeout 120s ./...` and ten repeated race runs of the CLI and daemon packages;
  - the command and internal packages with `CGO_ENABLED=0`;
  - `git diff --check`;
  - build, vet, and test-compile for darwin/arm64, darwin/amd64, windows/amd64, and linux/arm64.
- Coverage: CLI 89.7% (up from 80.1%), local IPC 82.3% (from 82.4%, because the new exported `ValidWorkspaceID` is exercised only by the CLI's tests), daemon 63.3%, registry 93.1%, workspace 93.1%, build info 100%, protocol 89.4%.
- Environment: this session also started in a fresh sandbox. A temporary index showed the restored working tree was byte-identical to the pushed tip `2b5aac0`, so a mixed reset re-attached the branch without touching files. The toolchain was rebuilt from the pinned source, and the baseline passed before any change.
- Follow-ups, not part of this increment:
  - removal by path;
  - a read-only doctor check for the registry;
  - revalidation status in listings;
  - cleanup of stale temporary files;
  - a decision on approvals and user presence before any command that authorizes an action.
- Pushes to `arena/c7a368c1-scopra`: test checkpoint `a389b37` (red by design), then the implementation commit that adds this record. CI was not checked, at the maintainer's request.

### 2026-10-16 — Step 11: Doctor checks the workspace registry

- Selection: the maintainer replied "Continue next" without choosing between this small doctor check and the containment work, so the smaller increment comes first. The question of whether an environment with rootless Podman is available for containment stays open.
- Motivation: since Step 8 a bad registry stops Mio, but doctor reports only `daemon: fail` (not running). The reason appears only in `scoprad`'s own error.
- Findings from the code:
  - `registry.Open` requires the caller to own the directory exclusively, which Mio does while it holds its lock. Doctor runs alongside Mio, so it needs an explicit read-only entry point. `registry.Check` never writes, and it is safe while Mio runs because Mio replaces the file with an atomic rename.
  - A missing state directory surfaces from the registry as a wrapped `os.ErrNotExist`, so doctor can report that nothing is registered yet.
  - A "workspaces" entry in `notChecked` would contradict a registry check. What doctor still does not do is revalidate registered workspaces, so that entry becomes `workspace revalidation`.
  - ADR-005's Mio integration amendment says that doctor does not inspect the registry; this step resolves that.

#### Acceptance checklist (agreed before implementation)

- [x] `registry.Check(dir)` validates the registry exactly as `Open` does: the same file and directory checks, strict decoding, and the registry-directory rule. It needs no validator, writes or creates nothing, does not block on a FIFO, and returns the number of registrations.
- [x] `scopra doctor` runs a `workspace-registry` check between `state-directory` and `daemon`, and the daemon check still runs:
  - `pass` with the number of registrations, including none (no file yet, or no state directory yet);
  - `fail` when the registry is unsafe, invalid, or unreadable. The hint names the file, says that `scoprad` will not start until it is fixed or removed, and says that removing it unregisters every workspace without changing any directory;
  - `skip` when the platform or state-directory check failed.
- [x] Doctor stays read-only. It never creates, changes, or removes the registry file or the state directory, and after a failing check the file's bytes and mode are unchanged.
- [x] The JSON schema stays at version 1, because adding a check is compatible, and `notChecked` lists `workspace revalidation` instead of `workspaces`. Text output escapes control characters in the new detail and hint.
- [x] ADR-005's amendment is brought up to date, and the CLI README (the doctor table, the JSON example, and the read-only promises), the plan, and this log are updated. The wire protocol and Mio are unchanged.

Non-goals: revalidating registered workspaces; repairing or removing the file; reading the registry through Mio; new IPC commands; changing other checks; and a new schema version.

#### Test-first record

- New and changed tests:
  - `internal/workspace/registry`:
    - `TestCheckCountsRegistrationsWithoutWriting`: zero without a file, then two after real additions, with the file and directory unchanged.
    - `TestCheckReportsMissingDirectory`: `os.ErrNotExist`, which doctor relies on, and nothing is created.
    - `TestSecurity_CheckRejectsUnsafeRegistryFile`: every occupant in the shared table also used for `Open`, including not blocking on a FIFO.
    - `TestSecurity_CheckRejectsInvalidRegistry`: an unsupported version, and a record containing the registry directory.
  - `cmd/scopra`:
    - `TestRunDoctorChecksWorkspaceRegistry`: no file, one and two registrations, invalid content, and a group-readable file. The cases check exact details, hints that name the file and both consequences, the renamed not-checked area, and that files are left unchanged or not created.
    - `TestRunDoctorEscapesControlCharactersInRegistryHint`.
    - Updated: the missing-state-directory test also checks the registry detail, and the human report must show the new check and `workspace revalidation`. The 11 existing check sequences gain `workspace-registry`: a passing or warning state directory gives `pass`, a failing one gives `skip`.
- First red: the registry package failed to build on exactly `Check`. Doctor built, and 14 tests failed at runtime: the 2 new ones and the 12 whose expectations changed. The other 18 passed.
- With a declarations-only `Check` skeleton, the 4 new registry tests failed at runtime (7 subtests). The other 19 passed.

#### Implementation and verification record

- `internal/workspace/registry`: `Open` and the new `Check` share `load`, which holds the former body of `Open`. They cannot drift apart: `Check` simply skips the validator and returns `len(records)`. `FileName` is exported for the doctor hint.
- `cmd/scopra/doctor.go`:
  - `diagnoseWorkspaceRegistry` maps `registry.Check` to a result. A missing state directory, or zero, one, or more registrations, gives `pass`. An unsafe, invalid, or unreadable file gives `fail`, with a hint naming the file and both consequences.
  - `diagnose` places the check between `state-directory` and `daemon`, skips it after a platform or state-directory failure, and still runs the daemon check.
  - `notCheckedAreas` lists `workspace revalidation`.
- Documentation: the CLI README gains a doctor table row, an updated JSON example, and the read-only promise for the registry. ADR-005's Mio integration amendment no longer says that doctor ignores the registry, and records `registry.Check`. The README status line, the architecture overview, and the plan are updated. The wire protocol and Mio are unchanged.
- Green: all tests pass under the race detector with no skips.
- 14 deliberate, compile-safe mutations, each caught by real test failures:
  - `load` skipping the registry-directory rule;
  - `Check` always counting zero, or hiding errors;
  - a missing state directory reported as a failure;
  - the wrong singular;
  - the invalid-file hint omitting its consequence;
  - an insecure file passing;
  - the check placed after `daemon`;
  - the check run despite a failed state directory;
  - the daemon check skipped after a registry failure;
  - `notChecked` keeping `workspaces`;
  - hints not escaped;
  - the hint naming the directory instead of the file;
  - a wrong registry file name.
- Hint escaping was untested before this step, because no hint ever contained a path. The new hint test is the first to catch it.
- Smoke test with built binaries in a fresh home directory, using the default state directory:
  - Before Mio ever ran, the check passes with the missing state directory explained, and the not-checked line lists `workspace revalidation`.
  - With Mio running and two registrations, doctor reads the registry alongside Mio and passes, in text and in JSON.
  - After the file is corrupted while Mio is stopped, doctor reports `workspace-registry: fail` with the file and both consequences, as well as `daemon: fail`, and leaves the file byte-identical with the same mode. `scoprad` then refuses to start for the same reason.
  - After the file is removed, as the hint suggests, the check passes, `scoprad` starts, and doctor passes.
- Security review: doctor stays read-only, reading through `registry.Check` with the same no-follow, non-blocking, size-capped, and strict rules as `Open`. Reading alongside a running Mio is safe because Mio replaces the file atomically. Paths in hints are escaped in text and JSON-escaped in `--json` output. No high-confidence exploitable issue was found.
- Local verification passed:
  - `make lint` and `make security-test`;
  - `go test -race -count=1 -timeout 120s ./...` and ten repeated race runs of the CLI and workspace packages;
  - the command and internal packages with `CGO_ENABLED=0`;
  - `git diff --check`;
  - build, vet, and test-compile for darwin/arm64, darwin/amd64, windows/amd64, and linux/arm64.
- Coverage: registry 93.4% (up from 93.1%), CLI 89.9% (up from 89.7%), local IPC 82.3%, daemon 63.3%, workspace 93.1%, build info 100%, protocol 89.4%.
- Environment: this session again started in a fresh sandbox. A temporary index showed the restored working tree was byte-identical to the pushed tip `021e014`, so a mixed reset re-attached the branch without touching files. The toolchain was rebuilt from the pinned source, and the baseline passed before any change.
- Follow-ups, not part of this increment:
  - When the registry check fails, the daemon check's generic hint ("Start Mio with `scoprad`") could point to the registry instead.
  - Revalidating registered workspaces in doctor or in `list`.
  - Removal by path.
  - Containment and execution, which need rootless Podman. That is not available here, and CI is still blocked by an organization setting.
- Pushes to `arena/c7a368c1-scopra`: test checkpoint `2bc83ca` (red by design), then the implementation commit that adds this record. CI was not checked, at the maintainer's request.

### 2026-10-16 — Step 12: Doctor revalidates registered workspaces

- Selection: the maintainer again replied "Continue next" without answering whether rootless Podman is available. The README's alpha defaults fix the shape of containment (rootless Podman, workspace-only mounts, network off, an empty environment, no PTY, and no native fallback). Two things remain open: the product decision of which container image runs commands, and any way to test execution here. This step is therefore the smallest useful one that needs neither, and it was offered at the end of Step 11.
- Doctor revalidates registered workspaces, filling the gap that Step 11 recorded as `workspace revalidation` in `notChecked`. Doctor reads the registry file, so it has each record's fingerprint and can apply ADR-004's read-only `Revalidate`. `scopra workspace list` cannot do this without a wire change, because its replies carry no fingerprints.
- `registry.Check` returns the registrations instead of a count, so that doctor gets the records from the same validated read.

#### Acceptance checklist (agreed before implementation)

- [x] `registry.Check(dir)` returns the registrations, sorted by path, from the same read-only validation as before.
- [x] `scopra doctor` runs a `workspaces` check after `workspace-registry` and before `daemon`. It revalidates each registration under ADR-004 with the validator configuration that Mio uses, which protects the state directory:
  - `pass` when none are registered or all still qualify;
  - `warn` naming up to five stale registrations, each with its ID, its path, and a short reason (missing, not a directory, not owned by you, is or contains a protected location, or changed since it was registered), plus a count of any others. The hint says to restore the directory or to run `scopra workspace remove ID`, which does not change the directory;
  - `fail` when validation cannot be prepared, because `scoprad` would not start either;
  - `skip` when the platform, state-directory, or workspace-registry check failed.
- [x] Doctor stays read-only: it never changes the registry file, the workspaces, or their contents, and a warning does not change the exit status (`0`).
- [x] `notChecked` no longer lists `workspace revalidation`. The JSON schema stays at version 1, and text output escapes control characters in the new detail.
- [x] The CLI README, the plan, and this log are updated. The wire protocol, Mio, and the ADRs are unchanged.

Non-goals: revalidation status in `scopra workspace list`, which needs a wire change; removing stale registrations automatically; execution-time revalidation; changing the daemon check's hint; and containment.

#### Test-first record

1. **Tests written first.**
   - The registry test `TestCheckReturnsRegistrationsWithoutWriting` (renamed from `TestCheckCountsRegistrationsWithoutWriting`) expects `Check` to return the same records, sorted by path, that `List` returns. The other `Check` tests now expect no records alongside each error.
   - The doctor tests expect a `workspaces` check after `workspace-registry` in every check sequence: `pass` without registrations, and `skip` when an earlier check failed. In the Step 11 table, registrations whose directories do not exist now expect `warn`. The `notChecked` list must be exactly enrollment, relay, policy, containment, and execution.
   - New doctor tests:
     - `TestRunDoctorRevalidatesRegisteredWorkspaces` registers real directories through `registry.Open` and `Add` with Mio's validator configuration. It covers seven cases: one and all qualifying; a deleted, a replaced, and a file-swapped directory; two of three missing; and seven stale, of which five are listed plus "and 2 more". It also checks which records are named, the hint, an unchanged registry file, and that a deleted workspace is not recreated.
     - `TestRunDoctorWarnsWithoutFailingWhenOnlyWorkspacesAreStale` checks exit 0 and result `warn` while Mio responds.
     - `TestRunDoctorEscapesControlCharactersInStaleWorkspaces` covers an escape sequence and a newline in a stale path.
2. **Build failure.** The registry tests did not compile, with 6 type errors, because `Check` still returned a count. The CLI tests compiled against existing APIs and 16 failed at runtime: every check sequence lacked `workspaces`, the human report still listed workspace revalidation, and so on.
3. **Declarations-only skeleton.** `Check` returned `([]Record, error)` without any records, and doctor compiled against it by counting them. Runtime red:
   - registry: only `TestCheckReturnsRegistrationsWithoutWriting` failed (`Check() = [], <nil>`);
   - CLI: the same 16 tests failed.

#### Implementation and verification record

- `internal/workspace/registry`: `Check` returns the records that `load` validated, already sorted by path, instead of their count. It still never writes and does not revalidate the workspaces.
- `cmd/scopra/doctor.go`:
  - `diagnoseWorkspaceRegistry` returns the records along with its result, so the new check revalidates exactly what the registry check read, with no second read. Its failure cases moved unchanged into `registryFailure`.
  - `diagnoseWorkspaces` builds the validator the way `scoprad` does (`workspace.NewValidator(stateDir)`) and calls ADR-004's `Revalidate` for each record. `staleReason` maps the ADR-004 errors to short reasons and falls back to the error text. At most `maxStaleWorkspacesShown` (5) entries are listed; any others are counted.
  - `diagnose` places the check between `workspace-registry` and `daemon`. The check is skipped after a platform, state-directory, or registry failure, and the daemon check still runs.
  - `notCheckedAreas` drops `workspace revalidation`.
- Revalidation inspects only metadata (`EvalSymlinks` and `Lstat`) and opens no workspace, so doctor stays read-only.
- Documentation:
  - The CLI README gains a doctor table row, the skip rules, a `warn` example, and an updated JSON example. It notes that the check is point-in-time and that ADR-004's inode-reuse limitation applies.
  - The README status line, the architecture overview, and the plan are updated.
  - The wire protocol, Mio, and the ADRs are unchanged.
- Test fix during implementation: the replaced-directory case first deleted the directory and then created a new one at the same path. The filesystem reused the freed inode number, so the fingerprint legitimately matched; this is the limitation ADR-004 already records. The replacement is now created before the original is removed, so it cannot reuse that inode.
- Tests added during implementation:
  - a case for a workspace that now contains the home directory (`HOME` moved inside it), with the reason "is or contains a protected location";
  - exact `workspaces` details for the no-registrations pass and for each skip (after a registry, state-directory, or platform failure). Before this step no test pinned any skip detail.
- Green: all tests pass under the race detector with no skips.
- Mutation testing: 25 deliberate, compile-safe mutations. 23 were caught by real test failures:
  - `Check` dropping its records;
  - the check running after a registry failure;
  - the registry check handing on no records, for one registration or for several;
  - revalidation errors ignored;
  - the ID omitted from stale entries;
  - a listing bound of 7, or no bound at all;
  - no "and N more";
  - inverted verb agreement;
  - each of the missing, changed, not-a-directory, and protected reasons removed;
  - stale reported as `pass`;
  - the hint losing `scopra workspace remove ID`;
  - `notChecked` keeping revalidation;
  - "all 1" for a single registration;
  - a validator built when nothing is registered;
  - each of the three skip details naming the wrong prerequisite;
  - an empty protected path, which makes validation fail. This tested a different fault from the one intended, so M23b repeats it properly.
- Two mutations survived, both as expected:
  - **M14**, removing the "not owned by you" reason: only root could hand a directory to another owner in a test.
  - **M23b**, building the validator without the state directory: equivalent in outcome. Registry loading already rejects any record that is or contains the state directory, so the `workspaces` check is skipped first. A record whose path later resolved there through a new symlink would still be flagged, as changed; only the reason shown would differ. The protection stays so that doctor matches Mio's configuration.
- Smoke test with built binaries, using a fresh home directory and the default state directory:
  - Two workspaces were registered through `scopra workspace add`; doctor passed with "all 2 registered workspaces still qualify", and the not-checked line no longer lists revalidation.
  - One directory was deleted and the other replaced through a rename. With Mio running, doctor reported `warn` naming both IDs and paths with "missing" and "changed since it was registered", printed result `warn`, and exited 0; `--json` agreed.
  - The registry file's SHA-256 was unchanged, and the deleted directory was not recreated. `scopra workspace list` still showed both registrations, as expected (a non-goal).
  - With Mio stopped, the warning remained and `daemon: fail` made doctor exit 1.
  - After both registrations were removed with `scopra workspace remove`, as the hint suggests, the directories were kept and doctor passed.
- Security review: doctor never writes; revalidation is metadata-only and safe alongside a running Mio. Stale paths are escaped in text output (tested with an escape sequence and a newline) and JSON-escaped in `--json` output. Listing at most five entries bounds the detail for a full registry. The warning names only the user's own registrations, which the same user can already list. No high-confidence exploitable issue was found.
- Local verification passed:
  - `make lint` and `make security-test`;
  - `go test -race -count=1 -timeout 60s ./...` and ten repeated race runs of the CLI and registry packages;
  - every package with `CGO_ENABLED=0`;
  - `git diff --check`;
  - build, vet, and test-compile for darwin/arm64, darwin/amd64, windows/amd64, and linux/arm64.
  - No files were added, so `docs/repository-tree.txt` is unchanged.
- Coverage: CLI 90.0% (up from 89.9%), registry 93.4%, local IPC 82.3%, daemon 63.3%, workspace 93.1%, build info 100%, protocol 89.4%.
- Environment: the sandbox persisted from Step 11, so no recovery or toolchain rebuild was needed. The baseline passed before any change.
- Follow-ups, not part of this increment:
  - When the registry check fails, the daemon check's generic hint could point to the registry instead.
  - Revalidation status in `scopra workspace list`, which needs a wire change.
  - Removal by path.
  - Containment and execution. They need rootless Podman, which is not available here, and a decision about which container image runs commands. CI is still blocked by an organization setting.
- Pushes to `arena/c7a368c1-scopra`: test checkpoint `559a6d6` (red by design), then the implementation commit that adds this record. CI was not checked, at the maintainer's request.

### 2026-10-16 — Step 13: Doctor's daemon hint points at a bad registry

- Selection: after Step 12 the maintainer was told that silence on the Podman and image questions would mean option (a), then (b). The reply was "Continue", so this is (a).
- Problem: `scoprad` refuses to start when the workspace registry is unusable (Step 8). Doctor then reports `workspace-registry: fail` and `daemon: fail` ("Scopra Mio is not running"). The daemon hint still says to start Mio with `scoprad`, which would only fail again for the same reason.

#### Acceptance checklist (agreed before implementation)

- [x] When the `workspace-registry` check fails and Mio is not running, the `daemon` check's hint says that `scoprad` will not start until the problem reported by the `workspace-registry` check is fixed, and only then says how to start it. The status (`fail`) and detail are unchanged.
- [x] Every other daemon result keeps its current hint. This includes an unsafe socket alongside a bad registry, and the not-running hint when the registry check passes.
- [x] Doctor stays read-only, the JSON schema stays at version 1, and text output still escapes control characters.
- [x] The CLI README's daemon row and this log are updated. The wire protocol, Mio, and the ADRs are unchanged.

Non-goals:
- Pointing the hint at the `workspaces` check when workspace validation cannot be prepared. That failure cannot be produced in tests here, and that check's own hint already says that `scoprad` will not start.
- Changing any other hint.
- Removal by path, which is the next step.

#### Test-first record

1. **Tests written first.**
   - `TestRunDoctorChecksWorkspaceRegistry` asserts the daemon hint for each registry outcome. For an invalid or unsafe registry it must begin "scoprad will not start until the problem reported by the workspace-registry check is fixed; " and then say how to start Mio. Otherwise it must be the plain start hint. The daemon detail must stay unchanged in every row.
   - The new `TestRunDoctorKeepsUnsafeSocketHintWithBadRegistry` pins the unsafe-socket hint, word for word, alongside an invalid registry.
2. **Runtime red.** The tests compiled against the existing code, so no skeleton was needed. Two subtests failed for the intended reason, still getting the plain start hint: `TestRunDoctorChecksWorkspaceRegistry/unsupported_version` and `/group-readable_file`. The unsafe-socket test already passed, as intended. It guards behaviour that must not change, so an implementation that rewrote every daemon hint would fail it.

#### Implementation and verification record

- `cmd/scopra/doctor.go`:
  - `diagnose` passes `diagnoseDaemon` the name of an earlier failed check that would stop `scoprad` from starting (`blockedBy`). Today that is only `workspace-registry`.
  - Only the not-running branch uses `blockedBy`. Its hint becomes "scoprad will not start until the problem reported by the workspace-registry check is fixed; then start Mio in the foreground with `scoprad` (pass the same --state-dir, if any)". The status, the detail, and every other branch are unchanged.
  - The check runs no new I/O, so doctor stays read-only.
- Documentation: the CLI README's daemon row describes the hint. The wire protocol, Mio, and the ADRs are unchanged.
- Green: all tests pass under the race detector with no skips.
- Mutation testing: 8 deliberate, compile-safe mutations, all caught by real test failures:
  - `blockedBy` never set, or always set;
  - the blocked prefix added to the unsafe-socket hint too;
  - an inverted condition;
  - the hint naming the `workspaces` check;
  - the start step dropped;
  - the `workspaces` check run despite a registry failure;
  - the daemon detail changed.
- Smoke test with built binaries, using a fresh home directory and the default state directory:
  - Before Mio ever ran, the plain start hint was shown.
  - With an unsupported registry version and Mio stopped, the daemon hint pointed at the `workspace-registry` check, in text and in `--json`. `scoprad` then refused to start with "open workspace registry: … unsupported version 2", as the hint says.
  - After the file was removed, the plain hint returned.
- Security review: no new input, output channel, or file access. The hint contains only a fixed check name and is escaped like every other hint. No exploitable issue was found.
- Local verification passed:
  - `make lint` and `make security-test`;
  - `go test -race -count=1 -timeout 60s ./...` and ten repeated race runs of the CLI package;
  - every package with `CGO_ENABLED=0`;
  - `git diff --check`;
  - build, vet, and test-compile for darwin/arm64, darwin/amd64, windows/amd64, and linux/arm64.
  - The tree is unchanged.
- Coverage: CLI 90.2% (up from 90.0%); the other packages are unchanged.
- Pushes to `arena/c7a368c1-scopra`: test checkpoint `3120f08` (red by design), then the implementation commit that adds this record. CI was not checked, at the maintainer's request.

### 2026-10-16 — Step 14: Remove a workspace by path

- Selection: option (b) from the Step 12 summary, announced as the step after (a) if the maintainer did not choose otherwise. Today a registration can be removed only by its 32-character ID, which must first be copied from `scopra workspace list` or from doctor's `workspaces` warning.
- Design: the CLI finds the registration through the existing `workspace.list` request and removes it with the existing `workspace.remove` request, so the wire protocol and Mio are unchanged. An explicit `--path` flag avoids guessing whether an argument is an ID or a path, since a relative directory may be named with 32 hexadecimal characters.

#### Acceptance checklist (agreed before implementation)

- [x] `scopra workspace remove --path PATH` unregisters the workspace registered for PATH and changes no directory. Its output, `--json` document (`schemaVersion` 1), and exit codes match removal by ID. A relative PATH is made absolute exactly as `add` does it.
- [x] PATH matches a registration in two ways:
  - it equals the registered path exactly, which still works after the directory is gone;
  - or it now resolves to the registered path. Resolution is read-only and follows symlinks; `..` is never cleaned lexically.

  If the two rules pick different registrations, the command refuses and asks for removal by ID, and nothing is removed.
- [x] When nothing matches, the command exits `1` with an escaped error that names PATH and suggests the path as `scopra workspace list` shows it or removal by ID; nothing is removed. These are usage errors (exit `2`), detected before Mio is contacted:
  - both an ID and `--path`, or neither;
  - an empty `--path`, or a missing value;
  - invalid UTF-8, or a path over 4095 bytes;
  - `--path` on `add` or `list`.
- [x] Removal by ID is unchanged, and Mio being unavailable is reported as for the other commands.
- [x] The CLI README, the usage text, and this log are updated. The wire protocol, Mio, and the ADRs are unchanged.

Non-goals:
- Lexical normalization of PATH for directories that no longer exist (trailing slashes, `.`, `..`). Cleaning could choose a different registration than the kernel's resolution would.
- Path matching inside Mio, which would be a wire change.
- Removing several workspaces at once.
- Any change to `add` or `list`.

#### Test-first record

1. **Tests written first** (`cmd/scopra/workspace_test.go`, against a real in-process Mio):
   - `TestRunWorkspaceRemoveByPath` removes one of two registrations by `--path`, given as:
     - the exact path, a relative path, or a symlink;
     - an existing directory with a trailing slash;
     - the exact path of a directory that no longer exists;
     - the exact path with `--json`.

     Each case checks the exact output, that the other registration remains, and that the directory is untouched.
   - `TestRunWorkspaceRemoveByPathRequiresOneMatch` covers four cases that must exit `1` with a specific error and leave both registrations in place:
     - an unregistered directory;
     - a missing directory written with a trailing slash, which pins the absence of lexical cleaning;
     - a path with an escape sequence and a newline, which must be escaped;
     - a registered path that now resolves, through a symlink, to the other workspace. The error must name both IDs.
   - Eight new usage-error rows, a removal-by-path row in the unavailable-Mio test, and a usage text naming `(ID | --path PATH)`.
2. **Build failure.** Five references to the undefined `errWorkspacePathNotRegistered` and `errAmbiguousWorkspacePath`.
3. **Declarations-only skeleton** (the two error values). Runtime red, each failure for the intended reason ("flag provided but not defined: -path"):
   - all 6 `TestRunWorkspaceRemoveByPath` subtests and all 4 `TestRunWorkspaceRemoveByPathRequiresOneMatch` subtests;
   - `TestRunWorkspaceReportsUnavailableMio` (exit `2` instead of `1`);
   - `TestRunWorkspaceHelp`.

   The new usage-error rows already passed, because an unknown flag is also a usage error. They guard the implementation against accepting bad input.

#### Implementation and verification record

- `cmd/scopra/workspace.go`:
  - `parseWorkspaceFlags` defines `--path` only for `remove`, and records whether it was given, so `--path ""` is rejected rather than ignored.
  - `absoluteWorkspacePath`, extracted unchanged from `add`, checks UTF-8 and length and prefixes the working directory without cleaning; `add` and `remove --path` share it.
  - `workspaceIDForPath` lists the registrations through the existing client. It looks for an exact match, then resolves PATH with `filepath.EvalSymlinks`, which is read-only and never cleaned first, and looks for a match on the result.
    - If the exact match and the resolved match disagree, it fails with `errAmbiguousWorkspacePath`, naming both IDs.
    - If neither matches, it fails with `errWorkspacePathNotRegistered`.
    - Otherwise the ID goes to the existing `RemoveWorkspace` call, so output, JSON, and Mio's own errors are exactly those of removal by ID.
  - A registration removed and re-added between the list and the removal gets a new random ID, so the removal then fails with `not_found` rather than removing anything else.
- `cmd/scopra/main.go`: the usage line reads `workspace remove [--state-dir PATH] [--json] (ID | --path PATH)`.
- Documentation: the CLI README describes `remove --path`, its two matching rules, the refusal, the missing-directory caveat, and the exit codes. The wire protocol, Mio, and the ADRs are unchanged.
- Green: all tests pass under the race detector with no skips.
- Mutation testing: 16 deliberate, compile-safe mutations, all caught by real test failures:
  - either matching rule disabled;
  - the ambiguity not refused;
  - PATH cleaned before matching;
  - `--path` defined for every subcommand;
  - an ID together with `--path` allowed;
  - an empty path accepted;
  - the length limit skipped;
  - relative paths left relative;
  - either error value not wrapped;
  - the ambiguity error without the IDs;
  - `--path` never recorded;
  - a list error swallowed;
  - the wrong registration removed;
  - a missing match falling back to the first registration.
- Smoke test with built binaries and a running `scoprad`, with five registered directories:
  - removal by exact path, by relative path, and (with `--json`) by the listed path of a deleted directory all succeeded;
  - a registered directory replaced by a symlink to another workspace was refused with exit `1`, naming both IDs, and removed nothing;
  - an unregistered path exited `1` with the suggestion;
  - an ID together with `--path` exited `2`;
  - the two remaining registrations stayed, the removed workspaces' directories were kept, and `scoprad` stopped cleanly.
- Security review:
  - `remove --path` changes only Mio's registrations, through the same same-UID channel and existing request as removal by ID.
  - Resolving PATH is read-only.
  - Refusing on disagreement prevents a replaced directory or a symlink from redirecting a removal to another workspace.
  - Errors quote PATH with `%q` and are escaped again on the terminal.
  - No high-confidence exploitable issue was found.
- Local verification passed:
  - `make lint` and `make security-test`;
  - `go test -race -count=1 -timeout 60s ./...` and ten repeated race runs of the CLI package;
  - every package with `CGO_ENABLED=0`;
  - `git diff --check`;
  - build, vet, and test-compile for darwin/arm64, darwin/amd64, windows/amd64, and linux/arm64.
  - The tree is unchanged.
- Coverage: CLI 91.6% (up from 90.2%); the other packages are unchanged.
- Follow-ups, not part of this increment:
  - Doctor's `workspaces` hint could mention `--path`.
  - Revalidation status in `scopra workspace list`, which needs a wire change.
  - Containment and execution. They need rootless Podman, which is not available here, and a decision about which container image runs commands.
- Pushes to `arena/c7a368c1-scopra`: test checkpoint `5095ae3` (red by design), then the implementation commit that adds this record. CI was not checked, at the maintainer's request.

### 2026-10-16 — Step 15: Execution request v1 decoding

- Selection: after Step 14 the maintainer was told that, without a choice, the plan and API docs would be checked for the execution request shape and option (b) proposed before any code was written. The reply was "Continue", so this entry and the ADR-007 draft are that proposal. Implementation starts only after confirmation; a "continue" accepts the proposal.
- Research:
  - `docs/architecture/protocol.md` lists the request's contents.
  - The PRD supplies the remaining requirements (Execution, Environment and network, Grants, Security Invariants, and principle 3), and the plan's Milestone 1 Messenger tasks ask for "protocol versions, IDs, canonical request, results, output chunks, errors, and execution states".
  - ADR-002 reserves `execution.request` and leaves payload validation to the receiver.
  - The ADR index already lists "canonical execution requests" as a required follow-up.
  - No Podman is needed: this is pure validation.
- Decisions proposed in ADR-007 that most need a maintainer's view:
  - `shell` must be `false`;
  - `workspaceId` is Mio's local registration ID;
  - the executable is a bare name, a clean absolute path, or `./relative`;
  - `workspaceAccess` and `network` replace named profiles for now, with `network` accepting only `none`;
  - the limit ranges are protocol sanity bounds, with policy setting the real maxima;
  - the request window is at most 1 hour, with UTC second-precision timestamps;
  - environment variables, standard input, artifacts, output policy, and the canonical form or action hash are absent from version 1.

- Confirmation: the maintainer replied "Continue", accepting ADR-007 and the checklist below as written.

#### Acceptance checklist (agreed before implementation)

- [x] ADR-007 is accepted, with any changes the maintainer asks for, and indexed as accepted. `docs/architecture/protocol.md` and the protocol package README describe it.
- [x] `protocol.DecodeExecutionRequest(payload)` returns a typed `ExecutionRequest` that follows ADR-007's shape and field rules. Its strict decoding covers size, trailing data, duplicate keys, `null`, invalid UTF-8, invalid `\u` escapes, exact key sets, and integer-only limits. Every error names the field, and no partially valid request is returned.
- [x] `Envelope.ExecutionRequest()` decodes only envelopes of type `execution.request`. `ExecutionRequest.ValidateAt(now)` enforces the request window: at most 30 seconds of future skew, strict expiry, and a lifetime of at most 1 hour.
- [x] Tests:
  - a valid request decodes to the expected values;
  - table-driven rejections cover every field rule and every strictness rule, at both sides of each bound;
  - a fuzz test shows that decoding never panics, and that every accepted request satisfies the field rules again.
- [x] The package stays dependency-free. No daemon, CLI, or wire change outside the payload contract.

Non-goals:
- The canonical form and action hash.
- Execution results, output chunks, errors, and execution states.
- Mio accepting remote requests.
- Policy, approval, and containment.

#### Test-first record

1. **Behaviour checked first** in a scratch program outside the repository:
   - the ADR example nonce is canonical base64url (16 bytes);
   - `time.Parse` silently accepts `.5` fractional seconds with the layout `2006-01-02T15:04:05Z`, so a round-trip comparison is needed;
   - the JSON decoder turns a lone surrogate into U+FFFD without error;
   - `null` into a `uint64` or `bool` leaves the value unchanged without error;
   - `1.0`, `1e3`, `-1`, `-0`, and values over 2^64 fail for `uint64`.
2. **Tests written first** (`packages/protocol/execution_request_test.go`):
   - `TestDecodeExecutionRequest_AcceptsADRExample` decodes ADR-007's example byte for byte into exact typed values.
   - `TestSecurity_DecodeExecutionRequestEnforcesFieldRules` covers every field rule with 134 rows, testing both sides of each bound and checking that each rejection names its field and returns no partial request.
   - `TestSecurity_DecodeExecutionRequestRejectsAmbiguousJSON` covers 23 cases:
     - size, trailing data, truncation, and a top-level array;
     - duplicate keys at the top level and nested;
     - `null` for a value, a nested value, `shell`, an argument, and an object;
     - invalid UTF-8;
     - five kinds of invalid `\u` escape, and an escaped NUL;
     - unknown fields, including one whose name holds an escape sequence that must not appear raw in the error.
   - `TestDecodeExecutionRequest_PreservesTransmittedText` covers valid escapes.
   - `TestDecodeExecutionRequest_RequiresEveryFieldAndNoOthers` removes each of the 27 fields, after first checking that this list matches the ADR example. It also adds an unknown field to each of the 5 objects and gives each object the wrong type.
   - Envelope tests check the message type, both directly and after `DecodeEnvelope`.
   - `ValidateAt` is tested at both edges of skew and expiry, with a zero clock, and on an invalid request.
   - `Validate` is tested on typed values: non-UTC or fractional times, NUL or invalid UTF-8, and `shell`.
   - `FuzzDecodeExecutionRequest` requires that every error wraps `ErrInvalidExecutionRequest`, and that every accepted request passes `Validate` and survives a round trip through an independent wire mirror.
3. **Build failure.** The new types and functions were undefined.
4. **Declarations-only skeleton.** It has the final types, constants, and error values; decoding always fails and the methods do nothing. Runtime red: 9 tests failed across 196 subtests. The fuzz seeds passed, as expected, because rejecting everything satisfies the fuzz property.

#### Implementation and verification record

- `packages/protocol/execution_request.go` decodes in three layers, all without new dependencies:
  1. **`checkRequestDocument`:**
     - the size bound, and exactly one JSON object;
     - valid UTF-8;
     - a raw scan that accepts a `\u` escape only if it decodes to a Unicode scalar value other than NUL, with each high surrogate followed by a low one;
     - a token walk that rejects duplicate keys and `null` at any depth, with nesting bounded at 64 levels. Errors quote field paths, because unknown keys are untrusted text.
  2. **`decodeRequestFields`:** `objectFields` requires exactly the listed keys for each of the 5 objects. A missing key "is required", and an unknown key is quoted. Each field is decoded into its typed target, and timestamps must round-trip through the layout `2006-01-02T15:04:05Z` exactly.
  3. **`ExecutionRequest.Validate`:** every ADR-007 field rule, also usable on requests built in memory. It additionally checks UTF-8, NUL, a UTC location, whole seconds, and years 0 to 9999.
- `Envelope.ExecutionRequest()` checks the message type before decoding. `ValidateAt` mirrors the envelope's: at most `AllowedClockSkew` (30 seconds) of future skew, strict expiry, and a zero clock is rejected.
- The bounds are exported constants.
- Tests added during implementation, all of which pass:
  - "is required" for each missing field;
  - quoted control characters in null and duplicate keys;
  - nesting beyond 64 levels;
  - a zero-fraction timestamp (`.000Z`) for `createdAt` and `expiresAt`;
  - uppercase `\u` escapes;
  - a year beyond 9999 on a typed value.
- Green: all tests pass under the race detector with no skips.
- Mutation testing: 55 deliberate, compile-safe mutations. 54 were caught by real test failures:
  - every document check, including size, UTF-8, escapes, NUL, surrogates, null, duplicates, depth, unknown and missing keys, and the skipping of simple escapes;
  - every field rule, at off-by-one bounds;
  - every time rule, including the round trip, UTC, nanoseconds, the year, order, lifetime, skew, strict expiry, and a zero clock;
  - the envelope type check;
  - `Validate` skipped, or a partial request returned;
  - a field decoded from the wrong key;
  - unquoted paths in three error kinds;
  - uppercase hexadecimal rejected.
- One mutation survived, as expected. Q3 weakened the top-level object check; it is equivalent, because `objectFields` still rejects a non-object with a `payload` error. The early check stays for its clearer message.
- Fuzzing: `FuzzDecodeExecutionRequest` ran for about 4 minutes in four sessions (60, 45, 40, and 90 seconds), about 9.6 million inputs in all, with no failure. Twice the exec count stopped for tens of seconds. A run with `-fuzzminimizetime 0` kept a steady 60,000 to 80,000 inputs per second, so those pauses were the fuzzer minimizing new inputs (up to a minute each by default), not a hang.
- Cost: decoding grows linearly with input size. In a throwaway timing test, since deleted, 4096 arguments took 2.2 ms, a 128 KiB argument 3.2 ms, and a 480 KB payload heavy with escapes 12 ms (rejected for its length).
- Coverage: protocol 93.9% (up from 89.4%). The only uncovered lines are defensive branches that cannot be reached once `json.Valid` has passed.
- Corrections:
  - The checkpoint commit `7c8bab6` left `docs/repository-tree.txt` without the two new files, because its first attempt stopped at the whitespace check before the tree step. This commit fixes it.
  - ADR-007's status line lost its trailing Markdown line-break spaces, which `git diff --cached --check` flagged, matching ADR-003 and ADR-006.
- Documentation: `docs/architecture/protocol.md`, the protocol package README, the plan's Messenger section, the README status line, the roadmap above, and the ADR index (ADR-007 accepted).
- Security review:
  - Decoding is bounded (512 KiB, 64 levels, linear time), never returns a partial request, and keeps decoded text equal to transmitted text, so the displayed, approved, and executed request cannot diverge through decoding.
  - Errors quote untrusted keys.
  - Nothing here authenticates, authorizes, or executes.
  - One gap is left as written in ADR-007: U+2028 and U+2029 (line and paragraph separators) are neither control nor format characters, so the executable, working directory, and reason accept them. Rejecting them is proposed as a small amendment.
  - No high-confidence exploitable issue was found.
- Local verification passed:
  - `make bootstrap`, `make lint`, `make test`, and `make security-test` (which now runs the two `TestSecurity_` request tests);
  - `go test -race -count=1 -timeout 120s ./...` and ten repeated race runs of the protocol package;
  - every package with `CGO_ENABLED=0`;
  - `git diff --cached --check`;
  - build, vet, and test-compile for darwin/arm64, darwin/amd64, windows/amd64, and linux/arm64.
- Follow-ups, not part of this increment:
  - the canonical request form and action hash, which need their own ADR;
  - execution results, output chunks, errors, and states;
  - Mio consuming requests, which needs the relay and verification first;
  - the U+2028 and U+2029 tightening.
- Pushes to `arena/c7a368c1-scopra`: the proposal `4716fab`, test checkpoint `7c8bab6` (red by design), then the implementation commit that adds this record. CI was not checked, at the maintainer's request.

### 2026-10-16 — Step 16: Reject line and paragraph separators in execution request text

- Selection: after Step 15 the default was to close the gap found in its security review, then to propose the canonical form and action hash. The maintainer replied "continue".
- The gap: U+2028 LINE SEPARATOR (category Zl) and U+2029 PARAGRAPH SEPARATOR (Zp) are neither control (Cc) nor format (Cf) characters, so ADR-007 as accepted lets them into `command.executable`, `command.workingDirectory`, and `reason`. Many renderers break lines at them, so they could fake a line in an approval display, as a newline could.

#### Acceptance checklist (agreed before implementation)

- [x] ADR-007 gains a dated amendment: those three fields also reject Zl and Zp. Arguments are unchanged: any character except NUL, and displays must escape.
- [x] Field-rule tests reject U+2028 and U+2029 in each of the three fields, and still accept both in arguments.
- [x] `validText` rejects Zl and Zp, and the error messages name line and paragraph separators.
- [x] The protocol README is updated, and full verification passes.

Non-goals:
- Other spaces (Zs, such as U+00A0), which add no confusion beyond the ordinary space that is already allowed.
- Confusable characters (homoglyphs), which are a display concern.
- The argument character set.
- Any other ADR-007 rule.

#### Test-first record

1. **Tests written first:** six field-rule rows reject `go\u2028`, `go\u2029`, `src\u2028`, `src\u2029`, and a reason holding either separator. The accepted-arguments row now includes both separators.
2. **Expected failure.** The tests compiled with no skeleton needed. Exactly the six new rejection rows failed (`error = <nil>`), and the arguments row passed, because arguments are unchanged.

#### Implementation and verification record

- `validText` now rejects `unicode.In(char, unicode.Cc, unicode.Cf, unicode.Zl, unicode.Zp)`; for Cc this is the same set that `unicode.IsControl` covered. The three error messages name line and paragraph separators. `validArguments` is unchanged.
- ADR-007 gained "Amendment — 2026-10-16: line and paragraph separators". The ADR index entry and the protocol README describe it, including the duty of approval displays to escape separators in arguments.
- Green: all tests pass under the race detector with no skips.
- Mutation testing: removing each of the four categories (Zl, Zp, Cc, Cf) was caught by `TestSecurity_DecodeExecutionRequestEnforcesFieldRules`.
- A 30-second fuzz run found no failure.
- Coverage: protocol 93.9%, unchanged.
- Local verification passed:
  - `make lint` and `make security-test`;
  - `go test -race -count=1 -timeout 120s ./...` and ten repeated race runs of the protocol package;
  - every package with `CGO_ENABLED=0`;
  - `git diff --cached --check`;
  - build, vet, and test-compile for darwin/arm64, darwin/amd64, windows/amd64, and linux/arm64.
  - The tree is unchanged.
- Pushes to `arena/c7a368c1-scopra`: test checkpoint `f5bebbb` (red by design), then the implementation commit that adds this record. CI was not checked, at the maintainer's request.

### 2026-10-16 — Step 17: Canonical execution request and action hash

- Selection: after Step 16 the default was to draft the canonical form and action hash as a proposal before writing any code, as was done for ADR-007. The maintainer replied "continue". Implementation starts only after confirmation; a "continue" accepts the proposal.
- Research:
  - The PRD's Approvals section and the plan's Milestone 3 approval tasks ("Freeze request and compute action hash", and "recompute locally, and consume atomically").
  - The threat model, and the risk register's "Canonical action hash, nonce, expiry, one-time use, local recomputation, and atomic consumption".
  - ADR-002's note that a separate canonical action representation is needed.
  - The ADR index's "canonical execution requests" follow-up.
- Test vectors: computed with an independent implementation (Python's `json` module with sorted keys, compact separators, and `ensure_ascii=False`, which equals JCS for this value domain). Vector 1's hash was checked again with `sha256sum` over the 28-byte label and the 619 canonical bytes.
- Decisions proposed in ADR-008 that most need a maintainer's view:
  - the hash covers the whole request, including the reason, the timestamps, and the IDs, so any change needs a new approval;
  - the canonical form is RFC 8785 JCS, computed from the decoded value rather than the transmitted bytes;
  - the hash is SHA-256 over the label `scopra/execution.request/v1` and a newline, then the canonical bytes, in lowercase hexadecimal;
  - Mio always recomputes the hash and never trusts a transmitted one;
  - the approval record's format is left to a later ADR.

- Confirmation: the maintainer replied "continue", accepting ADR-008 and the checklist below as written.

#### Acceptance checklist (agreed before implementation)

- [x] ADR-008 is accepted, with any changes the maintainer asks for, and indexed as accepted. The protocol docs describe it.
- [x] `ExecutionRequest.CanonicalJSON()` returns the JCS bytes of a valid request, or an error wrapping `ErrInvalidExecutionRequest`. It reproduces both test vectors byte for byte.
- [x] `ExecutionRequest.ActionHash()` returns the labelled SHA-256 in lowercase hexadecimal and reproduces both test vectors.
- [x] Property tests:
  - the canonical form decodes to an equal request, and canonicalizing it again gives the same bytes;
  - changing any one of the 23 leaf fields changes the hash;
  - payloads that differ only in whitespace, escaping, or key order share the hash;
  - the fuzz test also checks the canonical round trip for every accepted payload.
- [x] No new dependency, and no daemon, CLI, or wire change.

Non-goals:
- Approval records and signatures.
- Consumption and use counts.
- Policy versions.
- Display rendering.
- Other message types.

#### Test-first record

1. **Tests written first** (`packages/protocol/canonical_test.go`):
   - Both ADR-008 test vectors, copied verbatim: vector 1 byte for byte, and for vector 2 its length, the hex of its arguments member, and both action hashes.
   - `TestSecurity_CanonicalJSONEscapesExactlyAsRFC8785` covers every C0 control except NUL, the quote and the backslash, and characters that other encoders escape but JCS writes literally: DEL, `<`, `&`, `>`, `/`, U+2028, U+2029, and non-ASCII characters.
   - A round trip: the canonical form decodes to the same request, and canonicalizing it again gives the same bytes.
   - `TestSecurity_ActionHashChangesWithEveryField` covers 26 changes, all pairwise distinct: each of the 21 leaf fields that has another valid value, plus six argument variants (joined, containing an encoded `","`, an extra empty argument, reordered, none, and changed). `shell` and `network` have a single valid value in version 1, so the vectors pin their encoding instead.
   - Five transmission forms share vector 1's hash: as in ADR-007, compact, re-indented, keys reordered, and escaped.
   - Invalid requests get no canonical form and no hash.
   - `FuzzCanonicalExecutionRequest` checks every accepted payload against an independent RFC 8785 reference built from the generically decoded payload, then the round trip, stability, and the labelled hash.
2. **Build failure.** `CanonicalJSON` and `ActionHash` were undefined.
3. **Declarations-only skeleton.** Both methods fail. Runtime red: 7 tests failed across 12 subtests. `TestExecutionRequest_CanonicalJSONRejectsInvalidRequests` passed, as expected, because rejecting everything satisfies it.

#### Implementation and verification record

- `packages/protocol/canonical.go`, with no new dependency:
  - `CanonicalJSON` validates first, then builds each object from key/value members and sorts them by key at runtime. The code lists fields in ADR-007's order, which keeps it readable and still yields RFC 8785 order. Keys are ASCII, so byte order equals UTF-16 order.
  - `appendCanonicalString` escapes `"` and `\`, uses the five short forms, writes `\u00xx` with lowercase digits below U+0020, and copies every other byte literally. Bytes of multibyte characters are all at least 0x80.
  - Integers use `strconv.AppendUint` in base 10, booleans use `strconv.AppendBool`, and timestamps use ADR-007's layout.
  - `ActionHash` computes SHA-256 over the label `scopra/execution.request/v1` and a newline, then the canonical bytes, and returns lowercase hexadecimal.
- Both normative vectors, which were computed by the independent Python implementation, matched on the first green run.
- Fuzz restructuring during implementation:
  - The corpus of the generic canonical fuzz target held only 13 accepted requests out of 390 inputs, because coverage guidance favours rejection paths.
  - It was replaced by `FuzzCanonicalFieldValues`, which places fuzzed arguments, executable, working directory, reason, and timeout into a valid request. Of its 432 corpus inputs, 310 were valid requests, 213 held non-ASCII text, and 235 had arguments needing escapes.
  - `FuzzDecodeExecutionRequest` now also runs `checkCanonicalForm` on every payload it accepts.
- Field changes: of the 23 leaf fields, 21 have another valid value and are each covered by the hash-change test. `shell` and `network` have a single valid value in version 1, so no two valid requests differ in them, and the vectors pin their encoding.
- Green: all tests pass under the race detector with no skips.
- Mutation testing: 26 deliberate, compile-safe mutations, all caught by real test failures:
  - member sorting omitted or reversed;
  - the quote or backslash not escaped;
  - each of the five short forms replaced;
  - uppercase hexadecimal;
  - the control threshold off by one;
  - DEL or non-ASCII bytes escaped;
  - the label without its newline, or no label;
  - `Validate` skipped, or a canonical error ignored;
  - no argument separator;
  - the reason, executable, or workspace access replaced, or a member taken from the wrong field;
  - another time format, hexadecimal integers, an inverted boolean, or whitespace after commas.
- Fuzzing found no failures. The counted sessions cover more than 8 million inputs:
  - the generic canonical target, 30 seconds (2.18 million inputs);
  - the structured target, 45 seconds (1.27 million) and 60 seconds (1.62 million);
  - the decoder target with canonical checks, 60 seconds (3.22 million).
  - Two earlier sessions were not counted.
- Coverage: protocol 94.6% (up from 93.9%), with every function in `canonical.go` at 100%.
- Documentation: the protocol package README, `docs/architecture/protocol.md`, the plan's Messenger section, the README status line, the roadmap above, and the ADR index (ADR-008 accepted).
- Security review:
  - The action hash is computed only from validated, decoded values, so a sender cannot pair an approved hash with a different request.
  - JCS escaping is exact, so the TypeScript side can reproduce the same bytes with a standard library.
  - The label separates hash domains.
  - Nothing authenticates or authorizes yet: approval records, signatures, and atomic consumption remain future work.
  - No high-confidence exploitable issue was found.
- Local verification passed:
  - `make bootstrap`, `make lint`, `make test`, and `make security-test` (now including two canonical `TestSecurity_` tests);
  - `go test -race -count=1 -timeout 120s ./...` and ten repeated race runs of the protocol package;
  - every package with `CGO_ENABLED=0`;
  - `git diff --cached --check`;
  - build, vet, and test-compile for darwin/arm64, darwin/amd64, windows/amd64, and linux/arm64.
  - The tree is current.
- Follow-ups, not part of this increment:
  - approval records and their signatures, policy versions, and atomic consumption (a later ADR);
  - execution results, output chunks, errors, and states;
  - Mio consuming requests, which needs the relay and verification first.
- Pushes to `arena/c7a368c1-scopra`: the proposal `9bd038f`, test checkpoint `2e0ba82` (red by design), then the implementation commit that adds this record. CI was not checked, at the maintainer's request.

### 2026-10-16 — Step 18: Execution results (ADR-009, increment 1 of 3)

- Selection: after Step 17 the default was to draft ADR-009, covering execution states, results, output chunks, errors, and cancellation, as a proposal before any code. This finishes the plan's Messenger task "results, output chunks, errors, and execution states". The maintainer replied "continue". Implementation starts only after confirmation; a "continue" accepts the proposal.
- Research:
  - The PRD's Execution, Reliability, Performance, and Audit sections.
  - The plan's Execution and policy tasks ("sequence, limits, backpressure, and truncation"; "status, timing, exit code, cancellation, descendants, and cleanup"), its MCP tasks ("distinguish denial, approval, offline, timeout, and failure"), and its audit tasks ("Exclude tokens, secrets, and raw output from metadata").
  - The risk register's "Output leakage into cloud logs".
  - ADR-002's reserved `execution.result` and `execution.cancel`.
  - ADR-006's typed-error convention.
  - The example chunk's base64url value was computed and checked as canonical.
- Decisions proposed in ADR-009 that most need a maintainer's view:
  - four messages, one of them a terminal result sent exactly once, which requires two new message types in ADR-002's version 1 set;
  - seven terminal states, each with an exact key set; killed states carry no exit code;
  - output as base64url chunks of up to 64 KiB, numbered in one sequence across both streams, with truncation that does not stop the process and output kept out of metadata;
  - a closed set of error codes for senders, while receivers accept unknown well-formed codes;
  - no reply to requests that fail to decode or authenticate;
  - millisecond timestamps for timing;
  - retries and acknowledgements left to the relay ADR.
- Implementation is proposed in three increments: results first, then `execution.started` and `execution.output`, then `execution.cancel`.

- Confirmation: the maintainer replied "continue", accepting ADR-009 and the checklist below as written.

#### Acceptance checklist (agreed before implementation)

- [x] ADR-009 is accepted, with any changes the maintainer asks for, and indexed as accepted. The protocol docs describe it.
- [x] ADR-007's strict-document checks (size, UTF-8, a single object, escapes, duplicate keys, `null`, and exact key sets) are generalized for any payload, with no change in behaviour: every existing execution request test still passes unchanged.
- [x] `DecodeExecutionResult(payload)` strictly decodes `execution.result`: all seven states with exact key sets, every field rule, and errors that name the field. `ExecutionResult.Validate()` checks typed values.
- [x] `ExecutionResult.ValidateAgainst(request)` checks that the execution ID and action hash match the request, and that the output total is within the request's `limits.outputBytes`. `ExecutionResult.CanonicalJSON()` encodes a valid result with the ADR-008 writer, and the encoding decodes back to the same result.
- [x] Tests:
  - every state's example decodes;
  - table-driven rejections cover every field rule at both sides of its bounds, and wrong key sets for each state;
  - unknown well-formed error codes are accepted, and malformed codes rejected;
  - a fuzz test checks the round trip.

  There is no new dependency, and no daemon, CLI, or envelope change in this step.

Non-goals:
- `execution.started`, `execution.output`, and `execution.cancel`, which are increments 2 and 3.
- New envelope message types, which come in increment 2.
- Retries and acknowledgements.
- Mio producing results.
- Output storage and redaction.

#### Test-first record

1. **Independent reference:** the canonical forms of the `failed` and `rejected` examples were rendered with Python's `json` module, with sorted keys, compact separators, and `ensure_ascii=False`.
2. **Tests written first** (`packages/protocol/execution_result_test.go`):
   - one example per state, the `failed` example being ADR-009's own, each decoding to exact typed values;
   - canonical bytes for two states matching the Python rendering, and for all seven a round trip, stability, and equality with the independent RFC 8785 reference;
   - `TestSecurity_DecodeExecutionResultEnforcesFieldRules` with 60 rows at both sides of each bound. Besides the field bounds, they cover:
     - output consistency (chunks never more than bytes, bytes never more than chunks × 64 KiB, and no bytes without chunks);
     - the exit code rules for each state, including `-0` and `1.0`;
     - unknown well-formed error codes, and a known code used with another state, both accepted;
     - malformed error codes;
   - exact key sets: each state without each of its keys ("is required"), and with each key that belongs only to other states; missing, unknown, and wrongly typed `output` and `error` objects;
   - `TestSecurity_DecodeExecutionResultRejectsAmbiguousJSON`: 14 strictness cases that must wrap `ErrInvalidExecutionResult` and never `ErrInvalidExecutionRequest`, including a duplicate `state`;
   - `ValidateAgainst`:
     - every example matches the ADR-007 example request;
     - it fails for another execution ID, another action hash, a changed request, output over `limits.outputBytes`, an invalid result, and an invalid request (`ErrInvalidExecutionRequest`);
   - `Validate` on typed values: non-UTC times, sub-millisecond times, a year beyond 9999, and fields set for states that do not use them;
   - `FuzzDecodeExecutionResult`.
3. **Build failure.** The new types were undefined.
4. **Declarations-only skeleton.** It has the final types, state and error-code constants, and the new error value. Decoding and encoding fail, and both validators do nothing. Runtime red: 7 tests failed across 160 subtests. The fuzz seeds passed, as expected, because rejecting everything satisfies the fuzz property.

#### Implementation and verification record

- **Refactor, with no change in behaviour:** ADR-007's strict-document checks moved from `execution_request.go` into `packages/protocol/strict_json.go`. They are now methods on `payloadKind`, which carries the error value every failure wraps (`requestPayload` and `resultPayload`). Messages are byte for byte the same.
  - The moved methods are `checkDocument`, `scanValue`, `objectFields`, `fieldError`, and the new shared `decodeFields`. `validStringEscapes`, `hexEscape`, and `joinFieldPath` moved unchanged.
  - The test files were untouched at the time, and every existing request, envelope, and canonical test passed unchanged. The diff of `execution_request.go` contains only renamed call sites and removed code.
- **`packages/protocol/execution_result.go`:**
  - `DecodeExecutionResult` decodes `state` first, selects that state's exact key set from `resultKeys`, and then decodes the fields:
    - times must round-trip through `2006-01-02T15:04:05.000Z` exactly;
    - the exit code goes through `uint64`, so any sign, including `-0`, is rejected, and anything above 255 fails before conversion;
    - `output` and `error` are objects with exact keys.
  - `Validate` checks every field rule, plus three more:
    - fields that the state does not use must be zero;
    - output counts must be consistent with chunks of 1 to 65536 bytes (`chunks ≤ bytes ≤ chunks × 65536`), which follows from ADR-009's chunk rules;
    - error codes must match `[a-z][a-z_]*` within 32 bytes, with a known code accepted in any error state, because the ADR does not tie codes to states for receivers.
  - `ValidateAgainst(request)` validates both sides, then compares the execution ID, the action hash (ADR-008, recomputed), and the output total against `limits.outputBytes`.
  - `CanonicalJSON` encodes exactly the state's keys with ADR-008's writer.
- Tests added during implementation, all of which pass:
  - a typed `failed` result with exit code 256, which isolates `Validate`'s bound from the decoder's;
  - `startedAt` with a comma decimal separator (`01,250Z`). Go's `time.Parse` accepts it silently, so only the round-trip check rejects it. A scratch program confirmed this.
  - wrong JSON types for `executionId`, `actionHash`, `startedAt`, `endedAt`, `error.code`, and `error.message`, which cover the remaining reachable decoding branches.
- Green: all tests pass under the race detector with no skips.
- **Mutation testing incident:** the first mutation run was invalid. I started two harness batches in parallel, in the same tool call, and both edited and restored files in the same working tree. Builds mixed mutations from both batches, so the verdicts were wrong; one batch even reported a mutation as surviving that the tests catch.
  - Every file was checked afterwards for each mutation's replacement text: none was left behind, including from earlier harnesses.
  - All mutations were then rerun in one sequential batch. Harness batches must never run concurrently.
- **Clean mutation run:** 42 deliberate mutations, 41 caught after the comma row was added:
  - the key sets of two states;
  - action-hash length and case;
  - an unknown state;
  - sub-millisecond times, the time round trip, and the end before the start;
  - the exit code rules of each state;
  - fields set for states that do not use them;
  - the output count bound and both consistency bounds, including the chunk size;
  - three error-code rules and the message length;
  - each `ValidateAgainst` comparison, and the validation of both sides;
  - three canonical-encoding faults;
  - `Validate` skipped, a partial result returned, or a field decoded from the wrong key;
  - seven faults in the refactored strict-document code, including result errors wrapping the request error.
- One mutation survived, as expected. S12 loosened the decoder's exit-code bound above 255; it is equivalent, because `Validate` rejects any out-of-range code for both states that carry one.
- Fuzzing found no failures:
  - `FuzzDecodeExecutionResult` ran for 60, 20, and 45 seconds (1.24 million inputs in the 20-second session). Its corpus held 68 accepted results across four states, each passing the canonical round trip.
  - `FuzzDecodeExecutionRequest` ran for 45 seconds after the refactor.
- Coverage: protocol 95.9% (up from 94.6%). `decodeResultFields` is at 97.8%; the remaining branch cannot be reached once `checkDocument` has passed.
- Documentation: the protocol package README, `docs/architecture/protocol.md`, the plan's Messenger section, the README status line, the roadmap above, and the ADR index (ADR-009 accepted).
- Security review:
  - Results carry metadata and byte counts only, never output.
  - Errors quote untrusted keys, and error messages follow ADR-007's text rules.
  - `ValidateAgainst` lets Caloris reject a result that names another request.
  - Decoding is bounded and linear.
  - No high-confidence exploitable issue was found.
- Local verification passed:
  - `make bootstrap`, `make lint`, `make test`, and `make security-test` (now including the two result `TestSecurity_` tests);
  - `go test -race -count=1 -timeout 120s ./...` and ten repeated race runs of the protocol package;
  - every package with `CGO_ENABLED=0`;
  - `git diff --cached --check`;
  - build, vet, and test-compile for darwin/arm64, darwin/amd64, windows/amd64, and linux/arm64.
  - The tree is regenerated for `strict_json.go`.
- Follow-ups:
  - increment 2: `execution.started` and `execution.output`, the two new envelope message types, and envelope helpers for the daemon-to-cloud types;
  - increment 3: `execution.cancel`.
- Pushes to `arena/c7a368c1-scopra`: the proposal `b89978c`, test checkpoint `7d736ed` (red by design), then the implementation commit that adds this record. CI was not checked, at the maintainer's request.

### 2026-10-17 — Step 19: `execution.started` and `execution.output` (ADR-009, increment 2 of 3)

- Selection: ADR-009's implementation plan, which the maintainer accepted, makes this the second increment. After Step 18, "continue" was stated to start it, and the maintainer replied "Continue".
- Sandbox reset: before this step the sandbox reset again. The branch had been recreated at `main` with the work kept as uncommitted changes, and the Go toolchain was gone. A temporary index showed that the working tree was identical to `origin/arena/c7a368c1-scopra` (`d515017`), so the branch was restored with a mixed reset. The toolchain was rebuilt from the recorded, checksum-verified sources.

#### Acceptance checklist (agreed before implementation)

- [x] ADR-002 gains a dated amendment, and the envelope's closed set of message types gains `execution.started` and `execution.output` (ADR-009). `DecodeEnvelope` accepts both and still rejects unknown types.
- [x] `DecodeExecutionStarted` and `DecodeExecutionOutput` strictly decode their payloads, with exact keys and ADR-009's field rules. Their errors wrap `ErrInvalidExecutionStarted` and `ErrInvalidExecutionOutput` respectively and name the field. Both typed values get `Validate` and `CanonicalJSON`, and `ExecutionStarted.ValidateAgainst(request)` checks the execution ID and the action hash.
- [x] Output `data` must be canonical unpadded base64url of 1 to 65536 bytes. It decodes to bytes, and the canonical encoding reproduces it.
- [x] `Envelope.ExecutionResult()`, `Envelope.ExecutionStarted()`, and `Envelope.ExecutionOutput()` each decode only their own message type.
- [x] Tests:
  - examples decode, starting with ADR-009's own;
  - field rules are tested at both sides of their bounds, with exact key sets;
  - strictness errors wrap the right error value;
  - canonical bytes match an independent Python rendering;
  - envelope round trips work;
  - a fuzz test covers each decoder.

  The documentation is updated, and full verification passes.

Non-goals:
- `execution.cancel`, which is increment 3.
- Checking chunk sequences across messages or against a result, which needs receiver state.
- Retries and acknowledgements.
- Mio producing these messages.
- Output storage and redaction.

#### Test-first record

1. **Independent reference:** the canonical forms of both ADR-009 examples, and the bytes that the output example decodes to, were rendered with Python's `json` and `base64` modules.
2. **Tests written first:**
   - `execution_started_test.go`:
     - ADR-009's example decodes exactly, and its canonical bytes match the Python rendering;
     - 20 field-rule rows at both sides of each bound, including a comma decimal separator and wrong JSON types;
     - exact keys;
     - the shared strictness cases (`checkAmbiguousJSON`), which must wrap `ErrInvalidExecutionStarted` and no other payload's error value;
     - `ValidateAgainst`, `Validate` on typed values, and a fuzz test.
   - `execution_output_test.go`:
     - the example decodes to its exact bytes, and its canonical bytes match;
     - binary data round-trips exactly, including all 256 byte values and invalid UTF-8;
     - 28 field-rule rows, including `data` that is padded, uses the standard alphabet, has non-zero trailing bits, contains spaces or newlines, has an impossible length, is empty, or exceeds 64 KiB;
     - exact keys, shared strictness, and `Validate` on typed values;
     - a generic fuzz test, and a structured one over raw bytes.
   - `envelope_messages_test.go`:
     - the closed set holds exactly the seven version 1 types;
     - the three daemon-to-cloud messages round-trip through `DecodeEnvelope` and their helpers;
     - each of the four payload helpers rejects every other type without decoding the payload.
3. **Build failure.** The new types and constants were undefined.
4. **Declarations-only skeleton.** It has the final types, error values, stream constants, and the two message-type constants, which the envelope does not yet accept. The decoders and encoders fail, the validators do nothing, and the helpers return a zero value. Runtime red: 18 tests failed across 108 subtests, including the structured fuzz test. The two generic fuzz targets passed, as expected, because rejecting everything satisfies them.

#### Implementation and verification record

- **Envelope:**
  - `supportedMessageType` accepts `execution.started` and `execution.output`.
  - `Envelope.expectType` is the single type check behind all four payload helpers: `ExecutionRequest()`, which keeps its exact message, and the new `ExecutionResult()`, `ExecutionStarted()`, and `ExecutionOutput()`.
  - ADR-002 has a dated amendment, and the ADR index notes it.
- **Shared rules** (`strict_json.go`):
  - `startedPayload` and `outputPayload` kinds;
  - full-sentence rule texts for identifiers, action hashes, and millisecond times;
  - `millisecondTime`, which decodes and requires an exact round trip;
  - `validMillisecondTime` and `validActionHash`, moved from `execution_result.go`.

  The result decoder now uses them with the same messages, and its unchanged tests guard this refactor.
- **`execution_started.go`:** a strict decoder with exact keys, plus `Validate`, `ValidateAgainst` (execution ID, then the action hash recomputed with ADR-008), and `CanonicalJSON`.
- **`execution_output.go`:**
  - a strict decoder in which `data` must re-encode exactly (`decodeCanonicalBase64URL`). Go's base64 decoder silently skips line breaks and, by default, accepts non-zero trailing bits, so the re-encoding check is essential.
  - `Validate` alone bounds the size to 1 to 65536 bytes;
  - `CanonicalJSON` writes `data` as unpadded base64url.
- **Simplification found by mutation testing:** the base64url helper first had its own empty and size checks, and then an early length check. Each duplicated a rule that `Validate` applies on every decode, so each produced an equivalent mutant. They were removed, and the size rule now lives only in `Validate`, where the decode-path tests reach it directly.
- Green: all tests pass under the race detector with no skips.
- **Mutation testing**, run in one sequential batch: 25 deliberate mutations, all caught:
  - the closed set, a misspelled type constant, and the shared type check;
  - each field rule of both payloads;
  - `Validate` skipped, or a partial value returned;
  - a canonical key renamed;
  - both `ValidateAgainst` comparisons, and request validation skipped;
  - a key-set omission;
  - the time round trip, sub-millisecond and non-UTC times, and the action-hash length;
  - the `data` re-encoding check;
  - the sequence bounds, the stream set, and both typed size bounds;
  - padded `data` or a hexadecimal sequence in the encoding;
  - a helper checking the wrong type.

  The two redundant-guard mutants described above were removed together with the guards.
- Fuzzing: 30-second sessions, run one after another, with no failures (about 6.4 million inputs in total):
  - `FuzzDecodeExecutionStarted`: 1.89 million inputs;
  - `FuzzDecodeExecutionOutput`: 1.85 million;
  - `FuzzExecutionOutputValues`: 0.70 million;
  - `FuzzDecodeExecutionResult`, after the shared-parser refactor: 1.97 million.
- Coverage: protocol 96.4% (up from 95.9%), with every new function at 100%. The other packages are unchanged.
- Documentation: the protocol package README, `docs/architecture/protocol.md`, the plan's Messenger section, the README status line, the roadmap above, the ADR-002 amendment, and the ADR index.
- Security review:
  - Output bytes are kept exact and never interpreted.
  - The README and the type documentation say that output must never be logged.
  - Helpers refuse to decode a payload under another type's rules.
  - Every payload stays within the 512 KiB envelope bound, and a maximum chunk encodes to 87,382 characters.
  - No high-confidence exploitable issue was found.
- Local verification passed on the rebuilt toolchain:
  - `make lint` and `make security-test`;
  - `go test -race -count=1 -timeout 120s ./...` and ten repeated race runs of the protocol package;
  - every package with `CGO_ENABLED=0`;
  - `git diff --cached --check`;
  - build, vet, and test-compile for darwin/arm64, darwin/amd64, windows/amd64, and linux/arm64.
  - The tree is current.
- Follow-up: increment 3, `execution.cancel`, which closes the plan's Messenger task.
- Pushes to `arena/c7a368c1-scopra`: test checkpoint `f78e8aa` (red by design), then the implementation commit that adds this record. CI was not checked, at the maintainer's request.

### 2026-10-17 — Step 20: `execution.cancel` (ADR-009, increment 3 of 3)

- Selection: ADR-009's implementation plan, which the maintainer accepted, makes this the last increment. After Step 19, "continue" was stated to start it, and the maintainer replied "Continue". With it, every part of the plan's first Messenger task has been defined and implemented.

#### Acceptance checklist (agreed before implementation)

- [x] `DecodeExecutionCancel` strictly decodes `execution.cancel` with exact keys (including the `principals` object) and ADR-009's field rules: identifiers, action hash, and `reason` under ADR-007's text rules. Errors wrap `ErrInvalidExecutionCancel` and name the field. The typed value gets `Validate` and `CanonicalJSON`.
- [x] `ExecutionCancel.ValidateAgainst(request)` checks the execution ID and the action hash, which is how Mio ignores a cancel that does not match the running execution. Principals are not compared, because the canceller may differ from the requester.
- [x] `Envelope.ExecutionCancel()` decodes only `execution.cancel`, like the other helpers.
- [x] Tests:
  - ADR-009's example decodes, and its canonical bytes match an independent Python rendering;
  - field rules are tested at both sides of their bounds, with exact keys at both levels;
  - shared strictness errors wrap only `ErrInvalidExecutionCancel`;
  - the envelope round trip and the helper table include the new type;
  - a fuzz test covers the decoder.

  The documentation is updated: ADR-009 is marked fully implemented, and the plan's first Messenger task is checked. Full verification passes.

Non-goals:
- Mio acting on a cancel: process-tree termination, and the `cancelled` result.
- Authorizing who may cancel, which is cloud policy.
- Retries and acknowledgements.
- The plan's second Messenger task, compatibility tests across protocol versions.

#### Test-first record

1. **Independent reference:** the canonical form of ADR-009's cancel example was rendered with Python's `json` module.
2. **Tests written first** (`execution_cancel_test.go`):
   - the example decodes exactly, and its canonical bytes match the Python rendering;
   - 29 field-rule rows at both sides of each bound, covering identifiers, the action hash, all three principals, and the reason (newline, bidirectional controls, a line separator, and length) with wrong JSON types;
   - exact keys at both levels, and `principals` that is not an object;
   - the shared strictness cases, plus a nested `null` and a nested duplicate in `principals`;
   - `ValidateAgainst`: a cancel by *other* principals is still accepted (ADR-009), and another execution, another or changed action hash, an invalid cancel, or an invalid request is not;
   - `Validate` on typed values, and a fuzz test.
   - The shared tests were updated: the envelope round trip now includes `execution.cancel` (and the test was renamed `TestDecodeEnvelope_CarriesTheExecutionMessages`), the helper table includes `ExecutionCancel()`, and every shared strictness check must not wrap `ErrInvalidExecutionCancel` for other payloads.
3. **Build failure.** `ExecutionCancel` and `DecodeExecutionCancel` were undefined.
4. **Declarations-only skeleton.** Runtime red: 9 tests failed across 64 subtests. The fuzz seeds passed, as expected, because rejecting everything satisfies the fuzz property.

#### Implementation and verification record

- **`packages/protocol/execution_cancel.go`:**
  - a strict decoder with exact keys at both levels, using the shared `cancelPayload` kind;
  - `Validate`, covering four identifiers, the action hash, and the reason under ADR-007's text rules;
  - `ValidateAgainst`, covering the execution ID and the action hash recomputed with ADR-008, but deliberately not the principals;
  - `CanonicalJSON`, and `Envelope.ExecutionCancel()` through the shared `expectType`.
- **Small shared refactors, guarded by unchanged tests:**
  - `untrustedTextRule` now holds the rule text for the request's reason, the result's error message, and the cancel's reason, with the same wording as before.
  - `canonicalPrincipals` builds the principals object for requests and cancels. ADR-008's byte-exact vectors still pass.
- Green: all tests pass under the race detector with no skips.
- Mutation testing, run in one sequential batch: 17 deliberate mutations, all caught:
  - the identifier loop disabled, or the client ID dropped from it;
  - the action hash or the reason unchecked, or the reason bound off by one;
  - `Validate` skipped, or a partial value returned;
  - the principals key set without `clientId`;
  - swapped fields in the shared principals encoding, which ADR-008's vector 1 catches;
  - the reason encoded from the wrong field;
  - either comparison removed;
  - a principals comparison added, which the cancel-by-other-principals case catches;
  - the request or the cancel left unvalidated;
  - the helper checking the wrong type;
  - cancel errors wrapping the output error.
- Fuzzing, run one after another, found no failures:
  - `FuzzDecodeExecutionCancel`: 45 seconds, 2.92 million inputs. Its corpus held 19 accepted cancels with 16 distinct reasons, all passing the canonical round trip.
  - `FuzzCanonicalFieldValues`, after the shared-principals refactor: 20 seconds, 0.52 million inputs.
- Coverage: protocol 96.6% (up from 96.4%), with every new function at 100%.
- Documentation:
  - ADR-009 has an implementation status, and the ADR index says it is fully implemented.
  - The plan's Messenger section lists the increments and **checks the first task**: versions, IDs, the canonical request, results, output chunks, errors, and states are all defined and implemented. The second task, compatibility tests across protocol versions, stays open.
  - Also updated: the protocol package README, `docs/architecture/protocol.md`, the README status line, and the roadmap above.
- Security review:
  - A cancel identifies who asked, so the action is attributable.
  - It names the action by its recomputed action hash.
  - It authorizes nothing by itself.
  - Its reason is untrusted text under ADR-007's rules.
  - No high-confidence exploitable issue was found.
- Local verification passed:
  - `make lint` and `make security-test`;
  - `go test -race -count=1 -timeout 120s ./...` and ten repeated race runs of the protocol package;
  - every package with `CGO_ENABLED=0`;
  - `git diff --cached --check`;
  - build, vet, and test-compile for darwin/arm64, darwin/amd64, windows/amd64, and linux/arm64.
  - The tree is current.
- Pushes to `arena/c7a368c1-scopra`: test checkpoint `ac4bbff` (red by design), then the implementation commit that adds this record. CI was not checked, at the maintainer's request.

### 2026-10-17 — Step 21: Messenger v1 reference payloads in `packages/test-fixtures`

- Selection: after Step 20 the default was (a), shared reference payloads for every message type with their canonical bytes and hashes, checked by the Go tests. They guard the wire format against accidental change and give the future TypeScript side the same files. The maintainer replied "continue".
- No ADR: no protocol rule changes. The fixtures record the existing contract of ADR-002 and ADR-007 to ADR-009.

#### Acceptance checklist (agreed before implementation)

- [x] `packages/test-fixtures/messenger/v1/` holds a manifest and payload files:
  - valid fixtures: every ADR example, both ADR-008 vectors, all seven result states, and a binary output chunk, each with its canonical form and, for requests, its action hash;
  - invalid fixtures covering every strictness class and key rules for each of the five payload types.
- [x] Expected values come from independent sources, never from the Go code: the ADR texts, ADR-008's vectors, and Python's `json` and `base64` rendering.
- [x] `packages/protocol/fixtures_test.go` checks three things:
  - the manifest is complete: names are unique, every file is referenced exactly once, all five payload types have valid and invalid fixtures, and each entry has the right shape;
  - every valid payload decodes to exactly its canonical form, and to its action hash where it has one, and that form is stable;
  - every invalid payload is rejected with its type's error value, naming the field.
- [x] `packages/test-fixtures/README.md` documents the format, the provenance, and the change rule: a changed canonical form or hash is a wire-format change, which needs an ADR amendment before the alpha freeze and a protocol-version decision after it.
- [x] No production code changes, and full verification passes.

Non-goals:
- Envelope fixtures, whose freshness depends on the time and whose authentication proof does not exist yet.
- A TypeScript consumer.
- Tests across protocol versions, because only version 1 exists. The plan's second Messenger task stays open.
- A committed generator script.

#### Test-first record

1. **Contract tests written first** (`packages/protocol/fixtures_test.go`). They define the manifest format: `version`, `protocolVersion`, and fixtures with `name`, `messageType`, `payload`, and then either `canonical` (plus `actionHash` for requests) or `invalidField`. They also define what each fixture must prove:
   - `TestFixtures_ManifestIsComplete` checks unique names and payloads, a known message type for each entry, the right shape and directory for valid and invalid entries, at least one valid and one invalid fixture for each of the five payload types, no unlisted files, and no missing ones.
   - `TestFixtures_ValidPayloadsDecodeToTheirCanonicalForm` requires the exact canonical bytes and action hash, equality with the independent RFC 8785 reference, and a stable canonical form.
   - `TestSecurity_FixturesRejectInvalidPayloads` requires the type's own error value, naming the field.
2. **Expected failure.** This step adds tests over existing code, so no production skeleton applies. The red phase is the missing fixture set: all three tests failed with "read the fixture manifest: … no such file or directory".

#### Implementation and verification record

- **Fixtures** (`packages/test-fixtures/messenger/v1/`): `manifest.json`, 14 valid payloads, and 27 invalid ones.
  - **Valid:** the ADR-007 example; both ADR-008 vectors; a request with ESC, VT, and US in its arguments; all seven result states, including ADR-009's `failed` example; the ADR-009 `started`, `output`, and `cancel` examples; and an output chunk holding all 256 byte values.
  - **Invalid, requests:** `null`, a duplicate key, an unknown field, a missing field, a raw `0xFF` byte, a lone surrogate, an escaped NUL, `shell: true`, a working directory that escapes with `..`, a bidirectional control in the executable, `network: host`, a lifetime over one hour, and trailing data.
  - **Invalid, results:** an unknown state, an exit code on a `timed_out` result, inconsistent output counts, a comma in a time, and an exit code of `-0`.
  - **Invalid, start notices:** second precision, and an uppercase action hash.
  - **Invalid, output chunks:** padded data, a line break in the data, sequence 0, and stream `stdin`.
  - **Invalid, cancels:** a missing reason, `principals` that is not an object, and a nested `null`.
- **Provenance:**
  - The fixtures were generated by a throwaway Python script outside the repository.
  - ADR examples are extracted from the ADR Markdown, and canonical forms come from Python's JCS-equivalent `json.dumps`.
  - Action hashes come from `hashlib`. The script asserts both ADR-008 vectors, vector 2's 709-byte length, and the raw `0xFF` byte.
  - None of the expected values comes from the Go code.
- **Gap found during implementation:** a fault check showed that an uppercase-hex canonical writer went unnoticed, because the only escaped control character, `\u0001`, has no hex letters. The control-character request fixture (`\u001b`, `\u000b`, `\u001f`) closes that gap for every implementation that uses the fixtures.
- **Fault checks**, run one at a time with restore: all 9 caught:
  - a corrupted canonical form;
  - an invalid fixture naming the wrong field;
  - a duplicate fixture name;
  - an unknown message type;
  - a payload type left without invalid fixtures, with its files unlisted;
  - an unlisted file;
  - a missing listed file;
  - an uppercase-hex canonical writer;
  - the time round trip removed from the production code.
- `packages/test-fixtures/.gitattributes` sets `* -text`, so no checkout converts line endings, and `git check-attr` confirms it.
- `packages/test-fixtures/README.md` documents the format, the conformance conditions, the provenance, and the change rule. The protocol package README and the plan's Messenger section point to the fixtures. The plan's second task stays open until a second protocol version exists.
- There is no production code change, so coverage is unchanged (protocol 96.6%).
- Local verification passed:
  - `make bootstrap`, `make lint`, `make test`, and `make security-test` (including `TestSecurity_FixturesRejectInvalidPayloads`);
  - `go test -race -count=1 -timeout 120s ./...` and ten repeated race runs of the protocol package;
  - every package with `CGO_ENABLED=0`;
  - `git diff --cached --check`, which accepts the raw `0xFF` fixture;
  - build, vet, and test-compile for darwin/arm64, darwin/amd64, windows/amd64, and linux/arm64.
  - The tree is regenerated for the 43 new files.
- Pushes to `arena/c7a368c1-scopra`: test checkpoint `e028e5e` (red by design), then the commit that adds the fixtures and this record. CI was not checked, at the maintainer's request.

### 2026-10-17 — Step 22: Hollow `podman run` argument builder (ADR-010, increment 1 of 4)

- Selection: after Step 21 the default was (a), proposing the rootless Podman sandbox as ADR-010 before any code. The maintainer replied "continue". Implementation starts only after confirmation; a "continue" accepts the proposal.
- Research:
  - the plan's Workspace and containment, Execution and policy, adversarial-test, and exit-gate tasks;
  - the threat model;
  - the risk register (R-01 and R-09);
  - ADR-004, ADR-007, and ADR-009.
- Podman facts were checked against its documentation, its source code at v6.1.4 (released 2026-10-12, read through the GitHub API), and its release notes. No Podman exists in this sandbox.
  - The default `journald` log driver would store output, so ADR-010 uses `passthrough`.
  - Swap defaults to twice `--memory`, so ADR-010 sets `--memory-swap` equal to `--memory`.
  - Once `--entrypoint` is set, the image's `CMD` is ignored (`makeCommand`), which keeps the argument vector exact.
  - `--mount` is parsed as CSV with `encoding/csv` (`FindMountType`), and options split at their first `=`.
  - `relabel` and `U` rewrite host files.
  - `passthrough` and `--unsetenv-all` both arrived in Podman 4.0.0.
  - The documentation says `--memory-swap` "must be larger" than `--memory`, while the runtime accepts an equal value meaning no swap. Increment 3 must verify this on a real host.
- Decisions proposed in ADR-010 that most need a maintainer's view:
  - the image is a required setting pinned by digest, with no default, so nothing runs until it is chosen;
  - SELinux labels are disabled for the container rather than the workspace relabelled;
  - `/tmp` allows execution;
  - the minimum is Podman 4.0, which excludes Ubuntu 22.04;
  - increments 3 and 4 need a host with rootless Podman.

- Confirmation: the maintainer replied "continue", accepting ADR-010 and the checklist below as written.

#### Acceptance checklist (agreed before implementation)

- [x] ADR-010 is accepted, with any changes the maintainer asks for, and indexed as accepted.
- [x] `internal/hollow` builds the exact `podman run` argument vector of ADR-010 from a validated request, a canonical workspace path, and a pinned image. Tests pin the complete vector for the ADR-007 example and for ADR-008's vector 2.
- [x] It rejects, with typed errors, the following:
  - an invalid request;
  - a workspace path that is relative, not UTF-8, or contains control characters;
  - an image reference without a digest.

  It quotes the mount source as CSV, which is tested with paths containing `,`, `"`, `:`, `=`, and spaces, and checked against an `encoding/csv` round trip.
- [x] Property tests:
  - exactly one bind mount, the workspace, read-only unless access is read-write;
  - no forbidden argument ever appears;
  - everything after the image is the request's executable and arguments, verbatim, including arguments that look like Podman options;
  - limits map exactly: millicores to an exact decimal CPU count, bytes with no swap, PIDs, and the timeout plus 10 seconds.

  A fuzz test over request text and paths keeps that shape.
- [x] No process is started, there is no new dependency, and full verification passes.

Non-goals:
- Preflight checks (increment 2).
- Running Podman, streaming output, cancellation, and cleanup (increment 3).
- Adversarial tests (increment 4).
- Choosing the image.
- A Mio setting for the image, which comes with the runner.

#### Test-first record

1. **Tests written first** (`internal/hollow/podman_test.go`). The requests are decoded from the shared reference payloads in `packages/test-fixtures`, so the sandbox and the protocol are tested on the same inputs.
   - Complete, hand-written argument vectors for the ADR-007 example and ADR-008's vector 2.
   - `TestSecurity_PodmanRunArgsRejectsUnsafeInputs`, with 22 rejections:
     - invalid requests;
     - workspace paths that are empty, relative, the root, unclean, with a trailing slash, containing NUL, newline, carriage return, or DEL, not UTF-8, or over 4095 bytes;
     - images without a digest, with a short, long, or uppercase digest, another algorithm, an option-like name, a space, or no name.

     Three pinned images are accepted, including a registry port and a tag.
   - CSV quoting of mount sources containing `,`, `"`, `:`, `=`, spaces, non-ASCII characters, and an injection attempt (`x",dst=/etc,ro=false,"src=/`).
   - The exact limit mapping: nine CPU values from 0.001 to 1000, and memory, swap, PIDs, tmpfs, and timeout at their bounds.
   - Option-like request arguments (`--privileged`, `-v /:/host`, and the image name itself) must stay after the image.
   - `FuzzPodmanRunArgs`, over the executable, an argument, the working directory, the workspace path, and the access mode.
   - A shared `checkInvariants`, used by every test that gets a vector:
     - the vector is `run`, then options that all start with `--`, then the image, then exactly the arguments;
     - no forbidden option appears, and each required option appears once;
     - the security options are exactly `no-new-privileges` and `label=disable`, and the environment is exactly the fixed `PATH` and `HOME`;
     - the mount, parsed as Podman parses it (`encoding/csv`, then splitting at the first `=`), is exactly type, source, destination, and access;
     - the JSON entrypoint decodes to exactly the executable.
   - `make security-test` now includes `./internal/hollow`.
2. **Build failure.** `RunSpec`, `PodmanRunArgs`, and the error values were undefined.
3. **Declarations-only skeleton.** It rejects everything. Runtime red: all 7 tests failed across 24 subtests, including the fuzz seeds, because the skeleton's error is neither a request nor a workspace error.

#### Implementation and verification record

- **`internal/hollow/podman.go`:** `PodmanRunArgs(RunSpec)` emits ADR-010's table in order.
  - Validation comes first:
    - the request, through `ActionHash`, whose hash also names and labels the container;
    - the workspace path, which must be absolute, not the root, clean, valid UTF-8 with no control characters, and at most 4095 bytes;
    - the image, which needs a name starting with a letter or digit, only reference characters, and `@sha256:` with 64 lowercase hexadecimal characters.
  - Errors quote paths and images with `%q`.
  - The mount source is a CSV-quoted `src=` field with inner quotes doubled.
  - CPUs are an exact decimal built with integer arithmetic.
  - The entrypoint is a JSON array without HTML escaping.
  - The request's arguments follow the image verbatim.
- **Two rules go beyond ADR-010's text, as defence in depth that ADR-004 already guarantees for registered workspaces:** the path must be clean, and it must not be the filesystem root.
- **Test-data correction during implementation:** the injection-attempt path first ended in `src=/`, a trailing slash, so it was correctly rejected as unclean before quoting mattered. It now ends in `/y`, and the test checks that the attempt stays a single `src` field when parsed as Podman parses it.
- Green: all tests pass under the race detector with no skips. `make security-test` now covers `./internal/hollow`.
- Mutation testing, run in one sequential batch: 34 deliberate mutations, all caught:
  - every isolation option: network, capabilities, `no-new-privileges`, the SELinux option, the read-only root, logging, pulls, `/tmp` execution, the environment, `HOME`, init, and the user namespace;
  - the mount quoting and quote doubling, and the access mode inverted or forced;
  - the working-directory join;
  - the swap limit, the CPU divisor and decimal trimming, the timeout backstop, and PIDs;
  - the container name length;
  - five image rules and five path rules;
  - dropped arguments;
  - an invalid request accepted, which makes the builder panic.
- Fuzzing: `FuzzPodmanRunArgs` ran for 60 seconds, about 549,000 inputs, with no failure. Its corpus held 37 accepted inputs, 13 of them with `,`, `"`, `=`, `:`, or a space in the workspace path, each checked against Podman's CSV parsing.
- Coverage: `internal/hollow` 95.7%. The only uncovered lines are the two encode-error returns, which cannot fail for a string slice written to memory. The other packages are unchanged.
- Documentation: the ADR-010 implementation status, the ADR index, the plan, the system overview, the README status line, and the roadmap above. Each says that nothing runs Podman yet, and the plan's containment tasks stay unchecked.
- Security review:
  - The builder is a pure function: it starts no process and reads no environment.
  - Request text can reach Podman only after the image (arguments) or inside a JSON array (the executable). Option names never come from the request.
  - The mount source cannot inject options.
  - No high-confidence exploitable issue was found.
  - Every Podman behaviour that the vector relies on still has to be confirmed on a real rootless host in increment 3.
- Local verification passed:
  - `make lint` and `make security-test`;
  - `go test -race -count=1 -timeout 120s ./...` (8 packages) and ten repeated race runs of `internal/hollow`;
  - every package with `CGO_ENABLED=0`;
  - `git diff --cached --check`;
  - build, vet, and test-compile for darwin/arm64, darwin/amd64, windows/amd64, and linux/arm64.
  - The tree is current.
- Pushes to `arena/c7a368c1-scopra`: the proposal `a6d6d7b`, test checkpoint `1cf9fc6` (red by design), then the implementation commit that adds this record. CI was not checked, at the maintainer's request.

### 2026-10-17 — Step 23: Hollow preflight checks (ADR-010, increment 2 of 4)

- Selection: after Step 22 the default was (a), ADR-010's increment 2. The maintainer replied "continue".
- Research, from Podman's source code and documentation through the GitHub API:
  - At both v4.0.0 and v6.1.4, `podman info --format json` reports the needed values as `host.cgroupVersion`, `host.cgroupControllers`, `host.security.rootless`, and `version.Version`. The last key keeps Go's capital V because its struct field has no JSON tag.
  - The JSON example in `docs/source/markdown/podman-info.1.md` (v6.1.4) shows Podman 4.0.0, rootless, on cgroups v2, but with `cgroupControllers: []`, meaning no controllers are delegated to the user.
  - Podman's own end-to-end test skips its controller check when rootless, because it is "hard to tell which controllers are going to be enabled for rootless".
  - `podman info` includes the version, so one command covers every requirement.

#### Acceptance checklist (agreed before implementation)

- [x] `internal/hollow` parses `podman info --format json` output: bounded to 1 MiB, ignoring fields it does not need so that newer Podman versions can add some, and failing closed on missing or mistyped required fields, non-JSON input, or trailing data.
- [x] A check requires Podman 4.0 or later (compared numerically), rootless operation, cgroups v2, and the `cpu`, `memory`, and `pids` controllers. Every failure wraps a new `ErrContainmentUnavailable` (ADR-009's `containment_unavailable`) and names every unmet requirement.
- [x] Tests:
  - the verbatim documentation sample parses to its exact values and is refused for its missing delegation;
  - a delegating variant derived from it is accepted;
  - a table covers each requirement at its edge (3.4.4 and 4.0.0, 10.0.0 compared numerically, suffixes, unrecognized versions, root, cgroups v1, each missing controller, and all failures at once);
  - fail-closed parsing cases;
  - a fuzz test.
- [x] The sample's provenance is documented in `testdata`. The documentation and ADR-010's status are updated, and full verification passes.

Non-goals:
- Running `podman info` (the runner, increment 3).
- Checking that the image exists locally, and the Mio setting for the image.
- Systems without Podman.

#### Test-first record

1. **Recorded sample:** `internal/hollow/testdata/podman-info-v4.0.0-docs.json` is Podman's documentation example, copied verbatim. `testdata/README.md` records its provenance: repository, file, tag, lines, licence, and SHA-256.
2. **Tests written first** (`internal/hollow/preflight_test.go`):
   - the verbatim sample parses to its exact values (`4.0.0`, rootless, `v2`, and an empty controller list) and is refused for its missing delegation;
   - a variant derived from it with the controllers that a delegating systemd reports is accepted;
   - `TestPodmanInfo_Check`, with 21 rows:
     - accepted: the minimum version, the current version, `10.0.0` compared numerically, `-rc1` and `-dev` suffixes, major and minor only, and exactly the three needed controllers;
     - refused: 3.4.4 and 3.99.0, an empty version, a leading `v`, a major version alone, an overlong major version, an escape sequence in the version that must be quoted, root, cgroups v1, each missing controller, controller names that differ only in case or spacing, and all failures at once;
   - `TestSecurity_ParsePodmanInfoFailsClosed`, with 17 cases: empty input, non-JSON, `null`, an array, an empty object, trailing data, more than 1 MiB, and each required field missing or mistyped;
   - `FuzzParsePodmanInfo`: every error wraps `ErrContainmentUnavailable`, and any accepted host is rootless, on `v2`, with all three controllers, and an all-digit major version of at least 4.
   - While writing the tests, I tightened the fuzz invariant. It first rejected only single-digit majors below 4, which would have let an implementation that accepted `03.0` through.
3. **Build failure.** `ParsePodmanInfo`, `PodmanInfo`, and `ErrContainmentUnavailable` were undefined.
4. **Declarations-only skeleton.** Parsing fails, and the check accepts everything. Runtime red: 4 tests failed across 31 subtests. The fuzz seeds passed, as expected, because every parse error wraps the error value.

#### Implementation and verification record

- **`internal/hollow/preflight.go`:**
  - `ParsePodmanInfo` decodes the required fields into pointers, so a missing field is distinguishable from `false`, `""`, or `[]`. It ignores unknown fields, so newer Podman versions can add some. It rejects output over 1 MiB, anything other than one JSON object, trailing data, and a missing required field. Every error wraps the new `ErrContainmentUnavailable`.
  - `PodmanInfo.Check` collects every unmet requirement and reports them together, quoting untrusted text with `%q`.
  - `atLeastVersion` compares major and minor as integers of at most 6 digits each, and ignores further components and suffixes.
- **Found by the tests during implementation:** a version with an escape sequence after its numbers (`4.0.0\x1b[2J`) was first accepted, because suffixes were ignored. Versions are now limited to letters, digits, and `.-+~_`, so anything else fails closed.
- **Found by mutation testing:**
  - With a minimum of 4.0, loosening the major comparison was equivalent, because "minor of at least 0" always holds. `TestAtLeastVersion` now tests the helper with a 4.4 minimum, as a future minimum would use.
  - A redundant missing-dot check was removed, because an empty minor version already fails.
- Green: all tests pass under the race detector with no skips.
- Mutation testing, run in sequential batches: 17 deliberate mutations, all caught:
  - the size bound and the trailing-data check;
  - all four presence checks, whose removal makes the parser panic;
  - the version comparison and the minimum major version;
  - root and cgroups v1 accepted;
  - a missing `pids` requirement, and loose controller matching;
  - the version character set, the digit bound, and minor truncation;
  - reporting only the first problem, and the format of the controller list.
- Fuzzing: `FuzzParsePodmanInfo` ran for 60 seconds, about 3.09 million inputs, with no failure.
- Coverage: `internal/hollow` 98.0% (up from 95.7%), with every preflight function at 100%.
- Documentation: the ADR-010 implementation status, the ADR index, the plan, the README status line, and the roadmap above. Each says that nothing runs Podman yet.
- Security review:
  - The check fails closed on anything it cannot establish, and never trusts a default value for a missing field.
  - Podman's output is bounded, and text from it is quoted in errors.
  - The real trust question, the `podman` binary on `PATH`, belongs to the runner (increment 3).
  - No high-confidence exploitable issue was found.
- Local verification passed:
  - `make lint` and `make security-test`;
  - `go test -race -count=1 -timeout 120s ./...` and ten repeated race runs of `internal/hollow`;
  - every package with `CGO_ENABLED=0`;
  - `git diff --cached --check`;
  - build, vet, and test-compile for darwin/arm64, darwin/amd64, windows/amd64, and linux/arm64.
  - The tree is current.
- Pushes to `arena/c7a368c1-scopra`: test checkpoint `6f1064b` (red by design), then the implementation commit that adds this record. CI was not checked, at the maintainer's request.

### 2026-10-17 — Step 24: the `scoprad --sandbox-image` setting (ADR-010 amendment, increment 3a-1)

- Selection: after Step 23 the default was (a). The runner splits into parts testable here against a fake Podman, plus verification on a real host, starting with a short proposal for how Mio learns the image. The maintainer replied "continue". Implementation starts only after confirmation; a "continue" accepts the proposal.
- Research, from Podman v6.1.4's documentation and source code:
  - `podman run` exits with status 125 for Podman's own errors, 126 when the command cannot be invoked, 127 when it is not found, and otherwise with the command's exit code. A command that itself exits with 125 to 127 is therefore indistinguishable by status alone.
  - `podman inspect` reports `State.Status`, `Running`, `OOMKilled`, `ExitCode`, `Error`, `StartedAt`, and `FinishedAt`, so the amendment derives results from it.
  - `scoprad` currently takes `--state-dir` and `--version`.
- Decisions proposed in the ADR-010 amendment that most need a maintainer's view:
  - the image is a command-line flag only for now, with a persisted setting deferred to the service installation;
  - increment 3 is split into 3a, against a fake Podman, and 3b, on a real host;
  - results come from `podman inspect`, never from `podman run`'s exit status;
  - `execution.started` is sent once `State.StartedAt` is set;
  - preflight and an image-presence check run before every execution.

- Confirmation: the maintainer replied "continue", accepting the amendment and the checklist below as written.

#### Acceptance checklist (agreed before implementation)

- [x] The ADR-010 amendment is accepted, with any changes the maintainer asks for, and the ADR index is updated.
- [x] `hollow` exports the image rule that `PodmanRunArgs` uses (for example `ValidateImage`), and its tests are unchanged.
- [x] `scoprad --sandbox-image=REF`:
  - a valid reference is accepted, and Mio starts and serves status as before;
  - a malformed or empty one is a usage error (exit status 2) whose message states the rule, reported before the lock, the socket, or the registry are touched;
  - a repeated flag behaves as the existing flags do;
  - the value reaches Mio's configuration for later use by the runner.
- [x] Tests come first, the usage text and `cmd/scoprad/README.md` are updated, and full verification passes.

Non-goals:
- Any execution path.
- A persisted configuration.
- Reporting the setting in `status` or `doctor`.
- Checking that the image exists.
- The runner (3a-2 and 3a-3).

#### Test-first record

1. **Tests written first:**
   - `internal/hollow`: `TestValidateImage` checks the exported image rule directly. Three references are accepted. Eight are rejected, each wrapping `ErrInvalidImage` with a message that states the rule and never contains a raw newline: empty, no digest, a short or uppercase digest, an option-like name, a space, no name, and a trailing newline. The existing `PodmanRunArgs` tests are unchanged.
   - `cmd/scoprad`:
     - the help output lists `--sandbox-image NAME@sha256:DIGEST`;
     - `parseFlags` captures the image whether it is absent, set with `=`, set as a separate value, or repeated with the valid value last;
     - `TestSecurity_RunRejectsAMalformedSandboxImageBeforeTouchingState` covers an empty value, no digest, a short digest, an option-like name, a repeat with the invalid value last, and `--version` combined with a bad value. Each must be a usage error whose stderr states the flag, the rule, and the usage, with no stdout and no state directory created;
     - `TestRunStartsWithASandboxImage` checks that Mio still starts and reports that it is listening.
2. **Build failure.** `ValidateImage` and `parseFlags` were undefined.
3. **Declarations-only skeleton.** `ValidateImage` accepts everything, `parseFlags` returns an empty configuration, and `run` still parses inline. Runtime red: 5 tests failed across 9 subtests. The malformed-image cases were rejected only as an unknown flag, without the rule.

#### Implementation and verification record

- **`internal/hollow`:** `ValidateImage` exports the image rule, and `ImageRule` holds its wording, so `ValidateImage`, `PodmanRunArgs`, and `scoprad` state it identically. `PodmanRunArgs` now calls `ValidateImage`; its message is unchanged, and its tests needed no change.
- **`cmd/scoprad`:**
  - `parseFlags` returns a `daemonConfig` holding the state directory, the sandbox image, and whether `--version` was given. It uses `flag.Visit` to tell an empty value from an absent flag, and checks the image before anything else.
  - A malformed value produces `scoprad: --sandbox-image "<value quoted>" must be name@sha256: followed by 64 lowercase hexadecimal characters` and the usage line.
  - `run` gets its configuration from `parseFlags`. The image is held for the runner (3a-2).
- **Smoke test with the built binary:**
  - a malformed image exits with status 2 and states the rule, and no state directory is created;
  - a value containing a newline is printed escaped (`"evil\nline"`);
  - `--help` shows the new usage;
  - `--version` with a valid image exits with status 0.
- **Test improvements found by mutation testing:**
  - With validation disabled, the malformed-image test made Mio serve forever, so the mutation timed out the harness instead of failing a test. The harness restored the file and left no stray processes. The test now cancels its context on any stderr write, so the same fault fails promptly. The harness also runs `go test -timeout 120s` and reports a timed-out test as caught.
  - The rule check passed even with the rule missing from the message, because the usage line also contains `@sha256:`. It now asserts the rule's own words.
- Green: all tests pass under the race detector with no skips.
- Mutation testing, run in one sequential batch, all caught after these fixes: `ValidateImage` accepting everything; the flag never validated, an empty value treated as unset, or validation skipped with `--version`; the message without the rule; the value not stored; the usage text without the flag; a rejection that does not stop startup; the rule text without its digest form; and `PodmanRunArgs` skipping the image check.
- Coverage: `cmd/scoprad` 69.0% (up from 63.3%), and `internal/hollow` 98.1%.
- Documentation: `cmd/scoprad/README.md` (usage and the setting), the ADR-010 implementation status, the ADR index, the plan, and the roadmap above. Each says the setting has no visible effect until execution exists.
- Security review:
  - The image is fixed by digest, so tags cannot drift.
  - Malformed input fails before any state is touched.
  - The value is quoted in messages.
  - Mio never pulls images.
  - No high-confidence exploitable issue was found.
- Local verification passed:
  - `make lint` and `make security-test`;
  - `go test -race -count=1 -timeout 120s ./...` and ten repeated race runs of `internal/hollow` and `cmd/scoprad`;
  - every package with `CGO_ENABLED=0`;
  - `git diff --cached --check`;
  - build, vet, and test-compile for darwin/arm64, darwin/amd64, windows/amd64, and linux/arm64.
  - The tree is unchanged.
- Pushes to `arena/c7a368c1-scopra`: the proposal `4be6635`, test checkpoint `918183b` (red by design), then the implementation commit that adds this record. CI was not checked, at the maintainer's request.

### 2026-10-17 — Step 25: the Hollow runner against a fake Podman (ADR-010 amendment, increment 3a-2)

- Selection: the amendment's next increment. After Step 24, "continue" was stated to start it, and the maintainer replied "continue".
- Scope adjustment, stated up front: the amendment lists the out-of-memory and start-failure mapping under 3a-3. The mapping from `podman inspect` to a result is one function, so this step implements all of it (`succeeded`, `failed`, `out_of_memory`, and `errored` with `start_failed`). Increment 3a-3 keeps the timeout, cancellation, the image-presence check, and startup cleanup.

#### Acceptance checklist (agreed before implementation)

- [x] `hollow.Runner` (with the absolute path of `podman` and the pinned image) runs one validated request in a workspace and reports `execution.started` and `execution.output` through callbacks. It returns the terminal `execution.result`, or an error only for an invalid request or runner configuration.
- [x] **Launch:** `podman run` with exactly `PodmanRunArgs`, standard input from `/dev/null`, and stdout and stderr on separate pipes. If `PodmanRunArgs` refuses the workspace or image, the result is `rejected` (`workspace_unavailable` or `containment_unavailable`) and Podman is never run.
- [x] **Start notice:** `execution.started` is sent at most once, with `State.StartedAt` from `podman inspect` truncated to milliseconds, and before any output. Output readers wait, which keeps memory bounded and applies backpressure through the pipes. A container that never starts gets no start notice.
- [x] **Output (ADR-009):**
  - chunks hold 1 to 65536 bytes and are numbered in one sequence, in the order they are read;
  - once `limits.outputBytes` is reached, output is read and discarded and marked truncated;
  - the result's byte and chunk counts match what was sent.
- [x] **Result and cleanup:**
  - the result comes from `podman inspect`, never from `podman run`'s exit status: never started is `start_failed`, `OOMKilled` is `out_of_memory`, exit code 0 is `succeeded`, and 1 to 255 is `failed`, so a command's own exit code of 125 is `failed`;
  - times are truncated to milliseconds;
  - the result passes `ValidateAgainst(request)`;
  - `podman rm --force` removes the container in every case.
- [x] **Tests:** a fake Podman, implemented as the test binary re-executed with scripted scenarios, covers each outcome, the exact arguments, ordering, chunking and truncation, the rejections, and removal. The race detector is on, and full verification passes.

Non-goals (3a-3 and later):
- the timeout and cancellation (`podman kill`);
- preflight and the image-presence check before each run;
- startup cleanup of labelled containers;
- wiring the runner into Mio or the protocol;
- real Podman (3b).

#### Test-first record

1. **The fake Podman** (`internal/hollow/runner_test.go`): when `HOLLOW_FAKE_PODMAN` names a scenario directory, `TestMain` makes the test binary act as `podman`.
   - It logs every invocation's arguments.
   - `run` records `StartedAt` (after an optional delay) and then `FinishedAt`, `ExitCode`, and `OOMKilled`. It writes state atomically, writes the scripted output in a stream-specific pattern, and exits with the container's code, or with Podman's own code for a container that never starts.
   - `inspect --type container --format json NAME` prints Podman's `State` field names.
   - `rm --force NAME` records the removal.
   - Any other `inspect` or `rm` shape is refused, so the runner must call Podman exactly as designed.
2. **Tests written first**, all sharing `checkRun`, which requires:
   - the exact `PodmanRunArgs` as the first call, and `rm --force` of the container as the last;
   - exactly one removal;
   - a result valid for the request, with byte and chunk counts that match the chunks;
   - contiguous sequence numbers, and chunks of 1 to 65536 bytes;
   - the start notice before any output.

   The tests themselves:
   - a successful run: exact start notice and result, times truncated to milliseconds;
   - a failed run;
   - a command that itself exits with 125, which must be `failed`;
   - out of memory;
   - a start failure: exit status 127, no start notice, `start_failed` mentioning 127, and Podman's message in stderr;
   - a delayed start, with the start notice before the output;
   - 220,001 bytes over interleaved writes, all received exactly;
   - `TestSecurity_RunnerTruncatesOutputAtTheLimit`: exactly 100,000 bytes sent, marked truncated, each stream a prefix of what was written, and the command still `succeeded`;
   - rejections for an unmountable workspace or an undigested image, with no Podman call and no events;
   - an invalid request (`ErrInvalidExecutionRequest`), and an empty or relative Podman path (`ErrInvalidRunner`).
3. **Build failure.** `Runner`, `Events`, and `ErrInvalidRunner` were undefined.
4. **Declarations-only skeleton.** `Run` refuses everything. Runtime red: all 10 runner tests failed. The package's existing tests still passed under the new `TestMain`.

#### Implementation and verification record

- **`internal/hollow/runner.go`:** `Runner.Run` builds the arguments through an internal `podmanRunArgs`, which `PodmanRunArgs` now wraps and which also returns the action hash. The request is therefore validated exactly once, and a refused workspace or image gives a `rejected` result with a fixed message.
  - **Pipes:** the runner creates them itself with `os.Pipe`, so `Wait` never reads them and can run while two readers drain chunks of up to 64 KiB.
  - **Start watcher:** it polls `inspect` until `StartedAt` appears, sends the start notice, and lets output through. It then waits for Podman to exit and returns the final state from one last inspect. The result is built from that state, so `Run` cannot return while a start notice could still come.
  - **Output emission:** a single lock numbers the chunks, enforces `limits.outputBytes`, copies the data, and calls the callback.
  - **Result:** taken from `inspect`; `podman rm --force` always runs afterwards.
- **Problems found while making the tests green:**
  - Each fake call is its own process, so the log order of concurrent calls (`run` and the watcher's inspects) is not the launch order. The test now finds the single `run` call instead of assuming it comes first.
  - Every test took about 3 seconds, because race-enabled binaries sleep for a second at exit by default. The fake calls are race-enabled test binaries, so the tests now set `GORACE=atexit_sleep_ms=0` for them.
- **Changes found by mutation testing:**
  - The end time could equal the start time unnoticed, because the fake ran within one millisecond. The successful-run scenario now runs for 25 ms.
  - An early request check in `Run` was redundant with `PodmanRunArgs`'s. The internal `podmanRunArgs` replaced it, leaving one validation point.
  - The fake gained ways to report two containers or an exit code above 255, both of which must give `errored` with `containment_failed`.
  - The last inspect after Podman exits was exercised only by chance, because Go's `select` picks at random between the exit and a poll tick. In a test with a 10-second poll interval, the start is now visible only after exit.
  - A start notice delivered after `Run` returned was hidden by the time `podman rm` takes. The watcher now returns the final state that the result depends on, and the recorder fails any callback that arrives after `Run` returns.
  - For that test, the fake container now outlives the first slow inspect.
- **Tests added for reachable branches:** a missing `podman` binary (`start_failed`, no events), an end time before the start (clamped), and the default poll interval.
- Green: all tests pass under the race detector. The runner tests ran 25, 20, and 10 more times under the race detector, and 40 times without it, with no flakes.
- Mutation testing: 20 deliberate mutations, all caught, run in one sequential batch (with `go test -timeout`), plus one by hand because removing the copy also needs its import removed:
  - output not held for the start notice;
  - truncation not marked, or the limit ignored;
  - sequence numbers off by one, or stdout counted as stderr;
  - Podman's 125 to 127 read as start failures;
  - out of memory or a start failure not detected;
  - the start notice using the current time;
  - the end time equal to the start time;
  - the container not removed;
  - rejection codes swapped;
  - the Podman path or the request not checked;
  - chunk data not copied;
  - the inspect element count unchecked;
  - the last inspect sending no notice, or output released before it;
  - exit codes above 255 accepted;
  - the start-failure message without the status.
- Coverage: `internal/hollow` 97.9%. The only uncovered lines are the two pipe-creation failures.
- Documentation: the ADR-010 implementation status (including the scope note), the ADR index, the plan, the README status line, and the roadmap above. Each says the runner is tested only against a fake Podman and that Mio does not use it yet.
- Security review:
  - The runner never reads more than one chunk per stream ahead.
  - Output is never logged, and messages are fixed text.
  - The container is always removed.
  - The result never trusts `podman run`'s exit status.
  - Remaining risks for 3a-3 and 3b: a hung `podman inspect` would stall a run until the timeout exists (3a-3), and only a real host can confirm Podman's behaviour (3b).
  - No high-confidence exploitable issue was found.
- Local verification passed:
  - `make lint` and `make security-test`;
  - `go test -race -count=1 -timeout 180s ./...` and ten repeated race runs of `internal/hollow`;
  - every package with `CGO_ENABLED=0`;
  - `git diff --cached --check`;
  - build, vet, and test-compile for darwin/arm64, darwin/amd64, windows/amd64, and linux/arm64.
  - The tree is current.
- Pushes to `arena/c7a368c1-scopra`: test checkpoint `02b894c` (red by design), then the implementation commit that adds this record. CI was not checked, at the maintainer's request.

### 2026-10-17 — Step 26: runner timeout, cancellation, preflight, and leftover cleanup (ADR-010 amendment, increment 3a-3)

- Selection: the last increment that can be built without a real Podman. After Step 25, "continue" was stated to start it, and the maintainer replied "contnue".
- Sandbox reset before this step: the branch had been recreated at `main` with the work kept as uncommitted changes, and the toolchain was gone. The temporary-index tree comparison found **one difference**. The restore had rewritten the raw `0xFF` byte in `packages/test-fixtures/messenger/v1/invalid/execution-request-invalid-utf8.json` as U+FFFD (`EF BF BD`), silently turning the invalid-UTF-8 fixture into valid text. After the mixed reset to `origin/arena/c7a368c1-scopra` (`e7fa155`), the file was restored from the commit, and the tree then matched exactly. The toolchain was rebuilt from the checksum-verified sources. **Lesson: the sandbox's restore is not byte-exact for non-UTF-8 files, so recovery must always compare trees and must never trust the restored working tree as it stands.**

#### Acceptance checklist (agreed before implementation)

- [x] **Timeout:** Mio's own timer starts at launch with `limits.timeoutSeconds`. When it expires, the runner runs `podman kill --signal=KILL` on the container and reports `timed_out`. If the kill fails while Podman is still running, for example because the container is not yet created, it retries until Podman exits.
- [x] **Cancellation:** cancelling `Run`'s context kills the container in the same way and reports `cancelled`.
- [x] Rules for both:
  - the first kill wins;
  - a container that exits on its own before any kill succeeds keeps its natural result;
  - the timer is stopped when the run ends, so no kill happens afterwards.
- [x] **Before each run**, after the arguments are validated:
  - the preflight check (`podman info --format json`, then `ParsePodmanInfo` and `Check`);
  - `podman image exists IMAGE`.

  A failure is `rejected` with `containment_unavailable` and a message no longer than 1024 bytes, and Podman is never run.
- [x] Every helper call to Podman (`info`, `image exists`, `inspect`, `kill`, `rm`, `ps`) has a deadline of 30 seconds by default, so a hung `podman` cannot stall a run forever. A timed-out final inspect is `errored` with `containment_failed`.
- [x] **Leftover cleanup:** `Runner.RemoveLeftovers` lists containers labelled `scopra.action-hash` (`podman ps --all --filter label=scopra.action-hash --format {{.Names}}`). It removes only those named `scopra-` followed by 32 hexadecimal characters, and reports how many it removed.
- [x] **Tests:** the fake Podman gains `kill` (which ends the fake `run` process and records exit code 137), `info`, `image exists`, `ps`, and scenarios for each requirement. The race detector is on, and full verification passes.

Non-goals:
- Wiring the runner or the leftover cleanup into Mio.
- Real Podman (3b) and the adversarial tests.
- The expiry of the request's own validity window.

#### Test-first record

1. **The fake Podman gained:**
   - `kill --signal=KILL`, which records exit code 137 and SIGKILLs the fake `run` process (whose PID `run` now records). It fails while the container is not running, and also for the scenario's first `KillFailures` attempts;
   - `info --format json`, which reports a qualifying host by default or the scenario's output;
   - `image exists`, which can report the image missing;
   - `ps --all --filter label=scopra.action-hash --format {{.Names}}`, which lists the scenario's leftovers, and `rm`, which accepts them.
2. **Tests written first:**
   - `TestSecurity_RunnerTimesOut`: a 30-second container with a 1-second timeout gives `timed_out` after about one second, with exactly one correct `podman kill`.
   - `TestRunner_Cancels`: cancelled from the start-notice callback.
   - `TestRunner_FirstKillWins`: cancelled first; after waiting past the timeout there is still only one kill.
   - `TestRunner_RetriesAFailedKill`: two failed kills, then one that works.
   - `TestRunner_KeepsANaturalExitWhenNoKillSucceeds`: every kill fails, so the run is `succeeded`.
   - `TestSecurity_RunnerRefusesAHostThatFailsPreflight`: root, non-JSON output, a 5000-character version (whose message must stay within 1024 bytes), and a missing image. Each is `rejected` with `containment_unavailable`, after exactly the preflight calls, without running anything.
   - `TestRunner_BoundsPodmanHelperCalls`: inspects that hang past a 200 ms deadline give `errored` with `containment_failed` within seconds.
   - `TestRunner_RemovesLeftovers`: only `scopra-` names with 32 lowercase hexadecimal characters are removed, and a relative Podman path is refused.
   - The missing-Podman test now expects the preflight rejection.
3. **Build failure.** `helperTimeout` and `RemoveLeftovers` were undefined. My own test also wrongly compared slices of slices with `slices.Equal`; I fixed that before going on.
4. **Declarations-only skeleton.** It adds the field, and a `RemoveLeftovers` that does nothing. Runtime red: 9 tests failed. Without kills, the long-running fake containers ran their full 30 seconds.

#### Implementation and verification record

- **`internal/hollow/runner.go`:**
  - **The killer:** one goroutine owned by `run` waits for whichever comes first: the timeout timer (`limits.timeoutSeconds`), `ctx.Done()`, or Podman's exit.
    - On a timeout or a cancellation it records the reason, then runs `podman kill --signal=KILL NAME` until a kill succeeds or Podman exits.
    - `run` waits for the killer before building the result, so the first trigger wins and no kill can follow the removal.
    - The result is `timed_out` or `cancelled` only if a kill succeeded; otherwise the container's own result stands.
  - **`preflight`,** after the arguments are validated: `info --format json`, then `ParsePodmanInfo` and `Check`, then `image exists`. Exit status 1 means "not in local storage", and any other failure is reported as such. Messages go through `resultMessage`, which replaces control, format, and separator characters and bounds the text to 1024 bytes on a character boundary.
  - **Helper calls:** `helperOutput` and `helperRun` give every helper call (`info`, `image exists`, `inspect`, `kill`, `rm`, `ps`) a deadline (30 seconds by default), using `exec.CommandContext` with a `WaitDelay` so that a killed helper cannot hang on its pipes.
  - **`RemoveLeftovers`** lists labelled containers and removes only those named `scopra-` followed by 32 lowercase hexadecimal characters.
  - `Run` refuses a nil context (`ErrInvalidRunner`).
- **`internal/hollow/preflight.go`:** two messages now put the untrusted value last ("Podman is older than 4.0 or reports an unrecognized version: %q" and "cgroups are not v2: %q"), so a bounded message keeps its explanation. Step 23's tests needed no change.
- **Test changes during implementation:**
  - the fake's kill uses `os.Process.Kill` instead of `syscall.Kill`, which does not exist on Windows, so the tests compile for windows/amd64;
  - added tests for printable preflight messages (a Podman path containing a newline), a broken `image exists`, a failing `ps`, `resultMessage` itself, and kill retries at the default interval.
- Green: all tests pass under the race detector, and the kill tests ran repeatedly with no flakes (8, 15, 5, and 5 extra runs).
- Mutation testing, run in one sequential batch with `go test -timeout`: 18 deliberate mutations, all caught.
  - The mutations cover: the timeout or cancellation never killing; the reasons swapped; a successful kill ignored, or failed kills still overriding; no kill retry; preflight skipped, a missing image misread, or `Check` ignored; the message bound off by one or messages unsanitised; the helper deadline dropped or the configured one ignored; leftover names unfiltered, uppercase hexadecimal accepted, or a relative Podman path accepted; a nil context accepted; and the untrusted version placed first in the message.
  - One of them (a nil context accepted) is caught as a panic in the invalid-input test, confirmed by hand.
  - Three spot checks after the portable-kill change were still caught.
  - The mutation harnesses were recreated after the reset wiped `/tmp`.
- Coverage: `internal/hollow` 98.0%. The only remaining branches are failures to create pipes, and Podman vanishing between preflight and launch.
- Documentation: the ADR-010 implementation status (3a complete), the ADR index, the plan, and the roadmap above. Each says the runner is tested only against a fake Podman and that Mio does not use it yet.
- Security review:
  - Mio initiates and attributes every kill.
  - No container survives a run, and leftovers from a crash can be removed.
  - A hung Podman is bounded.
  - The host and the image are checked before every run, and nothing runs on failure.
  - Messages are bounded and printable.
  - No high-confidence exploitable issue was found.
- Local verification passed on the rebuilt toolchain:
  - `make lint` and `make security-test`;
  - `go test -race -count=1 -timeout 300s ./...` and five repeated race runs of `internal/hollow`;
  - every package with `CGO_ENABLED=0`;
  - `git diff --cached --check`;
  - build, vet, and test-compile for darwin/arm64, darwin/amd64, windows/amd64, and linux/arm64.
  - The tree is unchanged.
- Pushes to `arena/c7a368c1-scopra`:
  - test checkpoint `06f25b8` (red by design);
  - implementation `3e6b63c`;
  - this record, in a follow-up commit. My script that writes the record asserted 6 checklist boxes when the section has 7, so it failed, and the commit, which was not chained to the script, went ahead without the record.
- CI was not checked, at the maintainer's request.

### 2026-10-17 — Step 27: local execution over IPC, the protocol (ADR-011, increment 1 of 4)

- Selection: after Step 26 the default was (a), proposing local execution end to end through Mio and Hollow (the plan's "local execution" task for Mariner, and the first step towards the exit gate's "Direct execution works in rootless containment"). The maintainer replied "Continue". Implementation starts only after confirmation; a "continue" accepts the proposal.
- Research:
  - ADR-003: a bounded exchange, a 2-second deadline (`connectionTimeout`), and 16 concurrent connections (`maxConcurrentConnections`).
  - ADR-006: 64 KiB frames, one request per connection, exact key sets, no reply to a malformed request, and the sentence that every execution needs a grant, policy evaluation, approval, and Rupes.
  - ADR-009: output chunks of up to 64 KiB, about 87 KiB encoded, which do not fit an IPC frame.
- Decisions proposed in ADR-011 that most need a maintainer's view:
  - **local execution needs no grant or approval**, which qualifies ADR-006's sentence for local runs only;
  - Mio, not the client, builds the full request with local principals from the peer credentials;
  - streamed `started`, `output` (at most 32 KiB per frame), and `result` frames on one connection;
  - a 30-second write deadline, cancellation when the client disconnects, and at most 4 concurrent runs, rejecting a fifth with `busy`;
  - `scopra run` defaults (read-only access, 10 minutes, 2 GiB, 2 CPUs, 512 processes, 16 MiB of output), raw output by default with `--json` available, and exit statuses 124, 130, and 137 as shells use them.

- Confirmation: the maintainer replied "Continue", accepting ADR-011, including that local runs need no grant or approval, and the checklist below as written.
- Design detail settled before coding: Mio splices the client's `run` fields, unchanged, into a complete ADR-007 request with its own principals, IDs, and times, and decodes the whole request with `protocol.DecodeExecutionRequest`. The client's text therefore gets every Messenger strictness rule (`null`, escapes, exact keys, field rules), which IPC v1's checks alone do not cover.

#### Acceptance checklist (agreed before implementation)

- [x] ADR-011 is accepted, with any changes the maintainer asks for, and indexed. ADR-006 gains a dated note that ADR-011 qualifies its execution sentence for local runs.
- [x] `internal/localipc` serves `execution.run` when an executor is enabled (as `EnableWorkspaces` enables workspace commands). It decodes the request with an exact key set and ADR-007's field rules for the command, access, limits, and reason, and a malformed request gets no reply.
- [x] **Streaming:** the executor's start notice, output, and result are written as `started`, `output`, and `result` frames. Output is split to at most 32768 bytes per frame, and every frame stays within 64 KiB.
- [x] **Bounds:**
  - only reading the request is limited to 2 seconds;
  - each reply frame is limited to 30 seconds;
  - a client that disconnects or stops reading cancels the executor's context;
  - a fifth concurrent run is answered with `rejected` and `busy`.
- [x] A client function sends a run, delivers the events to callbacks, and returns the result, with typed errors as for the workspace commands.
- [x] **Tests**, against a real in-process Mio with a fake executor:
  - the frame shapes and order;
  - splitting at the 32 KiB bound;
  - malformed requests;
  - the concurrency cap;
  - cancellation on disconnect and on a stalled reader;
  - the deadlines.

  Status and workspace commands are unchanged. The race detector is on, and full verification passes.

Non-goals (later increments):
- Mio wiring to Hollow and workspace revalidation (increment 2). Building the request moved into this increment; see the deviation below.
- `scopra run` (increment 3).
- Real Podman (increment 4, with ADR-010's 3b).
- Audit.

#### Deviation from the agreed non-goals (recorded before implementation)

ADR-011's increment list and the non-goals above put "building the request" in increment 2. Two agreed boxes of this increment need it, so it moves here:
- Applying ADR-007's field rules means decoding a complete request, which this increment does by splicing the client's fields into one.
- The `busy` refusal is an ADR-009 `result`, which must carry the request's `executionId` and `actionHash`.

`internal/localipc` is part of Mio and holds the peer UID, so it builds the request: `local:<uid>`, `local`, `local-socket`, a random `local-` execution ID and nonce, a one-hour window, and network `none`. Increment 2 keeps Podman resolution, leftover cleanup, workspace revalidation, and running Hollow.

A related rule: an executor that returns an error, or a result or event that does not belong to its request, is a Mio fault, and Mio closes the connection without a `result`. The client reports `ErrNoRunResult`, so it never shows an invented outcome.

#### Test-first record

Tests added before any implementation:
- `internal/localipc/executions_test.go` (unit tests, all platforms): the wire form of a run, client-side validation before sending, Mio's request fields, ADR-007 rules on the client's own text, the output-frame bound, and the client's frame reader in order and on 22 invalid sequences.
- `internal/localipc/executions_linux_test.go` (a real in-process Mio, a fake executor): frame shapes and order with splitting at 32768 bytes, the request Mio built from the peer, 12 malformed requests, the fifth-run `busy` refusal, cancellation on disconnect, on a stalled reader, and on shutdown, the 2-second request deadline versus a longer run, executor faults, and enabling.

Expected failures, shown before implementing:
1. **Build red:** `go test ./internal/localipc/` failed with 18 missing identifiers, including `RunExecution`, `RunRequest`, `RunEvents`, `Executor`, `ExecutionEvents`, `ErrNoRunResult`, `EnableExecutions`, `buildExecutionRequest`, `readRunReplies`, and the frame encoders.
2. **Runtime red:** with a compiling skeleton whose bodies return "not implemented", all 19 new tests fail, and the 67 existing tests in the package still pass.

Checkpoint: `30531d5` (tests, skeleton, ADR-011 accepted, ADR-006 note), pushed before implementing.

#### Implementation record

- **`internal/localipc/executions.go`:**
  - `Daemon.EnableExecutions(executor)`, the `Executor` interface (shaped like Hollow's `Runner.Run`, so increment 2 can pass its events through), `ExecutionEvents`, `RunRequest`, `RunEvents`, `RunExecution`, and `ErrNoRunResult`.
  - `buildExecutionRequest` splices the client's `run` values into a complete ADR-007 request with Mio's own fields and decodes it with `protocol.DecodeExecutionRequest`.
  - `serveRun` and `runStream` handle streaming and fault handling. `decodeRunFrame` and `readRunReplies` are the client's strict frame reader.
- **`messages.go`:** the `run` field with an exact key set, and `run` refused on every other command.
- **`socket.go`:** `Serve` passes its context to each connection, so shutdown cancels runs. `handleConnection` dispatches `execution.run`. `roundTrip`'s connection checks moved, unchanged, into `dialMio`, which the run client shares. `Listen` initialises the frame-write timeout and the four run slots.
- **Docs:** ADR-011 gains an implementation-status section, and the README mentions the protocol with "tested only with a fake executor, and Mio does not enable it yet". Scopra still runs no commands, because `scoprad` does not call `EnableExecutions`.

**Mutation testing** (sequential, `go test -timeout 120s`, a timeout counted as caught):
- **Round 1:** 56 mutations: 43 caught, 11 survived, and 2 failed to build.
- **What the survivors showed:**
  - **Weak tests, fixed:**
    - the cap was not pinned to ADR-011's value of 4;
    - no invalid output chunk was tested;
    - no test checked the `run` key set in `decodeRequest`, or `run` on another command;
    - no result with the right execution ID but another action hash was tested;
    - no output data with an escaped newline was tested (Go's base64 decoder skips newlines, even in strict mode).
  - **Redundant code, removed or restructured:**
    - `checkJSONDocument` in `buildExecutionRequest`;
    - `encodeRun`'s wire re-check, which the typed check made redundant;
    - `Strict()`, which the re-encoding comparison made redundant;
    - the per-method failure checks, replaced by one guard in `write`.
  - **A guard that held only by accident:** without `EnableExecutions`, the zero write timeout and the nil slot channel also kept Mio silent, so the executor check could not be tested. `Listen` now initialises both, the check is the only guard, and removing it crashes the test binary on a nil executor.
- **Rounds 2 and 3:** the survivors and the two compile-safe rewrites were all caught.
- **Final run against the final code:** 53 of 53 caught, all by test assertions and none by timeout. They are the 49 round-1 mutations whose code still exists, the 2 compile-safe rewrites, and 2 new ones: the moved failure guard, and `run` with a path.

Tests added during implementation:
- `TestSecurity_FrameEncodersRefuseInvalidFrames`, for encoder guards that the coverage report showed untested;
- `TestDecodeRequest_ChecksTheRunObjectsKeys` and `TestSecurity_RunStaysWithItsCommand`, from mutation testing;
- the misbehaving-executor case "an invalid output chunk", and the frame-reader cases "another action hash's result" and "output with a newline".

Verification, all passing:
- `make lint` and `make security-test`;
- `go test -race -count=1 -timeout 60s ./...`, plus five more race runs of `internal/localipc` (and eight of the new tests during development);
- `CGO_ENABLED=0 go test ./...`;
- build, vet, and test-compile for darwin/arm64, darwin/amd64, windows/amd64, and linux/arm64;
- `git diff --cached --check`, no skipped tests, and an up-to-date tree.

Coverage of `internal/localipc` rose from 82.3% to 85.3%.

### 2026-10-17 — Step 28: Mio wiring to Hollow (ADR-011, increment 2 of 4)

- New session branch `arena/fe74ddf7-scopra` starts from `main` at `86ed7a6` (PR #2 merge).
- Selection: the user asked this session to "set up this project and read all the docs and start working on next important things". After Step 27 the next increment in ADR-011 is increment 2, the only step that turns the tested local execution protocol into a working local execution path: Mio resolves Podman, cleans up leftovers, revalidates the workspace, and runs Hollow, still tested against a fake Podman. `scopra run` (increment 3) and real-host verification (increment 4) stay out.
- Scope decisions to be recorded in ADR-011:
  - **Execution is always enabled.** Without `--sandbox-image`, or when `podman` cannot be found, Mio does not stay silent: every well-formed run is answered with a `rejected` result and `containment_unavailable`, so clients get a typed refusal instead of `ErrNoRunResult`.
  - **The workspace is checked first.** Unknown or changed workspaces are refused with `workspace_unavailable` before the containment check, as ADR-011's order says.
  - **Cleanup failure fails closed.** If the startup removal of labelled leftover containers fails, this Mio process refuses every run with `containment_unavailable` and says so on stderr. It still serves status and workspace commands.
  - **The client never supplies a path.** The service passes the canonical path from the registry record to Hollow; nothing in the request can select a directory.

#### Acceptance checklist (agreed before implementation)

- [x] `internal/execution` provides a `Service` implementing Mio's executor contract. For each request it:
  - looks the workspace ID up in the registry and revalidates the recorded workspace with the ADR-004 validator immediately before the run;
  - rejects with `workspace_unavailable` when the ID is unknown or revalidation fails;
  - rejects with `containment_unavailable` when no runner is configured, naming the reason;
  - otherwise calls Hollow with the record's canonical path, forwards the start notice and output chunks unchanged, and returns Hollow's result unchanged;
  - propagates a Hollow error as an error, so Mio ends the run without inventing a result;
  - never falls back to native execution.
- [x] `scoprad` builds one ADR-004 validator and uses it for both the registry and execution revalidation. It resolves `podman` once with `exec.LookPath` and keeps the absolute path.
- [x] When `--sandbox-image` is set and Podman exists, Mio removes labelled leftover containers before it starts serving. A failure disables executions for this process (fail closed) and is reported on stderr after the listening line.
- [x] Without an image or Podman, Mio starts and serves exactly as before, and every run is refused with `containment_unavailable`.
- [x] Status and workspace commands, the IPC wire format, result states, and Hollow's arguments are unchanged.
- [x] Tests:
  - `internal/execution` unit tests with a fake registry, validator, and runner: the canonical path, event and result passthrough, unknown and changed workspaces, containment unavailable, runner errors, dependency and context checks, and result validity;
  - `cmd/scoprad` end-to-end tests through a real Mio with a fake Podman: a run streams its start notice, output, and result; only the registered path is mounted; unknown and replaced workspaces never reach Podman; no image refuses; leftovers are removed at startup; a failed cleanup disables executions while status still answers.
- [x] `make security-test` includes `internal/execution`; the new tests pass under the race detector and the tree is regenerated.

Non-goals: `scopra run` (increment 3), verification on a real rootless host (increment 3b/4), cancellation UI, approvals, policy, and audit.

#### Test-first record

Tests added before any implementation:

- `internal/execution/service_test.go`: 13 tests (one with four dependency subtests) with a fake registry, validator, and runner.
- `cmd/scoprad/execution_test.go`: eight end-to-end tests against a real in-process Mio. The fake Podman is the test binary itself (`TestMain` with `SCOPRAD_FAKE_PODMAN`), reached through a `podman` symlink first on `PATH` so that Mio's real `exec.LookPath` runs. It implements `info`, `image exists`, `ps`, `run`, `inspect`, and `rm`, and writes the state files that the runner reads.

Red run:

- `go test ./internal/execution/` failed to build with 18 undefined identifiers, including `Service`, `Events`, `WorkspaceRegistry`, `WorkspaceValidator`, `Runner`, `ErrInvalidService`, and `reject`.
- `go test ./cmd/scoprad/` compiled and ran. The four run tests failed with `localipc: Mio ended the run without a result: localipc: invalid frame: incomplete frame header`, because Mio did not enable `execution.run`; the leftover test found no removals; the cleanup-failure test timed out waiting for the warning. The existing daemon tests still passed.

#### Implementation and verification record

- **`internal/execution/service.go`:** `Service`, `WorkspaceRegistry`, `WorkspaceValidator`, `Runner`, `Events`, `ErrInvalidService`, and `reject`.
  - A nil service, context, registry, or validator is `ErrInvalidService`.
  - An unknown workspace is rejected with `workspace_unavailable` and "the workspace is not registered"; a failed ADR-004 revalidation is rejected with `workspace_unavailable` and "the workspace failed revalidation". Both messages are fixed text.
  - A nil `Runner` or a non-empty `Unavailable` reason is rejected with `containment_unavailable`; the reason wins over a runner, so a configuration mistake cannot run anything.
  - Otherwise the service passes the record's canonical path and the caller's events to Hollow and returns its result unchanged. A runner error is returned as an error, so Mio ends the run without inventing a result.
  - Every rejection recomputes the action hash from the request.
- **`cmd/scoprad/execution.go`:** `newExecutor` and `localExecutor`.
  - Fixed refusal and warning text; one `exec.LookPath("podman")` at startup, kept as an absolute path (`LookPath` already fails closed on a relative result).
  - With an image and Podman, `RemoveLeftovers` runs before serving. On failure, executions are disabled for this process and a warning is returned.
  - Without an image, execution stays enabled but refuses with `containment_unavailable` naming `--sandbox-image`; without Podman, it names Podman. This keeps the ADR-011 promise that Mio "rejects executions" rather than staying silent.
- **`cmd/scoprad/main.go`:** `openWorkspaceRegistry` now also returns the validator, which execution reuses. `run` always calls `EnableExecutions` and prints the returned warning after the listening line.
- **`Makefile`:** `security-test` includes `internal/execution`.
- **Docs:** ADR-011 gains the increment 2 status and marks increments 1 and 2 done; ADR-010's status notes that Mio now uses the runner; the README, system overview, `internal/execution/README.md`, the repository tree, and this log are updated.

**Mutation testing** (sequential, against the new service, wiring, and startup path): 28 mutations, all caught. 23 were caught on the first round; the other 5 failed to build and were rewritten to compile before being caught. None survived, and none was caught only by a timeout. The mutations covered the nil guards, lookup errors and IDs, rejection codes and messages, revalidation and its order, the unavailable precedence, the canonical path, event forwarding, the action-hash error path, the image and Podman checks, leftover cleanup, and the warning lines.

**Security review.** The client still names only a registered workspace ID, and Mio resolves the path from its own registry and revalidates it immediately before use, so no request field can select a directory. Rejection text is fixed and carries no path. A cleanup failure fails closed. The service adds no writable state and no new trust boundary; same-UID callers already hold the user's authority. Residual risks: a same-UID process can race the window between revalidation and mount; local runs still have no policy, approval, or audit (ADR-011); real Podman is still unverified; and startup cleanup can delay the listening line by up to one 30-second helper deadline per leftover container.

**Verification, all passing** (on Go 1.26.9, the toolchain CI pins, built from the `golang/go` `go1.26.9` tag; the first checks in this session ran on Go 1.27.2 from the PyPI `go-bin` wheel):

- `make lint` and `make security-test`;
- `go test -race -count=1 -timeout 120s ./...`;
- `CGO_ENABLED=0 go test ./...`;
- build, vet, and test-compile for darwin/arm64, darwin/amd64, windows/amd64, and linux/arm64;
- `git diff --check`, no skipped tests, and a regenerated tree.

Coverage: `internal/execution` 100.0% (new), `cmd/scoprad` 73.1% (from 55.3%), `internal/localipc` 85.3%, `internal/hollow` 98.0%.

Commits `5751414` (`feat(execution): run local runs in Hollow`) and `1c6be69` (`docs(progress): record Step 28, Mio wiring to Hollow`) were pushed to `arena/fe74ddf7-scopra`. No red checkpoint was pushed for this step, unlike Steps 2 and 3.

Actions run `39007352867` for `1c6be69` ended in `startup_failure` with zero jobs, the same organization-level failure recorded in the Baseline section. It is not a code-test result; the evidence for this increment is the local verification above.

### 2026-10-17 — Step 29: `scopra run` (ADR-011, increment 3 of 4)

- Selection: the maintainer replied "continue" after Step 28, so the next ADR-011 increment is the Mariner `scopra run` command, which makes local execution usable and closes the plan's "local execution" item for Mariner. Real-host verification (increment 4) still needs rootless Podman and stays out.
- CLI shape follows ADR-011 exactly:

  ```text
  scopra run [--state-dir PATH] [--json] (--workspace ID | --path PATH) [--write] [--workdir DIR]
             [--timeout DURATION] [--memory BYTES] [--cpus N] [--pids N] [--output-limit BYTES]
             [--reason TEXT] -- EXECUTABLE [ARGUMENT...]
  ```

  Decisions, now recorded in ADR-011's Implementation Status (increment 3):
  - **Defaults** are read-only access, `.`, "local run by the workspace owner", 10 minutes, 2 GiB, 2 CPUs, 512 processes, and 16 MiB of output.
  - **`--cpus` takes a whole number** (1 to 1000), converted exactly to protocol millicores; fractional CPUs are not accepted in this increment. **`--timeout`** must be a whole number of seconds from 1s to 24h, because the protocol carries integer seconds.
  - **`--path`** uses the same resolution as `workspace remove --path`, so an exact recorded path or the directory it now resolves to both work, and an ambiguous match is refused.
  - **Output:** without `--json`, stdout and stderr bytes go unchanged to the CLI's streams, as a native run. With `--json`, stdout carries one JSON object per line: the ADR-009 start notice, each output frame as `{executionId, stream, data}` with unpadded base64url data in arrival order (the client receives frames, not ADR-009 chunk numbers), and the ADR-009 result. Diagnostics go to stderr in both modes.
  - **Exit status:** the command's own code, 124 for `timed_out`, 130 for `cancelled` and for Ctrl-C, 137 for `out_of_memory`, 1 for `rejected`/`errored` after printing the code and message, 2 for usage, and 1 for transport failures. Ctrl-C closes the connection, which cancels the run.

#### Acceptance checklist (agreed before implementation)

- [x] `scopra run` implements the synopsis, defaults, and flag rules above. `--workspace` and `--path` are mutually exclusive and one is required; `--workspace` is checked locally as 32 lowercase hexadecimal characters.
- [x] Invalid limits, an empty workdir, an empty executable, an invalid reason or working directory, and limits outside ADR-007's ranges are usage errors (exit 2) found before anything is sent to Mio.
- [x] Human output writes stdout bytes to stdout and stderr bytes to stderr, unchanged. `--json` writes only JSON objects to stdout and no other stdout.
- [x] Exit statuses are as above, including Ctrl-C → 130 with no error line, and `rejected`/`errored` → 1 with `<code>: <message>` on stderr, escaped.
- [x] `exitCode` recognizes the new typed status without printing it; the root usage text lists `run`.
- [x] Tests: a parse/validation table with defaults and every range; end-to-end against an in-process Mio with a fake executor for human and JSON output, flag propagation, `--path` resolution, every exit status, Ctrl-C cancellation, a missing Mio, and validation before dialing.
- [x] ADR-011 marks increment 3 done; README, the plan, the system overview, `docs/repository-tree.txt`, and this log are updated; `make security-test` covers the new security tests.

Non-goals: real Podman or real-host verification (increment 4), PTY, standard input, artifact output, redaction, approvals, policy, and audit.

#### Test-first record

Tests added before any implementation:

- `internal/localipc/executions_test.go`: `TestValidateRun_ChecksTheRunWithoutDialing`, six rejection cases.
- `cmd/scopra/run_test.go`: 14 tests.
  - Parsing: the defaults, overrides, the protocol's boundary values, and a 34-case rejection table that names the check that must reject each case.
  - End to end, against a real in-process Mio with a fake executor: stream routing, flag propagation, the `--json` line schema and base64url, every exit status, Ctrl-C, a missing Mio, `--path` through an exact and a symlinked path, an unregistered path, a failing stdout, and the help text.

Red run:

- `go test ./internal/localipc/` failed to build on the missing `ValidateRun`.
- `go test ./cmd/scopra/` failed to build on the missing `parseRunOptions`.

After the first green run, two tests were wrong and the code was right: a bare executable name may contain a space under ADR-007 (`echo hi` is a legal name), and a relative executable without `./` is rejected. The tests now use `bin/echo` and `.` for those cases.

#### Implementation and verification record

- **`cmd/scopra/run.go`:** `runRun`, `executeRun`, `writeOutput`, `runOutcome`, `parseRunOptions`, `wholeSeconds`, `commandExitError`, and `errNoRunSeparator`; defaults and bounds as constants.
  - **No separate client state:** the command builds `localipc.RunRequest` and reuses `RunExecution`, so output framing, splitting, deadlines, and cancellation are the ones increment 1 tested.
  - **Validation before the socket:** every check that does not need Mio runs first, through `localipc.ValidateRun`; `--path` is resolved after the other fields are checked with a placeholder registration ID, then checked again with the resolved ID.
  - **Output:** `--json` adds `"type"` to each canonical payload; writes go to stdout and stderr unchanged otherwise. A failed write ends the command instead of being ignored.
  - **Exit statuses:** `commandExitError` carries the command's own status, 124, 130, or 137 to `exitCode`, which reports it without printing an error line. `rejected` and `errored` become `<code>: <message>`, which `exitCode` prints escaped.
- **`cmd/scopra/main.go`:** the `run` command is dispatched and listed in the usage text; `exitCode` recognizes `commandExitError` before its generic failure branch.
- **`internal/localipc/executions.go`:** `ValidateRun` exposes the client-side ADR-007 check that `RunExecution` already performed, without dialing.
- **Docs:** `cmd/scopra/README.md` gains the command, its flags, its JSON lines, and its exit statuses, and drops the claim that nothing executes commands; ADR-011, the root README, the plan, the system overview, the tree, and this log are updated.

**Mutation testing** (sequential, against `run.go` and `main.go`): 35 mutations, 34 caught and one no longer applicable. The first round caught 23 and its eight survivors exposed weak assertions: the missing-separator, missing-workspace, workspace-ID, timeout-bound, and CPU-bound rejections all happened to be caught by a *later* check, so the tests passed for the wrong reason. The rejection table now asserts the message of the check that must reject each case, and new cases cover `--workspace` alone and a positional before `--`.

The remaining survivor (`arguments = nil`) was unreachable code and was removed: slicing after the executable can never produce nil, and `encodeRun` already encodes nil as the empty array. Two mutations failed to build and were rewritten to compile, after which both were caught. Every mutation that still applies was caught by a test assertion, not a timeout.

**Security review.** The command adds no authority: it can only name a registered workspace, Mio revalidates it before use, and Hollow still runs the command. `--write` is explicit, the default is read-only, and no flag can inject environment variables, a shell, or a mount. Arguments cannot contain NUL (ADR-007) and the executable cannot be a shell string; `--` makes an argument that looks like a flag stay an argument. Terminal output is untrusted: the human mode writes it unchanged, exactly as a native run would, and ADR-011 already says programs should use `--json`. Error text passes through `terminalSafe`. The client prints Mio's rejection text, which is fixed text from Mio, not output.

**Smoke test with the real binaries and a scripted Podman:**

| Case | Result |
|---|---|
| `scopra run -- echo hello` | `hello from the sandbox` on stdout, the warning on stderr, exit 0 |
| `--json` | one `started`, three `output` (split at 32768), one `result`; base64url exact; exit 0 |
| a command that exits 3 | exit 3, no error line |
| `--write` | `ro=false` in the mount |
| `--workdir ../etc` | exit 2, the ADR-007 reason and the usage |
| no `--` | exit 2, "run needs -- before the command" |
| Mio without `--sandbox-image` | `error: containment_unavailable: no sandbox image is configured; start Mio with --sandbox-image`, exit 1 |
| Ctrl-C during a run | exit 130, no error line; the run's context ended |

**Verification, all passing** (Go 1.26.9, the pinned toolchain):

- `make lint` and `make security-test`;
- `go test -race -count=1 -timeout 120s ./...`, twice;
- `CGO_ENABLED=0 go test ./...`, three times;
- build, vet, and test-compile for darwin/arm64, darwin/amd64, windows/amd64, and linux/arm64;
- `git diff --check`, no skipped tests, and a regenerated tree.

Coverage: `cmd/scopra` 91.9% (the new `run.go` functions are 85–100%), `internal/localipc` 85.3%, `internal/execution` 100.0%, `internal/hollow` 98.0%, `cmd/scoprad` 73.1%.

Commit `12bd706` (`feat(cli): add scopra run`) holds the implementation and the documentation updates, `a9d9564` records this log, `b4c0904` adds the `cmd/scopra/README.md` section, and `c244e01` corrects the ADR index's ADR-010 status summary. All were pushed to `arena/fe74ddf7-scopra` (`7808799..c244e01`), so PR #3 picks them up; the PR's title and body were updated to cover increments 2 and 3. CI is out of scope, as recorded in the Baseline section.

### 2026-10-17 — Step 30: `scopra doctor` containment readiness

- Selection: the maintainer replied "continue" to the proposal after Step 29. Real-host verification (ADR-011 increment 4) still cannot run here (no Podman, no cgroups), so this step makes the most likely runtime refusal — `containment_unavailable` — visible before a run.
- Shape: a new read-only `containment` check in `scopra doctor`, after `daemon`:
  - `podman` is resolved with `exec.LookPath`, exactly as Mio resolves it once at startup;
  - the ADR-010 preflight is reused through `hollow.ParsePodmanInfo` and `PodmanInfo.Check`: Podman 4.0 or later, rootless, cgroups v2, and the cpu, memory, and pids controllers;
  - with `--sandbox-image`, `hollow.ValidateImage` checks the pinned-digest rule and `podman image exists` checks local storage. Mio never pulls, so a missing image is a finding, not something doctor fixes;
  - every helper call gets a 10-second deadline; doctor never pulls, starts, or changes anything, and still contacts no new endpoint.
- Statuses, so that a healthy host is not noisy and a broken execution setup is not hidden:
  - no `podman` → `warn`: status, workspaces, and Mio still work, but every run is refused;
  - `podman info` fails, or the host fails ADR-010 → `fail`: containment exists but cannot work;
  - no `--sandbox-image` → `warn`: the host qualifies, but the image is unknown and Mio refuses every run without one;
  - an image that is not pinned by digest, or is not in local storage → `fail`: the configured setup cannot run;
  - everything present → `pass`.
- `--sandbox-image` is doctor's own flag and does not contact Mio; an invalid value is reported as a failing check rather than a usage error, so the report explains it.
- `containment` leaves `notChecked` (now enrollment, relay, policy, and execution remain); the closing line changes from "does not mean Scopra can authorize or execute commands" to "does not mean Scopra has authorized anything or run a command", which is what the check can and cannot say.
- No schema change: `schemaVersion` stays 1; checks and fields may be added, as documented.

#### Acceptance checklist (agreed before implementation)

- [x] The `containment` check runs after `daemon` and is skipped when the platform check fails; its name is `containment` in text and JSON.
- [x] It reuses the ADR-010 preflight and image rule (`hollow.ParsePodmanInfo`, `PodmanInfo.Check`, `hollow.ValidateImage`) instead of duplicating them, and bounds each `podman` call with a deadline.
- [x] Statuses and hints are as above; every `warn` and `fail` names what to do next.
- [x] It is read-only: only `podman info --format json` and `podman image exists IMAGE`; no pull, run, rm, ps, or socket request beyond the existing `daemon` check; it changes no file and creates no state.
- [x] `scopra doctor` accepts `--sandbox-image`; an invalid value is a failing check, not a usage error; positional arguments are still refused.
- [x] Tests are hermetic: the package controls `PATH`, and a fake `podman` (the test binary, as in `cmd/scoprad`) scripts every status. A table covers all statuses, the exact calls are asserted, the deadline is tested, and the unsupported-platform skip is covered.
- [x] Existing doctor expectations are updated for a host without `podman` (`containment=warn`, overall `warn`), and no test depends on the host's real Podman.
- [x] `containment` is removed from `notChecked`; the closing line is reworded; ADR-010, ADR-011, `cmd/scopra/README.md`, the root README, the plan, and the system overview are updated; `docs/repository-tree.txt` is regenerated; `make security-test` covers the read-only test.

Non-goals: running a container, pulling an image, installing Podman, probing Mio's configured image (doctor does not ask Mio), and real-host verification (ADR-011 increment 4).

#### Test-first record

Tests added before the implementation:

- `cmd/scopra/containment_test.go`: the package `TestMain`, a fake `podman` built from the test binary (the pattern `cmd/scoprad` already uses), and eight tests:
  - a ready host, with the exact podman calls asserted;
  - a status table with eleven cases: no podman, `podman info` failing, invalid `info` output, rootful Podman, cgroups v1, an undelegated controller, Podman 3.4, no image, an unpinned image, a missing image, and a failing image query;
  - a read-only security test that allows only `info --format json` and `image exists` and proves the state directory is unchanged;
  - a deadline test with a fake that sleeps for five seconds.
- `cmd/scopra/doctor_test.go`: every existing expectation gained the `containment` check, the `notChecked` list lost `containment`, and the human report test now checks the reworded closing line.

Red run: `go test ./cmd/scopra/` failed to build on `undefined: containmentHelperTimeout` — the tests were in place and the check was not. After the implementation, three subtests failed and all three were test-setup errors, not code errors:

- the warn cases used a private state directory without Mio, so the `daemon` check failed and doctor exited 1; they now start a status daemon, as the other doctor tests do;
- the unpinned-image case had no fake podman, so the missing-podman warning pre-empted the image rule; it now uses a healthy fake host.

#### Implementation and verification record

- **`cmd/scopra/containment.go`:** `diagnoseContainment` and `runPodmanHelper`, with a 10-second `containmentHelperTimeout`.
  - `exec.LookPath("podman")` mirrors Mio's one-time resolution; nothing is cached or installed.
  - The host check reuses `hollow.ParsePodmanInfo` and `PodmanInfo.Check`, so doctor and Mio cannot disagree about ADR-010's requirements.
  - The image check reuses `hollow.ValidateImage` and then runs `podman image exists`; exit code 1 is the documented "not present" answer, any other failure is reported as such.
  - Every helper call runs under a deadline derived from doctor's context, with `WaitDelay`, so a hung Podman cannot stall the report.
- **`cmd/scopra/doctor.go`:**
  - `--sandbox-image` is doctor's own flag; an invalid value is reported by the check, so the report explains it instead of a usage error.
  - `diagnose` gained the image parameter and appends `containment` after `daemon`; it is skipped when the platform check fails, because the rest of doctor is Linux-only too.
  - `containment` left `notChecked`; the closing line now says a pass does not mean Scopra "has authorized anything or run a command".
  - `schemaVersion` stays 1: a new check is an additive change, as the README already promises.
- **Test harness:** `TestMain` points `PATH` at a private empty directory, so no doctor test can see the host's Podman; `startFakePodman` prepends a directory whose `podman` is the test binary and sets `SCOPRA_DOCTOR_FAKE_PODMAN` plus `GORACE=atexit_sleep_ms=0`, as `cmd/scoprad` does.

**Mutation testing** (sequential, against `containment.go` and `doctor.go`): 19 mutations, all caught. The first round caught 14. Two survived and both exposed missing tests:

- a parse error from `podman info` with exit status 0 was never exercised, so a mutant that ignored it went unnoticed; an invalid-output case now covers it;
- the reworded closing line was not asserted; the human test now requires it and rejects the old wording.

Three mutants failed to build and were rewritten to compile (an unused variable or import), after which all three were caught.

**Security review.** The check adds no authority and no write:
- It runs exactly two read-only Podman queries, and a test asserts the exact calls;
- it never pulls, starts, removes, or inspects a container, and never touches the registry, the state directory, or a workspace;
- it does not ask Mio for anything new, so the status reply and the IPC surface are unchanged;
- Podman's output is untrusted input: `ParsePodmanInfo` bounds it at 1 MiB, requires exactly one object of the expected shape, and validation fails closed;
- the report escapes paths and error text with `terminalSafe` in human mode, and JSON encoding handles it in `--json` mode;
- a missing or broken Podman is a finding, never a reason to fall back to anything.

Residual risk: doctor checks the image it is given, not the one Mio is running with, so a mismatch stays possible; the hint says to pass the same `--sandbox-image`. The check is also a point-in-time observation; Mio re-runs the same preflight before every run.

**Smoke test with the real `scopra` binary and a scripted Podman:**

| Case | Result |
|---|---|
| ready host, image present | `[pass] containment: rootless Podman 5.2.0 with cgroups v2 and the cpu, memory, and pids controllers delegated; the sandbox image is in local storage` (doctor still exits 1 because Mio is not running) |
| image missing | `containment=fail`, "not in local storage, and Mio never pulls images", with the pull hint |
| `podman info` fails | `containment=fail`, "podman info failed: exit status 125" |
| no podman on `PATH` | `containment=warn`, "the podman command was not found" |
| unpinned image | `containment=fail`, the ADR-010 digest rule quoted in the detail |
| calls observed | only `info --format json` and `image exists IMAGE` |

**Verification, all passing** (Go 1.26.9): `make lint`, `make test`, `make security-test`, `CGO_ENABLED=0 go test ./...`, and build, vet, and test-compile for darwin/arm64, darwin/amd64, windows/amd64, and linux/arm64; `gofmt -l` and `git diff --check` are clean.

Coverage: `cmd/scopra` 92.9% in total; `diagnoseContainment` is 100% and `runPodmanHelper` 85.7% (the nil-context guard is unused in practice).

Commit `556f74f` (`feat(doctor): check containment readiness`) holds the implementation, the tests, and the documentation; a second commit records this log. Both were pushed to `arena/fe74ddf7-scopra`, so PR #3 picks them up. CI remains out of scope.

### 2026-10-17 — Step 31: `scopra service` (systemd user service)

- Selection: the maintainer replied "Continue" to the proposal after Step 30. ADR-010 deferred where a durable sandbox image setting lives to "the Linux service installation", so this step adds the service and settles that question in a new ADR-012.
- Shape:
  - `scopra service install [--state-dir PATH] [--sandbox-image IMAGE] [--scoprad PATH] [--force]` writes `$XDG_CONFIG_HOME/systemd/user/scoprad.service` (`~/.config/systemd/user/scoprad.service` by default), then runs `systemctl --user daemon-reload` and `systemctl --user enable --now scoprad.service`.
  - `scopra service uninstall` runs `systemctl --user disable --now scoprad.service`, removes the unit, and reloads.
  - `scopra service status [--json]` is read-only: it reports whether the unit is installed, whether Scopra wrote it, and systemd's `is-enabled` and `is-active` answers.
- Decisions:
  - **The unit is the durable configuration.** `ExecStart` holds the absolute `scoprad` path, an explicit `--state-dir`, and `--sandbox-image` when one is given. No new config file format, no environment variables, and no change to `scoprad`; changing the image means reinstalling. The file is visible and auditable.
  - **A user service, never root.** `WantedBy=default.target`, `Type=simple`, `Restart=on-failure` with `RestartSec=2` and a start limit, and `UMask=0077`. The unit deliberately omits `NoNewPrivileges`, `ProtectHome`, and `ProtectSystem`: rootless Podman needs its setuid helpers and the workspace under the home directory. The ADR records why.
  - **Escaped arguments.** Every `ExecStart` argument is escaped as systemd reads it: `%` → `%%`, `$` → `$$`, and quoting with backslash escapes for whitespace, quotes, and backslashes; control characters are refused. A test pins the exact line for a hostile path.
  - **Fail closed, no half-install.** The state directory is resolved exactly as `scoprad` resolves it; `systemctl` and the `scoprad` executable are resolved before anything is written; the directory chain is checked (real directories, not group/other writable); the unit is written atomically with mode 0600. If `daemon-reload` or `enable --now` fails, the previous file content (or absence) is restored and the error is reported.
  - **Recognition, not ownership.** The first line marks the file as Scopra's. `install` and `uninstall` refuse a different unit at that path; `install --force` replaces it. Repeating either command when already in the requested state succeeds.
  - **No shell.** Every `systemctl` call is a fixed argument vector with a 30-second deadline, like Hollow's helper calls.
  - **Linux only, for now.** Other platforms fail closed with a clear error and still cross-compile.
- Status JSON (`schemaVersion` 1): `{"schemaVersion":1,"unitPath":"...","installed":true,"managed":true,"enabled":"enabled","active":"active"}`; `installed:false` omits `managed`, `enabled`, and `active`. Exit 0 when the queries succeed, even if the service is stopped or failed.

#### Acceptance checklist (agreed before implementation)

- [x] `service install`, `service uninstall`, and `service status` exist and are listed in the root usage; unknown subcommands and stray arguments are usage errors (exit 2).
- [x] Install resolves the state directory with `localipc.ResolveStateDir`, refuses a non-executable or missing `scoprad`, refuses a non-pinned image as a usage error, and resolves `systemctl` before writing anything.
- [x] The unit path is `$XDG_CONFIG_HOME/systemd/user/scoprad.service` when `XDG_CONFIG_HOME` is absolute, otherwise `$HOME/.config/systemd/user/scoprad.service`; the directory chain is created owner-only and checked to be real directories that are not group/other writable; the unit is a regular file with mode 0600, written by temp file and rename.
- [x] Unit content is fixed and tested: marker line, `Description`, `StartLimitIntervalSec`/`StartLimitBurst`, `Type=simple`, the escaped `ExecStart`, `Restart=on-failure`, `RestartSec=2`, `UMask=0077`, `WantedBy=default.target`.
- [x] Install runs `daemon-reload` then `enable --now scoprad.service`; a failure in either restores the previous unit content (or removes the new file), reloads again, and returns an error.
- [x] A unit without Scopra's marker is never overwritten without `--force` and never disabled or removed by `uninstall`; both commands are idempotent when already in the requested state.
- [x] Status is read-only: it reads the unit file and runs only `systemctl --user is-enabled` and `is-active`; it reports installed/managed/enabled/active, supports `--json` schema 1, and exits 0 when the queries succeed.
- [x] Every `systemctl` call uses a fixed argument vector (no shell), a bounded deadline, and no added environment; paths and errors are escaped for the terminal.
- [x] Non-Linux returns a clear unsupported error; the tree still builds and vets for darwin, windows, and linux/arm64.
- [x] Tests use a fake `systemctl` (the test binary), cover exact unit content and escaping, rollback on both systemctl failures, foreign-unit refusal, idempotence, status shapes, missing systemctl, insecure directories, a symlinked unit, and the read-only status property.
- [x] ADR-012 records the decision; ADR-010's deferred durable setting is resolved; READMEs, the plan, the system overview, the tree, and this log are updated; `make security-test` covers the new security tests.

Non-goals: system (root) services, lingering, socket activation, auto-update, a new configuration file, changing the image without reinstalling, and verifying against a real systemd on this sandbox.

#### Test-first record

The tests were written first (`cmd/scopra/service_test.go`), and the implementation (`service.go`, `service_linux.go`) second. The first execution against the implementation found one expectation to correct, and later the red run was recorded by removing the implementation:

- `relative state dir` was initially expected to be a usage error. It is not: `localipc.ResolveStateDir` deliberately turns a relative `--state-dir` into an absolute one with `filepath.Abs`, so the case was removed rather than making the CLI stricter than the daemon. The acceptance item asks that install resolve the path exactly as `scoprad` does, and it does.
- `TestSystemdExecArg/tabs_and_separators` initially expected a tab to be quoted. `systemdExecArg` refuses every control character, tab included; the case moved to the refusal table. The rule is simpler and safer than special-casing tab.
- `TestServiceInstallForceReplacesAForeignUnit` first expected the refusal to say "not managed by scopra"; the message is "was not written by Scopra; remove it yourself or pass --force". The assertion follows the message.
- `TestServiceInstallReportsAnUnwritableDirectory` first passed 0500 to `MkdirAll`, which cannot create its own parents; it now creates the chain 0700 and chmods only the leaf before installing.

Recorded red run (implementation moved aside, tests left in place):

```text
# github.com/SynergyFlowLabs/scopra/cmd/scopra [github.com/SynergyFlowLabs/scopra/cmd/scopra.test]
cmd/scopra/main.go:50:10: undefined: runService
cmd/scopra/service_test.go:463:14: undefined: serviceStatusJSON
cmd/scopra/service_test.go:485:14: undefined: serviceStatusJSON
cmd/scopra/service_test.go:513:14: undefined: serviceStatusJSON
cmd/scopra/service_test.go:625:16: undefined: systemdExecArg
cmd/scopra/service_test.go:638:17: undefined: systemdExecArg
cmd/scopra/service_test.go:754:13: undefined: serviceStatusJSON
FAIL	github.com/SynergyFlowLabs/scopra/cmd/scopra [build failed]
```

Restoring the two files makes the same selection pass. The suite is 25 test functions (50 test and subtest cases), including a scripted fake `systemctl` (this test binary, dispatched from `TestMain`) whose every call is appended to `calls.log`, and whose `is-enabled`, `is-active`, `daemon-reload`, `enable`, `disable`, and each failure mode are controlled by marker files (`enabled`, `active`, `failed`, `fail-daemon-reload`, `fail-enable`, `fail-disable`, `silent-is-enabled`).

#### Implementation and verification record

Files: `cmd/scopra/service.go` (dispatch, install, uninstall, status, unit rendering, escaping, directory checks, bounded `systemctl`), `cmd/scopra/service_linux.go` (Linux platform and ownership predicates), `cmd/scopra/service_other.go` (fail-closed other platforms), `cmd/scopra/service_test.go`; `cmd/scopra/main.go` gains the `service` dispatch and three usage lines; `cmd/scopra/containment_test.go` `TestMain` dispatches to the fake `systemctl` when `SCOPRA_FAKE_SYSTEMCTL` is set.

Behavior as implemented:

- `install`: validates and resolves everything that can be checked (flag set with no host-derived defaults, `hollow.ValidateImage`, `localipc.ResolveStateDir`, `systemctl` on `PATH`, an executable `scoprad` given explicitly, then `PATH`, then beside `scopra`) before creating anything. Then it creates the unit directory owner-only, checks every component is a real directory that is not group/other writable (root-owned sticky directories such as `/tmp` are allowed) and that the leaf is owned by the effective user, refuses a non-regular or foreign unit without `--force`, writes by temp file + `fsync` + rename with mode 0600, and runs `--user daemon-reload` then `--user enable --now scoprad.service`. If either fails, it restores the previous content (or removes the new file) and reloads again before returning the error.
- `uninstall`: absent unit prints `Scopra Mio is not installed.` and exits 0; a foreign unit is refused; otherwise `--user disable --now scoprad.service`, remove the file, `--user daemon-reload`. A failed disable leaves the unit in place.
- `status`: read-only; it `Lstat`s the unit and, when present, runs only `--user is-enabled` and `--user is-active`. A non-zero exit with some output is an answer (`disabled`, `inactive`, `failed`); a non-zero exit with no output is an error. `--json` writes schema 1 on one line, omitting `managed`, `enabled`, and `active` when the unit is absent.
- `systemdExecArg`: refuses empty values and control characters, doubles `%` and `$`, and double-quotes with backslash escapes when whitespace, quotes, or backslashes remain. The unit passes no environment.
- Every `systemctl` call is a fixed argument vector with a 30-second deadline; there is no shell.

Verification (Go 1.26.9, `GORACE=atexit_sleep_ms=0` for the fake daemons):

- `make lint`: `gofmt -l` empty and `go vet ./...` clean.
- `make test`: every package passes with `-race`, including `cmd/scopra`.
- `make security-test`: every package passes with `-race`, including `TestSecurity_ServiceRefusesInsecureDirectories` (group/world-writable ancestor, symlinked unit, symlinked directory, a unit that is a directory).
- `go test -cover ./cmd/scopra`: 89.2% overall; the new code ranges from 50.0% (`resolveScoprad`, whose "beside `scopra`" fallback is unreachable without writing next to the test binary) to 100% (`runService`, `resolveSystemctl`, `systemdExecArg`, `runSystemctl`, `restoreServiceUnit`, `isManagedServiceUnit`, `ownedServiceDir`).
- Cross-checks: `go build` and `go vet` pass for darwin/amd64, windows/amd64, and linux/arm64, so `service_other.go` compiles and vets everywhere.
- Mutation testing: 30/30 mutants caught. Mutants covered unit name, marker, `ExecStart` contents, `%`/`$` doubling, quoting, control-character refusal, start limit, `UMask`, `WantedBy`, file mode, both rollbacks, reload-before-enable ordering, `--now`, `--force`, foreign-unit refusal, ignored directory check, `Lstat`→`Stat`, uninstall's foreign-unit refusal, disable failure, remove-before-disable, status queries, status's empty-answer rule, image validation, `scoprad` executable check, relative `$XDG_CONFIG_HOME`, `systemctl` resolution, and the root-owned sticky rule. Files were byte-identical before and after (`sha256 service.go 67cc1a95…bcca4`, `service_linux.go 6156980a…644dc`).
- Real-binary smoke (`/tmp/smoke/bin/scopra`, a shell-script fake `systemctl` on `PATH`, a temporary `HOME`): install printed the unit path and wrote the exact unit with mode 0600; `status --json` printed schema 1 with `enabled`/`active`; `status` printed `installed (managed by scopra)`, `Enabled: enabled`, `Active: active`; a second install succeeded; uninstall printed `Removed …; Scopra Mio is stopped and disabled.`; status then printed `not installed` and `installed:false` JSON without `managed`/`enabled`/`active`. The fake's call log was exactly `--user daemon-reload`, `--user enable --now scoprad.service`, `--user is-enabled scoprad.service`, `--user is-active scoprad.service` (twice), `--user daemon-reload`, `--user enable --now scoprad.service`, `--user disable --now scoprad.service`, `--user daemon-reload`.
- Not verified: a real systemd user manager and a real rootless Podman host, neither of which exists in this sandbox. The fakes script systemd's answers; they cannot prove its parsing. This is recorded, with the real-host check, as an open item rather than claimed.

Commit `9bc6081` (`feat(service): install Mio as a systemd user service`) holds the implementation, the tests, ADR-012, and the documentation; a second commit records this log. Both were pushed to `arena/fe74ddf7-scopra`, so PR #3 picks them up. CI remains out of scope.
### 2026-10-17 — Step 32: run journal and `scopra logs` (ADR-013)

- Selection: the maintainer replied "Continue" to the proposal after Step 31. The plan's Milestone 1 names "local execution, status, logs, cancellation": cancellation and status exist, logs do not. This step adds a bounded, metadata-only run journal and a read-only way to read it.
- Shape:
  - Mio appends one metadata-only record per answered run to `$STATE/scopra/runs.jsonl`: execution ID, workspace ID, state, recorded/started/ended times, duration, exit code, stdout/stderr byte counts, truncation flag, chunk count, and error code. It stores no command, arguments, reason, path, or output.
  - `scopra logs [--state-dir PATH] [--workspace ID] [--limit N] [--json]` reads the journal through a new read-only IPC command, `execution.logs`; `--limit` is 1–100 (default 20) and entries are newest first.
  - The journal is bounded to 1000 entries and 1 MiB. When either bound would be exceeded the oldest entries are dropped by writing a temp file and renaming it; the file is owner-only 0600 and must be a regular file, not a symlink.
  - Mio refuses to start when the journal cannot be opened or validated, so it never runs anything it cannot record. If an append fails later, the result the client already saw is left alone, the journal is marked unusable, and every later run is answered with an `errored`/`internal` result without executing.
- Decisions:
  - **Metadata only.** Output and command lines are exactly what a log would leak, so this increment records neither. The count, truncation flag, and chunk count still answer "did it produce output, and how much". A later ADR can add bounded output capture with its own retention and redaction rules.
  - **Journal at the IPC boundary.** Every result Mio sends is journaled where it is sent, so refusals (unknown workspace, missing containment, busy) are recorded too, and there is one place to audit. Runs that never receive a result (malformed requests, Mio crashes) are not journaled.
  - **Fail closed before running.** The journal is opened and validated before Mio serves. A later write failure cannot undo a finished run, so it is handled at the next boundary: the journal is marked unusable and later runs are refused without executing.
  - **Only newline-terminated lines are committed.** A crash can leave a partial final line; Mio discards the unterminated tail at startup. Any other malformed line is corruption and refuses startup.
  - **The read command is read-only and bounded.** `execution.logs` changes nothing, filters by workspace ID, caps the reply at 100 entries, and uses the same envelope, key checking, and error-reply rules as the workspace commands.
  - **No new protocol code and no new ADR-009 state.** Busy refusals keep `busy`; an unusable journal is an `errored` result with `internal`, which already means "Mio failed" (ADR-009).
- Status JSON and text: `scopra logs --json` prints exactly one object, `{"schemaVersion":1,"entries":[...]}`; each entry is the stored record. Human output prints one line per entry with the recorded time, state, exit or error detail, stream byte counts, workspace ID, and execution ID.
- Non-goals: capturing stdout/stderr (deferred), retention policies beyond the fixed bounds, a delete/export command, remote access, journaling runs that never got a result, and changing ADR-009's result codes.

#### Acceptance checklist (agreed before implementation)

- [x] `scopra logs [--state-dir PATH] [--workspace ID] [--limit N] [--json]` exists and is listed in the root usage; unknown flags, stray arguments, an invalid workspace ID, and a limit outside 1–100 are usage errors (exit 2); the default limit is 20.
- [x] Mio records one metadata-only entry per answered run, including rejections and busy refusals: execution ID, workspace ID, state, recorded/started/ended times, duration, exit code, stdout/stderr bytes, truncation flag, chunk count, and error code. It stores no command, arguments, reason, path, output, or user identity.
- [x] The journal is bounded to 1000 entries and 1 MiB; when either bound would be exceeded the oldest entries are dropped by an atomic temp-file rewrite. The file is a regular file owned by the user with mode 0600 and is never a symlink.
- [x] Opening the journal validates every committed line, discards a torn (unterminated) final line, and refuses any other corruption; Mio refuses to start when the journal cannot be opened or validated.
- [x] If an append fails after a run, the result already sent is unchanged, the journal becomes unusable, and every later run is answered with an `errored`/`internal` result without executing.
- [x] `execution.logs` is a read-only IPC command: request `{protocolVersion, requestId, command, workspaceId, limit}`, reply `{protocolVersion, requestId, entries}`; keys and values are strictly checked, entries are newest first, the workspace filter applies, and an unreadable journal gets an error reply.
- [x] `scopra logs` prints one line per entry (newest first) with recorded time, state, exit code or error code, stdout/stderr bytes, workspace ID, and execution ID; an empty journal prints a clear message; text is terminal-escaped.
- [x] `scopra logs --json` prints exactly one object `{"schemaVersion":1,"entries":[...]}` with the same newest-first order and fields as stored.
- [x] Tests cover entry encoding and validation, bounds and rotation, torn tails, permissions and symlinks, an unusable journal, IPC request/reply validation, the daemon and client paths, and CLI flags/output; no real Podman or systemd is needed.
- [x] Mutation testing of the journal and logs path catches every mutant; ADR-013 records the decision and its security considerations; ADR-011 and ADR-009 references, READMEs, the plan, system overview, tree, and this log are updated; `make security-test` covers the new security tests.
- [x] `make lint`, `make test`, `make security-test`, the cross-platform build and vet, and a real-binary smoke test with a fake sandbox all pass.

#### Test-first record

Tests were written before the implementation, in this order: `internal/journal/journal_test.go` (the entry contract, the file contract, and the bounds), `internal/localipc/logs_test.go` (wire request and reply validation, client pre-checks), `internal/localipc/logs_linux_test.go` (a daemon with a fake journal, result recording, busy refusals, a broken journal), and `cmd/scopra/logs_test.go` (flags, text lines, JSON, empty output, filtering, terminal safety). Only then were `internal/journal/journal.go`, `journal_linux.go`, `journal_other.go`, `internal/localipc/logs.go`, and `cmd/scopra/logs.go` written.

Recorded red run, captured by moving those five implementation files aside and leaving the tests in place (`sha256sum -c` confirmed every file was restored byte for byte):

```text
github.com/SynergyFlowLabs/scopra/internal/journal: no non-test Go files in /home/user/scopra/internal/journal
# github.com/SynergyFlowLabs/scopra/internal/journal [github.com/SynergyFlowLabs/scopra/internal/journal.test]
internal/journal/journal_test.go:51:61: undefined: Entry
internal/journal/journal_test.go:53:16: undefined: NewEntry
internal/journal/journal_test.go:67:18: undefined: NewEntry
internal/journal/journal_test.go:71:30: undefined: SchemaVersion
internal/journal/journal_test.go:157:17: undefined: NewEntry
internal/journal/journal_test.go:187:20: undefined: DecodeEntry
FAIL    github.com/SynergyFlowLabs/scopra/internal/journal [build failed]
# github.com/SynergyFlowLabs/scopra/internal/localipc
internal/localipc/socket.go:62:16: undefined: RunJournal
internal/localipc/socket.go:499:7: undefined: logsCommand
internal/localipc/socket.go:500:12: d.replyToLogsCommand undefined (type *Daemon has no field or method replyToLogsCommand)
internal/localipc/executions.go:94:65: undefined: RunJournal
internal/localipc/executions.go:343:81: undefined: journalUnavailableMessage
FAIL    github.com/SynergyFlowLabs/scopra/internal/localipc [build failed]
```

`cmd/scopra` failed the same way on `undefined: runLogs`, behind the other two packages' errors.

Two existing suites changed with the contract, so they were extended before the code existed: `internal/localipc/executions_linux_test.go` gained a `fakeRunJournal` and calls `EnableExecutions` with it, and `cmd/scopra/run_test.go` opens a real journal in each harness so the existing run tests exercise the recording path.

Corrections found by the red/green cycles, kept as test or design fixes rather than code changes:

- The decode table's `bad workspace ID` case replaced the execution ID first, because `testWorkspaceID` and the test request ID are the same string. It now replaces the complete `"workspaceId":"…"` pair.
- A `truncated null` decode case was removed: `json.Unmarshal` accepts `null` for a `bool` without an error, so the case could not fail. The object-shape rules are covered by the surrounding-key, missing-key, and trailing-data cases instead.
- `journal.NewEntry` rejects `failed` with exit code 0 (ADR-009 requires a failing code), so the fake-journal helpers use exit code 1 for a failed result.
- The filter test in `logs_linux_test.go` needed a second workspace ID that differs from `testWorkspaceID`; both were identical at first.
- The IPC request's unused-field rule means `workspaceId` and `limit` had to be added to the request struct, the exact key list, the encoder, the decoder, and the validator together; adding only the fields produced no reply.
- Three build/vet corrections: a duplicate `ValidateRun` was removed (`executions.go` already owns it), an unused `time` import was dropped from a test, and `logsSchemaVersion` was declared once instead of in both the CLI and its test.

#### Implementation and verification record

**Journal (`internal/journal`).** One file, `<state-dir>/runs.jsonl`. The proposal wrote `$STATE/scopra/runs.jsonl`; the default state directory already ends in `scopra`, so the implemented path is `<state-dir>/runs.jsonl` (`~/.local/state/scopra/runs.jsonl` by default).

- `Entry` carries exactly the fourteen stored fields: schema version, execution and workspace ID, state, recorded/started/ended times, duration, exit code, stdout/stderr byte counts, truncation flag, chunk count, and error code. `NewEntry` copies only those fields from a result, so a command, arguments, reason, path, or output cannot reach the file by construction.
- `Entry.Validate` mirrors ADR-009: `succeeded` needs times and exit 0; `failed` needs times and 1–255; `timed_out`, `cancelled`, and `out_of_memory` need times and no exit code; `errored` needs an error code; `rejected` needs an error code and has no output counts. A rejected run's output counts must all be zero. Byte counts must fit the chunk count (`chunks ≤ stdoutBytes + stderrBytes ≤ chunks × 64 KiB`), each count is at most 2^30, and timestamps are UTC with exactly millisecond precision.
- `MarshalJSON` validates first and emits the stored key order. `DecodeEntry` accepts valid UTF-8, exactly the fourteen keys, no duplicates, no unknown or incorrectly-cased keys, no trailing data, and then re-applies every `Validate` rule. Entry size is bounded at 8 KiB before parsing.
- `Open` requires an absolute real state directory, creates the file with `O_EXCL` and explicitly `chmod 0600` (the umask would otherwise mask it), refuses a non-regular file, a symlink, a foreign owner, or a mode other than `0600`, and re-checks the opened inode with `os.SameFile`. It then discards an unterminated final line, `fsync`s, and validates every committed line; any other malformed line refuses startup.
- `Append` writes one line and `fsync`s it. When the 1000-entry or 1 MiB bound would be exceeded, `compactLocked` keeps the newest entries that fit, `rewriteLocked` writes a `0600` temp file in the same directory, `fsync`s, renames over the journal, `fsync`s the directory, reopens, and re-validates the new file as owner-only. A failure at any point marks the journal broken.
- `Entries(workspaceID, limit)` reads the whole file under the 1 MiB bound, drops a torn tail, parses strictly, filters by workspace ID, and returns newest first. It does not mark the journal broken: a read failure is not a write failure.
- `Broken` reports the sticky write failure. `Close` is idempotent. `journal_linux.go` provides the platform flag and owner check (`syscall.Stat_t.Uid`); `journal_other.go` fails closed.

**Daemon and IPC (`internal/localipc`, `cmd/scoprad`).**

- `execution.logs` was added to the request struct, the exact-key list, the encoder, the decoder, and the validator: `workspaceId` is optional and must be 32 lowercase hexadecimal characters, `limit` is required and 1–100. The reply is `{protocolVersion, requestId, entries}` and is decoded with the existing strict envelope rules; an unreadable journal becomes a typed `ReplyError` with the journal's message.
- `RunJournal` is a small interface (`Append`, `Entries`, `Broken`) so the daemon tests can use a fake and the journal's own tests can cover the file.
- `Daemon.EnableExecutions(executor, runJournal)` now requires both; the reply switch serves `execution.logs` only when they were enabled. `replyToLogsCommand` answers without touching anything.
- `cmd/scoprad` opens the journal after the workspace registry and before serving. A failure exits 1, so Mio never serves runs it cannot record. `recordAndSend` appends every answered result — normal results, rejections, and busy refusals — before sending it; an append error is deliberately ignored for that result, because it has already been computed and the client must see the truth. The journal's sticky broken flag then answers every later run with an `errored`/`internal` result and the fixed message "Mio could not record its run journal", without executing.
- `localipc.WorkspaceError` became `localipc.ReplyError`, because the logs reply now uses the same typed error and its name no longer described only workspace replies. All call sites and comments were updated; the wire shape did not change.

**CLI (`cmd/scopra`).** `scopra logs [--state-dir PATH] [--workspace ID] [--limit N] [--json]` parses and checks its flags before contacting Mio: default limit 20, 1–100, a 32-hex workspace ID, no positional arguments. Text output is one line per entry, newest first, ending with the workspace and execution IDs; rejected and errored entries show `error <code>`, entries without times show `duration -`, and every line passes through `terminalSafe`. JSON output is exactly one object with `schemaVersion` 1 and `entries` always an array. The empty messages are `No runs have been recorded.` and `No runs have been recorded for workspace <id>.` Usage errors exit 2, an unavailable or refusing Mio exits 1, and interruption exits 130.

**Verification.**

- `make lint`: `gofmt -l` empty and `go vet ./...` clean.
- `make test` (`go test -race ./...`): all ten packages pass.
- `make security-test` now includes `./internal/journal`; every listed package passes with `-race`. The new security tests are `TestSecurity_JournalRefusesASymlink`, `TestSecurity_JournalRefusesUnsafeMode`, and `TestSecurity_LogsRejectsMalformedRequestsAndStaysReadOnly` (malformed logs requests are dropped with no journal or file change, and the existing entry is still readable).
- Cross-platform: `go build` and `go vet` pass for darwin/amd64, windows/amd64, and linux/arm64, so `journal_other.go` compiles and vets everywhere.
- Coverage over the three touched packages (total 86.6%): `internal/journal` 81.0%, `internal/localipc` 85.8%, `cmd/scopra` 89.9%. Every function in `internal/localipc/logs.go` is 100%, as are `parseLogsOptions` and `formatLogEntry`; `runLogs` is 90.9%, and the journal's remaining gaps are I/O-failure paths such as `rewriteLocked` (61.1%) and `openJournalFile` (62.5%).
- Mutation testing: 54 mutants across `internal/journal/journal.go`, `internal/localipc/{logs,messages,executions}.go`, and `cmd/scopra/logs.go`; 51 were caught and 3 survived, all three proven equivalent:
  - removing the explicit trailing-data check in `checkEntryKeys` is masked by `json.Unmarshal`, which also rejects a second top-level value;
  - removing the client's own limit check, or its workspace-ID check, is masked by `encodeRequest`, which re-runs the same validation before dialing. Both layers remain as defence in depth.
  One mutant initially failed to build because it left `info` unused; it was rewritten to compile (`maxBytes * 1024`) and was then caught. Every mutated file was restored byte for byte (`journal.go` `17cf37c5…6eff6`, `localipc/logs.go` `7743b28e…fff`, `cmd/scopra/logs.go` `74098535…e79e`, `executions.go` `b4b121ac…08b9`, `messages.go` `cf2fa0f1…225b`).
- Real-binary smoke on Linux (`/tmp/smoke32`, a shell-script fake `podman` that scripts `info`, `image exists`, `run`, `inspect`, `rm`, `ps`, and `kill`; a temporary `HOME`; state directory mode `0700`):
  - `scoprad --sandbox-image …` started and printed only `scoprad: listening`;
  - `workspace add --json` registered the directory;
  - a successful run printed the fake sandbox's stdout/stderr, exited 0, and was recorded as `succeeded`, exit 0, `stdoutBytes` 23, `stderrBytes` 18, `chunks` 2;
  - a run whose fake container exited 3 made `scopra run` exit 3 and was recorded as `failed`, exit 3;
  - a run with an unregistered workspace exited 1 and was recorded as `rejected`/`workspace_unavailable` with zero output;
  - `scopra logs` printed the three entries newest first (`rejected`, `failed`, `succeeded`), and `scopra logs --json` printed schema 1 with the same fields and order;
  - the journal file was `-rw-------`, contained no copy of the command (`grep -c "echo hello"` was 0), and survived a daemon restart (`logs --limit 10` still returned three lines);
  - `--workspace <absent>` printed the empty message in text and `"entries":[]` in JSON; `--limit 0`, `--limit 101`, `--workspace nope`, and a stray argument each exited 2 with a specific message; `-h` exited 0; a missing daemon exited 1.
- Security review: covered data minimisation (no command, arguments, reason, path, output, or identity beyond the two IDs), file permissions and ownership, symlink and inode-swap refusal, atomic replacement, the 1 MiB/1000-entry/8 KiB/1–100 limits, strict decoding of every stored line (unknown keys, duplicates, trailing data, invalid UTF-8, out-of-range counts), fail-closed startup, the one-run window after a failed append, terminal escaping, and the unchanged same-UID IPC boundary. No high-confidence exploitable issue was found. The documented limits are that the journal is not tamper-evident (the user can edit or delete it, and it is not Catena) and that same-UID processes can read it, as they could read the file.
- Not verified: a real rootless-Podman host and a real systemd user manager, neither of which exists in this sandbox; the fake `podman` scripts its answers and cannot prove parsing of real Podman output. Output capture, retention beyond the fixed bounds, and any remote log access remain out of scope and are recorded as such in ADR-013.

Commit `26590a6` (`feat(journal): record run metadata and add scopra logs`) holds the implementation, the tests, ADR-013, and the documentation; a second commit records this log. The increment was pushed to `arena/fe74ddf7-scopra` as `0294819..26590a6`, so PR #3 picks it up. CI remains out of scope.

### 2026-10-17 — Step 33: MIT license, no pricing model (ADR-014)

- Selection: the maintainer asked that every kind of pricing model be removed and that Scopra stay open source and free for everyone. The repository had no license at all — `LICENSE` said "All rights reserved. No permission is granted until a final license is selected" — so it was not legally open source, and the plan still listed "billing, and plan limits" as Milestone 6 work.
- Decision (ADR-014):
  - **MIT License.** `LICENSE` now carries the standard MIT text, `Copyright (c) 2026 Scopra contributors`. Permissive, with the usual warranty disclaimer; no CLA.
  - **No pricing model.** No paid tiers, subscriptions, usage-based billing, plan limits, feature gates, trials, account requirements, license keys, or entitlement checks. This is a product rule, not only a documentation edit, so it is recorded in the PRD, the plan, the README, and the ADR.
  - **Third-party material keeps its own license.** `NOTICE` records the Podman documentation fixture (Apache-2.0) and the installed development skills under `.agents/skills/`.
- Changes:
  - `LICENSE`: MIT text; `NOTICE`: new, with the no-pricing statement and third-party attributions.
  - `README.md`: a short free/open-source line in the introduction and a `## License` section stating that there is no pricing model.
  - `PRD.md`: product principle 9 ("Free and open source, with no pricing model"), a pricing/billing non-goal, and a `License` header line.
  - `docs/plans/00-full-implementation-plan.md`: Milestone 6 no longer lists "billing, and plan limits"; the technical baseline and header record the MIT/no-pricing position.
  - `CONTRIBUTING.md`: contributions are inbound under the MIT terms.
  - `docs/decisions/014-open-source-no-pricing.md` and its index entry.
- No code, protocol, or test behaviour changed. The removal is of a planned commercial feature, which never had code.
- Verification:
  - `grep -rniE "billing|pricing|paid|subscription|plan limits|feature gate|monetiz|revenue|commercial"` over the repository (excluding `.git` and the third-party `.agents/skills`) returns only the new statements that pricing is absent, the ADR and NOTICE that define the rule, and two historical CI notes in this log that refer to GitHub Actions billing as a diagnostic for `startup_failure`. No product pricing, plan, or billing content remains.
  - `go build ./...`, `go vet ./...`, and `go test -count=1 ./...` still pass; no Go file changed.
  - `docs/repository-tree.txt` regenerated for the new `NOTICE` and ADR files.
- Non-goals: relicensing third-party material, adding a CLA or DCO, hosted-service operations, and deciding whether a future hosted relay needs its own terms. Those are separate questions; the software itself stays free and complete.

### 2026-10-17 — Step 34: public repository and open-source community files

- Selection: the maintainer asked to make the repository public and to bring the README and the community documents up to the standard of a mature open-source project. ADR-014 had already licensed the code MIT; this step makes the project presentable and safe to publish.
- Pre-publication check (read-only, before any visibility change):
  - `git log --all --diff-filter=A --name-only` lists 238 distinct tracked paths, all source, docs, tests, or the packaged skills; no `.env`, key, certificate, or credential file has ever been committed.
  - A regex scan of every added line in all history for AWS keys, GitHub tokens, `sk-` keys, Slack tokens, Google API keys, and PEM private-key blocks found one hit: `AKIAIOSFODNN7EXAMPLE`, which is AWS's documentation placeholder inside `.agents/skills/security-review/references/supply-chain.md`. It is not a credential.
  - `gh api repos/… -f visibility=public` and `gh repo edit --visibility public` both return **HTTP 403 Resource not accessible by integration**, even though the token reports `admin: true` on the repository. The token is a fine-grained token without the repository-administration:write grant, so visibility cannot be changed from this environment. The repository is still `private`.
- Documentation added or rewritten:
  - `README.md`: rewritten in the shape common to mature open-source projects — badges (license, Go version, platform, status), a one-line pitch and blockquote, "Why Scopra", an ASCII architecture diagram with a component/status table, an explicit Status section split into *works today*, *not implemented*, and *not verified*, requirements, quick start with real commands, a command table, the security model with links, a documentation index, project layout, contributing, license, and acknowledgements.
  - `CONTRIBUTING.md`: ways to contribute, required reading, development setup, project conventions (test-first, fail closed, bounded, strict decoding, no pricing machinery), commit-message style, the pull-request process, and the inbound MIT license statement.
  - `CODE_OF_CONDUCT.md`: Contributor Covenant 2.1 with the enforcement ladder, adapted with a security-specific clause and the project's conduct contact.
  - `SECURITY.md`: private reporting through GitHub advisories or email, acknowledgement and disclosure timelines (3/10 days, 90-day disclosure), in-scope and out-of-scope lists, the documented security properties that are not vulnerabilities (same-UID trust, the journal not being audit, the unit omitting hardening), safe harbor, and user hardening.
  - `CHANGELOG.md`: Keep a Changelog format, an `[Unreleased]` section for Steps 31–34, and a `0.0.0` baseline covering the earlier foundation.
  - `.github/`: issue templates for bugs, feature proposals, and security-review requests, plus `config.yml` that disables blank issues and routes vulnerabilities to private reporting; a pull-request template with scope, security impact, testing, and documentation checklists; `dependabot.yml` for the GitHub Actions pins (there are no other dependency ecosystems); and `FUNDING.yml` that deliberately declares no funding, matching ADR-014.
  - `CODEOWNERS`, `.editorconfig`, and `.gitignore` additions for `runs.jsonl` and `.scopra/`, so local run data is never committed.
  - `.github/workflows/ci.yml`: the integrity step now requires the new community documents, and a new step builds both binaries and prints their versions. The workflow content is unchanged in behaviour for the Go checks; CI remains blocked at the account level (`startup_failure`).
- Verification:
  - Every relative link in the README, CONTRIBUTING, SECURITY, CODE_OF_CONDUCT, and CHANGELOG resolves (checked with a script; zero broken links).
  - `make lint` and `go test -count=1 ./...` pass; no Go file changed. `git diff --check` is clean.
  - Both binaries build and print their version.
  - The YAML files parse as expected in structure and contain no tab indentation.
- Blocked: making the repository public requires an owner action in GitHub repository settings, or a token with repository administration write. The exact commands are `gh api -X PATCH repos/SynergyFlowLabs/scopra -F private=false` (or Settings → General → Danger Zone → Change visibility → Make public).

### 2026-10-17 — Step 35: a generated documentation site on Vercel (ADR-015)

- Selection: the maintainer made the repository public and asked for a Vercel deployment "using the best practices for this project". The repository has no web application — `apps/*` and `deploy/*` are placeholder READMEs and the only real artifacts are the two Go binaries and the Markdown. The correct artifact to publish is therefore the documentation itself, generated from the same files reviewers approve.
- What was built:
  - `tools/docsite` — a Markdown-to-HTML generator with no dependencies beyond the Go standard library. It renders ATX headings (with stable, de-duplicated anchors), fenced code, GFM tables with alignment, nested ordered/unordered lists with task boxes, blockquotes, thematic breaks, and inline code, emphasis, strikethrough, links, images, and autolinks. Raw HTML is escaped, never passed through.
  - Link rewriting: a published page becomes its site URL, a fragment becomes an in-page anchor, and anything else (`LICENSE`, `progress.md`, fixtures) becomes a `blob/main` link on GitHub. An unresolvable link fails the build.
  - Pages: the README, PRD, CHANGELOG, CONTRIBUTING, SECURITY, CODE_OF_CONDUCT, every file under `docs/` except `progress.md`, the decision index at `/docs/decisions.html`, and every ADR discovered from `docs/decisions/*.md` (the template excluded). Plus a hand-written landing page, an index of every page, a 404, `robots.txt`, `sitemap.xml`, and one stylesheet.
  - `apps/docs-site/static/` holds the authored stylesheet and favicon; `apps/docs-site/public/` is the generated artifact and is committed.
- Deployment:
  - Vercel project `scopra` (`prj_8IUlxMQr92jfileaMhwprRyR6daH`) linked to `SynergyFlowLabs/scopra`, framework detection disabled, output directory `apps/docs-site/public`, no-op build and install commands.
  - `vercel.json` sets a CSP with `script-src 'none'`, `frame-ancestors 'none'`, and `base-uri 'none'`, plus HSTS, `nosniff`, `X-Frame-Options: DENY`, COOP, a restrictive referrer policy, and a `Permissions-Policy` that disables every unused feature.
  - The production alias `scopra-six.vercel.app` follows the promoted deployment for the final commit: <https://scopra-six.vercel.app>. A push creates a preview deployment (confirmed for four consecutive pushes); production follows the project's production branch (`main`), so it was promoted explicitly while this work lives on the pull-request branch.
  - HTML and the stylesheet are served with `max-age=0, must-revalidate`, so a promoted deployment is visible immediately; the favicon is cached for a day.
- Why the artifact is committed: a first deployment proved that Vercel's build image has no Go toolchain (`make site` → `make: go: No such file or directory`). The alternatives — a Node static-site generator, a toolchain download during the build, or an unbuildable project — were rejected in ADR-015. The output is a pure function of the Markdown (no timestamps, no commit hash), so the committed copy is verifiable: `TestCommittedSiteIsUpToDate` rebuilds into a temporary directory and fails `go test ./...` when `apps/docs-site/public` is stale, and `resetOutput` deletes the output directory first so a page that is no longer generated cannot survive a deploy.
- Verification:
  - `go test ./...` passes across 12 packages, including 26 new tests: markdown rendering (headings and anchors, raw-HTML escaping, fenced code, tables with alignment and pipes inside code spans, nested lists and task boxes, ordered-list `start`, blockquotes, intraword underscores, inline code, link resolution, badges, autolinks, hard breaks, empty input, slugs), the full-site build (expected pages, `progress.md` excluded, zero broken internal links and anchors across 39 files and ~1,890 links, no unrendered Markdown, canonical and social metadata, robots and sitemap, GitHub link fallback), the up-to-date check, and the output-directory guard.
  - `make lint` (gofmt + `go vet`) is clean; `git diff --check` is clean.
  - Live checks on <https://scopra-six.vercel.app>: `/` renders the landing page, `/docs/` resolves to the generated index, `/docs/overview.html` renders the README with badges, tables, and fenced code, and `/docs/decisions.html` lists ADR-001 … ADR-014 after the index.
  - The first deployment failed on the missing Go toolchain (`make: go: No such file or directory`); it was deleted once the working deployment was promoted, and survives only in this log as the evidence behind ADR-015.
- Not done: a custom domain, search, and hosting `progress.md`. Vercel's default protection setting (`all_except_custom_domains`) does not block the production alias, which was verified anonymously.

### 2026-10-17 — Step 36: a one-command install that ships itself

- Selection: the standing request is that a single copy-paste command produces an interactive, modern installer. Step 35 left the pieces in the repository; this step makes them reachable by a stranger, and proves it end to end on this machine.
- **The copy-paste command** is:
  ```console
  $ curl -fsSL https://scopra-six.vercel.app/install.sh | sh
  ```
  `install.sh` reads `/releases/latest.txt` (or `SCOPRA_VERSION`), downloads `checksums.txt` and `scopra_linux_<arch>.tar.gz` from the same origin, requires exactly one well-formed checksum line for the archive, compares SHA-256, unpacks into a `mktemp -d` directory, and hands the terminal to `scopra install`.
- Releases are now served by the documentation site (ADR-016, amended):
  - `scripts/release.sh vX.Y.Z [--publish]` builds the two archives and `checksums.txt`; `--publish` copies them to `apps/docs-site/public/releases/<tag>/` and rewrites `latest.txt`. Publishing is opt-in so a local build for testing cannot move the `latest` pointer.
  - `tools/docsite` preserves `public/releases/` when it rebuilds the site (`resetOutput`), and `TestCommittedSiteIsUpToDate` ignores archives so it compares pages, not gigabytes.
  - GitHub Releases were rejected for the install path: a second host, an upload step, an API call, and a redirect chain to deliver bytes that must match a checksum in the same breath. The release page is a notes-only mirror instead: <https://github.com/SynergyFlowLabs/scopra/releases/tag/v0.1.0-alpha.1> lists the one-liner, the archive URLs on the site, and the SHA-256 of each archive. It carries no binary assets, because asset uploads are impossible from this environment and would duplicate the site.
- **A real bug, found by driving the real interface:** `internal/tui/terminal_linux.go` returned from its reader on `io.EOF`. Raw mode was configured with `VMIN=0, VTIME=1`, so a quiet tenth of a second is a zero-byte read, which `os.File` reports as `io.EOF`; the reader exited on the first quiet moment after startup and every key press after that was dropped silently. Fixed by blocking reads (`VMIN=1, VTIME=0`) and deciding escape-sequence completeness in the event loop with an idle timer (60 ms), which keeps a lone `Escape` a key press and keeps split arrow sequences working.
- Verification:
  - `internal/tui/terminal_linux_test.go` (new, 5 tests): a quiet terminal produces no event and stays open, a later key still arrives; a lone `Escape` becomes a key and does not replay its bytes; `ESC` + `[B` split across reads decodes as Down; end of input closes the stream; cancellation closes it too. Run with `-count=3`.
  - `internal/installer/installsh_test.go` rewritten for the site layout: a fake site with `latest.txt` and `releases/<tag>/`, a pinned tag that must win over `latest.txt`, a corrupted archive, a missing checksum row, a wrong checksum, an unsupported platform, and a construction audit that the script never mentions `releases/latest/download`.
  - End to end, locally: `python3 -m http.server` over `apps/docs-site/public`, the static script with `site` pointed at it. With fake `systemctl` and `podman` on `PATH`, `sh install-live.sh --yes --dir /tmp/scopra-install-test --no-service` exited 0, verified the checksum, installed both binaries, and `scopra version` printed `v0.1.0-alpha.1`. Without the fakes the same command failed closed: exit 1, the exact systemd and Podman fix lines, and no files left behind.
  - Interactively, through a pty (`/tmp/drive_tui.py`): the welcome screen, the live preflight, the directory field, the image field, the service question, the running screen, and the summary all render; `q` at the welcome screen exits 130; a full keyboard-driven install completed into `/tmp/scopra-interactive-test` with both binaries present. The service screen's own verification refused the fake `systemctl` (it reports success but creates no unit) and the interface showed `Verify the service: scoprad.service is "unknown", want active` — fail-closed behaviour, observed rather than assumed.
  - `make lint` and `go test -count=1 ./...` pass (13 packages, including the new terminal tests); `GOOS=darwin` and `GOOS=linux GOARCH=arm64` builds still compile, which is what keeps `internal/tui/terminal_other.go` honest.
- Caching, set in `vercel.json`: `/install.sh` and `/releases/latest.txt` revalidate on every request, so a fixed script or a new `latest` tag is visible immediately; `/releases/<tag>/checksums.txt` and the archives are `immutable` for a year, because a tag directory is never rewritten (a rebuild publishes a new tag).
- The published archives were rebuilt a second time: `go build` stamps the working tree's revision and a dirty flag into the binary by default, so the first archives contained `vcs.modified=true` and a commit that was not the one serving them. `scripts/release.sh` now passes `-buildvcs=false`, and `go version -m` on the extracted binary shows no VCS lines and `(devel)` instead of a pseudo-version.
- The site and the README now put the copy-paste command where a reader looks first. The landing page's hero shows the one-liner and what it needs; an "Install, every way" section near the bottom lists scripted, pinned-version, custom-directory, no-service, pinned-image, verify-by-hand, and build-from-source variants. The README's install section points at a new "Detailed install commands" section with the same set. Every command in both places is the one `install.sh` actually accepts: the flags are passed through to `scopra install`, and the pinned-version example uses a tag that exists.
- Not verified: a real rootless-Podman host with a real systemd user session. Every install in this environment uses a fake `podman`/`systemctl`; the interface, the download, and the checksum are real, the containment is not.
- Out of scope, still: a signature over the archives (integrity is proven against `checksums.txt`, not authorship), `internal/update`, and GitHub release asset uploads, which this sandbox cannot reach.

### 2026-10-17 — Step 37: the installer sets up what it needs

- Selection: the one-command install stopped at the first machine it met. On a real Linux host the preflight reached `Rootless Podman 4.0+` and printed `podman is not available`, then told the user to install it and start again. That is the manual step the one-liner exists to remove, so the installer now offers to do it — with the user's permission, with the exact commands on the screen, and without ever handing a password to Scopra (ADR-017).
- What the user sees: the checks screen adds one line, `Press f and the installer can install it for you`, and one footer key. `f` opens a screen that prints every argument vector — `sudo apt-get update`, then `sudo apt-get install -y podman uidmap slirp4netns fuse-overlayfs`, for example — and waits for `y`. Enter, `n`, and Escape cancel; `q` still quits with 130. Nothing privileged can be reached with a stray Enter.
- How the plan is made (`internal/installer/provision.go`):
  - `/etc/os-release` is parsed for `ID` and `ID_LIKE` with a small `KEY=value` reader that handles quotes and comments; `ID` is tried first, then each `ID_LIKE` token, so Mint resolves through `ubuntu` and Rocky through `rhel`.
  - A fixed table maps the family to a manager and packages: `dnf` on Fedora/RHEL/CentOS/Rocky/AlmaLinux/Oracle, `apt-get` plus `uidmap`, `slirp4netns`, and `fuse-overlayfs` on Debian/Ubuntu, `pacman -Sy --needed --noconfirm` on Arch, `zypper --non-interactive install` on openSUSE/SLES, `apk add --no-cache` on Alpine.
  - `sudo` is prefixed only when the process is not root and `sudo` is on `PATH`. No plan is returned when Podman is already installed, the distribution is unknown, the manager is missing, or elevation would be needed without `sudo`. There is no "try each manager" fallback: an unknown system keeps the manual instructions.
  - Every command is an argument vector executed with `os/exec`; nothing from `os-release` or the terminal is ever re-parsed as shell syntax.
- How the terminal is handed over (`internal/tui`): before a child runs, the runner pauses the key reader (`SetReadDeadline(now)`, a sentinel the reader treats as "wait, do not exit"), waits for the event stream to close, calls `Terminal.Suspend()` (shows the cursor, leaves the alternate screen, restores the saved termios), and passes the terminal's own files to the child. `sudo` therefore prompts in the clear on a real terminal. Afterwards `Resume()` re-enters raw mode and the alternate screen, `Unpause()` clears the deadline, and the event stream restarts, so the interface never competes with the child for keystrokes and never reads the password.
- How the runner continues (`internal/installer/run.go`): `IntentProvision` takes the plan the model is showing, runs `RunProvision`, and sends `ProvisionDone` with the error. The model returns to the checks and the runner re-runs `Env.Checks` immediately, so a successful install lands on a passing preflight with the location step next; a failed one shows the error on the checks screen and on the setup screen, next to the manual fix.
- Non-interactive installs stay non-interactive: `scopra install --yes` never provisions. When Podman is missing it now prints the plan under `to install it now, run:` and exits 1, so a configuration-management run gets an actionable line instead of a fix sentence.
- Verification:
  - `internal/installer/provision_test.go` (7 tests): the exact argv per family (including the rootless helpers on apt and no `sudo` when root), `ID_LIKE` resolution for Rocky and Mint, quoting and comments in `os-release`, refusal for an installed Podman, an unknown distribution, a missing manager, and no `sudo` when not root, that `CommandLines` is shell-free, that a failure stops the sequence and is named in the error, and that an empty plan is an error rather than a silent success.
  - `internal/installer/model_test.go` (+3): the checks screen keeps the plan it is given; `f` opens the setup screen without producing an intent; Enter cancels while `y` produces `IntentProvision`; a `ProvisionDone` message returns to a fresh check and a passing re-check drops the offer.
  - `internal/installer/view_test.go` (+2): the checks screen shows the offer and the footer key; the setup screen prints both commands, the warning, the failure line, and `y install now`.
  - `internal/tui/terminal_linux_test.go` (+2): a paused reader wakes, accepts input again after unpause, and still closes on cancellation.
  - End to end through a pty against the real binary, with a fake `apt-get` (records its arguments, installs a fake `podman`), a fake `sudo` (`exec "$@"`), and a fake `systemctl`: the preflight failed on Podman, `f` showed both commands, `y` ran them, the fake `apt-get` logged `update` and `install -y podman uidmap slirp4netns fuse-overlayfs`, the re-check passed (`podman 4.9.3, rootless, cgroups v2`), and the install finished into a temporary directory with exit 0.
  - `scopra install --yes --no-service` with Podman absent printed the four checks, the two commands, and exited 1 without running them.
  - `make lint`, `gofmt`, and `go test ./...` are clean; `GOOS=darwin` still builds, so the installer stays compilable off Linux and refuses at the preflight.
- Not verified: a real distribution's package manager and a real `sudo` prompt. This sandbox has neither, so the plan, the terminal handover, and the continuation are exercised with fakes; the `dnf`, `pacman`, `zypper`, and `apk` argv come from the table and their unit tests, not from those systems. Ubuntu 22.04 is also unresolved by design: the plan installs 3.4, and the check then reports the version rather than reaching for a third-party source.
- Two terminal bugs surfaced only when a child process owned the terminal, and both are fixed:
  - Handing the terminal over needed a read that can be interrupted. `os.File.SetReadDeadline` does not work on a blocking terminal, so the first design left the reader parked in `read(2)` while the child ran; after the child finished, the old reader and the new one competed for keystrokes and roughly half of them disappeared (observed: the install never started after the service question). Raw mode now reads with `VMIN=0, VTIME=1`, `Pause` sets a flag the reader honours before each read, and the reader checks cancellation at the top of its loop, so it retires within a tenth of a second and nothing is read while the child owns the terminal.
  - A timer-driven read returns zero bytes on a quiet terminal, which Go reports as `io.EOF`. The reader treated that as "the terminal is gone", so the interface exited on its first quiet moment (exit 0, nothing installed, keys echoing into a restored shell). In raw mode `io.EOF` with no bytes is idle, and the reader now continues.
  - Both were caught by driving the real binary through a pty, not by unit tests: the first run stalled on the service question, the second exited on the welcome screen. `internal/tui` gained `TestTerminalPauseDefersInput` (input typed while paused stays queued and is delivered after `Unpause`) alongside the existing pause/cancel and quiet-terminal tests, all run with `-count=3`.
- Published as `v0.1.0-alpha.2`, `v0.1.0-alpha.3`, and `v0.1.0-alpha.4`: the archives and `checksums.txt` live under `/releases/<tag>/`, `latest.txt` names the newest, and the GitHub release page is a notes-only mirror. Alpha.3 exists because the first published build still carried the old welcome sentence ("It never uses sudo"), which stopped being true when provisioning was added; a published tag is immutable, so the correction ships as a new tag instead of a rewritten archive.
- Out of scope, unchanged: provisioning on `--yes`, a second source for newer Podman on old releases, pulling sandbox images, and Windows/macOS.

### 2026-10-17 — Step 38: the fix after the fix

- Report, from a real machine: `f` installed Podman, the interface came back, and the preflight stopped on `Rootless Podman 4.0+  cgroup controllers not delegated: cpu`. The offer line was gone (Podman existed, so the package plan no longer applied), and the screen said "Fix the item above, then run the installer again" — the manual step the feature exists to remove, one layer deeper.
- Diagnosis: systemd's `user@.service` delegates only `memory` and `pids` by default, so a rootless Podman session has no `cpu` controller and Hollow (ADR-010) rightly refuses. Podman is installed and healthy; the missing piece is a `Delegate=` drop-in, which is the standard rootless-Podman fix and needs root once.
- What changed:
  - `hollow.PodmanInfo.DelegationMissing()` names the case precisely: Podman 4.0+, rootless, cgroups v2, and at least one of `cpu`, `memory`, `pids` missing from the delegated set. It is tested against each of those conditions, so the installer never offers a drop-in for a problem a drop-in cannot fix.
  - `DetectDelegationFix` returns the four argument vectors (`mkdir`, `cp`, `chmod`, `systemctl daemon-reload`, each prefixed with `sudo` when needed) plus the drop-in content. The drop-in is staged in `<state-dir>/installer/99-scopra-delegate.conf` first, so the privileged commands stay argument-only and the file the screen prints is the file that lands. `RunProvision` stages files before running commands and removes them afterwards.
  - The remedy is now chosen from the failure rather than from "Podman is missing": `defaultProvision` reads `/etc/os-release` for the package case and `podman info` for the delegation case, and refuses when neither applies.
  - The screen says what will happen: the plan carries its own summary, note, and a `NeedsRelogin` flag. The checks screen offers "Press f and the installer can fix this for you"; after a delegation fix that did not take effect it adds "The change takes effect at your next login: log out, log back in, and run the installer again" instead of failing silently. `scopra install --yes` prints the commands plus the same login note and still runs none of them.
- Verification:
  - `internal/hollow` (+1 test with five cases) for `DelegationMissing`; `internal/installer/provision_test.go` (+3) for the exact four commands, the staged path and mode, the drop-in content, the three refusals (no staging directory, no `systemctl`, no `sudo` when not root), and that `RunProvision` stages a file, lets the command read it, and removes it.
  - `internal/installer/view_test.go` (+2): the checks screen explains the login step, and the setup screen prints the delegation summary, the staged `cp` line with its full path, and the note.
  - End to end through a pty with a fake `podman` whose reported controllers depend on a marker file, a fake `sudo` that validates the drop-in content before touching the marker, and fake `systemctl`: the preflight failed with `not delegated`, `f` showed the four commands, `y` ran them, the marker existed with the right content, the re-check printed `Everything Scopra needs is here`, and the install finished (exit 0, binaries present).
  - `go test ./...`, `make lint`, and `gofmt` are clean; the site builds and the link checker passes.
  - Correction, shipped in `v0.1.0-alpha.6`: `scopra install --yes` printed the staged `cp` without staging the file, so the command it
    recommended could not run. The scripted path now stages the drop-in before it prints anything and keeps it; `TestStageProvisionKeepsTheFileForAScriptedRun`
    covers the file, its mode, its content, and its removal, and a plain install now prints a `cp` whose source exists.
- Not verified: a real systemd user manager applying the drop-in. The staged file, the commands, the re-check, and the message are exercised with fakes; whether `Delegate=cpu cpuset io memory pids` is enough on a given distribution is systemd's behaviour, not something this sandbox can show. The re-login requirement is a documented property of `Delegate=` on a running unit, and the installer treats a still-failing check as the truth either way.
- Out of scope, unchanged: cgroups v1 (a boot parameter and a reboot), restarting `user@.service` for the user (it would kill the session running the installer), pulling sandbox images, and `--yes` provisioning.

### 2026-10-17 — Step 39: the loop after the fix

- Report, with screenshots, from the same machine: after `f` and `y`, the
  interface closed, the commands ran, and the checks screen came back with the
  same `cgroup controllers not delegated: cpu` and the same "Press f" offer.
  Pressing `f` again ran the four commands again. The second run stopped at
  sudo's password prompt, which the user interrupted.
- Diagnosis: nothing was wrong with the drop-in. systemd implements
  `Delegate=` when it starts `user@.service`, so a re-check inside the same
  session *must* fail; the defect was the interface's state. `defaultProvision`
  returned the delegation plan every time the check failed, so the screen
  offered a remedy that was already installed, forever. Two more mistakes
  followed from the same missing state: the frame was titled "set up podman"
  even for a cgroup fix, and the installer never checked that the drop-in it
  claimed to install was on disk.
- What changed:
  - `installer.DelegationConfigured` reads a drop-in and accepts it only when
    its last `Delegate=` assignment names `cpu`, `memory`, and `pids` — the
    assignment systemd honours — so a half-applied file is not mistaken for a
    fix. `Delegate=yes` is not accepted: it does not promise the controllers.
  - `defaultProvision` returns no plan when that file is in place, and the
    environment gains `NeedsRelogin`, which reports the one remaining gap: the
    drop-in is installed and `podman info` still lacks the controllers.
  - The model gains that state. The checks screen says the delegation is
    configured, systemd applies it when the user manager starts, and the
    installer should be run again after logging in; `f` is not offered, and
    Enter exits with status 1. The failing row's own fix text says the same
    thing instead of advising a change that has already been made.
  - The instruction outlives the screen: the runner prints `ReloginNote` after
    the alternate screen is restored, and `scopra install --yes` prints it in
    place of commands it would only repeat.
  - The commit verification is honest: after a successful run of a plan with
    `NeedsRelogin`, the runner checks that the drop-in exists and reports
    `…was not installed` when it does not.
  - The setup frame is titled from the plan ("delegate cgroup controllers",
    "install podman") instead of a hard-coded "set up podman", and pressing `y`
    clears the stale results so the spinner shows while the commands and the
    re-check run.
- Verification:
  - `provision_test.go`: eight `DelegationConfigured` cases (all three
    controllers, extras, each missing controller, last assignment wins,
    `Delegate=yes`, no assignment, empty file) plus a missing path.
  - `model_test.go`: a relogin result offers no plan, ignores `f`, and Enter
    quits with status 1 and a note naming the drop-in; `y` keeps the plan the
    runner reads and reports Running so the spinner draws.
  - `view_test.go`: the checks screen explains the login step, the footer says
    `enter exit`, and "Press f" is absent; a failed attempt still offers
    another one.
  - End to end through a pty inside a user and mount namespace with
    `/etc/systemd/system` bind-mounted from a temporary directory, so the real
    path is exercised: a machine with Podman and no drop-in goes checks → `f`
    (frame titled `delegate cgroup controllers`) → `y` → the drop-in appears at
    `/etc/systemd/system/user@.service.d/99-scopra-delegate.conf` with
    `Delegate=cpu cpuset io memory pids` → the screen becomes the relogin
    message → Enter exits 1 and the note prints after the screen is restored.
    A second run, with the drop-in present, goes straight to that message and
    never offers `f`.
  - `go test ./...`, `make lint`, and `gofmt` are clean; the site builds and the
    link checker passes.
- Not verified: a real Ubuntu host where the drop-in is applied by a new login
  and the check then passes. This sandbox has no systemd user manager to
  restart, which is exactly why the installer now stops at that boundary
  instead of pretending to cross it.
