# Installing Scopra

One command installs Scopra on a Linux machine:

```console
$ curl -fsSL https://scopra-six.vercel.app/install.sh | sh
```

It downloads the release for your architecture, verifies its SHA-256 against the
release's `checksums.txt`, and hands your terminal to an interactive installer.
Nothing is changed until you confirm; the installer never runs `sudo` and never
edits your shell profile.

## What the installer asks

1. **Preflight.** Linux, a systemd user session, a resolvable state directory,
   and rootless Podman 4.0 or later with cgroups v2 delegation. A failed check
   stops the install and shows what to fix; the installer never falls back to
   running commands without containment.
2. **Location.** Where to put `scopra` and `scoprad`. The default is
   `$XDG_BIN_HOME` when it is absolute, otherwise `~/.local/bin`.
3. **Sandbox image** (optional). A container image pinned by digest, such as
   `debian@sha256:…`. Mio never pulls an image, so the digest must already be in
   local storage. Leaving this empty installs the binaries only; without an image
   Mio refuses every run.
4. **Background service.** A systemd **user** unit (`scoprad.service`) that starts
   Mio with your session, or the command line only.
5. **Install.** It copies the two binaries into place atomically, writes the unit
   if you asked for one, then asks systemd for the service state rather than
   trusting the previous step.

The summary screen shows the install directory, the version, the service state,
and — when the directory is not on `PATH` — the exact `export PATH=…` line to add.

## Requirements

| Requirement | Why |
|---|---|
| Linux, x86-64 or arm64 | Hollow confines commands with Linux namespaces and cgroups; other platforms are refused before anything is downloaded. |
| systemd user session | `scopra service install` manages a user unit. Without systemd the command-line install still works. |
| Rootless Podman 4.0+ | Hollow runs each command in one rootless container (ADR-010). |
| cgroup v2 with `cpu`, `memory`, and `pids` delegated | The container's limits need them. |
| `curl` or `wget`, `tar`, `sha256sum` or `shasum` | The bootstrap script downloads, unpacks, and verifies. |

The preflight names the exact failing requirement and how to fix it. Ubuntu
22.04, for example, ships Podman 3.4 and is refused until Podman is upgraded.

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
--no-service          install only the command line
--yes                 no interface; print one line per step
```

Without a terminal and without `--yes`, the installer stops and says so: it will
not install silently because there was nobody to confirm.

## Pin a version

`install.sh` follows the latest release. To install a specific tag:

```console
$ curl -fsSL https://scopra-six.vercel.app/install.sh | SCOPRA_VERSION=v0.1.0-alpha.1 sh
```

## Verify the download yourself

```console
$ version=$(curl -fsSL https://api.github.com/repos/SynergyFlowLabs/scopra/releases/latest | grep -m1 '"tag_name"' | cut -d'"' -f4)
$ base="https://github.com/SynergyFlowLabs/scopra/releases/download/$version"
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
- writes nothing else: no shell profile edits, no `PATH` changes, no global
  state, no files outside the install directory and that unit.

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
| `systemctl --user is not answering` | Log in to a systemd user session (`loginctl enable-linger` is not needed for an interactive session) and run the installer from a login shell. |
| `podman is not available` | Install Podman 4.0 or later, then run `podman info` once to create its configuration. |
| `Podman runs as root, not rootless` | Install Podman as your user, not through a root service, and re-run `podman info`. |
| `cgroups are not v2` / `cgroup controllers not delegated` | The host must boot with cgroup v2 and delegate `cpu`, `memory`, and `pids` to your user session. See ADR-010. |
| `Your shell does not find scopra yet` | Add the `export PATH=…` line from the summary to `~/.profile` or `~/.zshrc`, then open a new terminal. |
| `the interactive installer needs a terminal` | Re-run with `--yes` in scripts and CI, or run the installer from an interactive terminal. |
| A checksum mismatch | The download was corrupted or has been tampered with. Nothing was executed; check the network and try again. |
| The script exits on macOS | Scopra is Linux-only: Mio's containment needs Linux namespaces and cgroups. |

## How releases are built

`scripts/release.sh vX.Y.Z` cross-compiles `scopra` and `scoprad` with `-trimpath`
and `CGO_ENABLED=0`, stamps the version into both binaries
(`-X …/internal/buildinfo.VersionOverride`), packs
`scopra_linux_amd64.tar.gz` and `scopra_linux_arm64.tar.gz` with the licence and
readme, and writes `checksums.txt`. The archives and the checksum file are
uploaded to a GitHub release; `install.sh` resolves `releases/latest/download`,
or a tag from `SCOPRA_VERSION`. The source of truth for the script is
`apps/docs-site/static/install.sh`, published at `/install.sh` by the same site
build that publishes this page.
