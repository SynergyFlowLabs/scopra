# Installing Scopra

One command installs Scopra on a Linux machine:

```console
$ curl -fsSL https://scopra-six.vercel.app/install.sh | sh
```

It downloads the release for your architecture, verifies its SHA-256 against the
release's `checksums.txt`, and hands your terminal to an interactive installer.
Nothing is changed until you confirm. The script itself never runs a privileged
command and never edits your shell profile: the install writes only into your
install directory and, if you ask for one, your systemd user unit.

## What the installer asks

1. **Preflight.** Linux, a systemd user session, a resolvable state directory,
   and rootless Podman 4.0 or later with cgroups v2 delegation. The Podman probe
   runs in a transient systemd user service requesting the same controllers as
   `scoprad.service`, not in the terminal's often less-delegated cgroup. A failed
   check stops the install; Scopra never falls back to running commands without
   containment. When Podman or its delegation is the missing piece and the
   installer can fix it safely, the screen offers an opt-in plan (next section).
2. **Location.** Where to put `scopra` and `scoprad`. The default is
   `$XDG_BIN_HOME` when it is absolute, otherwise `~/.local/bin`.
3. **Sandbox image** (optional). A container image pinned by digest, such as
   `debian@sha256:…`. Mio never pulls an image, so the digest must already be in
   local storage. Leaving this empty installs the binaries only; without an image
   Mio refuses every run.
4. **Background service.** A systemd **user** unit (`scoprad.service`) that starts
   Mio with `Delegate=cpu cpuset io memory pids`, or the command line only. If you
   explicitly skip the unit, the installer separately checks Podman in the
   current process context where you will start `scoprad` manually.
5. **Install.** It copies the two binaries into place atomically, writes the unit
   if you asked for one, then asks systemd for the service state rather than
   trusting the previous step.

## If a prerequisite is missing

The preflight does not stop at "install it yourself". When it knows how to fix
what failed, the checks screen adds one line:

```text
Press f and the installer can fix this for you.
```

`f` opens a screen that prints every command it would run — exactly, argument
for argument — and nothing runs until you press `y`:

```console
sudo apt-get update
sudo apt-get install -y podman uidmap slirp4netns fuse-overlayfs
```

Enter, `n`, or Escape cancels and leaves the manual instructions in place.
`--yes` never provisions: an unattended run has no one to ask. It does print the
same commands, with an absolute path to the staged drop-in where one is needed,
so a scripted run tells you exactly what to type next.

The plan comes from `/etc/os-release` and a fixed table, never from guessing:

| Distribution (`ID` or `ID_LIKE`) | Commands |
|---|---|
| Fedora, RHEL, CentOS, Rocky, AlmaLinux, Oracle Linux | `dnf install -y podman` |
| Debian, Ubuntu, and derivatives such as Mint | `apt-get update`, then `apt-get install -y podman uidmap slirp4netns fuse-overlayfs` |
| Arch | `pacman -Sy --needed --noconfirm podman` |
| openSUSE, SLES | `zypper --non-interactive install podman` |
| Alpine | `apk add --no-cache podman` |

`sudo` is prefixed only when the installer is not already root, and only if
`sudo` exists. An unknown distribution, a missing package manager, or a
non-root process without `sudo` produces no plan at all: the installer shows the
manual command instead of guessing. The packages come from your distribution's
own repositories; the installer never adds a repository, never downloads a
binary, and never runs a shell.

While the commands run, the interface steps aside: the terminal is restored, so
`sudo` asks for your password the way it always does. Scopra never sees it. The
installer then takes the terminal back, re-runs the preflight, and continues
with the location step. If a command fails, its output stays on the screen and
the checks screen shows the error next to the manual instructions.

A distribution whose repositories ship Podman older than 4.0 — Ubuntu 22.04
has 3.4 — is installed but still refused, with the version named. What the
installer will not do is install a newer Podman from somewhere else.

### When the delegated service context lacks cgroup controllers

Podman's `cgroupControllers` describes the cgroup of the Podman process. A
terminal launched under a desktop `app.slice` can report only `memory` and
`pids` even when the user manager has `cpu`; that terminal result is not proof
of what `scoprad.service` can use. The installer therefore runs the read-only
`podman info --format json` probe with `systemd-run --user` in a transient user
service requesting `Delegate=cpu cpuset io memory pids`. The generated
`scoprad.service` requests the same controllers. The regular Hollow preflight
is unchanged and still refuses every run when its actual daemon context lacks
`cpu`, `memory`, or `pids`.

