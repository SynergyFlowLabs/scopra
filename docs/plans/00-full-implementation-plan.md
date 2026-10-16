# Scopra Full Implementation Plan

**Status:** Execution-ready draft v1.1  
**Target:** Linux private alpha  
**Last updated:** 2026-10-16

## Implementation Rule

The smallest valid Scopra loop is:

```text
authenticated agent request
  -> scoped grant
  -> cloud authorization
  -> authenticated machine dispatch
  -> local authorization
  -> exact approval when required
  -> isolated bounded execution
  -> structured result
  -> attributable audit record
```

If any mandatory stage is unavailable or invalid, deny the request. Never add unrestricted terminal access as an MVP shortcut.

## Technical Baseline

- Monorepo
- Go Scopra Mio daemon, Scopra Mariner CLI, Scopra Goldstone relay, and security-sensitive services
- TypeScript, React, and Next.js dashboard
- PostgreSQL system of record
- Redis for ephemeral coordination
- Encrypted object storage for optional artifacts
- Scopra Goldstone outbound TLS WebSocket machine relay carrying authenticated Scopra Messenger Protocol envelopes
- Rootless Podman behind a containment interface for Scopra Hollow
- OpenTelemetry-compatible observability
- Canonical component names are defined in `docs/product/naming.md`; retain `scopra` and `scoprad` executable names unless a separate compatibility decision changes them.

## Milestone Summary

1. Discovery and architecture baseline
2. Local secure execution foundation
3. Cloud identity, enrollment, and relay
4. Grants, authorization, approvals, and audit
5. Remote MCP and dashboard alpha
6. Private alpha readiness and launch
7. Public beta hardening
8. General availability and team governance

# Milestone-by-Milestone Task Checklist

## Milestone 0: Discovery and Architecture Baseline

### Product validation

- [ ] Recruit 15–20 target developers.
- [ ] Observe at least 10 real relay workflows.
- [ ] Measure commands, application switches, approval decisions, output size, and elapsed time.
- [ ] Identify the three highest-frequency and highest-cost workflows.
- [ ] Test willingness to install a daemon and grant workspace access.
- [ ] Compare approval preferences.
- [ ] Document trust objections.
- [ ] Select one remote MCP client and one local test client.
- [ ] Build a compatibility matrix.
- [ ] Establish the manual workflow baseline.

### Scope and architecture

- [ ] Freeze alpha goals and non-goals.
- [ ] Confirm Linux x86_64, rootless containment, structured execution, and network-off defaults.
- [ ] Defer PTY, browser extension, production administration, and native unrestricted execution.
- [ ] Create system, component, execution, enrollment, approval, revocation, and audit diagrams.
- [ ] Document cloud outage, reconnect, offline, output, and retention behavior.

### ADRs

- [ ] Monorepo and languages.
- [ ] Device identity and enrollment.
- [ ] Relay transport and reconnect.
- [ ] Containment backend.
- [ ] Canonical request.
- [ ] Audit integrity.
- [ ] MCP authorization.
- [ ] Output storage and redaction.
- [ ] Token revocation.
- [ ] Update signing.

### Security

- [ ] Publish threat model and risk register.
- [ ] Assign owners to critical and high risks.
- [ ] Map invariants to negative tests.
- [ ] Document guarantees and non-guarantees.
- [ ] Define incident severity, retention, deletion, and vulnerability reporting.

### Exit gate

- [ ] Six qualified users agree to install a prototype.
- [ ] Selected integration can invoke tools.
- [ ] P0 ADRs are approved.
- [ ] Critical risks have owners and treatments.
- [ ] Team can explain the complete path and boundaries.

---

## Milestone 1: Local Secure Execution Foundation

### Engineering platform

- [ ] Initialize monorepo, branch protection, CODEOWNERS, CI, linting, testing, secret scanning, dependency checks, SBOM, schemas, and templates.
- [ ] Create one-command local development setup.

### Scopra Messenger Protocol

- [ ] Define protocol versions, IDs, canonical request, results, output chunks, errors, and execution states.
- [ ] Add validation and compatibility tests.

### Scopra Mio daemon and Scopra Mariner CLI

