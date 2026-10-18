# ADR-010: Hollow Containment Backend: Rootless Podman

**Status:** Accepted (proposed, then accepted as written by the maintainer)
**Date:** 2026-10-17

## Context

### Requirements

Milestone 1 of the plan requires:

- a rootless Podman adapter behind a containment interface, mounting only the workspace and read-only by default;
- no network, devices, privileged mode, Docker socket, or host PID namespace;
- dropped capabilities and `no-new-privileges`;
- CPU, memory, PID, runtime, temporary-storage, and output limits;
- direct execution with an empty environment and an ephemeral home;
- no native fallback.

The threat model adds restricted `/proc`, seccomp, and minimal mounts. The risk register adds that a command never reaches Mio's state directory or socket (R-01). The PRD adds that no host credentials are inherited.

### What earlier ADRs leave to the sandbox

- **ADR-007:** the lookup of a bare executable name in a fixed search path, the fixed `PATH` and ephemeral `HOME`, empty standard input, the workspace-relative working directory, `workspaceAccess`, `network: none`, and the limits.
- **ADR-004:** workspace revalidation immediately before use.
- **ADR-009:** the states that the sandbox must produce:
  - `timed_out` and `cancelled`, which are kills that Mio initiates;
  - `out_of_memory`;
  - `errored`, with `start_failed` or `containment_failed`;
  - `rejected`, with `containment_unavailable`.

  ADR-009 also requires that output is never logged.

### Facts checked against Podman

These were checked against Podman's documentation and source code at v6.1.4 (October 2026), with first versions from its release notes:

- **Logging:** the default log driver is `journald`, which would store command output in the user's journal. `--log-driver=passthrough` instead hands stdout and stderr straight to the `podman` process. Podman refuses `passthrough` on a TTY, which is safe because Mio uses pipes. It exists since Podman 4.0.
- **Environment:** `--unsetenv-all` clears the variables that come from Podman, the image, and `containers.conf`. It exists since Podman 4.0.
- **Swap:** without `--memory-swap`, Podman sets the memory-plus-swap limit to twice `--memory`, so a command could swap as much as its memory limit.
- **Command:** once `--entrypoint` is set, Podman ignores the image's `CMD`. The command is then exactly the entrypoint followed by the arguments (`makeCommand`).
- **Mount syntax:** `--mount` values are parsed as CSV (`FindMountType` uses `encoding/csv`), and each option is split at its first `=`. `-v` splits on `:`, so it cannot express every path.
- **Rewriting the user's files:** bind-mount options `relabel` (and `:z`/`:Z`) and `U`/`chown` recursively rewrite the SELinux labels or the ownership of the host files.

## Decision

### Backend

Hollow runs each execution as one container through the local `podman` command, never the remote API and never as a native process.

Mio requires all of the following:

- Podman 4.0 or later, running rootless;
- cgroups v2 with the `cpu`, `memory`, and `pids` controllers delegated to the user;
- a configured image.

If any of these is missing, Mio rejects the request with `containment_unavailable` (ADR-009).

The image is a required Mio setting with **no default**. It must be a reference pinned by digest (`name@sha256:` and 64 hexadecimal characters), and it must already be in local storage: Mio never pulls an image while running a command. Until the setting exists, every request is rejected.

### Invocation

For a validated request, a workspace revalidated immediately before the run with canonical path `P`, and the configured image `I`, Mio runs `podman run` with exactly these arguments, in this order:

