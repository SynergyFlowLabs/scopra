# Scopra Messenger Protocol

Scopra Messenger is the internal, versioned protocol for authenticated request and dispatch envelopes between cloud services and the local daemon. It is distinct from MCP.

## Implemented: envelope v1 structural validation

The Go package at `packages/protocol` provides a bounded JSON envelope decoder and validation for the v1 wire shape. It rejects oversized payloads, unknown envelope fields, duplicate JSON object keys, trailing values, unknown message types, invalid identifiers/nonces, and payload-digest mismatches. The payload digest is SHA-256 over the exact transmitted JSON payload bytes. Call `Envelope.ValidateAt(now)` after decoding to enforce the maximum lifetime, allowed future-clock skew, and strict expiry; `DecodeEnvelope` alone does not check freshness.

**Important:** parsing and structural/freshness validation do not authenticate `AuthenticationProof`, prevent replay, authorize a principal, validate a message-specific payload, or permit execution. Callers must perform those checks and apply local maximum policy before dispatch. See `docs/decisions/002-messenger-envelope-v1.md`.

Run the package tests with `go test -race ./packages/protocol`.
