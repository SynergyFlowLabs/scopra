# ADR-005: Workspace Registry Storage v1

**Status:** Accepted  
**Date:** 2026-10-16

## Context

The PRD requires registering and removing existing directories as workspaces, and says that removing authorization never deletes source files. ADR-004 defines which directories may become workspaces. Mio, as the local authority, now needs durable storage for registrations that it alone controls. The IPC commands and CLI that will expose registration change the IPC format and are left to a later decision.

## Decision

- `internal/workspace/registry` stores registrations in `<dir>/workspaces.json`, where `dir` is a private directory that the caller owns exclusively. Mio will pass its state directory while holding the state-directory lock (see the ADR-003 state-directory lock amendment), which makes Mio the single writer. Calls within a process are serialized.
- The version 1 format is `{"version":1,"workspaces":[{"id":"<32 lowercase hex digits>","path":"<canonical path>","fingerprint":{"device":<uint64>,"inode":<uint64>}}]}`, with records sorted by path. Each ID is a random 128-bit value.
- `Add` requires ADR-004 validation and rejects:
  - the same directory again, matched by canonical path or by fingerprint, so a moved directory still counts as the same workspace;
  - a workspace inside, or containing, a registered workspace;
  - a workspace containing the registry directory, whatever the validator's configuration, so that workspace processes can never edit their own authorization records (extended to workspaces inside the directory: see the Mio integration amendment);
  - a 257th workspace, or a change that would make the file larger than 1 MiB.
- `Remove` deletes only the registration. Scopra never deletes or modifies a workspace's contents when it removes a registration.
- Writes are atomic. An owner-only temporary file is written in the same directory, synced, and renamed over `workspaces.json`, and then the directory is synced.
  - A failure before the rename leaves the file and the in-memory registry unchanged and removes the temporary file.
  - If only the final directory sync fails, the change is already visible, so it stays in effect and the error is reported.
- Reads fail closed:
  - The directory must be a real directory owned by the effective user with no group or other access.
  - The file is opened without following symlinks or blocking on FIFOs, and must be an owner-only regular file owned by the effective user.
  - The content must be valid UTF-8 and no larger than 1 MiB. It must be strict version 1 JSON, with no unknown fields or trailing data and a workspace list that is present, and it must satisfy the same rules that `Add` enforces.
  - Anything else returns `ErrInsecureRegistry` or `ErrCorruptRegistry`. `Open` never repairs or rewrites the file.
- Loading checks structure only. Recorded workspaces must be revalidated with ADR-004's `Revalidate` before any use.

## Consequences

- Part 2 (Mio wiring, IPC commands, and the `scopra workspace` CLI) can build on tested storage. When this was accepted, Mio did not use the registry; see the Mio integration amendment.
- Mio's default state directory was under the user's cache directory (ADR-003), and cache cleaners may delete it. Losing the registry fails safe, because no workspace is then available, but it would surprise users, so part 2 needed a persistent default before exposing registration. Resolved: the ADR-003 persistent default state directory amendment moved the default to `$XDG_STATE_HOME/scopra`, or `~/.local/state/scopra`.
- A corrupt or unsafe registry blocks every registration until a person fixes or removes it; there is no automatic repair.
- Temporary files left by a crash (`.workspaces-*.tmp`) are not cleaned up yet. They are owner-only and never read.
- The overlap and duplicate rules may need revisiting when read-only and read-write modes arrive.

## Security Considerations

- The single-writer guarantee depends on the caller owning the directory exclusively; the package takes no lock of its own. As with ADR-003, same-UID processes can still edit the file.
- This package does not check the registry directory's ancestors. `localipc` validates Mio's state directory, rejecting symlinks and unsafely writable ancestors.
- Records remain untrusted until revalidated, because a same-UID process could edit the paths in the file. `Revalidate` and the ADR-004 rules apply at use time.
- Go's JSON decoder silently replaces invalid UTF-8, so the raw bytes are checked before decoding.

## Alternatives

- **A separate persistent data directory with its own lock:** rejected when the default location was decided (see the ADR-003 persistent default state directory amendment). It would duplicate Mio's single-writer guarantee and directory validation.
- **An append-only log or an embedded database:** unnecessary for at most 256 records, and it would add dependencies.
- **Skipping invalid records or repairing the file on load:** rejected. Silently dropping registrations hides tampering, and failing closed is easier to reason about.

## Amendment — 2026-10-16: Mio integration

**Extends:** the `Add` rule about the registry directory and the rules applied on load. The rest of this ADR is unchanged.

**Why:** Mio now opens the registry in its state directory, which also holds its socket and lock and will hold other state, such as audit records. Two gaps mattered once that was so. A workspace inside the registry directory was accepted, which would put agent-controlled files beside Mio's state. `Open` never compared records with its own directory, so a registry inside a registered workspace, for example after a state directory is moved, would let workspace processes edit their own authorization. That contradicted the rule that loaded content must satisfy the rules `Add` enforces.

- `Add` rejects a workspace that contains or lies inside the registry directory (`workspace.ErrProtectedPath`). `Open` rejects a file with such a record as `ErrCorruptRegistry`, comparing it with the canonical directory, and leaves the file unchanged.
- At startup, after taking the state-directory lock and before reporting that it is listening, Mio opens the registry in exactly the directory it locked. There are no workspace commands yet, so the registry is only checked.
- If the registry cannot be opened (an unsafe file or directory, invalid content, or a validator that cannot be prepared), Mio refuses to start. It reports the reason, removes its socket, releases the lock, and leaves the file unchanged. A person must fix or remove the file. Startup does not revalidate recorded workspaces, so a workspace directory that no longer exists does not stop Mio; it fails when it is used.
- Mio's validator protects the state directory, so revalidation before use also rejects a workspace that has come to contain it.

**Consequences and limits:**

- A bad registry now stops Mio entirely, not just registration. `scopra status` then reports only that Mio is not running, while `scoprad` prints the reason. `scopra doctor`'s `workspace-registry` check reports the reason too, through `registry.Check`, which validates the file exactly as `Open` does but never writes and may run while Mio owns the directory. This choice is the simplest fail-closed behaviour while only a manual edit can produce a bad file, and it should be revisited when workspace commands can report a degraded state.
- Mio needs a home directory, from `$HOME` or the account database, so that the validator can protect it. This applies even with `--state-dir`.
- Mio binds its socket before it opens the registry, so a client that connects during a refused start sees the connection closed instead of "not running". No request is served in that window.
