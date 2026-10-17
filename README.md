# Scopra

Scopra is the controlled execution layer for AI agents. It connects agents to approved developer environments through scoped grants, local policy, rootless isolation, exact-action approvals, structured results, and attributable audit records.

## Status

Early implementation: Scopra Messenger v1 envelope validation, strict decoding of execution request payloads (ADR-007, structure only), their canonical form and action hash (ADR-008), strict decoding of every execution lifecycle message (ADR-009), the exact rootless Podman invocation, preflight check, and a runner for the Hollow sandbox (ADR-010; tested only against a fake Podman), the local `execution.run` IPC protocol, which streams a run's start notice, output, and result (ADR-011; tested only with fakes), a read-only Mio status service over local IPC, and a read-only `scopra doctor` that checks the local platform, state directory, workspace registry, registered workspaces, and Mio status endpoint, and, when asked, whether the host can offer ADR-010's rootless containment with the configured image. Mio now wires local execution to Hollow: it resolves Podman once at startup, removes labelled leftover containers, revalidates the registered workspace before each run, and runs the command in rootless Podman when `--sandbox-image` is set. Without an image or Podman, every run is refused with `containment_unavailable`. `scopra workspace add/list/remove` manages Mio's workspace registrations over local IPC, and `scopra run` runs a command in a registered workspace through Mio and Hollow, streaming its output and returning its exit status (ADR-011; `--json` for machine use). No real Podman host has been used yet, and policy, authorization, approvals, and audit are not implemented.

## Start Here

- `PRD.md`
- `docs/plans/00-full-implementation-plan.md`
- `docs/security/threat-model.md`
- `docs/security/risk-register.md`
- `docs/security/security-architecture.md`
- `docs/architecture/system-overview.md`
- `progress.md`

## Planned Components

- **Scopra Mariner** — CLI/TUI onboarding and machine enrollment (`scopra` command).
- **Scopra Mio** — local execution daemon (`scoprad` command).
- **Scopra Caloris** — cloud API and control plane.
- **Scopra Goldstone** — secure agent-to-machine relay.
- **Scopra Messenger Protocol** — internal request and dispatch protocol.
- **Scopra Resolution** — cloud and local policy evaluation.
- **Scopra Rupes** — local policy-enforcement boundary.
- **Scopra Hollow** — ephemeral, isolated execution environment.
- **Scopra Catena** — append-only execution and audit event chain.
- **Scopra Facula** — security signals and anomaly detection.
- Remote MCP server, dashboard, and shared SDK, protocol, policy-schema, and UI packages.

See [`docs/product/naming.md`](docs/product/naming.md) for the component naming map.

## Alpha Defaults

Linux x86_64, rootless Podman, workspace-only mounts, network off, empty environment, no host credential sockets, no privilege elevation, no native fallback, and no PTY.
