---
name: Security review request
about: Ask for review of a design or change with security impact
title: "security: "
labels: ["security", "triage"]
---

> Do **not** use this template for a suspected vulnerability. Follow
> [SECURITY.md](../../SECURITY.md) and report privately.

## Change

Link the pull request, commit, or design document.

## Assets and principals

- What is being protected (workspaces, credentials, host, state directory,
  journal, audit trail)?
- Who can trigger this path (the user, another same-UID process, an agent, a
  remote caller)?

## Authorization point

Where is the decision made, and what enforces it? If nothing enforces it, say so.

## Failure modes

- What happens on a missing file, a malformed frame, a timeout, a full disk, a
  cancelled context, or a crash?
- Does any path fall back to something less safe (native execution, a shell, a
  broader mount, a longer deadline)?

## Negative tests

Which tests prove the refusal, the bounded read, and the denial? Name them.

## Residual risk

What remains after the change, and where is it recorded (risk register, ADR,
`progress.md`)?
