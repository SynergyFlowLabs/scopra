# Scopra Messenger Protocol

Scopra Messenger is the internal, versioned protocol for authenticated request and dispatch envelopes between cloud services and the local daemon. It is distinct from MCP.

## Implemented: envelope v1 structural validation

The Go package at `packages/protocol` provides a bounded JSON envelope decoder and validation for the v1 wire shape. It rejects oversized payloads, unknown envelope fields, duplicate JSON object keys, trailing values, unknown message types, invalid identifiers/nonces, and payload-digest mismatches. The payload digest is SHA-256 over the exact transmitted JSON payload bytes. Call `Envelope.ValidateAt(now)` after decoding to enforce the maximum lifetime, allowed future-clock skew, and strict expiry; `DecodeEnvelope` alone does not check freshness.

**Important:** parsing and structural/freshness validation do not authenticate `AuthenticationProof`, prevent replay, authorize a principal, validate a message-specific payload, or permit execution. Callers must perform those checks and apply local maximum policy before dispatch. See `docs/decisions/002-messenger-envelope-v1.md`.

## Implemented: execution request v1 decoding

`DecodeExecutionRequest(payload)`, or `Envelope.ExecutionRequest()` for an envelope of type `execution.request`, returns a typed `ExecutionRequest` that follows ADR-007.

- Every field is required, each object must have exactly the listed keys, and `null` is never allowed.
- Decoded text equals transmitted text. Invalid UTF-8, any `\u` escape that is NUL or not a Unicode scalar value, and duplicate keys are rejected. The standard JSON decoder would otherwise replace bad escapes with U+FFFD, keep the last duplicate, and treat `null` as a zero value.
- Field rules cover identifiers, Mio registration IDs, the three executable forms, arguments, `shell` (`false` only), a clean workspace-relative working directory, `workspaceAccess`, `network` (`none` only), sanity-bounded limits, the reason, UTC second-precision timestamps, and the nonce. The executable, working directory, and reason contain no control, format, or line or paragraph separator characters. Every error wraps `ErrInvalidExecutionRequest` and names the field; quoted names keep untrusted keys printable. No partially valid request is returned.
- `ExecutionRequest.Validate()` applies the same field rules to a typed request built in memory. `ExecutionRequest.ValidateAt(now)` also enforces the request window: at most 30 seconds of future skew, strict expiry, and a lifetime of at most one hour.

**Important:** decoding proves structure only. It does not authenticate, check replay state, authorize, evaluate policy, or permit execution, and the canonical form and action hash for approval binding are not defined yet. Approval displays must escape control, format, and line or paragraph separator characters in arguments, which may contain any character except NUL.

Run the package tests with `go test -race ./packages/protocol`, and fuzz the decoder with `go test -run '^$' -fuzz FuzzDecodeExecutionRequest ./packages/protocol`.
