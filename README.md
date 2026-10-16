# Scopra

Scopra is the controlled execution layer for AI agents. It connects agents to approved developer environments through scoped grants, local policy, rootless isolation, exact-action approvals, structured results, and attributable audit records.

## Status

Initial planning and repository scaffold. Production behavior is not implemented.

## Start Here

- `PRD.md`
- `docs/plans/00-full-implementation-plan.md`
- `docs/security/threat-model.md`
- `docs/security/risk-register.md`
- `docs/security/security-architecture.md`
- `docs/architecture/system-overview.md`

## Planned Components

- `scopra` CLI/TUI
- `scoprad` daemon
- API and control plane
- Relay
- Remote MCP server
- Dashboard
- Shared protocol and policy packages

## Alpha Defaults

Linux x86_64, rootless Podman, workspace-only mounts, network off, empty environment, no host credential sockets, no privilege elevation, no native fallback, and no PTY.
