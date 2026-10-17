# ADR-012: Mio as a systemd User Service

**Status:** Accepted (proposed, then accepted as written by the maintainer)
**Date:** 2026-10-17

## Context

Mio (`scoprad`) runs in the foreground. The plan asks for a Linux service so that Mio starts with the user's session and survives a terminal closing, and ADR-010 deferred the durable home of `--sandbox-image` to "the Linux service installation ... which get their own decision then".

Two facts constrain the design:

- **The user's own authority.** Mio already runs with the user's UID and can do anything that user can do. A service changes durability, not authority, but a unit file that another user can edit would let them choose what runs as this user, so its path must be private.
- **Rootless Podman needs its helpers.** `podman` relies on setuid `newuidmap`/`newgidmap` (unless the host uses a different mapping arrangement) and mounts workspaces from the home directory. Unit hardening such as `NoNewPrivileges=yes`, `ProtectHome=yes`, or `ProtectSystem=strict` would break rootless execution or workspace access, so the unit cannot use it.

## Decision

**Mio is installed as a systemd *user* service, never a system service.**

`scopra service install` writes one unit, `scoprad.service`, to `$XDG_CONFIG_HOME/systemd/user/` (default `~/.config/systemd/user/`), then runs:

```text
systemctl --user daemon-reload
systemctl --user enable --now scoprad.service
```

`scopra service uninstall` reverses it: `systemctl --user disable --now scoprad.service`, remove the unit, `systemctl --user daemon-reload`. `scopra service status` is read-only and runs only `systemctl --user is-enabled` and `is-active`.

### The unit is the durable configuration

`ExecStart` holds the absolute path of `scoprad`, an explicit `--state-dir`, and `--sandbox-image` when one is given:

```ini
# Managed by scopra: `scopra service install` rewrites this file.
[Unit]
Description=Scopra Mio local execution daemon
Documentation=https://github.com/SynergyFlowLabs/scopra
StartLimitIntervalSec=60
StartLimitBurst=3

[Service]
Type=simple
ExecStart=/path/to/scoprad --state-dir=/home/alice/.local/state/scopra --sandbox-image=name@sha256:...
Restart=on-failure
RestartSec=2
UMask=0077

[Install]
WantedBy=default.target
```

This settles ADR-010's deferred question: **there is no separate configuration file and no environment variable.** The image is visible in the unit, changing it means reinstalling, and `scoprad` itself is unchanged. The first line marks the file as Scopra's; `install` and `uninstall` refuse a different unit at that path, and `install --force` replaces it.

### Fail closed, no half-install

- Every value that can be checked without the filesystem is checked first: the image must satisfy ADR-010's digest rule, the state directory is resolved exactly as `scoprad` resolves it, and `systemctl` and an executable `scoprad` (explicit, then `PATH`, then beside `scopra`) must be found. A missing `systemctl` or `scoprad` therefore writes nothing.
- The directory chain must consist of real directories that other users cannot write to (a root-owned sticky directory such as `/tmp` is allowed), and the final directory must be owned by the user. The unit is written by temp file, `fsync`, and rename, with mode 0600. A symlink at the unit path is refused, so a write cannot be redirected.
- If `daemon-reload` or `enable --now` fails, the previous file content (or its absence) is restored and systemd is reloaded again before the error is returned.
- Every `systemctl` call is a fixed argument vector with a 30-second deadline. There is no shell.

### Escaping

`ExecStart` is parsed by systemd, not by a shell. `%` starts a specifier and `$` a variable, so both are doubled. Arguments containing whitespace, quotes, or backslashes are double-quoted with backslash escapes. Control characters are refused. A test pins the exact line for a path containing a space, `%`, `$`, and a quote.

### Platforms

Systemd user services are Linux-only. Other platforms fail closed with a clear error; the package still cross-compiles.

## Consequences

- Mio starts with the user's session and is visible to `systemctl --user`; `scopra service status` reports installed, managed, enabled, and active.
- The unit cannot enable `NoNewPrivileges`, `ProtectHome`, or `ProtectSystem`. The containment boundary remains Hollow's container, not systemd's sandbox.
- The image cannot be changed without reinstalling. That is deliberate: one place, visible and auditable.
- Reinstalling is idempotent; a failed reinstall restores the previous unit.
- Real systemd behaviour was not exercised in this environment, where no user manager runs. The commands are tested against a fake `systemctl` that scripts every call and state; verifying on a real systemd host remains an open item, together with ADR-010's real-host verification.
- Lingering, socket activation, and starting at boot without a login session are out of scope; they need a separate decision because they extend when Mio runs without the user present.

## Security Considerations

- The service runs with the user's rights, like the foreground daemon; it grants nothing new.
- A unit file editable by another user would let them choose what runs as this user, which is why the directory chain must be private and the file is 0600.
- `systemctl` is resolved from `PATH`; the install command then invokes that absolute path. A hostile `PATH` can already run programs as the user, so this adds no authority.
- The unit passes no environment beyond systemd's defaults; `scoprad` reads no configuration from the unit except its arguments.
- The state directory and image in `ExecStart` are not secret. The unit contains no credentials.
- A failed enable leaves no unit behind unless a previous one was restored; uninstall refuses a unit Scopra did not write, so it can never disable an unrelated service that happens to share the name.

## Alternatives

- **A system (root) service or a `--system` install.** Rejected: Mio needs the user's files, Podman rootless session, and user socket; running it as root would expand authority instead of containing it.
- **A configuration file (`~/.config/scopra/config.json`) plus a generic unit.** Rejected for now: it adds a format, parsing, permission rules, and another place to get the image wrong. The unit already is a durable, visible file.
- **An environment variable in the unit or the user environment.** Rejected: it is invisible in `systemctl status`, inherited unpredictably, and easy to override.
- **A template unit with `%h` specifiers.** Rejected: it assumes a standard home layout, cannot express an arbitrary `--state-dir`, and still needs escaping for the image.
- **Running `systemctl` through a shell.** Rejected: it adds an injection surface for paths and image names.
