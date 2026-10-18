# ADR-013: Bounded Local Run Journal and `scopra logs`

**Status:** Accepted (proposed, then accepted as written by the maintainer)
**Date:** 2026-10-17

## Context

Mio (`scoprad`) runs commands locally (ADR-011) and reports each result to the client that asked for it. Nothing durable records that a run happened. The plan's Milestone 1 lists "local execution, status, logs, cancellation", and ADR-009 deliberately keeps results metadata-only: the `execution.result` payload has no output and no command line. A local log therefore has two possible shapes:

- **metadata only** — when a run happened, for which workspace, in which state, with which exit code, and how many output bytes;
- **output capture** — the actual stdout/stderr of the run.

Output is exactly the data that ADR-010 refuses to persist ("output passed straight to Mio and never logged"), and it is the data most likely to contain secrets, tokens, and repository contents. Output retention also needs its own redaction, access, and deletion rules. The two shapes should not be decided together.

ADR-011 also leaves audit unimplemented. A durable local record is useful before Catena exists, but it must not pretend to be an audit trail: it is one file the user can edit, delete, or truncate at will.

## Decision

**Mio keeps a bounded, metadata-only journal of answered runs, and `scopra logs` reads it through a new read-only IPC command.**

### What is recorded

One JSON object per line in `<state-dir>/runs.jsonl` — by default `~/.local/state/scopra/runs.jsonl` — written after Mio has answered a run with a result. Each entry has exactly fourteen keys:

```json
{"schemaVersion":1,"executionId":"local-…","workspaceId":"…","state":"succeeded","recordedAt":"2026-10-17T06:42:04.184Z","startedAt":"2026-10-17T06:42:04.000Z","endedAt":"2026-10-17T06:42:04.000Z","durationMs":0,"exitCode":0,"stdoutBytes":23,"stderrBytes":18,"truncated":false,"chunks":2,"errorCode":""}
```

- `schemaVersion` is 1. An incompatible change needs a new version.
- `executionId` and `workspaceId` are the IDs the client already sees (ADR-009, ADR-006).
- `state` is one of ADR-009's seven states.
- `recordedAt` is when Mio answered; `startedAt` and `endedAt` are the result's times. All timestamps are UTC with exactly millisecond precision, and are empty strings when the state carries no times.
- `durationMs` is `endedAt - startedAt` in whole milliseconds.
- `exitCode` is the result's exit code.
- `stdoutBytes`, `stderrBytes`, `truncated`, and `chunks` copy ADR-009's output summary: how much output there was, never the output itself.
- `errorCode` is the result's code for `errored` and `rejected`, and empty otherwise.

**No command, argument, working directory, reason, workspace path, output, or user identity is stored.** The journal cannot reconstruct what ran, only that something ran in a workspace and how it ended. `workspaceId` identifies a directory registration the user already owns; it is not a secret, but it is the only link to the workspace.

The per-state rules mirror ADR-009's result rules: `succeeded` has times and exit code 0; `failed` has times and an exit code from 1 to 255; `timed_out`, `cancelled`, and `out_of_memory` have times and no exit code; `errored` and `rejected` have only an error code, and a rejected run has no output counts. Byte counts must fit the chunk count (`chunks ≤ stdoutBytes + stderrBytes ≤ chunks × 64 KiB`, as in ADR-009).

### Bounds and durability

- At most **1000 entries** and **1 MiB** on disk. When either bound would be exceeded, Mio keeps the newest entries and drops the oldest ones by writing a temporary file in the same directory (mode 0600), `fsync`ing it, renaming it over the journal, and `fsync`ing the directory. A crash leaves either the old file or the new one, never a half-rewritten journal.
- One encoded line is at most 8 KiB. This is far above any valid entry and lets a reader bound its work before parsing.
- Each append is written in one call and `fsync`ed before the result is considered recorded.
- The file must be a **regular file, owned by the effective user, with mode 0600, and not a symlink**. Anything else refuses startup and is left unchanged. A freshly created file is explicitly `chmod 0600`, because the umask would otherwise mask the creation mode.
- At startup Mio reads and validates every committed line. Only newline-terminated lines are committed: an unterminated final line is a torn write from a crash, so Mio discards it. Any other empty or malformed line is corruption, and Mio refuses to start rather than silently dropping history.
- Reads check the 1 MiB bound before allocating or parsing, so a replaced or grown file cannot make a reader allocate unbounded memory.

### Fail closed

- Mio opens the journal after taking the state-directory lock and opening the workspace registry, and **before it serves**. If the journal cannot be opened or validated, `scoprad` exits 1 and runs nothing.
- Every answered result is appended at the IPC boundary, including rejections (unknown workspace, containment unavailable) and busy refusals, so refusals are visible too. A run that never gets a result — a malformed request, a disconnect mid-run, a Mio crash — is not recorded.
- If an append fails, the result has already been computed and is still sent, because refusing it cannot undo the run. Mio then marks the journal unusable, and every later run is answered with an `errored` result (`internal`, fixed message) **without executing**. One unrecorded run is the documented window; not running anything afterwards is the fail-closed part.
- `scopra logs` never writes. It opens the journal read-only, parses it under the same rules, and returns entries.

