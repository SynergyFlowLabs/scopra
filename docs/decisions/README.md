# Architecture Decision Records

Use `000-template.md`. Accepted initial decisions:

- `001-go-module-layout.md` — one Go module at the repository root until independent release/versioning is justified.
- `002-messenger-envelope-v1.md` — strict, bounded envelope structure; cryptographic verification and authorization remain separate.
- `003-local-status-ipc.md` — foreground Mio status service over bounded, same-UID-authenticated Linux Unix-socket IPC; amended 2026-10-16 so that an `flock` on `scoprad.lock` serves as the single-daemon lock, and again so that the default state directory is the persistent `$XDG_STATE_HOME/scopra` (`~/.local/state/scopra`).
- `004-workspace-validation.md` — canonical workspace paths; rejection of the filesystem root, home directories, and other protected directories; owner-only roots; filesystem-identity fingerprint v1 and revalidation.
- `005-workspace-registry-storage.md` — version 1 `workspaces.json` in a directory the caller owns exclusively; no duplicate or overlapping workspaces and none containing the registry; atomic writes and fail-closed reads. Amended 2026-10-16: Mio opens the registry at startup and refuses to serve if it is unusable, and no workspace may contain or lie inside the registry directory.
- `006-local-workspace-commands.md` — `workspace.add`, `workspace.list`, and `workspace.remove` over IPC v1 with typed error replies and ID-cursor paging that fits each page in one frame; same-UID authorization for registration only, on the local socket only.

Required follow-up ADRs cover device identity, relay transport/reconnect, containment backend, canonical execution requests, audit integrity, MCP authorization, output/redaction, token revocation, and update signing.
