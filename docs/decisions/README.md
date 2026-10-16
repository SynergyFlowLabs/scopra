# Architecture Decision Records

Use `000-template.md`. Accepted initial decisions:

- `001-go-module-layout.md` — one Go module at the repository root until independent release/versioning is justified.
- `002-messenger-envelope-v1.md` — strict, bounded envelope structure; cryptographic verification and authorization remain separate.
- `003-local-status-ipc.md` — foreground Mio status service over bounded, same-UID-authenticated Linux Unix-socket IPC; amended 2026-10-16 so that an `flock` on `scoprad.lock` serves as the single-daemon lock.
- `004-workspace-validation.md` — canonical workspace paths; rejection of the filesystem root, home directories, and other protected directories; owner-only roots; filesystem-identity fingerprint v1 and revalidation.

Required follow-up ADRs cover device identity, relay transport/reconnect, containment backend, canonical execution requests, audit integrity, MCP authorization, output/redaction, token revocation, and update signing.
