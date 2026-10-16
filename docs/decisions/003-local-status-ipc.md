# ADR-003: Mio Foreground Status Daemon and Local IPC v1

**Status:** Accepted
**Date:** 2026-10-16

## Context

Scopra Mio and Scopra Mariner need a small local control channel before workspace management or execution can be added. The first increment must establish lifecycle, framing, and peer identity without granting the CLI authority to execute commands.

## Decision

- Keep the existing executable names: `scoprad` starts Mio in the foreground; `scopra status` queries it.
- Use a Unix-domain socket at `<state-dir>/scoprad.sock`. The default state directory is beneath the user's cache directory; both binaries accept `--state-dir` for service configuration and tests.
- Create/check the state directory as owned by the effective user with owner-only permissions, and the socket as owner-only (`0700` directory, `0600` socket). Reject symlinks, insecure paths, non-socket occupants, and overlong Unix socket paths. Binding the socket is the single-daemon lock (superseded: see the 2026-10-16 amendment). A stale socket may be removed only after an unsuccessful connection probe and an inode recheck.
- On Linux, authenticate the operating-system peer using `SO_PEERCRED`; both client and server require the peer UID to match their effective UID. Unsupported platforms fail closed until an equivalent peer-credential implementation exists.
- Use one request and one response per connection, framed by a four-byte big-endian length. The frame limit is 64 KiB. The versioned JSON request and response are strict: exact field names and casing are required; unknown fields, duplicate keys, trailing values, invalid request IDs, and unknown commands are rejected.
- This increment exposes only the read-only `status` command. It does not carry Messenger envelopes, execute processes, mutate workspaces, authorize actions, or establish a security boundary between processes running under the same OS user.
- Respect context cancellation, bound each connection's read/write time, and cap concurrent clients. The daemon runs in the foreground and removes only the socket inode it created on shutdown.

## Consequences

- Mio has a testable, bounded local status endpoint; Mariner can report whether the daemon process and IPC are responsive without shelling out or using a network listener. `running` is not a claim that execution is ready or authorized.
- The socket permissions and peer credentials protect against other OS users, not malicious code running as the same user. Future state-changing commands need their own authorization and approval checks; same-UID IPC alone is not sufficient.
- The protocol is intentionally narrow. Adding commands, fields, or other operating systems requires compatibility and security tests before dispatch is enabled.

## Amendment — 2026-10-16: state-directory lock

**Supersedes:** "Binding the socket is the single-daemon lock" in the Decision section. The rest of this ADR is unchanged.

**Why:** a connection probe plus an inode recheck cannot make "remove the socket only if it is still stale" atomic. Two daemons starting against a stale socket could each remove it, and one could unlink the other's freshly bound socket. Both then run, and one is unreachable. A stress test of concurrent starts reproduced this. The same end state followed deterministically when the socket file alone was deleted while Mio was running.

- Mio takes a non-blocking exclusive `flock` on `<state-dir>/scoprad.lock`. It does so after validating or creating the state directory and before it inspects, removes, or binds the socket. If another process holds the lock, startup fails with "daemon is already running", even when the socket is missing.
- The lock file is opened read-only, without following symlinks, blocking on FIFOs, or truncating. Its descriptor is close-on-exec. It is created with mode `0600` and never written. Only a regular file owned by the effective user, with no group or other access, is accepted; anything else fails closed as an insecure state directory and is left unchanged.
- Mio holds the lock until it can no longer act on the state directory, including while in-flight requests finish. It releases the lock after removing its own socket. The lock file is never deleted, because unlinking it would let one process lock the old inode while another creates and locks a new file.
- Stale sockets are still removed only after a failed connection probe and an inode recheck, now while the lock is held. These checks still protect against daemons from before the lock existed and against unrelated listeners.
- Read-only diagnostics (`CheckStateDir` and `scopra doctor`) validate an existing lock file but never create, open, or lock it, because a probing lock could make a concurrent daemon start fail.

**Consequences and limits:** at most one Mio serves a state directory at a time, which prepares Mio for single-writer persistent state. Any process running as the same user can hold the lock and prevent Mio from starting; same-UID processes remain outside this security boundary. If the whole state directory is deleted while Mio is running, a new start creates a fresh lock file and cannot detect the orphaned daemon. That case needs a separate liveness or self-check design.
