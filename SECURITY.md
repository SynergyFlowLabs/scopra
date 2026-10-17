# Security Policy

Scopra runs commands and holds a boundary around a user's machine. Security reports are taken seriously, and the project is grateful to anyone who takes the time to report one responsibly.

Scopra is **pre-alpha**. It is not ready for production or sensitive systems, and no security guarantee is offered yet.

## Reporting a vulnerability

**Please do not open a public issue, pull request, or discussion for a suspected vulnerability.**

Report privately through either channel, in order of preference:

1. **GitHub Private Vulnerability Reporting** — use the [Security tab](https://github.com/SynergyFlowLabs/scopra/security/advisories/new) → *Report a vulnerability*. This creates a private advisory visible only to you and the maintainers.
2. **Email** — `security@synergyflowlabs.com`. Encrypt if you can; ask for a key first rather than sending secrets in an unencrypted message.

Please include, as far as you can:

- the component and version or commit (`scopra version`, `scoprad --version`);
- the platform (distribution, kernel, Podman version, cgroup mode);
- the exact command line or sequence of steps;
- what you expected and what happened;
- the impact you believe applies (for example: workspace escape, privilege escalation, command execution as the user, data disclosure);
- any proof of concept, ideally minimal and safe to run.

Do **not** include real secrets, third-party personal data, or output from someone else's machine.

## What to expect

- **Acknowledgement within 3 business days.**
- A first assessment and severity within **10 business days**, with a remediation plan or an explanation of why it is not a vulnerability.
- Progress updates at least every **14 days** while a fix is in progress.
- Credit in the release notes and, if you wish, in the security advisory. Let us know if you prefer to stay anonymous.

## Disclosure

The maintainers coordinate disclosure:

- Fixes are prepared privately, tested, and released before details are published.
- A GitHub Security Advisory is published when a fix is available, or after **90 days**, whichever comes first, unless a longer period is agreed with the reporter.
- If a fix cannot be produced in time, the advisory describes the limitation so users can protect themselves.

## Scope

In scope:

- the `scopra` and `scoprad` binaries, and the Go packages in `internal/` and `packages/`;
- the local IPC protocol and its trust boundary;
- the Hollow sandbox invocation and workspace validation;
- the run journal and its file handling;
- the systemd unit written by `scopra service`;
- the protocol decoders: envelope, execution request, lifecycle messages.

Out of scope:

- vulnerabilities in Podman, the kernel, systemd, or a container image (report those upstream; if Scopra's use of them is the problem, that *is* in scope);
- attacks that require the attacker to already run code as the same user owning the workspace, unless they cross a boundary Scopra claims to enforce;
- denial of service caused by a user exhausting their own machine or state directory;
- the pre-alpha absence of features that are documented as not implemented (policy, approvals, audit chain);
- findings that depend on running Scopra as root when the documentation says not to.

## Known security properties and limits

These are deliberate, documented, and not vulnerabilities in themselves:

- **Same-UID trust.** Any process running as you can talk to Mio's socket and use `scopra`, exactly as it could already run commands. Peer credentials do not isolate processes sharing a UID.
- **Local runs grant nothing.** `scopra run` is your own action. There is no policy engine or approval gate yet.
- **The journal is not an audit trail.** It is metadata-only, owner-only (`0600`), bounded, and the user can edit or delete it. Tamper-evident audit is planned (Catena) but not implemented (ADR-013).
- **Containment is Hollow's container.** The systemd unit deliberately omits `NoNewPrivileges`, `ProtectHome`, and `ProtectSystem` because rootless Podman needs its setuid helpers and home-directory workspaces (ADR-012).
- **No real-host verification yet.** Execution is tested against a fake Podman, and the service against a fake `systemctl`.

## Safe harbor

The maintainers will not pursue legal action against researchers who:

- act in good faith and follow this policy;
- test only against their own installation and data;
- do not access, modify, or exfiltrate another person's data;
- do not degrade service for others;
- give the project reasonable time to fix the issue before public disclosure.

If in doubt, ask first: `security@synergyflowlabs.com`.

## Hardening for users

Until Scopra leaves pre-alpha:

- run it only on a machine you can reimage;
- keep the state directory (`~/.local/state/scopra`) owner-only and never share it;
- use a digest-pinned image, and keep it updated;
- do not grant the workspace anything you would not grant a shell — the sandbox narrows authority, it does not make untrusted code safe;
- keep Podman and the host kernel patched.
