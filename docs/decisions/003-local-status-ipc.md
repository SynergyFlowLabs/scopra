# ADR-003: Mio Foreground Status Daemon and Local IPC v1

**Status:** Accepted
**Date:** 2026-10-16

## Context

Scopra Mio and Scopra Mariner need a small local control channel before workspace management or execution can be added. The first increment must establish lifecycle, framing, and peer identity without granting the CLI authority to execute commands.

## Decision

- Keep the existing executable names: `scoprad` starts Mio in the foreground; `scopra status` queries it.
- Use a Unix-domain socket at `<state-dir>/scoprad.sock`. The default state directory is beneath the user's cache directory (superseded: see the persistent default state directory amendment); both binaries accept `--state-dir` for service configuration and tests.
- Create/check the state directory as owned by the effective user with owner-only permissions, and the socket as owner-only (`0700` directory, `0600` socket). Reject symlinks, insecure paths, non-socket occupants, and overlong Unix socket paths. Binding the socket is the single-daemon lock (superseded: see the state-directory lock amendment). A stale socket may be removed only after an unsuccessful connection probe and an inode recheck.
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

## Amendment — 2026-10-16: persistent default state directory

**Supersedes:** "The default state directory is beneath the user's cache directory" in the Decision section. The rest of this ADR and the state-directory lock amendment are unchanged.

**Why:** the state directory is about to hold persistent state. Mio will keep its workspace registry there (ADR-005), under the state-directory lock that makes Mio the single writer. Users and cleaning tools may delete the cache directory at any time. Losing registrations fails safe, because lost records grant no access, but it would surprise users.

- With no `--state-dir`, both binaries use `$XDG_STATE_HOME/scopra`, or `$HOME/.local/state/scopra` when `XDG_STATE_HOME` is unset or empty. The XDG Base Directory specification defines this location for state that should persist across restarts but is not important or portable enough to belong in `$XDG_DATA_HOME`. Workspace registrations record host-specific paths and filesystem fingerprints.
- A relative `XDG_STATE_HOME` or `HOME` fails closed as an insecure state directory, and resolution fails if neither variable is set. The specification says to ignore relative values. Scopra rejects them instead, as Go's `os.UserCacheDir` does for `XDG_CACHE_HOME`, so that a misconfiguration is reported rather than silently moving Mio's state. A relative value is never resolved against the working directory, which could be inside a workspace.
- The socket, the lock, and Mio's persistent state stay in this one directory, so the lock always sits beside the state it protects. Mio creates missing path components as owner-only (`0700`) directories, and every existing state-directory check still applies.
- `--state-dir` still overrides the default. Pass the same value to both binaries.

**Alternatives:**

- **Socket in `$XDG_RUNTIME_DIR`, state elsewhere:** rejected for now. The lock must stay beside the persistent state, and `XDG_RUNTIME_DIR` is not set in every session (some `su`, `sudo`, and cron environments lack it), so the two binaries could disagree about where the socket is.
- **Keep the cache default and add a separate persistent directory:** rejected. It would need its own lock, or a second location that both binaries must agree on (see ADR-005).
- **`$XDG_DATA_HOME`:** rejected. The specification sends state that is not important or portable enough for `$XDG_DATA_HOME` to `$XDG_STATE_HOME`, and Mio's records are host-specific and can be recreated.
- **Native macOS and Windows locations:** deferred until those platforms are supported. Mio fails closed there, and the same rule is computed meanwhile.

**Consequences and limits:**

- Clearing caches no longer removes Mio's state. Because the directory persists, a stale socket can survive a crash or a reboot; the existing connection probe, inode recheck, and lock handle it.
- Development builds from before this amendment used `<user-cache-dir>/scopra/run`, which held only the socket and the lock file, so there is nothing to migrate. Older and newer binaries do not find each other by default: stop an older `scoprad` before upgrading, after which the old directory can be deleted.
- Sharing a state directory between hosts, for example through a network home directory, is unsupported. Unix sockets are host-local, and whether `flock` excludes a daemon on another host depends on the file system. Use a host-local `--state-dir` there.
- As before, the default fails closed if any path component is a symlink, such as a symlinked `~/.local`, or if an existing ancestor is writable by group or others without being a root-owned sticky directory. A `002` umask can leave directories like `~/.local` group-writable. Doctor reports the cause; fix the permissions or pass a different `--state-dir`.
