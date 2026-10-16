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
       -> Scopra Hollow (rootless isolated execution environment)
       -> Scopra Catena (append-only audit and execution-event chain)
```

Scopra Resolution evaluates applicable cloud and local policy. Scopra Rupes applies the local maximum and is the final authorization and policy-enforcement authority; Scopra Hollow supplies execution containment. Cloud services may narrow but cannot broaden local authority. Approved operations run inside Scopra Hollow. Scopra Facula represents the security-signal and anomaly-detection layer; Scopra Mariner provides local CLI/TUI onboarding and machine enrollment.

The first local-control increment runs `scoprad` (Mio) in the foreground and lets `scopra status` (Mariner) query a read-only status endpoint over a bounded Unix socket. An exclusive state-directory lock ensures that at most one Mio serves a state directory. `scopra doctor` reuses that endpoint for read-only platform, state-directory, and daemon checks; it never starts Mio or changes local state. This is lifecycle/transport scaffolding only: workspace management, policy decisions, approvals, audit, and execution are not exposed by this endpoint. See ADR-003.
