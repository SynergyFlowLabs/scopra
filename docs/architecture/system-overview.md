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
