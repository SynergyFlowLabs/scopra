# ADR-011: Local Execution over the Mio Socket

**Status:** Accepted (proposed, then accepted as written by the maintainer, including that local runs need no grant or approval)
**Date:** 2026-10-17

## Context

The plan's Milestone 1 asks Mariner for "local execution, status, logs, cancellation", and its exit gate begins "Direct execution works in rootless containment". Hollow's runner (ADR-010) can run one validated request, but nothing calls it.

ADR-006 made the local socket the user's administrative channel, authorized by same-UID peer credentials. It also said that any execution "will also need a grant, policy evaluation, approval where policy requires it, and enforcement by Rupes". That sentence was written for **delegated** execution: an agent acting for the user through Caloris. A local run, in which the user asks for a command in their own workspace, is a different case. This ADR decides it explicitly.

IPC v1 is bounded:

- frames of 64 KiB;
- one request per connection;
- a 2-second deadline for the whole exchange;
- 16 concurrent connections.

ADR-009 output chunks can hold 64 KiB, which is about 87 KiB in base64, so they do not fit in an IPC frame.

## Decision

### Who may run what

**Local execution is the user's own action, contained.** Any process with the user's UID can ask Mio, through the local socket only, to run a command in a registered workspace. The command runs in Hollow, exactly as a delegated execution would.

**It needs no grant or approval**, because nothing is delegated. The caller could already run the same command natively with the same rights, so the sandbox only removes authority; it adds none.

This **qualifies ADR-006's sentence for local runs only**. Delegated execution through Caloris still needs everything that sentence lists. The local socket is never reachable through Caloris, Goldstone, or Messenger, so a remote agent cannot use this path. When Resolution and Rupes exist, their local maximum policy also applies to local runs.

### The request

`execution.run` carries:

- `workspaceId`: a registration ID;
- `command`: ADR-007's executable, arguments, `shell: false`, and working directory;
- `workspaceAccess`;
- `limits`;
- `reason`.

The usual `protocolVersion`, `requestId`, and `command` keys are reused, with the payload under a `run` field. The key set is exact, and the same field rules as ADR-007 apply.

**Mio builds the full ADR-007 request itself,** so a client cannot claim to be anyone else:

- `principals`, with `userId` set to `local:<uid>` from the peer credentials, `agentId` set to `local`, and `clientId` set to `local-socket`;
- `grantId` and `sessionId` set to `local`;
- a random `executionId` and nonce;
- `createdAt` set to now and `expiresAt` set to now plus one hour;
- `network: none`.

**Before the runner, Mio:**

1. looks up the workspace and revalidates it (ADR-004), rejecting with `workspace_unavailable` on failure;
2. requires a configured `--sandbox-image` and a `podman` found at startup, rejecting with `containment_unavailable` otherwise;
3. allows at most **4** executions at a time, rejecting a fifth with `busy`, so that runs can never hold all 16 connections.

### Streaming replies

**Frames on one connection.** After the request, Mio answers on the same connection with several frames, each `{protocolVersion, requestId, ...}`:

1. at most one `started` frame, carrying ADR-009's start notice;
2. any number of `output` frames, each `{stream, data}`, with `data` as unpadded base64url of 1 to 32768 bytes;
3. exactly one final `result` frame, carrying ADR-009's result.

Mio splits each runner chunk to fit the frame limit, and the result's counts still describe the chunks that ADR-009 defines.

**Connection deadlines.** An `execution.run` connection keeps the 2-second deadline only for reading the request. After that:

- every reply frame must be written within **30 seconds**;
- a client that stops reading therefore slows the command (the pipes apply backpressure) until the deadline cancels it;
- **closing the connection cancels the run.** Mio kills the container, and the result is lost with the connection.

**What gets no reply.** A malformed request gets no reply, as in ADR-006. A well-formed request that Mio refuses gets a single `result` frame with state `rejected`.

### Mio startup

- Mio resolves `podman` once, with `exec.LookPath`, and keeps its absolute path.
- If `--sandbox-image` is set and `podman` is found, Mio removes leftover containers (`Runner.RemoveLeftovers`).
- Without either one, Mio starts as before and rejects executions.

### The `scopra run` command (Mariner)

```text
scopra run [--state-dir PATH] [--json] (--workspace ID | --path PATH) [--write] [--workdir DIR]
           [--timeout DURATION] [--memory BYTES] [--cpus N] [--pids N] [--output-limit BYTES]
           [--reason TEXT] -- EXECUTABLE [ARGUMENT...]
```

**Defaults:**

- read-only access, working directory `.`, and reason "local run by the workspace owner";
- a timeout of 10 minutes, 2 GiB of memory, 2 CPUs, 512 processes, and 16 MiB of output.

