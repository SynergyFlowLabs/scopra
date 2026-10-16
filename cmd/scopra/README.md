# Scopra Mariner

Scopra Mariner is the CLI/TUI for onboarding and machine enrollment. The executable remains `scopra`.

## Implemented commands

```text
scopra status [--state-dir PATH] [--json]
scopra doctor [--state-dir PATH] [--json]
scopra workspace add [--state-dir PATH] [--json] PATH
scopra workspace list [--state-dir PATH] [--json]
scopra workspace remove [--state-dir PATH] [--json] ID
scopra version
```

By default both binaries use `$XDG_STATE_HOME/scopra`, or `~/.local/state/scopra` when `XDG_STATE_HOME` is unset; a relative value fails closed. Pass the same `--state-dir` to `scoprad` and `scopra` to override it. No command starts the daemon automatically. Flags go before `PATH` or `ID`. Errors go to stderr, with control characters escaped.

### `scopra status`

`scopra status` queries the local Scopra Mio status endpoint and exits nonzero if Mio is unavailable. Its `running` result means only that Mio's local IPC endpoint responds; it does not indicate workspace, policy, containment, or execution readiness. `--json` writes the status response to stdout; diagnostics go to stderr.

### `scopra doctor`

`scopra doctor` runs read-only local diagnostics in a fixed order:

| Check | Passes when | Otherwise |
|---|---|---|
| `platform` | The OS supports authenticated local IPC (Linux). | `fail`; Mio fails closed, and the later checks are `skip`. |
| `state-directory` | The resolved directory exists and is owned by the current user with mode `0700`. It has no symlinked or unsafely writable ancestors, leaves room for the socket path, and any existing `scoprad.lock` is an owner-only regular file. | `warn` if it does not exist yet. `fail` if it cannot be resolved, is insecure, or is too long; `workspace-registry`, `workspaces`, and `daemon` are then skipped. |
| `workspace-registry` | Mio's `workspaces.json` is missing, or is an owner-only regular file with valid content, as `scoprad` requires at startup. The detail gives the number of registrations. | `fail`, with a hint naming the file, when it is unsafe, invalid, or unreadable: `scoprad` will not start until it is fixed or removed, and removing it unregisters every workspace without changing any directory. `workspaces` is then skipped. |
| `workspaces` | No workspaces are registered, or every registration still qualifies under ADR-004. Each one is revalidated with the validator configuration that Mio uses. Its directory must still be at the recorded canonical path, have the filesystem identity recorded at registration, and be owned by you. It must not be, or contain, a protected location such as your home directory or Mio's state directory. | `warn` naming up to five stale registrations, each with its ID, path, and reason (missing, not a directory, not owned by you, is or contains a protected location, or changed since it was registered), and counting the rest. The hint says to restore the directory or run `scopra workspace remove ID`, which does not change the directory. `fail` if validation cannot be prepared because no home directory is known; `scoprad` would not start either. |
| `daemon` | Mio's local IPC endpoint answers the existing authenticated `status` request. | `fail`, with a hint, when Mio is not running, its socket is unsafe, or its response is invalid. |

Doctor never creates or repairs the state directory, removes stale sockets, starts Mio, or contacts a socket with unsafe ownership or permissions. It never opens or locks Mio's lock file, so it cannot make a concurrent `scoprad` start fail. It reads the workspace registry without changing it, which is safe while Mio runs because Mio replaces the file atomically. Revalidation inspects only the metadata of registered directories; it changes neither the registry nor any workspace. It sends only the existing `status` request. A passing result means only that the local platform, state directory, workspace registry, registered workspaces, and Mio IPC endpoint look healthy. Enrollment, relay, policy, containment, and execution are not checked because they are not implemented. A pass does not mean Scopra can authorize or execute commands.

Revalidation is a point-in-time diagnostic: a workspace can change after doctor runs, so a later check by Mio is still needed before any use. A directory that is deleted and recreated at the same path can reuse its inode number and go unnoticed (ADR-004). A stale registration is a warning, not a failure, so on its own it leaves the exit status `0`:

