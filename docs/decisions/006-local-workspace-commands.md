# ADR-006: Local Workspace Commands over IPC v1

**Status:** Accepted
**Date:** 2026-10-16

## Context

Mio opens its workspace registry at startup (ADR-005, Mio integration amendment), but nothing can register, list, or remove workspaces. The `scopra workspace` CLI needs commands on Mio's local socket.

ADR-003 sets three constraints. IPC v1 serves only `status`. New commands need compatibility and security tests before dispatch is enabled. State-changing commands need more than same-UID IPC. IPC v1 also has no error replies: the daemon closes the connection on any invalid request. That is not enough for registration, which can fail in many distinct ways.

The threat model routes agents through Scopra Caloris and Scopra Goldstone, never through Mio's local socket, and requires that agents cannot approve themselves. A registered path can be up to 4095 bytes (Linux `PATH_MAX` without its terminating NUL) and can grow sixfold when escaped as JSON. A full registry of 256 such paths is therefore far larger than one 64 KiB frame.

## Decision

- **IPC v1 is extended, not replaced.** Framing, the 64 KiB frame limit, one request per connection, timeouts, the connection cap, peer credentials, and `protocolVersion` 1 are unchanged. The status request and reply are byte-for-byte unchanged, so older clients keep working.
- **Commands.** Each request has an exact key set: `protocolVersion`, `requestId`, and `command`, plus the command's single field.

  | Command | Field | Success reply (besides `protocolVersion` and `requestId`) |
  |---|---|---|
  | `workspace.add` | `path` | `workspace: {id, path}` with the canonical path that was registered |
  | `workspace.list` | `cursor` | `workspaces: [{id, path}, ...]` and `nextCursor` |
  | `workspace.remove` | `id` | `workspace: {id, path}` of the removed registration; the directory is never touched |

- **Field rules.** `path` is non-empty, valid UTF-8, and at most 4095 bytes. `id` is 32 lowercase hexadecimal characters, the registry's ID format. `cursor` is empty for the first page, or a previous `nextCursor`. Whole frames must be valid UTF-8, because the JSON decoder would otherwise silently replace invalid bytes and could name a different directory. Fields must have their exact JSON type; `null` is rejected. A JSON escape for an unpaired surrogate decodes to U+FFFD, as in Go, and the reply always carries the path that Mio resolved. Mio never resolves a relative path: the ADR-004 validator rejects it before any lookup.
- **Error replies.** A well-formed workspace request that fails gets `{protocolVersion, requestId, error: {code, message}}`, with a code from this set:

  | Code | Meaning |
  |---|---|
  | `invalid_path` | not absolute or not valid UTF-8 |
  | `unresolvable_path` | missing, or not resolvable (new `workspace.ErrUnresolvable`) |
  | `not_directory` | does not resolve to a directory |
  | `protected_path` | the root, a home directory, or Mio's state directory, or a directory containing one; or a directory inside the state directory |
  | `not_owned` | not owned by the user running Mio |
  | `already_registered` | the same directory, by path or fingerprint |
  | `overlap` | inside, or containing, a registered workspace |
  | `registry_full` | 256 workspaces, or a file larger than 1 MiB |
  | `not_found` | no workspace has that ID |
  | `internal` | any other failure, such as an I/O error; the change may still have taken effect |

  Malformed requests still get no reply. These include bad framing, JSON, keys, version, request ID, field values, and unknown commands. Clients must accept unknown well-formed codes (`[a-z][a-z_]*`, at most 32 bytes) as errors, so that codes can be added. `message` is diagnostic text of 1 to 1024 bytes of valid UTF-8. Mio truncates longer messages on a character boundary. Clients must treat the message as untrusted text and escape control characters before display.
- **Paging.** Mio orders a listing by ID and fills each page with the records after `cursor` while their encoded size stays within the frame, less a margin for the rest of the reply. A page holds at least one record while any remain. `nextCursor` is the page's last ID, or empty after the last page. The cursor is therefore fixed-size and stateless. A listing is not a snapshot: records added or removed between pages may be missed or included. Clients reject pages whose IDs do not ascend after the cursor or whose `nextCursor` is not the last ID. They stop after 256 records, sort the result by path, and validate every record (a lowercase-hexadecimal ID and a canonical absolute path of at most 4095 bytes).
- **Registry bound.** `registry.Open` rejects paths longer than 4095 bytes as invalid. `Add` cannot produce such paths, and the bound guarantees that every record fits in a page.
- **Dispatch.** Mio enables workspace commands only after opening its registry, by calling `Daemon.EnableWorkspaces` before `Serve`. A daemon without a registry treats workspace commands as unknown.
- **Authorization (qualifies ADR-003).** Same-UID peer credentials authorize the workspace commands; the local socket is the user's administrative channel.
  - Registration is accepted only on this socket, never through Caloris, Goldstone, or Messenger.
  - Registering a workspace authorizes nothing by itself. Any execution will also need a grant, policy evaluation, approval where policy requires it, and enforcement by Rupes.
  - Any process running as the user can already read and write the registry file and every file the user owns, so a stronger check on this channel would not stop same-UID code.
  - ADR-003's rule still holds for commands that approve or authorize actions. They must not use this trust level, because agents cannot approve themselves; they need a separate decision, such as proof of user presence.

## Consequences

- **The CLI can be built.** Mariner can implement `scopra workspace add/list/remove` on tested client functions (`AddWorkspace`, `ListWorkspaces`, and `RemoveWorkspace`), with typed errors (`*WorkspaceError`).
- **Same-UID processes can change registrations.** This includes local agents running as the user outside a sandbox, which can already act as the user.
- **The sandbox must not expose Mio.** Hollow must keep workspace processes from reaching Mio's socket: no state directory in the sandbox and no inherited descriptors. The registry's rules already keep the state directory out of every workspace.
- **Lost replies.** A request and its reply share the 2-second connection timeout. If a reply is lost after Mio committed a change, the change stands; listing shows the outcome, and adding again reports `already_registered` with the existing ID.
- **Version skew.** An older Mio closes the connection on workspace commands, so a newer client reports that Mio did not answer, not a typed error.
- **Unchanged.** `scopra status`, `scopra doctor`, and the status wire format are unchanged.

## Alternatives considered

- **Raise the frame limit for listings:** rejected. A full registry can encode to about 6 MiB, and bounded frames are a core defense of the IPC.
- **Page by path:** rejected. Path cursors could be about 24 KiB; ID cursors are fixed-size and easy to validate.
- **Close the connection on application errors:** rejected. Users need to know why `add` failed.
- **`protocolVersion` 2:** rejected. The commands are additive, and a new version would break status between versions.
- **A separate admin socket or an access token:** rejected for now. Same-UID code can read any token the user can, so neither would add a boundary.

## Note — 2026-10-17: local execution

ADR-011 qualifies the sentence above that every execution needs a grant, policy evaluation, approval where policy requires it, and Rupes. A **local** run is the user's own action on the local socket, contained in Hollow, so it needs no grant or approval. Delegated execution through Caloris still needs everything that sentence lists, and the local maximum policy will apply to local runs once Resolution and Rupes exist.