**Initial increment implemented:** foreground `scoprad`, `scopra status`, local version reporting, context-driven shutdown, and strict same-UID Unix-socket status IPC are in place. Workspace CRUD, service installation, `doctor`, execution, and the broader CLI remain pending; the checklist items below describe those larger milestones and are intentionally unchecked.

- [ ] Implement Scopra Mio (`scoprad`), foreground mode, Linux service, state directory, lock, shutdown, health, logging, and versioning.
- [ ] Implement Scopra Mariner commands, including `scopra doctor`, daemon status, workspace CRUD, local execution, status, logs, cancellation, JSON output, and TTY output.
- [ ] Implement authenticated bounded Unix-socket IPC and malformed-message tests.

### Workspace and containment

- [ ] Canonicalize workspace and record repository fingerprint.
- [ ] Reject root and complete home directory.
- [ ] Revalidate workspace before execution.
- [ ] Implement the Scopra Hollow rootless Podman adapter behind the containment interface.
- [ ] Mount only workspace; default read-only.
- [ ] Disable network, devices, privileged mode, Docker socket, and host PID namespace.
- [ ] Drop capabilities and enable `no-new-privileges`.
- [ ] Enforce CPU, memory, PID, runtime, temporary storage, and output limits.
- [ ] Fail closed without containment.

### Execution and policy

- [ ] Invoke direct executable plus argument array.
- [ ] Construct an empty environment and ephemeral home.
- [ ] Exclude credential sockets and host secrets.
- [ ] Capture stdout and stderr with sequence, limits, backpressure, and truncation.
- [ ] Track status, timing, exit code, cancellation, descendants, and cleanup.
- [ ] Implement Scopra Resolution default-deny evaluation and initial policy profiles; enforce the local maximum through Scopra Rupes.

### Adversarial tests

- [ ] Path traversal, symlinks, hard links, `/proc`, `/sys`, metadata endpoints, sockets, environment leakage, fork bomb, output flood, disk exhaustion, orphan process, setuid, `sudo`, terminal controls, and runtime failure.

### Exit gate

- [ ] Direct execution works in rootless containment.
- [ ] Workspace is the only approved mount.
- [ ] Home, network, and credential sockets are unavailable.
- [ ] Limits and cancellation work.
- [ ] No native fallback exists.
- [ ] Adversarial tests pass.

---

## Milestone 2: Cloud Identity, Enrollment, and Relay

### Foundation and authentication

- [ ] Provision development, staging, PostgreSQL, Redis, secrets, identities, TLS, DNS, migrations, logs, metrics, and traces.
- [ ] Implement account creation, sign-in, secure sessions, recent authentication, listing, revocation, and enrollment notices.

### Scopra Mariner device enrollment

- [ ] Generate key locally and store securely.
- [ ] Define machine credential, expiry, rotation, and revocation.
- [ ] Implement short-lived, single-use, public-key-bound activation.
- [ ] Display machine characteristics and fingerprint in browser.
- [ ] Audit and notify enrollment.

### Scopra Goldstone relay

- [ ] Implement authenticated outbound connection.
- [ ] Validate revocation and minimum version.
- [ ] Add presence leases, heartbeat, stale cleanup, bounded queues, frame limits, and rates.
- [ ] Route by authenticated machine identity.
- [ ] Add message IDs, sequences, acknowledgements, connection epochs, duplicate prevention, reconciliation, and lost-state behavior.
- [ ] Add machine list, detail, presence, and revocation UI.

### Security tests

- [ ] Code reuse, key substitution, expiry, activation race, wrong user, response replay, wrong machine routing, revoked reconnect, oversized frames, and invalid sequence.

### Exit gate

- [ ] Clean daemon activates without bearer token in URL.
- [ ] Enrollment is bound, single-use, and expiring.
- [ ] Presence is accurate.
- [ ] Message reaches only intended daemon.
- [ ] Revoked daemon cannot reconnect.

---

## Milestone 3: Grants, Authorization, Approvals, and Audit

### Data and identity

- [ ] Create schemas for users, sessions, machines, credentials, workspaces, agents, clients, grants, capabilities, executions, transitions, approvals, policies, revocations, and audit.
- [ ] Add ownership and idempotency constraints.
- [ ] Keep human, agent, and client identities separate.

### Grants and tokens

