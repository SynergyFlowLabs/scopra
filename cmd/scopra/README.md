# Scopra Mariner

Scopra Mariner is the CLI/TUI for onboarding and machine enrollment. The executable remains `scopra`.

## Implemented commands

```text
scopra status [--state-dir PATH] [--json]
scopra doctor [--state-dir PATH] [--json]
scopra version
```

By default both binaries use `<user-cache-dir>/scopra/run`; pass the same `--state-dir` to `scoprad` and `scopra` to override it. Neither command starts the daemon automatically.

### `scopra status`

`scopra status` queries the local Scopra Mio status endpoint and exits nonzero if Mio is unavailable. Its `running` result means only that Mio's local IPC endpoint responds; it does not indicate workspace, policy, containment, or execution readiness. `--json` writes the status response to stdout; diagnostics go to stderr.

### `scopra doctor`

`scopra doctor` runs read-only local diagnostics in a fixed order:

| Check | Passes when | Otherwise |
|---|---|---|
| `platform` | The OS supports authenticated local IPC (Linux). | `fail`; Mio fails closed, and the later checks are `skip`. |
| `state-directory` | The resolved directory exists and is owned by the current user with mode `0700`. It has no symlinked or unsafely writable ancestors, leaves room for the socket path, and any existing `scoprad.lock` is an owner-only regular file. | `warn` if it does not exist yet. `fail` if it cannot be resolved, is insecure, or is too long; `daemon` is then skipped. |
| `daemon` | Mio's local IPC endpoint answers the existing authenticated `status` request. | `fail`, with a hint, when Mio is not running, its socket is unsafe, or its response is invalid. |

Doctor never creates or repairs the state directory, removes stale sockets, starts Mio, or contacts a socket with unsafe ownership or permissions. It never opens or locks Mio's lock file, so it cannot make a concurrent `scoprad` start fail. It sends only the existing `status` request. A passing result means only that the local platform, state directory, and Mio IPC endpoint look healthy. Enrollment, relay, workspaces, policy, containment, and execution are not checked because they are not implemented. A pass does not mean Scopra can authorize or execute commands.

The human-readable report goes to stdout, with control characters in paths and error text escaped. `--json` writes a single JSON object on one line to stdout. Here it is pretty-printed for a machine where Mio has never started:

```json
{
  "schemaVersion": 1,
  "scopraVersion": "v0.1.0",
  "stateDir": "/home/alice/.cache/scopra/run",
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
      "name": "daemon",
      "status": "fail",
      "detail": "Scopra Mio is not running (its socket is missing or refuses connections)",
      "hint": "Start Mio in the foreground with `scoprad` (pass the same --state-dir, if any)."
    }
  ],
  "notChecked": ["enrollment", "relay", "workspaces", "policy", "containment", "execution"]
}
```

- `result` is `fail` if any check failed, otherwise `warn` if any check warned, otherwise `pass`. `skip` marks a check that did not run because a prerequisite failed.
- `stateDir` is omitted when the directory cannot be resolved. `hint` is present for every `warn` and `fail`. `detail` text is informational and may change.
- `schemaVersion` changes only for incompatible changes. New checks or fields may be added, so look up checks by `name`.

Exit status: `0` when no check fails, `1` when any check fails (after the report is written), `2` for usage errors, and `130` when interrupted.

These commands do not register workspaces or authorize/execute commands. See ADR-003 for the local IPC trust boundary and remaining work.
