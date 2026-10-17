---
name: Feature proposal
about: Propose a product or engineering change
title: "feat: "
labels: ["enhancement", "triage"]
---

## Problem

What is hard, unsafe, or impossible today? Describe the user and the workflow,
not the solution.

## Proposed outcome

What should be true when this is done? Include the smallest version that would
be useful.

## Alternatives considered

## Security boundaries

- What authority does this add, and to whom?
- What is the trust boundary, and does it change?
- What happens when a check fails? (Scopra fails closed.)
- Does it store output, credentials, paths, or identity? If so, what are the
  retention, redaction, and access rules?
- Does it touch the same-UID IPC boundary, workspace validation, the sandbox
  invocation, or the journal?

## Acceptance criteria

- [ ]
- [ ]

## ADR needed?

- [ ] Yes — a durable decision changes and needs an ADR in `docs/decisions/`.
- [ ] No — this is an implementation detail under an existing ADR.
