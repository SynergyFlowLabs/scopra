# Scopra

**Controlled execution for AI agents.**

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go 1.26.9](https://img.shields.io/badge/Go-1.26.9-00ADD8?logo=go&logoColor=white)](go.mod)
[![Platform: Linux](https://img.shields.io/badge/Platform-Linux-FCC624?logo=linux&logoColor=black)](#requirements)
[![Status: pre-alpha](https://img.shields.io/badge/Status-pre--alpha-orange)](#status)
[![Documentation](https://img.shields.io/badge/docs-scopra--six.vercel.app-5ee0c0)](https://scopra-six.vercel.app)

> Give agents enough access to complete the task — never unlimited access to the machine.

Scopra lets AI agents run commands in approved developer environments without handing them the machine. It connects agents to workspaces through scoped grants, local policy, exact-action approvals, rootless isolation, structured results, and attributable audit records.

It is **free and open-source software** under the [MIT License](LICENSE), with no pricing model: no paid tiers, subscriptions, usage-based billing, plan limits, or feature gates. No account is required.

---

## Install

One command, on a Linux machine with a terminal:

```console
$ curl -fsSL https://scopra-six.vercel.app/install.sh | sh
```

The script downloads the release archive for your architecture, checks its
SHA-256 against the release's `checksums.txt`, and opens an interactive
installer. The installer checks the system first (Linux, systemd user session,
rootless Podman 4.0+ with cgroups v2 in a transient delegated service), then asks
where to put the binaries, whether to pin a sandbox image, and whether Mio should
run as a systemd user service. The generated unit requests the same controllers.
Nothing changes until you confirm. If rootless Podman is missing — or the
service context lacks cgroup controllers — the checks screen may offer an
opt-in fix: it prints every command first and runs nothing until you press `y`.
An explicit no-service install also checks the current process context where you
will start `scoprad`.

Every variant — scripted installs, a pinned version, a custom directory, a pinned
image, verifying the archive by hand — is in
[Detailed install commands](#detailed-install-commands) below. The full
reference is [Installing Scopra](docs/operations/installation.md): the flags,
the requirements, the exact files an install writes, verification, and
troubleshooting.

---

## Why Scopra

Developers paste commands from AI tools into a terminal and paste the output back. That is slow, loses context, and pressures people into granting broad, unmonitored access.

Scopra replaces that with a controlled path:

- **Least authority by default.** Agents get a workspace, not a shell. Network is off, the mount is read-only unless asked otherwise, and the environment is empty.
- **Local enforcement is authoritative.** The decision and the sandbox live on the machine that runs the command, not in a cloud service.
- **Structured execution.** Commands are executable-plus-arguments, never shell strings or a PTY.
- **Bounded and attributable.** Timeouts, memory, CPU, process, and output limits apply to every run, and results carry metadata that can be audited.
- **Free forever.** MIT licensed, local-first, and complete without a paid tier or an account.

---

## How it works

```text
  you / an agent
        │  scopra run …
        ▼
  ┌─────────────┐    authenticated Unix socket    ┌─────────────┐
  │   scopra    │ ──────────────────────────────▶ │   scoprad   │
  │  (Mariner)  │                                 │    (Mio)    │
  └─────────────┘                                 └──────┬──────┘
                                                         │ rootless Podman
                                                         ▼
                                                   ┌───────────┐
                                                   │  Hollow   │
                                                   │  sandbox  │
                                                   └───────────┘
```

| Component | Command | Role | Status |
|---|---|---|---|
| **Scopra Mariner** | `scopra` | CLI/TUI onboarding, workspaces, runs, logs, service management | Implemented |
| **Scopra Mio** | `scoprad` | Local daemon: IPC, workspace registry, execution, journal | Implemented |
| **Scopra Hollow** | — | Rootless Podman sandbox for one command | Implemented, tested against a fake Podman |
| **Scopra Messenger** | — | Versioned protocol for requests, output, and results (ADR-007/008/009) | Implemented |
| Scopra Caloris / Goldstone / Rupes / Resolution / Catena / Facula | — | Cloud control plane, relay, policy, audit chain, signals | Planned |

The containment boundary is Hollow. Mio resolves `podman` once at startup, revalidates the registered workspace immediately before use, and runs one container per request with no network, no capabilities, `no-new-privileges`, a read-only root, an empty environment, and explicit limits.

---

## Status

Scopra is **pre-alpha**. It is not ready for production or sensitive machines.

**Works today**

- `scoprad` foreground daemon with a private state directory, single-instance lock, and graceful shutdown.
- Strict same-UID Unix-socket IPC, with exact-key, bounded, fail-closed decoding.
- Workspace registration with canonical paths, ownership checks, overlap refusal, and filesystem-identity fingerprints (ADR-004/005/006).
- Local execution through Hollow when a digest-pinned image and rootless Podman are present (ADR-010/011).
- `scopra run` with human and JSON output, cancellation, and the command's own exit status.
- `scopra doctor` read-only diagnostics, including containment readiness.
- `scopra service install/uninstall/status` for a systemd **user** service (ADR-012).
- A bounded, metadata-only journal of answered runs and `scopra logs` (ADR-013).
- Protocol packages for Messenger v1 envelopes, execution requests, and lifecycle messages.
- A one-command interactive installer (`install.sh` + `scopra install`) with a
  preflight, a digest-pinned image field, and an optional systemd user service
  (ADR-016).

**Not implemented yet**

- Policy evaluation, grants, approvals, and the audit chain (Catena).
- Enrollment, relay (Goldstone), and the cloud control plane (Caloris).
- MCP server, dashboard, and SDKs.
- Output capture in the journal (deliberately deferred), and remote log access.
- macOS, Windows, and non-Linux sandboxing.

**Not verified yet**

- A real rootless Podman host. Execution is tested against a fake Podman; no real container has been run in this repository's development environment.
- A real systemd user session. The unit is tested against a scripted fake `systemctl`.

See [`progress.md`](progress.md) for the full engineering log, per-increment verification, and known gaps.

---

## Requirements

- **Linux** (x86_64 for the alpha). Other platforms fail closed.
- **Go 1.26.9** to build from source.
- **Rootless Podman 4.0+** with cgroups v2 and the `cpu`, `memory`, and `pids` controllers — only needed to execute commands.
- **A container image pinned by digest**, already present in local storage. Mio never pulls images.
- Optional: a **systemd user session** for `scopra service`.

Without Podman or a configured image, Mio still serves status, workspaces, and logs, and refuses every run with `containment_unavailable`.

---

## Quick start

```sh
git clone https://github.com/SynergyFlowLabs/scopra.git
cd scopra

make bootstrap   # checks the toolchain and modules
make lint        # gofmt + go vet
make test        # go test -race ./...
make security-test

go build -o bin/scopra ./cmd/scopra
go build -o bin/scoprad ./cmd/scoprad
```

Pull the image you intend to use, then start Mio in one terminal:

```sh
podman pull alpine@sha256:<digest>

./bin/scoprad --sandbox-image alpine@sha256:<digest>
```

In another terminal:

```sh
./bin/scopra doctor                     # check platform, state dir, workspaces, Podman
./bin/scopra workspace add ~/src/project
./bin/scopra run --workspace <id> -- uname -a
./bin/scopra run --workspace <id> --json -- ls -la
./bin/scopra logs
```

Both binaries default to `$XDG_STATE_HOME/scopra` (`~/.local/state/scopra`). Pass the same `--state-dir` to each command when overriding it. The state directory is created owner-only (`0700`).

---

## Detailed install commands

The command at the top of this file is the short version. These are the same
installer with the choices spelled out; every flag reaches `scopra install`
unchanged.

**Scripted, no interface** — for configuration management and CI, one line per
step:

```console
$ curl -fsSL https://scopra-six.vercel.app/install.sh | sh -s -- --yes
```

**Pin a version** — [`/releases/latest.txt`](https://scopra-six.vercel.app/releases/latest.txt)
names the current tag; setting `SCOPRA_VERSION` repeats an exact install:

```console
$ curl -fsSL https://scopra-six.vercel.app/install.sh | SCOPRA_VERSION=v0.1.0-alpha.8 sh
```

**Choose the directory and skip the service** — `--dir` defaults to
`$XDG_BIN_HOME` when it is absolute, otherwise `~/.local/bin`; `--no-service`
installs without a systemd unit. Because you start `scoprad` yourself in this
mode, the installer also verifies that Podman has the required controllers in
your current process context; otherwise rerun without `--no-service` and choose
the delegated systemd service:

```console
$ curl -fsSL https://scopra-six.vercel.app/install.sh | sh -s -- --dir ~/.local/bin --no-service
```

**Pin the sandbox image** — Mio never pulls an image, so the digest must already
be in local storage:

```console
$ curl -fsSL https://scopra-six.vercel.app/install.sh | sh -s -- --sandbox-image debian@sha256:<digest>
```

**Verify the download yourself** — same bytes, same check, without the bootstrap
script (use `arm64` on ARM):

```console
$ version=$(curl -fsSL https://scopra-six.vercel.app/releases/latest.txt)
$ base="https://scopra-six.vercel.app/releases/$version"
$ curl -fsSLO "$base/scopra_linux_amd64.tar.gz" -O "$base/checksums.txt"
$ grep " scopra_linux_amd64.tar.gz$" checksums.txt | sha256sum -c -
$ tar xzf scopra_linux_amd64.tar.gz && ./scopra install
```

The checksum proves the archive is the one the release publishes; it does not
prove who published it. A signature is planned, not implemented.

macOS and Windows are refused before anything is downloaded, because Mio's
containment needs Linux namespaces and cgroups. To build from source instead,
see [Quick start](#quick-start).

---

## Commands

| Command | What it does |
|---|---|
| `scopra status [--json]` | Query Mio's local IPC endpoint |
| `scopra doctor [--sandbox-image IMAGE] [--json]` | Read-only local diagnostics, including containment readiness |
| `scopra workspace add PATH` | Register a workspace directory (validated and fingerprinted) |
| `scopra workspace list` | List registrations |
| `scopra workspace remove ID \| --path PATH` | Remove a registration; never touches the directory |
| `scopra run (--workspace ID \| --path PATH) -- CMD [ARGS…]` | Run one command in Hollow |
| `scopra logs [--workspace ID] [--limit N] [--json]` | Read the metadata-only run journal |
| `scopra service install \| uninstall \| status` | Manage Mio as a systemd user service |
| `scopra install [--dir PATH] [--sandbox-image IMAGE] [--yes]` | Interactive installer; `--yes` installs without the interface |
| `scopra version` / `scoprad --version` | Print build version |

Run flags include `--write`, `--workdir`, `--timeout`, `--memory`, `--cpus`, `--pids`, `--output-limit`, and `--reason`. See [`cmd/scopra/README.md`](cmd/scopra/README.md) and [`cmd/scoprad/README.md`](cmd/scoprad/README.md) for the full contract, exit statuses, and JSON shapes.

---

## Security model

- **Same-user trust.** Any process running as you can use the local socket, exactly as it can already run commands. Peer credentials do not isolate processes that share a UID.
- **No new authority.** A local run is your own action. It grants nothing and approves nothing; it only takes capabilities away.
- **Containment.** Rootless Podman, no network, no capabilities, `no-new-privileges`, read-only root filesystem, workspace-only mount, empty environment, no host credential sockets, no PTY, no native fallback.
- **Fail closed.** An unusable registry, journal, socket, or containment setup refuses the operation instead of degrading.
- **Bounded.** Every frame, request, entry, output chunk, and file is size-limited; every helper call has a deadline.
- **No telemetry, no account.** Scopra does not phone home.

Read the [threat model](docs/security/threat-model.md), [risk register](docs/security/risk-register.md), and [security architecture](docs/security/security-architecture.md) before relying on any of it. To report a vulnerability, see [`SECURITY.md`](SECURITY.md) — please do not open a public issue.

---

## Documentation

| Document | Contents |
|---|---|
| <https://scopra-six.vercel.app> | The same documentation as a website, generated from these files |
| [`PRD.md`](PRD.md) | Product requirements, users, principles, alpha scope |
| [`docs/plans/00-full-implementation-plan.md`](docs/plans/00-full-implementation-plan.md) | Milestones and acceptance criteria |
| [`docs/architecture/system-overview.md`](docs/architecture/system-overview.md) | How the pieces fit together |
| [`docs/decisions/`](docs/decisions/) | Architecture Decision Records (ADR-001 … ADR-016) |
| [`docs/security/`](docs/security/) | Threat model, risk register, security architecture |
| [`docs/product/`](docs/product/) | Naming and product notes |
| [`docs/operations/installation.md`](docs/operations/installation.md) | The one-command install, requirements, flags, and troubleshooting |
| [`docs/operations/deployment.md`](docs/operations/deployment.md) | How the documentation site is built, deployed, verified, and rolled back |
| [`progress.md`](progress.md) | Running engineering log with verification evidence |
| [`CHANGELOG.md`](CHANGELOG.md) | User-visible changes by release |

---

## Project layout

```text
cmd/scopra/           Mariner CLI
cmd/scoprad/          Mio daemon
internal/journal/     Bounded, metadata-only run journal
internal/localipc/    Strict IPC: envelope, workspace, execution, logs
internal/workspace/   Canonical paths, ownership, fingerprints, registry
internal/execution/   Workspace lookup, revalidation, executor wiring
internal/hollow/      Rootless Podman argument builder, preflight, runner
internal/installer/   Installer state machine, preflight, install, screens
internal/tui/         Raw-mode terminal, key decoding, cell-grid renderer
packages/protocol/    Messenger v1 envelopes and lifecycle messages
apps/docs-site/       Generated static documentation site
docs/                 Plan, architecture, security, ADRs
scripts/              Bootstrap and development helpers
tools/docsite/        Markdown-to-HTML generator for the site
```

---

## Contributing

Contributions are welcome — code, docs, tests, threat-model review, and bug reports.

- Read [`CONTRIBUTING.md`](CONTRIBUTING.md) for the development setup, conventions, and review process.
- Everyone participating agrees to the [`CODE_OF_CONDUCT.md`](CODE_OF_CONDUCT.md).
- Good first issues are labelled [`good first issue`](https://github.com/SynergyFlowLabs/scopra/labels/good%20first%20issue).
- Security-sensitive changes need an issue, cross-review, positive and negative tests, and documentation in the same pull request.
- Report vulnerabilities privately as described in [`SECURITY.md`](SECURITY.md).

---

## License

Scopra is **free and open-source software**, released under the [MIT License](LICENSE).

There is no pricing model: no paid tiers, subscriptions, usage-based billing, plan limits, feature gates, or account requirements. Every capability is available to everyone under the same license. See [`NOTICE`](NOTICE) for third-party material and [`docs/decisions/014-open-source-no-pricing.md`](docs/decisions/014-open-source-no-pricing.md) for the decision.

## Acknowledgements

- The protocol and security design borrows heavily from the OWASP Cheat Sheet Series and the broader Go security community.
- `internal/hollow/testdata/podman-info-v4.0.0-docs.json` is Podman's documented example (Apache-2.0); see [`internal/hollow/testdata/README.md`](internal/hollow/testdata/README.md).
- Development used the open-source skills recorded in [`skills-lock.json`](skills-lock.json).
