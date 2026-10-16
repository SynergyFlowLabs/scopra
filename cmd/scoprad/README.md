# Scopra Mio

Scopra Mio is the local execution daemon. The executable remains `scoprad`.

## Implemented foreground status service

```text
scoprad [--state-dir PATH] [--version]
```

Mio runs in the foreground, handles SIGINT/SIGTERM, and exposes a read-only status command to `scopra status` and `scopra doctor`. Both executables default to `<user-cache-dir>/scopra/run`; supply the same `--state-dir` to both when overriding it. On Linux the state directory must be private to the effective user (mode `0700`), the socket is owner-only (`0600`), and both endpoints verify the peer UID with `SO_PEERCRED`. Unsupported operating systems fail closed in this increment.

Only one Mio serves a state directory at a time. While running, `scoprad` holds an exclusive lock on `<state-dir>/scoprad.lock`, which must be an owner-only regular file. A second `scoprad` for the same directory exits with `daemon is already running`, even if the socket file was deleted. The lock is released once Mio stops serving, including after a crash. The lock file is intentionally kept between runs: its presence does not mean Mio is running. If the whole state directory is deleted while Mio runs, a new start cannot detect the old daemon, so stop it first.

This daemon does not yet register workspaces, connect to Caloris/Goldstone, authorize actions, or execute commands. Same-UID local processes share the OS identity; peer credentials do not isolate them from one another. See ADR-003 for the IPC contract and security limits.
