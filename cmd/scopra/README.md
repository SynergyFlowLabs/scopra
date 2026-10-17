# Scopra Mariner

Scopra Mariner is the CLI/TUI for onboarding and machine enrollment. The executable remains `scopra`.

## Implemented commands

```text
scopra status [--state-dir PATH] [--json]
scopra doctor [--state-dir PATH] [--sandbox-image IMAGE] [--json]
scopra workspace add [--state-dir PATH] [--json] PATH
scopra workspace list [--state-dir PATH] [--json]
scopra workspace remove [--state-dir PATH] [--json] ID
scopra run [--state-dir PATH] [--json] (--workspace ID | --path PATH) [--write]
           [--workdir DIR] [--timeout DURATION] [--memory BYTES] [--cpus N]
           [--pids N] [--output-limit BYTES] [--reason TEXT] -- EXECUTABLE [ARGUMENT...]
scopra logs [--state-dir PATH] [--workspace ID] [--limit N] [--json]
scopra service install [--state-dir PATH] [--sandbox-image IMAGE] [--scoprad PATH] [--force]
scopra service uninstall
scopra service status [--json]
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
| `daemon` | Mio's local IPC endpoint answers the existing authenticated `status` request. | `fail`, with a hint, when Mio is not running, its socket is unsafe, or its response is invalid. If Mio is not running and the `workspace-registry` check failed, the hint says to fix the registry first, because `scoprad` would refuse to start. |
| `containment` | `podman` and `systemd-run` are on `PATH`; `podman info` runs in a transient systemd user service with `Delegate=cpu cpuset io memory pids` and meets ADR-010 (rootless, cgroups v2, the `cpu`, `memory`, and `pids` controllers, Podman 4.0 or later); and, with `--sandbox-image`, the image is pinned by digest and present in local storage. | `warn` when `podman` is not installed, or when no `--sandbox-image` was given: status and workspaces still work, but Mio refuses every run. `fail` when the delegated probe fails or the host does not meet ADR-010, or when the configured image is invalid or not in local storage: the configured execution setup cannot run. |

Doctor never creates or repairs the state directory, removes stale sockets, starts Mio, or contacts a socket with unsafe ownership or permissions. It never opens or locks Mio's lock file, so it cannot make a concurrent `scoprad` start fail. It reads the workspace registry without changing it, which is safe while Mio runs because Mio replaces the file atomically. Revalidation inspects only the metadata of registered directories; it changes neither the registry nor any workspace. It sends only the existing `status` request. The containment check runs `podman info --format json` through `systemd-run --user` in a transient service requesting `Delegate=cpu cpuset io memory pids`, then, when `--sandbox-image` is given, runs `podman image exists IMAGE` read-only. Each command is bounded by a 10-second deadline, and doctor never pulls, starts, or removes a container. It does not ask Mio which image is configured, so it checks the image it is given. A passing result means only that the local platform, state directory, workspace registry, registered workspaces, Mio IPC endpoint, and the requested containment setup look healthy. Enrollment, relay, policy, and execution are not checked because they are not implemented. A pass does not mean Scopra has authorized anything or run a command.

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
  "notChecked": ["enrollment", "relay", "policy", "execution"]
}
```

- `result` is `fail` if any check failed, otherwise `warn` if any check warned, otherwise `pass`. `skip` marks a check that did not run because a prerequisite failed.
- `stateDir` is omitted when the directory cannot be resolved. `hint` is present for every `warn` and `fail`. `detail` text is informational and may change.
- `schemaVersion` changes only for incompatible changes. New checks or fields may be added, so look up checks by `name`.

Exit status: `0` when no check fails, `1` when any check fails (after the report is written), `2` for usage errors, and `130` when interrupted.

### `scopra run`

`scopra run` runs one command in a registered workspace through Mio and Scopra Hollow (ADR-011). Mio must be running, must have been started with `--sandbox-image`, and must find `podman`; otherwise the run is refused. The workspace is selected by `--workspace ID` (as `workspace list` shows it) or by `--path PATH`, which resolves like `workspace remove --path`: an exact recorded path or the directory it now resolves to, with an ambiguous match refused. The two flags are mutually exclusive and one is required.

The command follows `--`, so an argument that looks like an option stays an argument. The flags and their defaults are:

