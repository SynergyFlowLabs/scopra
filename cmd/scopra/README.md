# Scopra Mariner

Scopra Mariner is the CLI/TUI for onboarding and machine enrollment. The executable remains `scopra`.

## Implemented command

```text
scopra status [--state-dir PATH] [--json]
scopra version
```

`scopra status` queries the local Scopra Mio status endpoint and exits nonzero if Mio is unavailable. Its `running` result means only that Mio's local IPC endpoint responds; it does not indicate workspace, policy, containment, or execution readiness. `--json` writes the status response to stdout; diagnostics go to stderr. By default both binaries use `<user-cache-dir>/scopra/run`; pass the same `--state-dir` to `scoprad` and `scopra` to override it.

This status-only increment does not start the daemon automatically, register workspaces, or authorize/execute commands. See ADR-003 for the local IPC trust boundary and remaining work.