```text
[warn] workspaces: 1 of 2 registered workspaces no longer qualifies: 9f1c2d3e4b5a69788796a5b4c3d2e1f0 /home/alice/src/old (missing)
       hint: Scopra revalidates a workspace before using it, so these would be refused. Restore each directory, or run `scopra workspace remove ID`, which does not change the directory.
```

The human-readable report goes to stdout, with control characters in paths and error text escaped. `--json` writes a single JSON object on one line to stdout. Here it is pretty-printed for a machine where Mio has never started:

```json
{
  "schemaVersion": 1,
  "scopraVersion": "v0.1.0",
  "stateDir": "/home/alice/.local/state/scopra",
  "result": "fail",
  "checks": [
    {
      "name": "platform",
      "status": "pass",
      "detail": "linux/amd64 supports authenticated local IPC (same-user peer credentials)"
    },
    {
      "name": "state-directory",
      "status": "warn",
      "detail": "does not exist yet",
      "hint": "scoprad creates it with owner-only permissions when it starts."
    },
    {
      "name": "workspace-registry",
      "status": "pass",
      "detail": "no workspaces are registered; the state directory does not exist yet"
    },
    {
      "name": "workspaces",
      "status": "pass",
      "detail": "no workspaces are registered"
    },
    {
      "name": "daemon",
      "status": "fail",
      "detail": "Scopra Mio is not running (its socket is missing or refuses connections)",
      "hint": "Start Mio in the foreground with `scoprad` (pass the same --state-dir, if any)."
    }
  ],
  "notChecked": ["enrollment", "relay", "policy", "containment", "execution"]
}
```

- `result` is `fail` if any check failed, otherwise `warn` if any check warned, otherwise `pass`. `skip` marks a check that did not run because a prerequisite failed.
- `stateDir` is omitted when the directory cannot be resolved. `hint` is present for every `warn` and `fail`. `detail` text is informational and may change.
- `schemaVersion` changes only for incompatible changes. New checks or fields may be added, so look up checks by `name`.

Exit status: `0` when no check fails, `1` when any check fails (after the report is written), `2` for usage errors, and `130` when interrupted.

### `scopra workspace`

`scopra workspace` manages Mio's workspace registrations over local IPC (ADR-006). Mio must be running.

- `add PATH` registers a directory. A relative `PATH` is joined to the current directory without being cleaned, so `..` is resolved after symlinks, as it is for any other program. Mio then resolves the path and applies ADR-004 validation. It rejects:
  - the filesystem root, your home directory, Mio's state directory, and any directory containing one of them;
  - any directory inside the state directory;
  - directories you do not own;
  - directories that overlap a registered workspace.

  The output shows the new ID and the canonical path that was registered.
- `list` prints one `ID  path` line per registration, sorted by path, or `No workspaces are registered.`
- `remove ID` removes the registration with that ID, as shown by `add` or `list`. It never changes the directory itself and does not ask for confirmation.

Text output escapes control characters in paths and in Mio's messages. `--json` writes a single JSON object on one line. `add` and `remove` write the affected registration, and `list` writes every registration, with an empty array when there are none:

```json
{"schemaVersion":1,"workspace":{"id":"3f2a9c0b7d1e4f6a8b5c2d0e1f3a4b6c","path":"/home/alice/projects/api"}}
{"schemaVersion":1,"workspaces":[{"id":"3f2a9c0b7d1e4f6a8b5c2d0e1f3a4b6c","path":"/home/alice/projects/api"}]}
```

`schemaVersion` changes only for incompatible changes; fields may be added.

Exit status:

- `0` on success.
- `1` when Mio is unavailable or refuses the request. The error names Mio's code, such as `protected_path`, `overlap`, or `not_found` (see ADR-006).
- `2` for usage errors, which are detected before Mio is contacted. These include a malformed ID and a path that is not valid UTF-8 or would be longer than 4095 bytes.
- `130` when interrupted.

Registering a workspace authorizes nothing by itself; nothing in Scopra executes commands yet. Any process running as you can use these commands, just as it can edit your files.

`scopra status` and `scopra doctor` change nothing, and `scopra workspace` changes only Mio's registrations. See ADR-003 and ADR-006 for the local IPC trust boundary and remaining work.