If the delegated probe reports missing controllers—or cannot start, while a
direct Podman query confirms controller absence—and the user-manager drop-in
is not already installed, `f` offers this opt-in fix:

```console
sudo mkdir -p /etc/systemd/system/user@.service.d
sudo cp /home/you/.local/state/scopra/installer/99-scopra-delegate.conf /etc/systemd/system/user@.service.d/99-scopra-delegate.conf
sudo chmod 0644 /etc/systemd/system/user@.service.d/99-scopra-delegate.conf
sudo systemctl daemon-reload
```

The staged file is `[Service]` plus `Delegate=cpu cpuset io memory pids`. It is
written in your state directory first, so every privileged command is an
argument vector rather than a shell redirection, and the file it installs is
the one printed above. Provisioning remains opt-in: nothing runs until you
review the commands and press `y`; `sudo` prompts on the restored terminal, and
Scopra never reads or stores the password.

After the commands finish, the installer reruns the delegated probe in the same
session. That probe—not the existence of the drop-in and not a guess about a
future login—is the result. If it still fails and the drop-in is already
present, Scopra does not offer the same privileged commands again or claim the
context is ready; it displays the failing check and stops. This incremental
change does not restart `user@.service` or terminate your active sessions.

The `--no-service` path is checked separately. If `scoprad` is present, the
installer also runs Podman's probe directly in the current process context
because that is where a manually launched daemon would run. If that context
lacks controllers, the no-service install stops before writing binaries and
tells you to rerun without `--no-service` and choose the delegated systemd
service, or start `scoprad` from a context that passes the check. The scripted
`--yes --no-service` path follows the same rule.

The fix is offered only when Podman is rootless, version 4.0 or later, the host
uses cgroups v2, and the required systemd tools are available. On cgroups v1
the fix is a kernel boot parameter and a reboot, so the installer explains
instead of touching anything.

The summary screen shows the install directory, the version, the service state,
and — when the directory is not on `PATH` — the exact `export PATH=…` line to add.

## Requirements

| Requirement | Why |
|---|---|
| Linux, x86-64 or arm64 | Hollow confines commands with Linux namespaces and cgroups; other platforms are refused before anything is downloaded. |
| systemd user session | `scopra service install` manages a user unit, and the installer checks the session before it changes anything. Without one the install stops rather than leaving a service that cannot start. |
| Rootless Podman 4.0+ | Hollow runs each command in one rootless container (ADR-010). |
| cgroup v2 with `cpu`, `memory`, and `pids` delegated to the daemon's actual context | The generated service requests them with `Delegate=cpu cpuset io memory pids`; the installer probes that service context. `--no-service` must pass the same check in the current process context where `scoprad` will be launched. |
| `curl` or `wget`, `tar`, `sha256sum` or `shasum` | The bootstrap script downloads, unpacks, and verifies. |

The preflight names the exact failing requirement and how to fix it. Ubuntu
22.04, for example, ships Podman 3.4 and is refused until Podman is upgraded;
on that distribution the automatic install is offered and stops at the same
version check.

## Non-interactive install

`scopra install` is the same installer without the interface, which is what a
configuration-management system wants:

```console
$ curl -fsSL https://scopra-six.vercel.app/install.sh | sh -s -- --yes
```

Flags reach `scopra install` unchanged:

```text
--dir PATH            install directory (default: $XDG_BIN_HOME or ~/.local/bin)
--sandbox-image IMAGE digest-pinned image for the service
--scoprad PATH        path of the scoprad executable
--no-service          install the command line, then verify the manual daemon context
--yes                 no interface; print one line per step
```

Without a terminal and without `--yes`, the installer stops and says so: it will
not install silently because there was nobody to confirm.

## Pin a version

`install.sh` follows the latest release. To install a specific tag:

```console
$ curl -fsSL https://scopra-six.vercel.app/install.sh | SCOPRA_VERSION=v0.1.0-alpha.8 sh
```

## Verify the download yourself

```console
$ version=$(curl -fsSL https://scopra-six.vercel.app/releases/latest.txt)
$ base="https://scopra-six.vercel.app/releases/$version"
$ curl -fsSLO "$base/scopra_linux_amd64.tar.gz" -O "$base/checksums.txt"
$ grep " scopra_linux_amd64.tar.gz$" checksums.txt | sha256sum -c -
$ tar xzf scopra_linux_amd64.tar.gz
$ ./scopra install
```

