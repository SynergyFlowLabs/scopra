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

**Important:** decoding proves structure only. It does not authenticate, check replay state, authorize, evaluate policy, or permit execution. Approval displays must escape control, format, and line or paragraph separator characters in arguments, which may contain any character except NUL.

## Implemented: canonical form and action hash

`ExecutionRequest.CanonicalJSON()` returns the canonical form of a valid request (ADR-008). It is the request's ADR-007 wire object serialized with the JSON Canonicalization Scheme (RFC 8785):

- members sorted by key, and no whitespace;
- minimal string escaping, with other characters written as literal UTF-8;
- integers in plain decimal.

The form is computed from the typed value, never from transmitted bytes, and it decodes back to the same request.

`ExecutionRequest.ActionHash()` returns the lowercase hexadecimal SHA-256 of the label `scopra/execution.request/v1`, a newline, and the canonical form. Both methods fail with `ErrInvalidExecutionRequest` for an invalid request, and both reproduce ADR-008's normative test vectors.

**Important:** the action hash identifies an action and authenticates nothing. A receiver must compute it from the request it decoded and verified, never trust a transmitted one, and get approval integrity from a signed approval record, which is not implemented yet.

## Implemented: execution results

`DecodeExecutionResult(payload)` strictly decodes an `execution.result` payload (ADR-009). The state comes first, because it selects the exact key set:

- `succeeded` and `failed` carry times, an exit code (0, or 1 to 255), and an output summary;
- `timed_out`, `cancelled`, and `out_of_memory` carry times and an output summary;
- `errored` carries an error and an output summary;
- `rejected` carries only an error.

Every other rule is ADR-007's, through the same strict-document checks, and every error wraps `ErrInvalidExecutionResult`.

- Times are RFC 3339 in UTC with exactly three fractional digits.
- Output summaries count bytes and chunks, never output itself. Their counts must be consistent with chunks of 1 to 65536 bytes.
- Error codes match `[a-z][a-z_]*`. Unknown well-formed codes are accepted, so that codes can be added.

`ExecutionResult.Validate()` checks typed values, including that the fields a state does not use are zero. `ValidateAgainst(request)` ties a result to its request: the same execution ID, the request's action hash, and output within its `limits.outputBytes`. `CanonicalJSON()` encodes a valid result with ADR-008's writer, for Mio to send.

## Implemented: start notices and output chunks

`DecodeExecutionStarted` decodes `execution.started`, which holds the execution ID, the action hash, and `startedAt` with millisecond precision. `ExecutionStarted.ValidateAgainst(request)` ties it to its request.

`DecodeExecutionOutput` decodes `execution.output`, which holds the execution ID, a `sequence` from 1 to 2^30, the `stream` (`stdout` or `stderr`), and `data`:

- `data` must be canonical unpadded base64url. Text that does not re-encode exactly is rejected, including padding, the standard alphabet, non-zero trailing bits, and the line breaks that Go's base64 decoder would skip.
- It decodes to 1 to 65536 bytes, kept exactly as the command wrote them, even when they are not UTF-8.
- Output data is untrusted and may hold secrets. It must never be logged.

Both payloads follow ADR-007's strict-document rules with exact keys. Their errors wrap `ErrInvalidExecutionStarted` and `ErrInvalidExecutionOutput` respectively, and each value has `Validate` and a canonical encoding.

The envelope's closed set now includes both types (ADR-002, amended). `Envelope.ExecutionRequest()`, `ExecutionResult()`, `ExecutionStarted()`, and `ExecutionOutput()` each decode only their own message type and fail with `ErrUnexpectedMessageType` otherwise.

Checking a sequence of chunks against a result is the receiver's job.

## Implemented: cancellation

`DecodeExecutionCancel` decodes `execution.cancel`, which holds the execution ID, the action hash, the principals who asked for the cancellation, and an untrusted `reason`. It follows ADR-007's strict-document rules with exact keys at both levels, and its errors wrap `ErrInvalidExecutionCancel`.

`ExecutionCancel.ValidateAgainst(request)` checks the execution ID and the action hash, so that Mio can ignore a cancel that does not match the running execution. It deliberately does not compare principals, because a user may cancel an agent's run. `Envelope.ExecutionCancel()` decodes only `execution.cancel`.

Decoding authorizes nothing: who may cancel is cloud policy, and the envelope must already have been verified.

Run the package tests with `go test -race ./packages/protocol`. Fuzz the decoders and the canonical forms with `go test -run '^$' -fuzz FuzzDecodeExecutionRequest ./packages/protocol`, `go test -run '^$' -fuzz FuzzCanonicalFieldValues ./packages/protocol`, `go test -run '^$' -fuzz FuzzDecodeExecutionResult ./packages/protocol`, `go test -run '^$' -fuzz FuzzDecodeExecutionStarted ./packages/protocol`, `go test -run '^$' -fuzz FuzzDecodeExecutionOutput ./packages/protocol`, `go test -run '^$' -fuzz FuzzExecutionOutputValues ./packages/protocol`, and `go test -run '^$' -fuzz FuzzDecodeExecutionCancel ./packages/protocol`.