### The IPC command

`execution.logs` is a read-only extension of IPC v1, like the workspace commands:

- request: `{protocolVersion, requestId, command, workspaceId, limit}` — `workspaceId` is optional (`""` means all workspaces) and `limit` is required, 1 to 100;
- reply: `{protocolVersion, requestId, entries}` — newest first, entries encoded exactly as stored, at most `limit` of them;
- keys are checked exactly, values are validated, an invalid request gets no reply and no side effect, and an unreadable journal gets a typed error reply (ADR-006's error shape).

The daemon serves this command only when executions are enabled, because it is the journal of those executions. `EnableExecutions` therefore requires both the executor and the journal.

### `scopra logs`

```text
scopra logs [--state-dir PATH] [--workspace ID] [--limit N] [--json]
```

- `--limit` defaults to 20 and accepts 1 to 100; `--workspace` accepts a 32-character lowercase hexadecimal ID; unknown flags, stray arguments, and invalid values are usage errors (exit 2) found before Mio is contacted.
- Without `--json`, one line per entry, newest first:

  ```text
  2026-10-17T06:42:04.217Z  failed  exit 3  duration 1.5s  stdout 23 B  stderr 18 B  workspace …  execution local-…
  ```

  `rejected` and `errored` entries show `error <code>` instead of an exit code, and entries without times show `duration -`. All text is terminal-escaped.
- With `--json`, exactly one object: `{"schemaVersion":1,"entries":[…]}`. Entries are the stored objects, newest first. An empty result is `"entries":[]`, never `null`.
- Empty results print `No runs have been recorded.` or, with a filter, `No runs have been recorded for workspace <id>.`

Exit status follows the other read-only commands: 0 on success, 1 when Mio is unavailable or refuses, 2 for usage errors, 130 when interrupted.

## Consequences

- A user can see that runs happened, when, in which workspace, and how they ended, without a relay, a cloud service, or Catena.
- The journal is metadata, not audit: the user (or any process running as the user) can delete or rewrite it. It is not tamper-evident and must not be presented as evidence. Catena remains the planned integrity layer.
- Output is still never persisted. Capturing output is deferred to its own ADR with retention, redaction, access, and deletion rules.
- The journal grows by one bounded line per answered run and never more than 1 MiB. Old runs disappear silently; there is no archive and no export.
- A journal that cannot be written disables execution until Mio restarts and the file is fixed. That is deliberate: Mio does not run what it cannot record.
- `scopra logs` is local-only and requires the same user. There is no remote log access, which keeps the IPC surface unchanged.

## Security Considerations

- **Data minimization.** The journal stores no command, argument, reason, path, output, or identity beyond the two IDs that the client already received. The most sensitive data a log could hold — what ran and what it printed — is absent by construction.
- **Access.** The file is owner-only (0600), inside the already owner-only state directory, must be a regular file owned by the effective user, and symlinks are refused at every step, so another user cannot read it or redirect the write. Same-UID processes could read it with `scopra logs` or by reading the file, exactly as they could already read the user's files.
- **Bounds.** Entry count, file size, line size, request limit, reply frame, and read size are all bounded, so a corrupted or replaced journal cannot cause unbounded allocation or an unbounded reply.
- **Strict parsing.** The reader accepts exactly the fourteen keys, rejects duplicates, unknown or incorrectly-cased keys, trailing data, invalid UTF-8, non-canonical timestamps, out-of-range counts, and states that do not match their fields. Corruption fails closed instead of being skipped.
- **Fail closed.** An unopenable journal stops Mio at startup, and a failed append stops later runs. The one run whose result was already sent cannot be un-run; the ADR records that window instead of hiding it.
- **Injection.** Workspace and execution IDs are constrained, and the human form escapes every field with the same terminal-safe rule as the other commands. JSON output is written by the encoder, so a hostile field cannot break the object.
- **No new authority.** Reading logs requires no capability: the same user can read the file. `execution.logs` is read-only and cannot record, delete, or change anything.

## Alternatives

- **Store the command line and output.** Rejected for this increment: it is the highest-risk data, and it needs its own retention and redaction decision. A later ADR can add bounded output capture.
- **Write the journal through the workspace registry or a database.** Rejected: a JSON-lines file is bounded, inspectable, atomically replaceable, and needs no new dependency or background process.
- **One journal per workspace.** Rejected: it multiplies file-permission and rotation logic, and the workspace ID filter answers the same question.
- **Keep everything forever.** Rejected: unbounded local state, unbounded read time, and a growing privacy exposure. Fixed bounds with silent rotation are enough for a debug aid.
- **Expose logs over the relay.** Rejected: it expands the trust boundary before policy and authorization exist.
- **Record a line before the run instead of after.** Rejected for now: it would journal intent that may never have a result, and the current fail-closed rule already stops future runs after a write failure. A started/terminal pair is worth considering together with output capture.