| Argument | Purpose |
|---|---|
| `--name=scopra-<first 32 hex characters of the action hash>` | lets Mio address the container for cancellation, inspection, and removal |
| `--label=scopra.action-hash=<action hash>` | lets Mio find containers that a crashed run left behind |
| `--pull=never` | never contacts a registry |
| `--log-driver=passthrough` | output reaches only Mio's pipes, never `journald` or a log file |
| `--network=none` | no network (`network: none`) |
| `--userns=keep-id` | the command runs as the user's own UID, not as root, and workspace files keep their ownership |
| `--cap-drop=all` | no capabilities |
| `--security-opt=no-new-privileges` | setuid bits and file capabilities cannot raise privileges |
| `--security-opt=label=disable` | see "SELinux" below |
| `--read-only` | the image's root filesystem cannot be changed |
| `--tmpfs=/tmp:rw,exec,nosuid,nodev,mode=1777,size=<memoryBytes>` | writable scratch space; build tools run binaries from `/tmp`; it counts against the memory limit |
| `--mount=type=bind,"src=<P>",dst=/workspace,ro=<true\|false>` | the only host path; `ro=true` unless `workspaceAccess` is `read-write`. The `src` field is quoted as CSV, with inner quotes doubled. |
| `--workdir=/workspace` or `/workspace/<workingDirectory>` | ADR-007's working directory |
| `--unsetenv-all`, `--env=PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin`, `--env=HOME=/tmp` | an empty environment plus the fixed search path and an ephemeral home |
| `--init` | a minimal init runs as process 1, forwards signals, and reaps processes; ending the container ends every descendant |
| `--memory=<memoryBytes>b`, `--memory-swap=<memoryBytes>b` | the memory limit, with no swap |
| `--cpus=<cpuMillicores ÷ 1000, as an exact decimal>` | the CPU limit |
| `--pids-limit=<pids>` | the process limit |
| `--timeout=<timeoutSeconds + 10>` | a backstop if Mio dies; Mio's own timer decides `timed_out` |
| `--entrypoint=["<executable>"]` (JSON) | the exact executable, with no image entrypoint and no image `CMD` |
| `I`, then each argument | the exact argument vector, even for arguments that look like Podman options |

**Never passed:**

- `--privileged`, `--cap-add`, and `--device`;
- any other `--mount`, `--volume`, or host path;
- any host namespace (`--pid`, `--network`, `--ipc`, `--uts`, or `--userns` set to `host`);
- `seccomp=unconfined` and `unmask`;
- `--group-add`;
- the `relabel` and `U` mount options;
- `--env-host`, `--env-file`, and `--secret`;
- `--interactive` and `--tty`.

Podman's default seccomp profile, its masked and read-only `/proc` and `/sys` paths, and its private PID, IPC, and UTS namespaces stay in force. Standard input is `/dev/null`, and stdout and stderr are pipes that Mio reads.

### Paths

`P` must be absolute and valid UTF-8, with no control characters. Go's CSV reader drops a carriage return before a newline inside a quoted field, which would change the path. Any other character, including `,`, `"`, `:`, `=`, and spaces, travels safely in the quoted `src` field. A workspace that cannot be mounted this way is rejected with `workspace_unavailable` and a message that says why.

Symlinks inside the workspace resolve inside the container's filesystem, never the host's. ADR-004 and ADR-005 keep Mio's state directory, and with it Mio's socket, outside every workspace.

### SELinux

To let a confined container read the workspace, Podman would have to relabel it, rewriting the labels of the user's files recursively. Hollow never relabels. It passes `label=disable` instead, so SELinux separation does not apply to the container. The other layers remain: the user namespace, no capabilities, `no-new-privileges`, seccomp, the read-only root, no network, and the single mount. A dedicated SELinux policy may replace this later.

### Lifecycle

These belong to later increments:

- Mio's own timer and `podman kill` implement the timeout and cancellation.
- After the container exits, `podman inspect` tells `out_of_memory` (`OOMKilled`) apart from `failed`.
- `podman rm` removes the container.
- At startup, Mio removes containers that carry a `scopra.action-hash` label from an earlier run.
- Output is read from the pipes under ADR-009's limits and turned into chunks.

### Increments

