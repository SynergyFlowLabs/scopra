# Scopra

Scopra is the controlled execution layer for AI agents. It connects agents to approved developer environments through scoped grants, local policy, rootless isolation, exact-action approvals, structured results, and attributable audit records.

Scopra is free and open-source software, licensed under the MIT License, with no pricing model.

## Status

Early implementation: Scopra Messenger v1 envelope validation, strict decoding of execution request payloads (ADR-007, structure only), their canonical form and action hash (ADR-008), strict decoding of every execution lifecycle message (ADR-009), the exact rootless Podman invocation, preflight check, and a runner for the Hollow sandbox (ADR-010; tested only against a fake Podman), the local `execution.run` IPC protocol, which streams a run's start notice, output, and result (ADR-011; tested only with fakes), a read-only Mio status service over local IPC, and a read-only `scopra doctor` that checks the local platform, state directory, workspace registry, registered workspaces, and Mio status endpoint, and, when asked, whether the host can offer ADR-010's rootless containment with the configured image. Mio now wires local execution to Hollow: it resolves Podman once at startup, removes labelled leftover containers, revalidates the registered workspace before each run, and runs the command in rootless Podman when `--sandbox-image` is set. Without an image or Podman, every run is refused with `containment_unavailable`. `scopra workspace add/list/remove` manages Mio's workspace registrations over local IPC, `scopra run` runs a command in a registered workspace through Mio and Hollow, streaming its output and returning its exit status (ADR-011; `--json` for machine use), and `scopra service install/uninstall/status` manages Mio as a systemd user service whose unit carries the durable configuration (ADR-012). Mio keeps a bounded, metadata-only journal of answered runs (ADR-013), which `scopra logs` reads over the read-only `execution.logs` command as text or schema-v1 JSON; it records the execution and workspace IDs, state, times, exit code, and stdout/stderr byte counts, and never the command line or output. No real Podman host or real systemd session has been used yet, and policy, authorization, approvals, and audit are not implemented.

## License

Scopra is **free and open-source software**, released under the [MIT License](LICENSE).

There is no pricing model: no paid tiers, subscriptions, usage-based billing, plan limits, feature gates, or account requirements. Every capability is available to everyone under the same license. See [`NOTICE`](NOTICE) for third-party material.

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
