# Scopra Mio

Scopra Mio is the local execution daemon. The executable remains `scoprad`.

## Implemented foreground status service

```text
scoprad [--state-dir PATH] [--sandbox-image NAME@sha256:DIGEST] [--version]
```

Mio runs in the foreground, handles SIGINT/SIGTERM, and exposes a read-only status command to `scopra status` and `scopra doctor`. Both executables default to the persistent `$XDG_STATE_HOME/scopra`, or `~/.local/state/scopra` when `XDG_STATE_HOME` is unset; a relative value fails closed. Supply the same `--state-dir` to both when overriding it. Development builds before this default used `<user-cache-dir>/scopra/run`; stop any such `scoprad` before upgrading, after which that directory can be deleted. On Linux the state directory must be private to the effective user (mode `0700`), the socket is owner-only (`0600`), and both endpoints verify the peer UID with `SO_PEERCRED`. Unsupported operating systems fail closed in this increment.

Only one Mio serves a state directory at a time. While running, `scoprad` holds an exclusive lock on `<state-dir>/scoprad.lock`, which must be an owner-only regular file. A second `scoprad` for the same directory exits with `daemon is already running`, even if the socket file was deleted. The lock is released once Mio stops serving, including after a crash. The lock file is intentionally kept between runs: its presence does not mean Mio is running. If the whole state directory is deleted while Mio runs, a new start cannot detect the old daemon, so stop it first.

At startup, after taking the lock, Mio opens its workspace registry, `<state-dir>/workspaces.json`, if the file exists (ADR-005). Mio does not create or rewrite the file at startup. If the file is unsafe (for example, readable by others, or a symlink) or invalid, `scoprad` exits 1 with the reason and leaves the file unchanged; fix or remove it yourself. Recorded workspaces are checked again before use, so a workspace directory that no longer exists does not stop Mio. Mio also needs to know your home directory, from `$HOME` or the account database, so that no workspace can contain it.

`--sandbox-image` names the container image that the Hollow sandbox will run commands in (ADR-010).

- It must be pinned by digest, as `name@sha256:` followed by 64 lowercase hexadecimal characters, for example `registry.example/scopra/runner@sha256:…`.
- A malformed value, including an empty one, is a usage error (exit status 2) that is reported before Mio touches its state directory, even with `--version`.
- Mio never pulls the image.
- Nothing runs commands yet, so the setting has no visible effect for now. Without it, Mio will reject every execution with `containment_unavailable` once execution exists.
- It is a command-line setting only. A persisted setting comes with the service installation.

Mio serves `workspace.add`, `workspace.list`, and `workspace.remove` on the same socket (ADR-006). Any process running as you can use them, as it can edit your files, and they are accepted only on this local socket. Use `scopra workspace add/list/remove` to manage them. Registering a workspace authorizes nothing by itself: this daemon does not connect to Caloris/Goldstone, authorize actions, or execute commands. Same-UID local processes share the OS identity; peer credentials do not isolate them from one another. See ADR-003 for the IPC contract and security limits.