- [ ] Implement grant creation, inspection, narrowing, and revocation.
- [ ] Require resource, principal, capability, execution, network, environment, limit, expiration, and approval constraints.
- [ ] Issue short-lived audience-bound tokens and store opaque tokens as hashes.

### Authorization

- [ ] Authenticate client and resolve all principals.
- [ ] Validate grant, scope, capability, and limits.
- [ ] Apply cloud policy and create canonical request.
- [ ] Sign dispatch envelope.
- [ ] Locally verify issuer, audience, signature, time, nonce, replay, payload hash, resource, local maximum policy, and effective limits.
- [ ] Deny if any layer denies.

### Revocation

- [ ] Revoke users, clients, grants, workspaces, machines, sessions, and pending executions.
- [ ] Push revocation to online daemons.
- [ ] Maintain local revocation high-water mark and reject stale dispatches.
- [ ] Measure propagation and define offline behavior.

### Approval

- [ ] Freeze request and compute action hash.
- [ ] Bind principals, resources, policies, expiry, nonce, and use count.
- [ ] Generate independent risk reasons.
- [ ] Show exact request and separate agent explanation.
- [ ] Provide Reject and Allow once; no global always-allow.
- [ ] Verify approver, integrity-protect decision, recompute locally, and consume atomically.
- [ ] Invalidate after change or revocation.

### Scopra Catena audit

- [ ] Emit versioned Scopra Catena events for all identity, grant, session, execution, policy, approval, revocation, timeout, cancellation, truncation, and resource-limit actions.
- [ ] Persist the Catena event chain append-only with sequence integrity, buffering, and reconciliation.
- [ ] Exclude tokens, secrets, and raw output from metadata.

### Negative tests and exit gate

- [ ] Wrong client, machine, workspace, audience, capability, limits, expired grant, revoked grant, replay, modified action, reused approval, expired approval, post-revocation approval, double submit, cloud allow/local deny, and cross-user object access all fail safely.
- [ ] Grant authorizes only exact scope.
- [ ] Revocation meets target.
- [ ] Audit timeline is complete.

---

## Milestone 4: Remote MCP and Dashboard Alpha

### MCP

- [ ] Implement protected-resource metadata and delegated authorization with exact redirects and PKCE where applicable.
- [ ] Bind authorization to a grant and support revocation.
- [ ] Implement all initial MCP tools.
- [ ] Bound output, use cursors, return execution IDs, and distinguish denial, approval, offline, timeout, and failure.
- [ ] Label output as untrusted and version tool schemas.
- [ ] Publish agent instructions for resources, direct execution, polling, approvals, capabilities, untrusted content, and artifacts.

### Dashboard

- [ ] Build overview, machine, workspace, grant, approval, session, execution, integration, and security surfaces.
- [ ] Keep approval UI separate from output and render command as untrusted text.
- [ ] Never display a reusable master key for chat copy-paste.

### Compatibility and exit gate

- [ ] Test authorization, discovery, safe execution, approval, streaming, cancellation, offline behavior, revocation, and refresh.
- [ ] Client sees only authorized resources.
- [ ] End-to-end command and approval complete.
- [ ] Structured result returns through MCP.
- [ ] Dashboard timeline is complete.

---

## Milestone 5: Private Alpha Readiness and Launch

### Release and updates

- [ ] Controlled builds, separate signing identities, Linux artifacts, checksums, signatures, SBOM, scans, notes, and clean-machine verification.
- [ ] Installer detects platform, verifies package, uses minimum privilege, rolls back partial install, and documents removal.
- [ ] Add channels, signed updates, staged rollout, minimum safe version, rollback protection, and emergency revocation.

### Security, reliability, and operations

- [ ] Run all auth, authorization, enrollment, replay, approval, escape, leakage, exhaustion, and audit suites.
- [ ] Fuzz protocol and security parsers.
- [ ] Review crypto, tokens, daemon, containment, and approval UX.
- [ ] Resolve critical findings.
- [ ] Test reconnect, duplication, terminal states, audit reconciliation, scale, latency, backpressure, approvals, and revocation.
- [ ] Add dashboards, alerts, backups, restore, and runbooks.

### Documentation, metrics, and cohort

- [ ] Publish installation, activation, workspace, policies, MCP, approvals, naming, troubleshooting, removal, limitations, security, and data handling.
- [ ] Instrument activation and execution funnels without commands, raw output, secrets, or full paths.
- [ ] Recruit 20–50 qualified users and establish onboarding, support, diagnostics, triage, patching, and emergency communication.