Change `amd64` to `arm64` on an ARM machine.

**What the checksum proves:** the archive is exactly the one the release
publishes, so a corrupted download or a tampered mirror is caught. It does not
prove who published the release: `checksums.txt` travels beside the archive and
is fetched over the same TLS connection. A signature (Sigstore or minisign) is
planned, not implemented; until then, install from a network you trust.

## What an install changes

- creates the install directory if it is missing;
- writes `scopra` and `scoprad` there, mode `0755`, through a temporary file and
  a rename, so a reader never sees a half-written binary;
- writes `~/.config/systemd/user/scoprad.service` when you choose the service,
  then enables and starts it;
- if you explicitly approve the cgroup-delegation plan, writes
  `/etc/systemd/system/user@.service.d/99-scopra-delegate.conf` and reloads
  systemd; no other global changes, shell profile edits, or `PATH` changes are
  made.

The install is repeatable. Running it again replaces the binaries and rewrites
the unit it owns; a unit Scopra did not write is refused unless
`scopra service install --force` is used.

## Uninstall

```console
$ scopra service uninstall
$ rm -f ~/.local/bin/scopra ~/.local/bin/scoprad
```

`scopra service uninstall` stops and removes the unit; your registry and journal
stay in `$XDG_STATE_HOME/scopra` until you delete that directory.

## Troubleshooting

| Symptom | Fix |
|---|---|
| `systemctl --user is not answering` | Log in to a systemd user session and run the installer from a login shell. A machine without systemd cannot host `scoprad`; the installer refuses rather than writing a unit nothing will start. |
| `the delegated systemd service probe could not run` | Check that `systemd-run --user` can start a transient service and that `podman` is available to the user manager. Scopra does not accept a terminal-scope result as proof for the service. |
| `podman is not available` | On the checks screen press `f` to have it installed with your distribution's package manager, or install Podman 4.0 or later yourself and run `podman info` once to create its configuration. |
| `cgroup controllers not delegated: cpu` or a delegated-probe failure | Press `f` only when the installer offers the opt-in delegation plan. It re-tests the transient delegated service in the same session and does not guess that logging out will help. If you chose `--no-service`, use the delegated systemd service or start `scoprad` from a context whose direct Podman check passes. On cgroups v1, boot with `systemd.unified_cgroup_hierarchy=1` instead. |
| `Podman runs as root, not rootless` | Install Podman as your user, not through a root service, and re-run `podman info`. |
| `cgroups are not v2` | The host must boot with cgroup v2 (`systemd.unified_cgroup_hierarchy=1`). That is a boot parameter and a reboot, so the installer explains rather than changing it. See ADR-010. |
| `Your shell does not find scopra yet` | Add the `export PATH=…` line from the summary to `~/.profile` or `~/.zshrc`, then open a new terminal. |
| `the interactive installer needs a terminal` | Re-run with `--yes` in scripts and CI, or run the installer from an interactive terminal. |
| A checksum mismatch | The download was corrupted or has been tampered with. Nothing was executed; check the network and try again. |
| The script exits on macOS | Scopra is Linux-only: Mio's containment needs Linux namespaces and cgroups. |

## How releases are built

`scripts/release.sh vX.Y.Z [--publish]` cross-compiles `scopra` and `scoprad`
with `-trimpath` and `CGO_ENABLED=0`, stamps the version into both binaries
(`-X …/internal/buildinfo.VersionOverride`), packs `scopra_linux_amd64.tar.gz`
and `scopra_linux_arm64.tar.gz` with the licence and readme, and writes
`checksums.txt` into `dist/`.

With `--publish` it also copies the two archives and `checksums.txt` into
`apps/docs-site/public/releases/<tag>/` and writes
`apps/docs-site/public/releases/latest.txt` with the tag on a single line. The
site build preserves that directory, so the next deployment serves
`/releases/<tag>/…` and `/releases/latest.txt`. `install.sh` reads
`latest.txt` — or a tag from `SCOPRA_VERSION` — and downloads from the same
origin it came from. Commit both the archives and `latest.txt`: the site is
built from the repository, and `go test ./tools/docsite` fails if the committed
output and the sources disagree.

The source of truth for the script is `apps/docs-site/static/install.sh`,
published at `/install.sh` by the same site build that publishes this page.