| Flag | Default | Meaning |
|---|---|---|
| `--write` | off | Mount the workspace read-write instead of read-only. |
| `--workdir DIR` | `.` | Workspace-relative working directory. |
| `--timeout DURATION` | `10m` | Run time limit: a whole number of seconds, 1s to 24h. |
| `--memory BYTES` | `2 GiB` | Memory limit in bytes (1 MiB to 1 TiB). |
| `--cpus N` | `2` | CPU limit as a whole number, 1 to 1000 (converted to millicores). |
| `--pids N` | `512` | Process limit (1 to 65536). |
| `--output-limit BYTES` | `16 MiB` | Combined stdout and stderr limit (1 to 1 GiB). |
| `--reason TEXT` | "local run by the workspace owner" | Why the command runs. It grants nothing. |

The limits are defaults, not policy: Mio enforces its own maximum and the container limits still apply. Without `--json`, the command's stdout and stderr bytes are written unchanged to `scopra`'s stdout and stderr, as a native run would write them. With `--json`, stdout carries one JSON object per line, in arrival order:

```json
{"type":"started","actionHash":"…","executionId":"local-…","startedAt":"…"}
{"type":"output","executionId":"local-…","stream":"stdout","data":"aGVsbG8K"}
{"type":"output","executionId":"local-…","stream":"stderr","data":"d2FybmluZwo"}
{"type":"result","actionHash":"…","endedAt":"…","executionId":"local-…","exitCode":0,"output":{"chunks":2,"stderrBytes":8,"stdoutBytes":6,"truncated":false},"startedAt":"…","state":"succeeded"}
```

Each line is the canonical ADR-008 encoding of that message plus a `type` field. `data` is unpadded base64url. Frames, not ADR-009 chunk numbers, are reported, because Mio splits a chunk to fit its IPC frames. Diagnostics go to stderr in both modes, so with `--json` stdout holds nothing but JSON objects.

Exit status:

- the command's own exit status when it ran, whether it succeeded or failed;
- `124` when the run timed out;
- `130` when it was cancelled, including Ctrl-C, which closes the connection and cancels the run;
- `137` when it was killed for using too much memory;
- `1` when Mio rejected the run or it could not start, after `<code>: <message>` is written to stderr; also `1` when Mio is unreachable;
- `2` for usage errors, which are detected before Mio is contacted.

A dropped connection after the start notice ends the run with an error; `scopra run` never invents an exit status. Output is untrusted: `--json` is the safe form for programs, while the human mode writes bytes unchanged, exactly as a native run would.

### `scopra logs`

`scopra logs` reads Mio's bounded, metadata-only journal of answered runs (ADR-013) through the read-only `execution.logs` command. Mio records one line per answered run — including rejections and busy refusals — in `<state-dir>/runs.jsonl`: execution and workspace IDs, state, recorded/started/ended times, duration, exit code, stdout/stderr byte counts, the truncation flag, the chunk count, and the error code. It stores no command, arguments, working directory, reason, path, or output.

```text
scopra logs [--state-dir PATH] [--workspace ID] [--limit N] [--json]
```

- `--limit` is how many entries to show, 1 to 100, newest first. Default 20.
- `--workspace ID` limits the result to one workspace, using the ID `workspace list` shows.
- `--json` writes exactly one object, `{"schemaVersion":1,"entries":[…]}`, with `entries` always an array. Each entry has the stored form:

```json
{"schemaVersion":1,"executionId":"local-…","workspaceId":"…","state":"failed","recordedAt":"2026-10-17T06:42:04.217Z","startedAt":"2026-10-17T06:42:04.000Z","endedAt":"2026-10-17T06:42:04.000Z","durationMs":0,"exitCode":3,"stdoutBytes":23,"stderrBytes":18,"truncated":false,"chunks":2,"errorCode":""}
```

Without `--json`, one line per entry, newest first:

```text
2026-10-17T06:42:04.217Z  failed  exit 3  duration 0s  stdout 23 B  stderr 18 B  workspace …  execution local-…
```

`errored` and `rejected` entries show `error <code>` instead of an exit code, and entries with no times (rejected and errored results) show `duration -`. Text is terminal-escaped. An empty journal prints `No runs have been recorded.`, or `No runs have been recorded for workspace <id>.` with a filter.

The journal is not an audit trail: it is owner-only (`0600`), bounded to 1000 entries and 1 MiB, rotates silently, and the user can edit or delete it. Output capture is not implemented. If Mio cannot open the journal it refuses to start; if an append fails, the result already computed is still returned but later runs are refused until Mio restarts.

Exit status: `0` on success, `1` when Mio is unavailable or refuses, `2` for usage errors (an unknown flag, a stray argument, an invalid workspace ID, or a limit outside 1–100), and `130` when interrupted.

