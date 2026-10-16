# Scopra Product Requirements Document

**Status:** Draft v1.0  
**Product:** Scopra  
**Category:** Controlled execution infrastructure for AI agents  
**Initial target:** Linux private alpha  
**Last updated:** 2026-10-16

## Executive Summary

Scopra is a secure execution gateway that lets AI agents interact with approved developer environments without unrestricted machine access. A local daemon registers workspaces, connects outbound to the control plane, enforces local policy, executes permitted operations in rootless isolation, and returns structured results.

> Give agents enough access to complete the task, never unlimited access to the machine.

## Problem

Developers repeatedly copy commands from browser AI tools into terminals and paste output back. This causes context switching, weak execution continuity, error-prone transfer, and pressure to grant unsafe broad access.

## Vision

Scopra becomes the trusted execution layer between AI agents and real computing environments: local machines, development servers, containers, Git, tests, databases, cloud systems, deployments, browsers, and internal tools.

## Goals

1. Eliminate manual command/output relay loops.
2. Provide machine-, workspace-, capability-, and time-scoped access.
3. Keep local policy as final authority.
4. Require exact-action approval for sensitive operations.
5. Return structured execution results.
6. Produce attributable audit records.
7. Support MCP and future integration methods.
8. Reach first successful execution in under five minutes.

## Alpha Non-Goals

- Unrestricted shell or remote desktop
- Root or administrator execution
- Production infrastructure administration
- Interactive PTY
- Browser extension
- Windows
- Multi-agent delegation
- Secret injection
- Team RBAC, SSO, or self-hosting

## Users

### AI-assisted developer

Runs tests, builds, linting, debugging, and project commands while controlling writes, network use, and sensitive actions.

### Technical founder

Operates multiple products and machines and needs resource selection, approvals, revocation, and auditability.

### Future administrator

Governs shared machines, agents, policies, approvals, identity lifecycle, and retention.

## Product Principles

1. Least authority by default.
2. Local enforcement is authoritative.
3. Structured execution before shell strings or PTY.
4. Approval binds to exact effect.
5. Repository and process output are untrusted.
6. Security remains usable.
7. Auditability is part of execution.
8. Escalation is narrow and explicit.

## Alpha Scope

- Linux x86_64
- Go daemon and CLI/TUI
- Short-lived device enrollment
- Authenticated outbound machine channel
- Workspace registration
- Rootless Podman execution
- Structured command execution
- Process status, streaming, and cancellation
- Scoped grants
- Exact-action approvals
- Minimal environment
- Network disabled by default
- Cloud and local policy
- Remote MCP
- Dashboard and audit timeline
- Grant and machine revocation

## Domain Model

- User
- Machine
- Workspace
- Agent
- Client
- Grant
- Session
- Execution
- Approval
- Policy and PolicyVersion
- Artifact
- AuditEvent
- Revocation

## Functional Requirements

### Installation and lifecycle

- Signed packages and checksums
- Guided TUI onboarding
- Device-generated asymmetric key
- Single-use browser activation code
- Machine inspection, rotation, and revocation
- Signed updates with rollback protection

### Workspaces

- Register and remove an existing directory
- Canonical path and repository fingerprint
- Read-only and authorized read-write modes
- Local maximum policy
- Removing authorization never deletes source files

### Grants

- Scope by user, agent, client, machine, workspace, capability, constraints, and expiration
- Short-lived access tokens
- Prompt revocation for online machines
- Mandatory cloud and local authorization
- Default deny for unknown operations and fields

### Execution

- Executable plus typed argument array
- Workspace-relative working directory
- Runtime, CPU, memory, PID, output, artifact, and concurrency limits
- Structured stdout, stderr, status, exit code, timing, truncation, and artifacts
- Cursor-based output reads
- Cancellation terminates descendants

### Approvals

- Freeze canonical request
- Bind approval to action hash, principals, resources, policy versions, expiry, and use count
- Display exact command and independent risk explanation
- Support Reject and Allow once
- Invalidate after material changes or revocation

### Environment and network

- Start from an empty environment
- No inherited SSH, GPG, browser, cloud, Docker, or Kubernetes credentials
- Network disabled by default and represented as a separate capability
- Block cloud metadata and sensitive link-local endpoints

### MCP tools

- `list_machines`
- `list_workspaces`
- `get_workspace`
- `execute_command`
- `get_execution`
- `read_execution_output`
- `cancel_execution`
- `request_capability`
- `get_approval_status`

### Dashboard

- Overview
- Machines and workspaces
- Grants
- Approvals
- Sessions and executions
- Integrations
- Account and security

### Audit

- Attributable security events
- Append-only history
- Local buffering and reconciliation
- Separate retention for metadata, output, and artifacts
- No bearer tokens or raw secrets in audit metadata

## Non-Functional Requirements

### Security

- Rootless containment
- No privilege elevation
- No host Docker socket
- No permanent master terminal credential
- Signed installer and update chain
- Encryption in transit and at rest
- Negative authorization tests for every resource

### Performance Targets

- Local policy p95 below 50 ms
- Cloud authorization p95 below 150 ms excluding network latency
- Request-to-local-start overhead p95 below 500 ms for online daemon
- Output streaming p95 below 500 ms
- Approval propagation p95 below 2 seconds

### Reliability

- Idempotent creation
- Reconnection without duplicate execution
- Every accepted execution reaches a terminal state
- Audit gap detection and reconciliation

## Security Invariants

1. No anonymous execution.
2. Every action is attributable.
3. Cloud cannot broaden local authority.
4. Agents cannot approve themselves.
5. Material changes invalidate approval.
6. Output cannot modify authorization.
7. Revocation has a defined objective.
8. Workers cannot modify Scopra security components.
9. Unknown capabilities are denied.
10. High-risk operations fail closed when required auditability is unavailable.

## Success Metrics

**North star:** Successful agent execution loops completed without manual terminal copy-paste.

Track installation, activation, workspace creation, integration connection, first execution, approval outcomes, revocation latency, resource-limit events, duplicate or lost executions, and audit gaps.

## Alpha Acceptance Criteria

A new user can install Scopra, activate a machine, register a workspace, connect an MCP client, issue a scoped grant, run a bounded command in rootless isolation, receive structured output, approve a sensitive action, cancel a process, inspect the timeline, and revoke access without exposing the home directory, host credentials, network, or Docker socket.
