# Machine identity

`identity.go` owns Scopra's Linux machine Ed25519 key and token-free cloud
pairing metadata. `machine.ed25519` and `cloud.json` live in Mio's private state
directory; both are regular files owned by the effective user with mode
`0600`, and the key file must have one link. The private key is binary
Ed25519 material, never included in an API payload, and signatures use the
versioned newline-delimited protocol strings shared with Caloris.

The CLI creates the key once, uses it for enrollment proof, and writes the
cloud mapping only after dashboard approval. Mio loads (but never creates) the
key when pairing metadata exists and sends only signed, outbound presence
heartbeats. Missing or malformed pairing state disables cloud presence without
altering local workspace functionality. The pairing code and dashboard/API
assertion tokens are not stored here.

The package intentionally has no external Go dependencies. On non-Linux
platforms, ownership checks fail closed; authenticated local IPC and the
current machine-enrollment implementation are Linux-only.
