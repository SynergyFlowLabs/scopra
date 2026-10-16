# ADR-007: Execution Request v1 Structural Contract

**Status:** Proposed. It awaits the maintainer's confirmation, and no code implements it yet.  
**Date:** 2026-10-16

## Context

ADR-002 defines the Messenger envelope and reserves the `execution.request` message type, but leaves payload-specific validation to the receiver. The architecture lists what an execution request carries (`docs/architecture/protocol.md`): principals, resources, grant and session IDs, executable, arguments, an explicit shell flag, working directory, profiles, resource limits, output policy, reason, timestamps, and nonce. The PRD adds five requirements:

- an executable plus a typed argument array;
- a workspace-relative working directory;
- an empty environment;
- network disabled by default and represented as a separate capability;
- default deny for unknown operations and fields.

Approvals must later bind to the exact request, so its decoded meaning must equal what was transmitted. A JSON detail matters here: Go's decoder silently replaces invalid UTF-8 and lone surrogate escapes with U+FFFD, and it accepts `null` or a missing field as a zero value. Either would let a displayed or hashed request differ from the transmitted one.

## Decision

### Shape

The `execution.request` payload is one JSON object. Every field is required, each object must have exactly the listed keys, and `null` is never allowed:

```json
{
  "executionId": "exe_01HZX3",
  "principals": {"userId": "usr_1", "agentId": "agt_1", "clientId": "cli_1"},
  "resources": {"machineId": "mch_1", "workspaceId": "3f2a9c0b7d1e4f6a8b5c2d0e1f3a4b6c"},
  "grantId": "grt_1",
  "sessionId": "ses_1",
  "command": {
    "executable": "go",
    "arguments": ["test", "./..."],
    "shell": false,
    "workingDirectory": "."
  },
  "workspaceAccess": "read-only",
  "network": "none",
  "limits": {
    "timeoutSeconds": 600,
    "cpuMillicores": 2000,
    "memoryBytes": 2147483648,
    "pids": 512,
    "outputBytes": 1048576
  },
  "reason": "Run the unit tests after the fix",
  "createdAt": "2026-10-16T12:00:00Z",
  "expiresAt": "2026-10-16T12:15:00Z",
  "nonce": "kJ3b9sR0cT1uV2wX3yZ4aA"
}
```

### Field rules

| Field | Rule |
|---|---|
| `executionId`, `principals.*`, `resources.machineId`, `grantId`, `sessionId` | Envelope identifier syntax (ADR-002): 1–128 bytes of `A–Z a–z 0–9 - _ . :`. `executionId` names the execution for idempotency: a receiver must not start the same ID twice. |
| `resources.workspaceId` | A Mio registration ID: 32 lowercase hexadecimal characters (ADR-005). Mio looks the workspace up locally and must revalidate it before use (ADR-004). |
| `command.executable` | 1–4095 bytes with no control (Cc) or format (Cf) characters, in one of three forms:<br>(1) a bare name without `/`, other than `.` and `..`, looked up in a fixed search path set by the sandbox, never by the request;<br>(2) an absolute path inside the sandbox, in clean form;<br>(3) `./` followed by a clean relative path with no `..` component, resolved against the working directory (for example `./gradlew`). |
| `command.arguments` | An array, possibly empty, of at most 4096 strings, each at most 131072 bytes (Linux `MAX_ARG_STRLEN`). Any character except NUL is allowed, because tabs and newlines are legitimate arguments. |
| `command.shell` | A boolean. Version 1 accepts only `false`: shell strings are deferred (PRD principle 3), and the field keeps any future shell use explicit. |
| `command.workingDirectory` | `.` or a clean relative path with no `..` component and no control or format characters, at most 4095 bytes. It is resolved inside the workspace. |
| `workspaceAccess` | `read-only` or `read-write`. The wire format has no default, so an approval shows any write access. |
| `network` | Only `none` in version 1. Network access is a separate capability that is not designed yet. |
| `limits.timeoutSeconds` | 1–86400. |
| `limits.cpuMillicores` | 1–1000000. |
| `limits.memoryBytes` | 1 MiB–1 TiB. |
| `limits.pids` | 1–65536. |
| `limits.outputBytes` | 1–1 GiB, for stdout and stderr combined. |
| `reason` | The agent's untrusted explanation: 1–1024 bytes with no control or format characters. It is displayed apart from the exact command and grants nothing. |
| `createdAt`, `expiresAt` | RFC 3339 in UTC with second precision (`YYYY-MM-DDTHH:MM:SSZ`). `expiresAt` must be after `createdAt` and at most 1 hour later. |
| `nonce` | Envelope nonce syntax: unpadded base64url encoding 16–128 bytes. |

