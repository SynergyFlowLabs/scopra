# ADR-003: Mio Foreground Status Daemon and Local IPC v1

**Status:** Accepted
**Date:** 2026-10-16

## Context

Scopra Mio and Scopra Mariner need a small local control channel before workspace management or execution can be added. The first increment must establish lifecycle, framing, and peer identity without granting the CLI authority to execute commands.

## Decision

- Keep the existing executable names: `scoprad` starts Mio in the foreground; `scopra status` queries it.
- Use a Unix-domain socket at `<state-dir>/scoprad.sock`. The default state directory is beneath the user's cache directory; both binaries accept `--state-dir` for service configuration and tests.
- Create/check the state directory as owned by the effective user with owner-only permissions, and the socket as owner-only (`0700` directory, `0600` socket). Reject symlinks, insecure paths, non-socket occupants, and overlong Unix socket paths. Binding the socket is the single-daemon lock. A stale socket may be removed only after an unsuccessful connection probe and an inode recheck.
- On Linux, authenticate the operating-system peer using `SO_PEERCRED`; both client and server require the peer UID to match their effective UID. Unsupported platforms fail closed until an equivalent peer-credential implementation exists.
- Use one request and one response per connection, framed by a four-byte big-endian length. The frame limit is 64 KiB. The versioned JSON request and response are strict: exact field names and casing are required; unknown fields, duplicate keys, trailing values, invalid request IDs, and unknown commands are rejected.
- This increment exposes only the read-only `status` command. It does not carry Messenger envelopes, execute processes, mutate workspaces, authorize actions, or establish a security boundary between processes running under the same OS user.
- Respect context cancellation, bound each connection's read/write time, and cap concurrent clients. The daemon runs in the foreground and removes only the socket inode it created on shutdown.

## Consequences

- Mio has a testable, bounded local status endpoint; Mariner can report whether the daemon process and IPC are responsive without shelling out or using a network listener. `running` is not a claim that execution is ready or authorized.
- The socket permissions and peer credentials protect against other OS users, not malicious code running as the same user. Future state-changing commands need their own authorization and approval checks; same-UID IPC alone is not sufficient.
- The protocol is intentionally narrow. Adding commands, fields, or other operating systems requires compatibility and security tests before dispatch is enabled.
