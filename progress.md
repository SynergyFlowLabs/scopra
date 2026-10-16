# Scopra Development Progress

**Started:** 2026-10-16 (Asia/Karachi)
**Current phase:** Milestone 1 — local secure execution foundation
**Current increment:** Doctor revalidates registered workspaces (Step 12)
**Status:** In progress on `arena/c7a368c1-scopra`: acceptance checklist agreed, tests first; CI is out of scope for now at the maintainer's request

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
2. **Local daemon and CLI** — the initial foreground Mio status service with a persistent per-user state directory and an exclusive state-directory lock, Mariner `scopra status`, a minimal read-only `scopra doctor` (platform, state-directory, workspace-registry, and daemon checks), and `scopra workspace add/list/remove` are implemented. Workspace validation (ADR-004) and registry storage (ADR-005) exist; Mio checks its registry at startup and serves add, list, and remove over local IPC (ADR-006). Broader doctor checks, execution, service installation, and expanded CLI/IPC authorization remain pending.
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
  - `make bootstrap`, `make lint`, `make test`, and `make security-test`;
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
  - `make bootstrap`, `make lint`, `make test`, and `make security-test`;
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
  - `make bootstrap`, `make lint`, `make test`, and `make security-test`;
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
  - `make bootstrap`, `make lint`, `make test`, and `make security-test`;
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
  - `make bootstrap`, `make lint`, `make test`, and `make security-test`;
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

- [ ] `registry.Check(dir)` returns the registrations, sorted by path, from the same read-only validation as before.
- [ ] `scopra doctor` runs a `workspaces` check after `workspace-registry` and before `daemon`. It revalidates each registration under ADR-004 with the validator configuration that Mio uses, which protects the state directory:
  - `pass` when none are registered or all still qualify;
  - `warn` naming up to five stale registrations, each with its ID, its path, and a short reason (missing, not a directory, not owned by you, is or contains a protected location, or changed since it was registered), plus a count of any others. The hint says to restore the directory or to run `scopra workspace remove ID`, which does not change the directory;
  - `fail` when validation cannot be prepared, because `scoprad` would not start either;
  - `skip` when the platform, state-directory, or workspace-registry check failed.
- [ ] Doctor stays read-only: it never changes the registry file, the workspaces, or their contents, and a warning does not change the exit status (`0`).
- [ ] `notChecked` no longer lists `workspace revalidation`. The JSON schema stays at version 1, and text output escapes control characters in the new detail.
- [ ] The CLI README, the plan, and this log are updated. The wire protocol, Mio, and the ADRs are unchanged.

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