### `scopra service`

`scopra service` manages Scopra Mio as a systemd **user** service (ADR-012). It never installs a system service and never uses root.

- `install` writes `$XDG_CONFIG_HOME/systemd/user/scoprad.service` (`~/.config/systemd/user/scoprad.service` by default), runs `systemctl --user daemon-reload`, then `systemctl --user enable --now scoprad.service`. The unit requests `Delegate=cpu cpuset io memory pids`, the controllers Hollow needs. `--state-dir` is resolved exactly as `scoprad` resolves it and is written into the unit. `--sandbox-image` must be pinned by digest and is written into the unit; without it, Mio starts but refuses every run, and the command says so. `--scoprad` selects the daemon binary (default: `scoprad` on `PATH`, then beside `scopra`).
- `uninstall` stops and disables the unit, removes it, and reloads systemd. It removes nothing when Scopra Mio is not installed.
- `status` is read-only. It reports whether the unit exists, whether Scopra wrote it, and systemd's `is-enabled` and `is-active` answers. `--json` writes one object on one line:

```json
{"schemaVersion":1,"unitPath":"/home/alice/.config/systemd/user/scoprad.service","installed":true,"managed":true,"enabled":"enabled","active":"active"}
```

`schemaVersion` changes only for incompatible changes. When the unit is absent, only `schemaVersion`, `unitPath`, and `installed` are present.

The unit runs `scoprad` with the absolute paths and the image in `ExecStart`; there is no separate configuration file. Arguments are escaped as systemd reads them, control characters are refused, the unit is written atomically with mode 0600, and its directory chain must be private (a root-owned sticky directory such as `/tmp` is allowed). Install refuses to overwrite a unit it did not write unless `--force` is given, and uninstall always refuses one. If `daemon-reload` or `enable --now` fails, the previous unit (or its absence) is restored before the error is reported.

The unit deliberately omits `NoNewPrivileges`, `ProtectHome`, and `ProtectSystem`: rootless Podman needs its setuid helpers and the workspace under the home directory. Its containment boundary is Hollow, not systemd.

Exit status: `0` on success, including "not installed" and a stopped service; `1` when `systemctl`, `scoprad`, the filesystem, or the unit refuses the operation; `2` for usage errors, such as an unpinned image or an unknown flag. `130` when interrupted.

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
- `remove --path PATH` removes a registration by path instead. A relative `PATH` is made absolute as for `add`. The CLI lists the registrations and removes the one that `PATH` selects by ID, reusing the existing requests. `PATH` selects a registration in either of two ways:
  - it equals the registered path exactly, which still works after the directory is gone;
  - it now resolves to the registered path through symlinks.

  If the two ways select different registrations, for example because a registered directory was replaced by a symlink to another workspace, nothing is removed and you are asked to remove by ID. Paths of directories that no longer exist are never normalized: write them exactly as `list` shows them, without a trailing slash, `.`, or `..`.

Text output escapes control characters in paths and in Mio's messages. `--json` writes a single JSON object on one line. `add` and `remove` write the affected registration, and `list` writes every registration, with an empty array when there are none:

```json
{"schemaVersion":1,"workspace":{"id":"3f2a9c0b7d1e4f6a8b5c2d0e1f3a4b6c","path":"/home/alice/projects/api"}}
{"schemaVersion":1,"workspaces":[{"id":"3f2a9c0b7d1e4f6a8b5c2d0e1f3a4b6c","path":"/home/alice/projects/api"}]}
```

`schemaVersion` changes only for incompatible changes; fields may be added.

Exit status:

- `0` on success.
- `1` when Mio is unavailable or refuses the request, or when `remove --path` matches no registration or two. Mio's errors name its code, such as `protected_path`, `overlap`, or `not_found` (see ADR-006).
- `2` for usage errors, which are detected before Mio is contacted. These include a malformed ID, both an ID and `--path`, and a path that is empty, is not valid UTF-8, or would be longer than 4095 bytes.
- `130` when interrupted.

Registering a workspace authorizes nothing by itself. `scopra run` runs a command in a registered workspace through Mio and Hollow; policy, approvals, and audit are not implemented. Any process running as you can use these commands, just as it can edit your files.

`scopra status` and `scopra doctor` change nothing, `scopra logs` only reads Mio's run journal, `scopra workspace` changes only Mio's registrations, and `scopra run` runs a command in a registered workspace, read-only unless `--write` is given. See ADR-003 and ADR-006 for the local IPC trust boundary and remaining work.
