# ADR-008: Canonical Execution Request and Action Hash

**Status:** Proposed (awaiting the maintainer's confirmation; no code implements it yet)
**Date:** 2026-10-16

## Context

Approvals must bind to the exact effect. The PRD and the plan ask the system to:

- freeze the canonical request;
- bind an approval to an action hash, principals, resources, policy versions, expiry, and use count;
- invalidate an approval after any material change;
- have Mio recompute the hash locally before an approval is consumed.

ADR-002's payload hash covers the transmitted bytes, which change with whitespace, escaping, or key order. It therefore cannot identify one action consistently across Caloris, an approval display, and Mio.

ADR-007 made decoding lossless: every field is required, nothing has a default, `null` is refused, and decoded text equals transmitted text. A request's meaning is therefore exactly its typed value, which can be serialized deterministically. The cloud side and the TypeScript SDK must produce the same bytes as the Go code.

## Decision

### What is hashed

The hash covers the whole version 1 `ExecutionRequest`. That includes the principals, the grant and session, the reason, the timestamps, the execution ID, and the nonce. Changing any field gives a different hash, which needs a new approval.

Execution IDs and nonces are unique, so an approval can never cover a second request. "Allow once" therefore follows from the hash itself.

### Canonical form

The canonical form of a valid request is its ADR-007 wire object, serialized with the JSON Canonicalization Scheme (JCS, RFC 8785). For this schema that means:

- **Object members** are sorted by key. All keys are ASCII, so byte order equals JCS's UTF-16 order.
- **Whitespace:** none.
- **Strings** use minimal escaping:
  - `\"` and `\\`;
  - `\b`, `\t`, `\n`, `\f`, and `\r`;
  - `\u00xx` with lowercase hexadecimal digits for every other character below U+0020.

  Every other character appears as literal UTF-8. That includes U+007F, U+2028, U+2029, and all non-ASCII characters.
- **Integers** are written in plain decimal. Every limit is below 2^53, where JCS's number form is the plain integer.
- **Booleans** are `true` and `false`.
- **Arrays** keep their order, and an empty argument list is `[]`.
- **Timestamps** use ADR-007's form.

The canonical form is computed from the decoded and validated typed value, never from transmitted bytes. What is hashed is therefore exactly what Mio would run. The canonical bytes are themselves a valid ADR-007 payload that decodes to the same request, and canonicalizing that request again gives the same bytes.

### Action hash

```text
actionHash = lowercase-hex( SHA-256( "scopra/execution.request/v1\n" || canonical bytes ) )
```

The result is 64 characters long. The ASCII label separates this hash from ADR-002's payload hash and from any other Scopra object hashed the same way. A change to the canonical form or the algorithm changes the label's version. Canonical JSON never contains a raw newline, so the label and the canonical bytes cannot run together.

### Use

- **Caloris** freezes a request by computing its canonical form and action hash.
- **Approval displays** render the canonical request, escaping as ADR-007 requires, and show the agent's reason separately.
- **The approval record**, defined in a later ADR, binds the action hash to the approver, the decision, the policy versions, the approval's expiry, and the use count.
- **Mio** recomputes the action hash from the request it decoded and verified, and requires it to equal the approval's hash. It never trusts an action hash that was transmitted to it.

### Test vectors (normative)

Implementations must reproduce both vectors exactly. They were computed by an independent implementation, Python's `json` module with sorted keys, compact separators, and `ensure_ascii=False`, which matches JCS for this value domain. The first vector's hash was checked again with `sha256sum`.

**Vector 1.** This is ADR-007's example request. Its canonical form is 619 bytes:

```text
{"command":{"arguments":["test","./..."],"executable":"go","shell":false,"workingDirectory":"."},"createdAt":"2026-10-16T12:00:00Z","executionId":"exe_01HZX3","expiresAt":"2026-10-16T12:15:00Z","grantId":"grt_1","limits":{"cpuMillicores":2000,"memoryBytes":2147483648,"outputBytes":1048576,"pids":512,"timeoutSeconds":600},"network":"none","nonce":"kJ3b9sR0cT1uV2wX3yZ4aA","principals":{"agentId":"agt_1","clientId":"cli_1","userId":"usr_1"},"reason":"Run the unit tests after the fix","resources":{"machineId":"mch_1","workspaceId":"3f2a9c0b7d1e4f6a8b5c2d0e1f3a4b6c"},"sessionId":"ses_1","workspaceAccess":"read-only"}
```

Action hash: `b4f47c942fc6499aea49d76df1f89c630f8683bd69c98f292b9e03eed4dd44e2`

**Vector 2.** This is the same request with these changes:

```json
"command": {
  "executable": "./scripts/test.sh",
  "arguments": ["tab\there", "new\nline", "\u0001", "\u007f", "\u2028", "quote\" back\\slash", "\u00e9", "\ud83d\ude00", ""],
  "shell": false,
  "workingDirectory": "src/app"
},
"workspaceAccess": "read-write",
"limits": {"timeoutSeconds": 600, "cpuMillicores": 2000, "memoryBytes": 1099511627776, "pids": 512, "outputBytes": 1048576}
```

Its canonical form is 709 bytes. Its `arguments` member is encoded as these bytes:

```text
22 61 72 67 75 6d 65 6e 74 73 22 3a 5b 22 74 61 62 5c 74 68 65 72 65 22 2c 22 6e 65 77 5c 6e 6c 69 6e 65 22
2c 22 5c 75 30 30 30 31 22 2c 22 7f 22 2c 22 e2 80 a8 22 2c 22 71 75 6f 74 65 5c 22 20 62 61 63 6b 5c 5c 73
6c 61 73 68 22 2c 22 c3 a9 22 2c 22 f0 9f 98 80 22 2c 22 22 5d
```

Action hash: `9df45c503097070244bdcf27d06fb6c66e9328ee908d234ae03f3831a5731533`

## Consequences

- One identifier names an action across Caloris, approval displays, approval records, Catena audit events, and Mio.
- Any change, including to the reason or the timestamps, invalidates an approval. An agent that rewords its explanation, or retries after expiry, needs a new approval. This is intended: approvals are "Allow once", and material changes invalidate them.
- Transmissions of the same request that differ only in whitespace, escaping, or key order share one action hash, although their ADR-002 payload hashes differ.
- A future request version needs its own canonical form and label, such as `/v2`.

## Security Considerations

- **The action hash identifies; it does not authenticate.** Integrity comes from the signed approval record and from Mio's checks, both defined in later ADRs.
- **Local recomputation** from the decoded value stops a sender from pairing an approved hash with a different request. With ADR-007's lossless decoding, what is displayed, what is approved, and what runs cannot diverge.
- **SHA-256 collision resistance** is assumed, and the label prevents confusion between object types.
- **Bounded cost:** canonicalization runs only on validated requests and is linear in their size.

## Alternatives

- **Hashing the transmitted payload with ADR-002's payload hash:** rejected. Formatting changes would break approvals, and every party would need identical bytes.
- **Hashing only the effect (command, access, network, limits, and resources):** rejected. An approval must bind its principals and context, and the PRD forbids reusable approvals.
- **A custom or binary canonical form, such as deterministic CBOR or protobuf:** rejected. JCS is a published standard with TypeScript implementations, the wire format stays JSON, and for this fixed schema Go needs only a small encoder with no new dependency.
- **A base64url hash or a `sha256:` prefix:** rejected. Hexadecimal matches ADR-002's payload hash, and the label's version already allows the algorithm to change.
