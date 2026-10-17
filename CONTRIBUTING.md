# Contributing to Scopra

Thanks for helping. Scopra is a security-sensitive, local-first project: a small change to path handling, IPC, or container arguments can remove the boundary that protects a user's machine. This guide explains how to propose, build, test, and land a change.

By contributing, you agree to follow the [Code of Conduct](CODE_OF_CONDUCT.md) and to license your contribution under the project's [MIT License](LICENSE).

## Ways to contribute

- **Bug reports** — use the [bug report template](https://github.com/SynergyFlowLabs/scopra/issues/new?template=bug.md).
- **Feature proposals** — use the [feature template](https://github.com/SynergyFlowLabs/scopra/issues/new?template=feature.md) and describe the problem before the solution.
- **Security review** — use the [security review template](https://github.com/SynergyFlowLabs/scopra/issues/new?template=security-review.md) for design-level concerns, or report vulnerabilities privately as described in [`SECURITY.md`](SECURITY.md).
- **Documentation, tests, and threat-model work** count as first-class contributions.
- **Code** — start with an issue for anything larger than a typo or a one-line fix, so the design can be agreed before you spend time.

Look for [`good first issue`](https://github.com/SynergyFlowLabs/scopra/labels/good%20first%20issue) and [`help wanted`](https://github.com/SynergyFlowLabs/scopra/labels/help%20wanted) labels.

## Before you start

Read, in this order:

1. [`README.md`](README.md) — what exists and what does not.
2. [`PRD.md`](PRD.md) — product rules, including the alpha non-goals.
3. [`docs/plans/00-full-implementation-plan.md`](docs/plans/00-full-implementation-plan.md) — milestones.
4. [`docs/security/threat-model.md`](docs/security/threat-model.md) and [`docs/security/risk-register.md`](docs/security/risk-register.md).
5. The ADRs relevant to your change in [`docs/decisions/`](docs/decisions/).

If your change contradicts an ADR, propose a new ADR in the pull request rather than silently diverging.

## Development setup

Scopra needs **Go 1.26.9** on Linux. Podman is only needed to exercise real execution; the test suite uses fakes.

```sh
git clone https://github.com/SynergyFlowLabs/scopra.git
cd scopra
make bootstrap   # verify the toolchain and download modules
make lint        # gofmt -l must be empty, then go vet ./...
make test        # go test -race ./...
make security-test
```

Build the binaries when you want to try a change end to end:

```sh
go build -o bin/scopra ./cmd/scopra
go build -o bin/scoprad ./cmd/scoprad
./bin/scoprad --state-dir /tmp/scopra-state
./bin/scopra doctor --state-dir /tmp/scopra-state
```

The repository has **no external Go dependencies**. Prefer the standard library; a new dependency needs a justification in the pull request (maintenance, supply chain, and audit surface all count against it).

## Project conventions

- **Test first.** Write the failing test, then the code. Tests are part of the change, never a follow-up.
- **Security-sensitive changes** need an issue, cross-review, positive and negative tests, secret-safe logs, and documentation in the same pull request.
- **Fail closed.** When a check cannot be completed, refuse the operation. Never add a native fallback, an unrestricted shell, a PTY, or a "just this once" bypass.
- **Bounded everything.** Bound new files, frames, reads, and helper calls, and validate lengths before allocating.
- **Strict decoding.** New wire formats use exact keys, no duplicates, no nulls, no trailing data, and typed errors.
- **Small, reviewable commits.** One logical change per commit; keep formatting-only changes separate.
- **Formatting.** `gofmt` is mandatory. `go vet` must be clean. New code needs tests, and exported identifiers need doc comments.
- **ADRs and the log.** A durable design decision gets an ADR in `docs/decisions/` and an index entry. Each increment gets an entry in [`progress.md`](progress.md) with its acceptance checklist, test-first record, and verification evidence.
- **No secrets, no raw output.** Do not log command output, credentials, tokens, or user data. The run journal is metadata-only by design; keep it that way.
- **No pricing machinery.** Scopra is free and open source. Do not add license keys, entitlement checks, plan gating, payment code, telemetry, or "upgrade" prompts (ADR-014).
- **The documentation site is generated.** Editing a published Markdown file (the README, the PRD, an ADR, anything under `docs/` except `progress.md`) means running `make site` and committing `apps/docs-site/public` in the same change. `go test ./...` fails when the committed site is stale (ADR-015).

## Commit messages

Use a short imperative subject line, optionally with a conventional prefix:

```text
feat(run): add --workdir validation before dialing
fix(journal): refuse a torn tail before parsing
docs(adr): record ADR-014
test(ipc): cover a duplicate key in a logs reply
```

Explain *why* in the body, not just *what*. Reference the issue (`Fixes #123`) and the ADR when relevant.

## Pull request process

1. Fork the repository and create a topic branch from `main`.
2. Make the change test-first, update the docs and `progress.md`, and run the local checks:
   ```sh
   make lint && make test && make security-test
   git diff --check
   ```
3. Open a pull request using the template. Fill in the summary, security impact, testing, and documentation sections. Do not leave checklist items unticked without an explanation.
4. Keep the pull request focused. Unrelated refactors belong in a separate change.
5. CI runs formatting, `go vet`, and the race-enabled test suite. If CI is unavailable, paste the local output in the pull request.
6. A maintainer reviews. Security-sensitive changes need a second reviewer and an explicit "no unresolved finding" statement before merge.
7. Squash or merge is the maintainer's choice; keep the history readable either way.

Reviewers look for: correct failure behavior, bounded inputs, no new authority, tests that would fail if the fix were removed, and documentation that matches the code.

## Reporting security issues

Do **not** open a public issue for a suspected vulnerability. Follow [`SECURITY.md`](SECURITY.md) so a fix can be prepared before disclosure.

## License of contributions

Scopra is licensed under the MIT License. By submitting a contribution you confirm you have the right to do so and agree that it is licensed under the same terms. No separate contributor license agreement is required.