The limit ranges are protocol sanity bounds, not policy. Effective limits are the minimum of the request, the grant, and Rupes's local maximum.

### Strict decoding

`protocol.DecodeExecutionRequest(payload)` must:

- reject a payload over `MaxPayloadBytes`, trailing data, duplicate keys at any depth, a `null` anywhere, invalid UTF-8, and any `\u` escape that does not form a valid Unicode scalar value (including lone surrogates and an escaped NUL);
- require each object to have exactly the listed keys, and accept integers only (no fraction, exponent, or sign) for limits;
- return a typed `ExecutionRequest` or an error that names the offending field, and never a partially valid request.

`Envelope.ExecutionRequest()` decodes the payload only when the envelope's type is `execution.request`. `ExecutionRequest.ValidateAt(now)` enforces the request's own validity window: `createdAt` may be at most 30 seconds in the future (ADR-002's allowed skew), and expiry is strict. This window is separate from the envelope's lifetime of at most 5 minutes. A request may wait for approval for up to an hour, but each envelope that carries it must be fresh.

### Deferred, and absent from version 1

- **Environment variables.** The command runs with an empty environment plus the fixed variables that the sandbox profile defines, such as `PATH` and an ephemeral `HOME`. Request-chosen variables need a policy on allowed names (R-15).
- **Standard input.** It is always empty.
- **Named profiles and output policy.** `protocol.md` lists them, but version 1 states the concrete effects directly (`workspaceAccess`, `network`, and `limits.outputBytes`). Named policy profiles, output retention, and redaction come with Resolution and the output ADR.
- **Artifacts and concurrency limits.** No artifact design exists yet, and concurrency is a grant or session limit, not a per-request one.
- **The canonical form and action hash used for approval binding.** These need their own decision (the "canonical execution requests" follow-up). This contract is designed to make that possible: there are no defaults, nulls, or ambiguous paths, and decoded text equals transmitted text.

## Consequences

- Hollow, Rupes, and approvals get one strictly typed request with explicit effects (mount mode, network, limits) and no defaults that could change meaning later.
- Strict decoding rejects some harmless documents, such as a field with a `null` value or a timestamp with a numeric offset. Senders must emit the exact form.
- Until the alpha freeze nothing depends on this payload, so incompatible changes before then amend this ADR. After the freeze, ADR-002's rule applies: such changes need a protocol-version decision and compatibility tests.

## Security Considerations

Decoding proves only structure. It does not authenticate the envelope, check replay state, authorize any principal, evaluate policy, or permit execution. Every check in the security architecture remains mandatory, and Rupes must still enforce the local maximum and revalidate the workspace.

Rejecting silent Unicode replacement and requiring clean paths and explicit fields keep the displayed, approved, and executed request identical. Rejecting format characters in the executable, working directory, and reason closes bidirectional-text spoofing in those fields. Arguments may contain any character except NUL, so approval displays must escape control and format characters in arguments.

## Alternatives

- **Generic JSON with optional fields and defaults:** rejected, because defaults and missing-versus-empty ambiguity break exact approval binding.
- **Accepting shell strings now:** rejected for version 1 by PRD principle 3. The explicit field avoids a silent future change.
- **Cloud-side workspace IDs:** rejected for version 1. Registration is local (ADR-006), and Rupes must resolve the workspace from Mio's registry.