1. **Argument builder:** a pure function in `internal/hollow` that builds the argument vector above from a validated request, a canonical workspace path, and a pinned image. It starts no process.
2. **Preflight:** checks that parse Podman's version and `podman info` output and fail closed unless every requirement under "Backend" holds. They are tested on recorded samples.
3. **Runner:** start, stream output, time out, cancel, map exits, and clean up. It must also verify every argument above against a real rootless Podman.
4. **Adversarial tests:** the plan's list, on that same host.

Increments 1 and 2 can be built and tested in this sandbox. Increments 3 and 4 need a machine with rootless Podman.

**Implementation status (2026-10-17):** increments 1 and 2 are implemented in `internal/hollow`: `hollow.PodmanRunArgs` (Step 22), and `hollow.ParsePodmanInfo` with `PodmanInfo.Check` (Step 23). The preflight reads `podman info --format json`, which includes the version, and is tested on Podman's own documentation sample. Nothing ran Podman at that point; the runner status below records what came next.

## Consequences

- Commands run only where rootless Podman 4.0 or later and cgroup delegation exist. That excludes, for example, Ubuntu 22.04, which ships Podman 3.4. Elsewhere Mio says why it refuses and never falls back.
- The user pulls and pins the image ahead of time. The image's entrypoint, `CMD`, `USER`, and environment are ignored.
- `/tmp` allows execution inside the container. Without it, common build and test tools fail.
- The flag set comes from documentation and source code. Increment 3 must confirm it on a real host before Mio runs anything.

## Security Considerations

The boundary has several layers:

- a user namespace;
- no capabilities;
- `no-new-privileges`;
- default seccomp;
- a read-only root;
- no network;
- the workspace as the only host path.

The command can change the workspace when access is `read-write`, and nothing else on the host filesystem. Output never reaches `journald` or Podman's log files. Mio never pulls images, so what runs is exactly the pinned digest the user chose. Disabling SELinux labels for the container trades one layer for not rewriting the user's files. The kernel remains shared, which rootless operation mitigates but does not remove.

## Alternatives

- **Native execution:** rejected. The plan forbids a native fallback.
- **Docker:** rejected. Its daemon is rootful, and access to its socket is equivalent to root.
- **`podman run --rm`:** rejected. Mio could not inspect the out-of-memory state after exit.
- **The default `journald` or `k8s-file` log drivers:** rejected. They store output.
- **`-v` volumes:** rejected. They cannot express every path.
- **Relabelling the workspace:** rejected. It rewrites the user's files.
- **gVisor, Kata, or Firecracker:** deferred. They are stronger, but beyond the alpha and harder to run rootless.
- **The Podman remote API socket:** rejected. It exposes a powerful socket, and the local client is enough.

## Amendment — 2026-10-17: image setting and runner plan

Proposed, then accepted as written by the maintainer.

### Image setting

- `scoprad --sandbox-image=REF` sets the image for one run of Mio.
- `REF` must pass the same rule that `hollow.PodmanRunArgs` applies: a name, then `@sha256:` and 64 lowercase hexadecimal characters.
- A malformed value is a usage error (exit status 2) that is reported before Mio takes its lock or touches the state directory.
- Without the flag, Mio starts as it does today and will reject every execution with `containment_unavailable`. No execution path exists yet, so nothing visible changes until then.
- Mio never pulls the image. Whether it exists locally is checked before each run (see below).
- **Command line only, until ADR-012.** A persisted setting needed a durable place and format; ADR-012 settled that by putting it in the systemd user unit that `scopra service install` writes. `scopra status` does not report the setting, because that would change the status reply (ADR-003). `scopra doctor` checks it only when given its own `--sandbox-image`, read-only, so the status reply is unchanged.

### Splitting increment 3

**3a, buildable without Podman:** the runner, tested against a fake Podman, a helper process in the tests that imitates `podman run`, `inspect`, `kill`, `rm`, and `ps` with scripted behaviour. It proves Mio's own logic:

- process handling;
- the start notice;
- ADR-009 output chunks;
- the timeout and cancellation;
- the mapping to result states;
- cleanup.

It cannot prove isolation.

**3b, which needs a host with rootless Podman:** the same runner against real Podman, verifying every argument above, followed by increment 4. The arguments to verify are:

- `passthrough` logging;
- `--memory-swap` equal to `--memory`;
- the entrypoint override;
- the `/tmp` tmpfs replacing the default one;
- CSV-quoted mounts;
- `label=disable`.

### Runner rules

**Before the run**

- Mio runs the absolute path of `podman` that it resolves once at startup.
- Before each execution it runs the preflight check (`podman info`) and `podman image exists REF`. A failure rejects the request with `containment_unavailable`.

**Launch and start notice**

- Mio launches `podman run` with the arguments above, standard input from `/dev/null`, and stdout and stderr on pipes.
- It polls `podman inspect` until `State.StartedAt` is set or `podman` exits. It then sends `execution.started` with that time, truncated to milliseconds, so the notice reports the command's start rather than the launch of Podman.

**Output**

- Mio reads both pipes concurrently.
- Chunks hold at most 64 KiB and are numbered in one sequence, in the order they are read.
- Once the bytes sent reach `limits.outputBytes`, Mio keeps reading but discards the rest and marks the output truncated.

**Timeout and cancellation**

- Mio's timer starts at launch. When it expires, Mio runs `podman kill --signal=KILL` on the container and reports `timed_out`.
- A cancel does the same and reports `cancelled`.
- The first kill wins.

**Result**

The result comes from `podman inspect`, never from `podman run`'s own exit status. That status is 125, 126, or 127 for Podman's own failures, which a command can also return. The mapping is:

- a container that never started is `errored`, with `start_failed`;
- `OOMKilled` is `out_of_memory`;
- otherwise `State.ExitCode` 0 is `succeeded`, and 1 to 255 is `failed`.

With `passthrough` logging, Podman's own error text for a start failure may appear in the stderr stream. Increment 3b must confirm this.

**Implementation status (2026-10-17):** 3a-1 is implemented (Step 24). `scoprad --sandbox-image` is validated by `hollow.ValidateImage` and stated with `hollow.ImageRule`; it is a usage error reported before the state directory is touched, and the value reaches Mio's configuration for the runner. 3a-2 is implemented as `hollow.Runner` (Step 25) and tested against a fake Podman. It includes the whole mapping from `podman inspect` to a result, out-of-memory and start failure included, so 3a-3 keeps the timeout, cancellation, the image-presence check, and startup cleanup. 3a-3 is implemented (Step 26):

- Mio's own timer and context cancellation drive `podman kill --signal=KILL`, retried until it works or Podman exits. The first trigger wins, and a container that exits before any kill succeeds keeps its own result.
- The preflight check and `podman image exists` run before every execution and reject with `containment_unavailable`, using a bounded and sanitised message.
- Every helper call has a 30-second deadline.
- `Runner.RemoveLeftovers` removes labelled `scopra-<32 hex>` containers.

Increment 3a is therefore complete. Mio now uses the runner (ADR-011 increment 2), and it has not met a real Podman. Increment 3b and the adversarial tests need a host with rootless Podman.

**Cleanup**

- `podman rm --force` removes the container after its result.
- At startup, Mio removes every container that carries a `scopra.action-hash` label.

### Revised increments

1. Done: the argument builder (Step 22).
2. Done: the preflight check (Step 23).
3. The runner, in parts:
   - **3a-1:** the image setting (Step 24).
   - **3a-2:** the runner's launch, start notice, output chunks, normal exits, and cleanup, against the fake Podman.
   - **3a-3:** timeout, cancellation, out-of-memory and start-failure mapping, the image-presence check, and startup cleanup, against the fake Podman.
   - **3b:** verification on a real rootless host.
4. Adversarial tests, which also need that host.
