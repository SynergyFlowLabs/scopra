# Scopra Development Progress

**Started:** 2026-10-16 (Asia/Karachi)  
**Current phase:** Milestone 1 — local secure execution foundation  
**Status:** In progress

This file is the running engineering log for implementation work. Each increment should be recorded with its scope, verification, commit, and push status. Security-sensitive work must stay incremental, testable, and fail-closed.

## Baseline

- The repository is an initial planning scaffold: no Go source files or `go.mod` existed at kickoff; CI only checked documentation and secret-file presence.
- The product/architecture naming map from the previous step is being retained: Scopra remains the product; Mariner, Mio, Caloris, Goldstone, Messenger, Resolution, Rupes, Hollow, Catena, and Facula name components.
- The sandbox currently has Node.js and Python, but no Go toolchain. Go implementation will not be marked verified until a Go test run succeeds; use CI or install a trusted toolchain before relying on it.

## Working practices

- Reviewed and installed selected skills from skills.sh into `.agents/skills/` (project-local, telemetry disabled): `go-project-layout`, `go-coding-standards`, `go-test-quality`, `go-cli`, `test-driven-development`, and `security-review`.
- Reviewed the installed skill instructions before use. Apply test-first development, strict input validation, small interfaces, and security review for the execution/protocol boundaries.
- Keep the existing executable identifiers `scopra` and `scoprad` until a compatibility decision says otherwise.

## Roadmap

1. **Foundation and protocol contract** — establish a testable Go module/CI path, then define a versioned, bounded, strictly decoded protocol slice with tests.
2. **Local daemon and CLI** — add testable lifecycle/IPC foundations; keep command handling separate from policy and execution.
3. **Policy and containment boundary** — implement default-deny policy and rootless containment before enabling execution; no native fallback.
4. **Structured execution and audit** — limits, cancellation, bounded output, and attributable events with adversarial tests.
5. **Enrollment, relay, approvals, and integrations** — proceed only after local foundations and security invariants are testable.

This roadmap is staged; each entry remains pending until implementation and verification evidence are recorded below.

## Progress log

### 2026-10-16 — Kickoff and repository review

- Read the project’s PRD, implementation plan, architecture, product, security, API, operations, and component documentation.
- Confirmed the project is pre-alpha and has no implementation code yet.
- Discovered relevant Go, CLI, testing, TDD, and security-review skills from skills.sh; installed six selected project-local skills, reviewed their instructions, and recorded source hashes in `skills-lock.json`. Skills CLI telemetry was disabled.
- Confirmed the sandbox has no Go compiler. Do not mark Go work verified until a pinned Go CI run passes.

### 2026-10-16 — Naming checkpoint and implementation setup

- Adopted the supplied Mercury-inspired component naming system and pushed it in commit `398b337` on `arena/0b81a55b-scopra`.
- Added this progress log and installed the selected skills locally in the repository; setup was pushed in `ae6779d`.
- Confirmed Go 1.26.9 is the current patch toolchain for this project baseline, but the sandbox itself has no Go compiler.

### 2026-10-16 — Step 1: Go module, CI, and protocol tests (in progress)

- Chose a single root Go module for the monorepo; recorded the choice in ADR-001. No external Go dependencies are needed for the first protocol increment.
- Added ADR-002 for a strict, bounded Scopra Messenger v1 envelope. It explicitly separates structural parsing from signature verification, replay defense, authorization, and typed payload validation.
- Added a Go 1.26.9 CI path with read-only permissions, pinned GitHub Actions, formatting, `go test -race`, and `go vet`. CI now runs on branch pushes as well as pull requests.
- Wrote protocol behavior/security tests before the implementation. Next: push the red test checkpoint, confirm the expected CI failure, then implement the minimum passing behavior and push the green checkpoint.
- Verification limitation: no local Go binary is available, so Go execution must be confirmed by the remote CI run; no local Go test result is claimed.
