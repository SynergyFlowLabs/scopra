# Scopra

Scopra is the controlled execution layer for AI agents. It connects agents to approved developer environments through scoped grants, local policy, rootless isolation, exact-action approvals, structured results, and attributable audit records.

## Status

Early implementation: Scopra Messenger v1 envelope validation, strict decoding of execution request payloads (ADR-007, structure only), their canonical form and action hash (ADR-008), strict decoding of every execution lifecycle message (ADR-009), the exact rootless Podman invocation for the Hollow sandbox (ADR-010; nothing runs it yet), a read-only Mio status service over local IPC, and a read-only `scopra doctor` that checks only the local platform, state directory, workspace registry, registered workspaces, and Mio status endpoint. `scopra workspace add/list/remove` manages Mio's workspace registrations over local IPC; policy, authorization, and execution are not implemented.

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
