# Scopra Security Threat Model

## Objective

Permit explicitly authorized agent operations in an explicitly authorized workspace while preventing escape, credential theft, privilege escalation, approval bypass, replay, audit evasion, exfiltration, persistence, lateral movement, and unbounded resource use.

## Trust Assumptions

Agents, prompts, repositories, terminal output, web content, dependencies, networks, users, cloud components, and hosts may be malicious or compromised. Content never grants authority.

## Assets

Operating system, source and files, credentials, device keys, grants, sessions, policies, approvals, audit records, output, artifacts, signing keys, and ownership boundaries.

## Actors

External attacker, compromised agent, prompt-injected agent, malicious repository or dependency, compromised user, malicious insider, and compromised daemon or host.

## Boundaries

```text
AI host -> Scopra Caloris -> Scopra Goldstone / Messenger -> Scopra Mio -> Scopra Rupes -> Scopra Hollow -> workspace / network
```

## Security Invariants

1. No ambient terminal authority.
2. Every request is attributable.
3. Local authorization is mandatory.
4. Agents cannot approve themselves.
5. Approval binds to exact effect.
6. Default deny applies.
7. Daemon cannot silently increase privilege.
8. Audit is append-only.
9. Output is untrusted.
10. Revocation is prompt for connected systems.

## Threats and Controls

### Spoofing and credential theft

Short-lived grants, audience/resource binding, asymmetric machine identity, public-key-bound activation, secure storage, revocation, and no permanent chat-pasted master key.

### Tampering and replay

Canonical requests, action hash, authenticated envelopes, nonce, sequence, expiry, replay cache, local recomputation, and atomic approval use.

### Workspace escape

Rootless containment, minimal mounts, canonical paths, restricted `/proc`, no Docker socket, no host PID namespace, dropped capabilities, seccomp/MAC where available, and adversarial tests.

### Prompt injection and tool abuse

Model-independent permissions, untrusted content labeling, network and secrets denied by default, session-aware limits, and exact approval.

### Disclosure and exfiltration

Empty environment baseline, no home or credential sockets, separate network capability, destination controls, metadata blocking, local redaction, and bounded retention.

### Privilege escalation

Unprivileged daemon and worker, no `sudo`, rootless runtime, no privileged mode or device mounts, and separate updater/executor.

### Confused deputy

Preserve requesting principal, authorize the final resource, validate token audience, and never use a broad shared downstream credential.

### Denial of service or wallet

CPU, memory, PID, runtime, output, artifact, concurrency, request, and budget limits; backpressure; process-tree termination; loop detection.

### Audit repudiation

Append-only events, sequence integrity, checkpoints, buffering, reconciliation, and fail-closed behavior for configured high-risk actions.

### Supply chain

Signed artifacts and metadata, checksums, SBOM, dependency scanning, protected keys, staged rollout, minimum safe versions, and emergency revocation.

## Residual Risks

Approved actions may expose data; unknown sandbox vulnerabilities may exist; users may approve danger; redaction may miss formats; root-compromised hosts are untrustworthy; agents may split harmful goals across actions.

## Verification

Fuzzing; path, symlink, replay, approval, ownership, exhaustion, leakage, and prompt-injection tests; external penetration testing; and release-pipeline review.
