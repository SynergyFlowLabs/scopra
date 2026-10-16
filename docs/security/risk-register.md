# Scopra Prioritized Risk Register

**Scoring:** Likelihood 1–5 × Impact 1–5. 20–25 Critical, 15–19 High, 8–14 Medium, 4–7 Low.

## Critical

### R-01 Workspace or sandbox escape, 20

Rootless containment, minimal mounts, canonical paths, restricted `/proc`, no Docker socket or host PID namespace, no access to Mio's state directory or socket (ADR-006), seccomp/MAC, and adversarial tests.

### R-02 Long-lived credential compromise, 20

Short-lived grants, audience binding, token hashing, revocation, and no tokens in URLs or chats.

### R-03 Approval bypass or substitution, 20

Canonical action hash, nonce, expiry, one-time use, local recomputation, and atomic consumption.

### R-04 Prompt-injection-driven abuse, 25

Permissions outside the model, untrusted output, network/secrets denied by default, and exact approvals.

### R-05 Secret exfiltration, 20

Empty environment, no home or credential sockets, separate network capability, redaction, and sensitive-read/external-write detection.

## High

- **R-06 Cross-boundary authorization failure, 15:** Central authorization, ownership constraints, negative tests, penetration testing.
- **R-07 Daemon RCE, 15:** Memory-safe core, strict schemas, limits, fuzzing, unprivileged components.
- **R-08 Supply-chain compromise, 15:** Signed releases, protected keys, SBOM, provenance, staged updates.
- **R-09 Privilege escalation, 15:** No `sudo`, rootless workers, dropped capabilities, no privileged sockets.
- **R-10 Message tampering or replay, 15:** TLS, authenticated envelope, audience, nonce, sequence, replay cache.
- **R-11 Policy tampering, 15:** Local maximum policy, signed versions, permissions, audit, notification.
- **R-12 Misleading agent explanation, 16:** Independent risk explanation and exact command display.
- **R-13 Confused deputy, 15:** Preserve principal, resource-specific authorization, audience validation.
- **R-14 Network exfiltration, 16:** Network off by default, destination grants, metadata blocking, quotas.
- **R-15 Environment inheritance, 16:** Empty baseline, explicit variables, no credential sockets.
- **R-16 Risk-engine evasion, 16:** Classifier never grants authority; hard capabilities and isolation remain authoritative.
- **R-17 Malicious dependency scripts, 16:** Approval, sandbox, scripts-disabled mode, network control, lockfile review.

## Medium

- Audit tampering or gaps
- Output leakage into cloud logs
- Resource exhaustion
- Denial of wallet
- Terminal escape sequences
- Enrollment phishing
- Delayed revocation
- Wrong machine or workspace
- Orphaned processes

## Product and Business

Security incident destroys trust; claims exceed guarantees; platform scope weakens isolation; AI incompatibility limits adoption; command forwarding becomes commoditized; naming conflict forces rebrand; retention expands liability; over-engineering delays validation.

## Priority Order

1. Remove permanent API keys.
2. Require rootless isolation.
3. Enforce locally.
4. Bind approval exactly.
5. Use minimal environment.
6. Enforce limits.
7. Disable network by default.
8. Attribute every action.
9. Sign installation and updates.
10. Build regression tests before broadening features.