### Exit gate

- [ ] Clean install works.
- [ ] Median setup is under five minutes.
- [ ] End-to-end flow passes.
- [ ] No critical finding remains.
- [ ] Execution loss and revocation meet targets.
- [ ] Restore and runbooks are verified.

---

## Milestone 6: Public Beta Hardening

- [ ] Analyze alpha activation, approvals, failures, unmet workflows, remaining manual relay, trust, and churn.
- [ ] Add local redaction, destination profiles, egress records, encrypted artifacts, retention, custom policy, simulation, improved integrations, billing, and plan limits.
- [ ] Decide Linux ARM64 and macOS based on evidence.
- [ ] Commission penetration test and remediate serious findings.
- [ ] Verify audit integrity, stronger approval, anomaly detection, key rotation, incident response, recovery, and deletion.
- [ ] Redundantly run services, load-balance relay, validate database capacity and failover, quotas, reconciliation, and status communication.
- [ ] Publish OpenAPI and TypeScript SDK.
- [ ] Decide browser extension from measured unmet demand; if built, require separate threat model, explicit gesture, trusted UI, restricted origins, isolated credentials, no remote code, and no silent execution.
- [ ] Public beta requires redaction, retention, cross-boundary tests, replay tests, prompt-injection-safe policy behavior, tested incident response, and documented capacity.

---

## Milestone 7: General Availability and Team Governance

- [ ] Add organizations, memberships, roles, shared machines, workspaces, team agents, approval routing, central policies, and audit export.
- [ ] Add SSO, lifecycle provisioning, authentication policy, and just-in-time administration.
- [ ] Add policy-as-code validation, simulation, review, signed bundles, local pinning, rollback, and audit.
- [ ] Add private networking, private/self-hosted options, customer-managed encryption where needed, data residency, SIEM, customer retention, and advanced isolation.
- [ ] Remove standing operator terminal access and require customer-authorized time-limited support grants with named operators and complete audit.
- [ ] Establish service objectives, on-call, support escalation, recovery exercises, capacity review, terms, privacy, security questionnaires, procurement material, and customer success.
- [ ] GA requires measured service objectives, demonstrated disaster recovery, operating security disclosure and patching, verified retention/deletion, audited JIT administration, and capacity headroom.

## Critical Path

```text
protocol
 -> Scopra Mio daemon and IPC
 -> Scopra Hollow containment
 -> Scopra Mariner enrollment
 -> Scopra Goldstone relay
 -> grants and authorization
 -> Scopra Rupes local policy enforcement
 -> approval
 -> Scopra Catena audit
 -> remote MCP
 -> end-to-end tests
 -> signed release
```

## First 30 Days

### Week 1

- [ ] Freeze alpha workflows.
- [ ] Complete five observations.
- [ ] Bootstrap repo and CI.
- [ ] Approve core ADRs.
- [ ] Build Scopra Mio daemon and Scopra Mariner doctor spike.
- [ ] Validate rootless Podman.
- [ ] Draft protocol.

### Week 2

- [ ] Build workspace registry.
- [ ] Execute direct commands in containment.
- [ ] Add limits and cancellation.
- [ ] Start policy schema.
- [ ] Prototype key dashboard screens.

### Week 3

- [ ] Add local IPC and CLI.
- [ ] Add structured output and state machine.
- [ ] Add adversarial harness.
- [ ] Begin identity and device-code service.
- [ ] Generate machine key and fingerprint.

### Week 4

- [ ] Complete development enrollment.
- [ ] Establish authenticated Scopra Goldstone relay.
- [ ] Show presence.
- [ ] Route signed no-op.
- [ ] Review risks and estimates.
- [ ] Demonstrate walking skeleton from clean VM.

## Checklist Rules

- [ ] Every item has an owner before work begins.
- [ ] Every active item links to implementation evidence.
- [ ] Blocked work records dependencies and decision dates.
- [ ] Security gates cannot be waived only for schedule.
- [ ] Deferred work records rationale.
- [ ] Architecture changes update ADRs.
- [ ] Exit gates have recorded sign-off.
- [ ] Review checklist weekly and after each retrospective.
