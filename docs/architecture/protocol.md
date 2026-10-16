# Scopra Messenger Protocol

Scopra Messenger is the internal, versioned request and dispatch protocol used between cloud services and the local daemon. It is distinct from the Model Context Protocol (MCP), which is an agent integration interface.

Cloud-to-Mio envelopes contain version, type, message ID, device audience, issuer, sequence, creation time, expiry, nonce, payload hash, and authentication proof.

Execution requests contain all principals, resources, grant/session IDs, executable, arguments, explicit shell flag, working directory, profiles, resource limits, output policy, reason, timestamps, and nonce.

The Go package `packages/protocol` implements bounded structural decoding for envelope v1. After `DecodeEnvelope`, callers must invoke `Envelope.ValidateAt(now)` to enforce freshness. Neither method verifies the authentication proof, performs replay checks, authorizes requests, or validates message-specific payloads; callers must fail closed until all required checks are complete.
