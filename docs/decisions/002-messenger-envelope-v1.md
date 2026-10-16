# ADR-002: Scopra Messenger Envelope v1 Structural Contract

**Status:** Accepted  
**Date:** 2026-10-16

## Context

Cloud-to-daemon messages carry an authenticated envelope and an arbitrary typed payload. The receiver must reject malformed, ambiguous, oversized, stale, or unknown messages before dispatch. Structural validation is not authentication, authorization, replay protection, or execution permission.

## Decision

- Use a strict JSON envelope with protocol version `v1` and a closed set of message types.
- Reject unknown envelope fields, duplicate JSON object keys at every nesting level, trailing JSON values, and documents over 1 MiB.
- Limit an individual payload to 512 KiB and bind its exact transmitted JSON bytes with a lowercase SHA-256 digest.
- Require a nonzero sequence, opaque message/issuer/audience identifiers, a 128-bit-or-longer URL-safe nonce, and a nonempty bounded authentication-proof field.
- Limit envelope lifetime to five minutes and allow up to 30 seconds of issuer clock skew for a future creation time. Expiry itself is strict.
- Keep signature verification, issuer/device key binding, sequence/replay storage, authorization, and payload-specific decoding as separate mandatory checks before dispatch. The protocol package must not present structural validation as proof of trust.

## Consequences

- Every receiver gets consistent size, ambiguity, version, and freshness checks.
- A payload digest changes if the transmitted JSON bytes change, including whitespace. A separate canonical action representation is still required for approval binding.
- Adding message types or changing wire semantics requires compatibility tests and an explicit protocol-version decision.

## Security Considerations

The envelope is untrusted until cryptographic verification and local authorization succeed. `DecodeEnvelope` only parses and validates structure. Callers must fail closed, verify the proof against the expected issuer and device audience, enforce sequence/nonce replay state, validate the typed payload, and apply local maximum policy before any action.
