# ADR-004: Workspace Path Validation and Fingerprint v1

**Status:** Accepted  
**Date:** 2026-10-16

## Context

A workspace is the only host directory that Scopra may expose to agent commands. The PRD requires registering an existing directory with a "canonical path and repository fingerprint". The plan requires rejecting the filesystem root and the complete home directory, and revalidating a workspace before execution. Neither document defines the fingerprint. The registry, IPC commands, and CLI will all depend on one vetted rule set, and Mio, as the local authority, must apply the rules itself rather than trusting Mariner or the cloud.

## Decision

- The standard-library package `internal/workspace` validates workspace roots. Mio will call it when a workspace is registered and again before each use. Only Linux is supported; other platforms fail closed.
- The input must be an absolute, valid UTF-8 path, so Mio never interprets a path relative to its own working directory. The canonical path has every symlink resolved, with `..` resolved physically after the symlinks before it, as the kernel resolves it. The canonical path must also be valid UTF-8, so it can be stored and transmitted unambiguously.
- The root must be an existing directory owned by the effective user. This excludes system directories and other users' trees without a denylist.
- A workspace must not be, or contain, any of the following:
  - the filesystem root;
  - the current user's home directories, taken from both `$HOME` and the account database, so that overriding `$HOME` cannot unprotect the account home;
  - additional protected directories that Mio supplies, such as its state directory, whose control socket and lock must never be visible to workspace processes.
- Protected paths are compared after resolving symlinks in their existing prefix, so they need not exist yet. Containment respects path-separator boundaries, and directories inside the home directory remain allowed.
- Fingerprint version 1 is the root directory's device and inode numbers.
- `Revalidate` re-applies every rule. It then requires that the recorded canonical path still resolves to itself and that the fingerprint still matches; otherwise it returns `ErrChanged`.
- Validation reads only filesystem metadata. It never reads repository contents and never runs commands such as `git`, whose repository-controlled configuration can execute code.

## Consequences

- The registry, IPC, and CLI increments can rely on a single rule set. That rule set is tested against symlinks, physical `..` resolution, aliases for the root and home directory, protected paths that do not exist yet, ownership, and directory replacement.
- The fingerprint identifies the directory, not the repository's history or remotes. A directory deleted and recreated at the same path may reuse its inode number and go undetected. Some filesystems renumber devices on remount, which causes a spurious `ErrChanged`; that fails safe by requiring re-registration. A content-based repository identity, such as the Git root commit, needs a separate decision.
- Nothing is persisted yet, so the fingerprint definition can still change without migrating data.

## Security Considerations

- Validation is a point-in-time check: a path can change between validation and use. Callers must revalidate immediately before use, and execution should later bind the workspace by file descriptor (for example, `openat2` with `RESOLVE_BENEATH`) when mounting it.
- Paths are compared byte by byte. A bind mount or other mount alias can expose a protected directory under an unrelated path; mount-aware checks belong with the containment work.
- Sensitive subdirectories inside the home directory, such as `~/.ssh`, are not denylisted. The user's explicit registration remains the authority, and containment, not path rules, keeps credentials and sockets out of the workspace mount.
- Any process running as the same user can change a workspace between checks. Same-UID processes remain outside this boundary, as in ADR-003.

## Alternatives

- **Git-based fingerprint (root commit or remote URL):** rejected for version 1. It requires parsing untrusted repository data or running `git`, and remote URLs are mutable.
- **Lexical canonicalization (`filepath.Clean`, then resolve):** rejected, because it resolves `..` differently from the kernel when a symlink precedes it.
- **Rejecting only the exact home directory:** rejected, because an ancestor such as `/home` or `/` exposes the whole home directory just the same.
- **A denylist of system directories:** rejected in favour of the ownership rule, which needs no maintenance and also covers other users' directories.
