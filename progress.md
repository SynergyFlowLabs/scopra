# Scopra Development Progress

**Started:** 2026-10-16 (Asia/Karachi)
**Current phase:** Milestone 1 — local secure execution foundation
**Current increment:** Mio foreground status service and Mariner local IPC foundation
**Status:** Implemented and pushed; local checks passed; Actions startup failure remains unresolved

This file is the running engineering log. Each increment records scope, verification, commit, and push status. Security-sensitive work remains incremental, testable, and fail-closed.

## Baseline

- The repository was an initial planning scaffold at kickoff: no Go source files or `go.mod` existed; CI only checked documentation and secret-file presence.
- Scopra remains the umbrella product. The Mercury-inspired component names are documented in `docs/product/naming.md`.
- The sandbox initially had no Go compiler. A temporary Go 1.26.9 toolchain was built from official `golang/go` source tags under `/tmp`; no toolchain artifacts are committed.
- GitHub Actions push runs currently return `startup_failure` before creating jobs. Runs `38854979363` (`90724b2`) and `38855061603` (`fbf97ed`) had no jobs or logs; prior implementation runs `38851033476` and `38851101051` failed the same way. The workflow passes local `actionlint`; GitHub provides no diagnostic, so the cause remains unconfirmed and these are not code-test results.

## Working practices

- Installed and reviewed selected skills from skills.sh in `.agents/skills/` (project-local; CLI telemetry disabled): `go-project-layout`, `go-coding-standards`, `go-test-quality`, `go-cli`, `test-driven-development`, and `security-review`.
- The sources and content hashes are recorded in `skills-lock.json`.
- Keep the existing executable identifiers `scopra` and `scoprad` until a compatibility decision says otherwise.
- Go verification used Go 1.26.9 with `CGO_ENABLED=1` for the race detector. No external Go dependencies were added.

## Roadmap

1. **Protocol foundation** — envelope v1 structural validation is implemented and locally verified. Typed execution requests/results, canonical approval binding, cryptographic verification, replay storage, and compatibility/version guarantees remain pending.
2. **Local daemon and CLI** — the initial foreground Mio status service and Mariner `scopra status` command are implemented. Workspace CRUD, doctor, execution, service installation, and expanded CLI/IPC authorization remain pending.
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

### 2026-10-16 — Step 2: Mio and Mariner status IPC (in progress)

- Accepted ADR-003 for the local status-only IPC boundary. Wrote tests before implementation and confirmed the expected red build for missing frame, message, daemon, and CLI APIs. A later strict-JSON review added case-alias rejection tests first; they failed against Go's case-insensitive struct-field decoding and passed after exact top-level key validation was added.
- Added foreground `scoprad`, `scopra status` (human and JSON output), `scopra version`, and build-info version reporting. Status value `running` means the local IPC endpoint responds; it does not imply execution readiness. Socket binding prevents a second daemon; stale sockets are removed only after a failed probe and inode recheck; shutdown removes only the socket inode Mio created.
- Added four-byte big-endian framing capped at 64 KiB; strict versioned JSON with exact field names, unknown/duplicate fields, trailing JSON, invalid request IDs, and unknown commands rejected; a 16-client concurrency cap; and two-second I/O deadlines.
- On Linux, both ends verify same-effective-UID `SO_PEERCRED`; the state path is checked against symlinks and unsafe writable ancestors, with mode `0700`, and the socket is `0600`. Darwin and Windows cross-compilation passed; unsupported non-Linux runtime paths fail closed. Same-UID processes are not isolated from each other.
- This endpoint is read-only and exposes status only. It does not register workspaces, authorize actions, connect to the cloud, or execute commands. These remain mandatory future increments.
- Security review covered untrusted JSON, filesystem paths and permissions, peer authentication, fail-closed errors, resource limits, and the IPC trust boundary. No high-confidence exploitable issue remains in this status-only slice; the same-UID limitation and all missing execution authorization are documented.
- Local verification passed: `make bootstrap`, `make lint`, `make test`, `make security-test`, `go test -race -count=1 -timeout 60s ./...`, and `git diff --check`. Coverage: local IPC 75.5%, build info 100%, CLI 53.8%, daemon 55.3%. The final 10-second IPC decoder fuzz run completed 89,260 executions without a crash or failure.
- The repository tree was regenerated. Commit `90724b2` (`feat(local): add Mio status IPC foundation`) and progress commit `fbf97ed` were pushed to `arena/0b81a55b-scopra`. Their Actions runs (`38854979363`, `38855061603`) ended in `startup_failure` before jobs/logs; `actionlint` passes locally, but GitHub provides no diagnostic.
