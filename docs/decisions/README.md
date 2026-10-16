# Architecture Decision Records

Use `000-template.md`. Accepted initial decisions:

- `001-go-module-layout.md` — one Go module at the repository root until independent release/versioning is justified.
- `002-messenger-envelope-v1.md` — strict, bounded envelope structure; cryptographic verification and authorization remain separate.
- `003-local-status-ipc.md` — foreground Mio status service over bounded, same-UID-authenticated Linux Unix-socket IPC; amended 2026-10-16 so that an `flock` on `scoprad.lock` serves as the single-daemon lock, and again so that the default state directory is the persistent `$XDG_STATE_HOME/scopra` (`~/.local/state/scopra`).
- `004-workspace-validation.md` — canonical workspace paths; rejection of the filesystem root, home directories, and other protected directories; owner-only roots; filesystem-identity fingerprint v1 and revalidation.
- `005-workspace-registry-storage.md` — version 1 `workspaces.json` in a directory the caller owns exclusively; no duplicate or overlapping workspaces and none containing the registry; atomic writes and fail-closed reads. Amended 2026-10-16: Mio opens the registry at startup and refuses to serve if it is unusable, and no workspace may contain or lie inside the registry directory.
- `006-local-workspace-commands.md` — `workspace.add`, `workspace.list`, and `workspace.remove` over IPC v1 with typed error replies and ID-cursor paging that fits each page in one frame; same-UID authorization for registration only, on the local socket only.
- `007-execution-request-v1.md` — the strict `execution.request` payload: exact keys, no nulls, transmitted text preserved exactly, `shell` false only, a workspace-relative working directory, explicit workspace access, network `none` only, sanity-bounded limits, and a request window of at most one hour. The canonical form and action hash remain a follow-up. Amended 2026-10-16 so that the executable, working directory, and reason also reject the line and paragraph separators U+2028 and U+2029.
- `008-canonical-execution-request.md` — the canonical form of a whole version 1 execution request (RFC 8785 JCS, computed from the decoded value) and its action hash: lowercase hex SHA-256 over the label `scopra/execution.request/v1` and a newline, then the canonical bytes. Mio always recomputes the hash. Includes two normative test vectors.

Required follow-up ADRs cover device identity, relay transport/reconnect, containment backend, canonical execution requests, audit integrity, MCP authorization, output/redaction, token revocation, and update signing.
