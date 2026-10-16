# Scopra Development Progress

**Started:** 2026-10-16 (Asia/Karachi)
**Current phase:** Milestone 1 — local secure execution foundation
**Current increment:** Scopra Messenger envelope v1 structural validation
**Status:** Implemented; final checks and push in progress

This file is the running engineering log. Each increment records scope, verification, commit, and push status. Security-sensitive work remains incremental, testable, and fail-closed.

## Baseline

- The repository was an initial planning scaffold at kickoff: no Go source files or `go.mod` existed; CI only checked documentation and secret-file presence.
- Scopra remains the umbrella product. The Mercury-inspired component names are documented in `docs/product/naming.md`.
- The sandbox initially had no Go compiler. A temporary Go 1.26.9 toolchain was built from official `golang/go` source tags under `/tmp`; no toolchain artifacts are committed.
- GitHub Actions push runs currently return `startup_failure` before creating jobs. Local Go verification is working; the workflow is still configured for a functioning Actions runner.

## Working practices

- Installed and reviewed selected skills from skills.sh in `.agents/skills/` (project-local; CLI telemetry disabled): `go-project-layout`, `go-coding-standards`, `go-test-quality`, `go-cli`, `test-driven-development`, and `security-review`.
- The sources and content hashes are recorded in `skills-lock.json`.
- Keep the existing executable identifiers `scopra` and `scoprad` until a compatibility decision says otherwise.
- Go verification used Go 1.26.9 with `CGO_ENABLED=1` for the race detector. No external Go dependencies were added.

## Roadmap

1. **Protocol foundation** — envelope v1 structural validation is implemented and locally verified. Typed execution requests/results, canonical approval binding, cryptographic verification, replay storage, and compatibility/version guarantees remain pending.
2. **Local daemon and CLI** — build testable Mio lifecycle and Mariner CLI foundations, then bounded Unix-socket IPC.
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
- GitHub Actions run `38848464758` ended in `startup_failure` with no jobs, so it is not treated as a code test result.

### 2026-10-16 — Step 1: Messenger envelope v1

- Implemented a bounded JSON envelope decoder with unknown envelope-field rejection, duplicate-key rejection at every JSON depth, a closed message-type set, opaque identifier checks, canonical 128-bit-or-longer base64url nonces, strict expiry, freshness validation via `Envelope.ValidateAt` (five-minute maximum lifetime, 30-second future-clock tolerance, and strict expiry), and exact-payload SHA-256 verification.
- Added focused rejection tests and a fuzz target. Structural validation is explicitly not signature verification, replay protection, authorization, or typed payload validation; all remain required before dispatch.
- Security review covered untrusted JSON parsing, digest handling, authorization separation, and the surrounding architecture. No high-confidence exploitable issue was identified in this structural-only slice; the omitted trust/authorization checks are documented as mandatory follow-up work.
- Verification passed locally: `make bootstrap`, `make lint`, `make test`, `make security-test`, `go vet ./...`, and `go test -cover ./...` (89.4% statement coverage). A 10-second fuzz run completed 644,563 executions with no crash or finding.
- Final step: record the implementation commit and push it to `arena/0b81a55b-scopra`.
