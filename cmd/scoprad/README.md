# Scopra Mio

Scopra Mio is the local execution daemon. The executable remains `scoprad`.

## Implemented foreground status service

```text
scoprad [--state-dir PATH] [--version]
```

Mio runs in the foreground, handles SIGINT/SIGTERM, and exposes a read-only status command to `scopra status`. Both executables default to `<user-cache-dir>/scopra/run`; supply the same `--state-dir` to both when overriding it. On Linux the state directory must be private to the effective user (mode `0700`), the socket is owner-only (`0600`), and both endpoints verify the peer UID with `SO_PEERCRED`. Unsupported operating systems fail closed in this increment.

This daemon does not yet register workspaces, connect to Caloris/Goldstone, authorize actions, or execute commands. Same-UID local processes share the OS identity; peer credentials do not isolate them from one another. See ADR-003 for the IPC contract and security limits.
