# System Overview

```text
AI agent / MCP host
  -> MCP or HTTPS entry point
  -> Scopra Caloris (identity, grants, cloud policy, sessions, approvals)
  -> Scopra Goldstone (secure agent-to-machine relay)
       carries Scopra Messenger Protocol envelopes
  -> Scopra Mio (local daemon)
       -> Scopra Resolution (policy evaluation)
       -> Scopra Rupes (final local authorization and enforcement boundary)
       -> Scopra Hollow (rootless isolated execution environment; ADR-010)
       -> Scopra Catena (append-only audit and execution-event chain)
```

Scopra Resolution evaluates applicable cloud and local policy. Scopra Rupes applies the local maximum and is the final authorization and policy-enforcement authority; Scopra Hollow supplies execution containment. Cloud services may narrow but cannot broaden local authority. Approved operations run inside Scopra Hollow. Scopra Facula represents the security-signal and anomaly-detection layer; Scopra Mariner provides local CLI/TUI onboarding and machine enrollment.

The first local-control increment runs `scoprad` (Mio) in the foreground and lets `scopra status` (Mariner) query a read-only status endpoint over a bounded Unix socket. Mio keeps its socket and lock in a persistent per-user state directory (`$XDG_STATE_HOME/scopra` by default), and an exclusive lock ensures that at most one Mio serves a state directory. `scopra doctor` reuses that endpoint for read-only platform, state-directory, workspace-registry, and daemon checks, and revalidates registered workspaces read-only (ADR-004); it never starts Mio or changes local state. At startup Mio opens its workspace registry (ADR-005) in the locked state directory and refuses to serve if the registry is unusable. It then serves workspace registration (`add`, `list`, `remove`) on the same socket (ADR-006), which `scopra workspace` uses; workspaces are validated under ADR-004. Policy decisions, approvals, audit, and execution are not exposed by this endpoint. See ADR-003.