`--path` resolves a workspace as `workspace remove --path` does.

**Output:**

- **Default:** stdout and stderr bytes go unchanged to the terminal's stdout and stderr, as for a native run.
- **`--json`:** writes one JSON object per line for the start notice, each output frame (in base64url), and the result.

**Exit status:**

| Outcome | Exit status |
|---|---|
| The command exited | Its own exit status |
| `timed_out` | 124 (as `timeout(1)`) |
| `cancelled` | 130 |
| `out_of_memory` | 137 |
| `rejected` or `errored` | 1, with the error code and message |
| A usage error | 2 |

Ctrl-C closes the connection, which cancels the run.

### Increments

1. **IPC protocol:** `execution.run` in `internal/localipc`, with:
   - request validation and the exact key set;
   - streamed frames and splitting;
   - the deadlines, the concurrency cap, and cancellation on disconnect;
   - a client function that returns the events and the result.

   It is tested with a fake executor, not Hollow.
2. **Mio wiring:** Podman resolution, leftover cleanup, workspace revalidation, building the request, and running Hollow. It is tested with the fake Podman.
3. **`scopra run`:** flags, output, exit status, and `--json`.
4. **Real-host verification,** together with ADR-010's increment 3b.

## Implementation Status

**Increment 1 (IPC protocol): implemented** in `internal/localipc`, tested against a real in-process Mio with a fake executor. `scoprad` does not enable it yet, so Mio still runs no commands.

- **Server:** `Daemon.EnableExecutions(executor)`, before `Serve`, enables `execution.run`. Without it, the command gets no reply.
- **Client:** `RunExecution(ctx, stateDir, run, events)` delivers the start notice and output frames to callbacks and returns the result. A refusal is a `rejected` result, not an error.
- **Building the request moved into increment 1.** Applying ADR-007's rules means decoding a complete request, and the `busy` refusal must carry the request's `executionId` and `actionHash`. Mio splices the client's `run` values, unchanged, into a request with its own fields:
  - execution IDs are `local-` plus 32 random hex digits;
  - nonces are 16 random bytes.

  It then decodes the whole request with `protocol.DecodeExecutionRequest`, so a `null` or a lone surrogate escape is rejected instead of becoming zero or U+FFFD. Increment 2 keeps the rest of its list.
- **Faults end the run without a result.** This happens when the executor returns an error, or returns a result, start notice, or chunk that does not belong to the request, or when events arrive out of order. Mio then cancels the run and closes the connection without a `result`, so a client never shows an invented outcome. The client reports `ErrNoRunResult` for any connection that ends before the result.
- **Shutdown cancels runs.** Each run's context ends with `Serve`'s, so stopping Mio cancels the run and still sends a reading client its `cancelled` result. A client that has stopped reading can delay shutdown by at most one frame-write deadline (30 seconds).
- **Client bounds:** the client keeps the 2-second deadline for sending its request. After that, only its context bounds the run.

## Consequences

- Scopra can run a command end to end on one machine. That is what the exit gate needs, and it is the first real use of the sandbox.
- Local runs are attributed to `local:<uid>`. They are not yet audited, because Catena is not implemented.
- A long run holds one IPC connection, and at most 4 runs can hold connections at once.

## Security Considerations

**Same-UID callers.** Any process running as the user, including a locally running agent, can start sandboxed commands. That gives it nothing it lacks: it can already run commands natively. The containment, the read-only default, and the limits only take authority away.

**Remote callers.** Remote callers cannot reach this path. ADR-003's rule still holds: commands that approve or authorize actions for others must not use same-UID trust. `execution.run` approves nothing; it is the user's own action.

**Spoofed identity.** Mio, not the client, fills the principals, so a client cannot pose as a cloud principal.

**Terminal output.** Output is untrusted. By default it reaches the terminal as raw bytes, exactly as a native run would, so terminal escape sequences are as dangerous as they are natively. Programs should use `--json`.

**Bounds.** Frames stay at 64 KiB, the write deadline and the cancellation on disconnect bound a stuck client, and the concurrency cap bounds resource use.

## Alternatives

- **Requiring a grant for local runs:** rejected. No principal other than the user is involved, and same-UID code could bypass a local grant anyway.
- **Letting the client build the full ADR-007 request:** rejected. It would let any local process forge principals.
- **Raising the IPC frame limit for output:** rejected. Bounded frames are a core defence (ADR-006).
- **A separate socket for execution:** rejected. It is the same trust level, and a second socket would add surface without adding protection.
- **Running natively when containment is unavailable:** rejected (ADR-010).
