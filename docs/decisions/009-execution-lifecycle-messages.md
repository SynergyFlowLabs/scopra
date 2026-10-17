# ADR-009: Execution States, Results, Output, Errors, and Cancellation

**Status:** Accepted (proposed, then accepted as written by the maintainer)
**Date:** 2026-10-16

## Context

ADR-007 and ADR-008 define what Caloris sends to Mio. The plan's Messenger milestone also asks for "results, output chunks, errors, and execution states". The PRD requires:

- structured stdout and stderr, status, exit code, timing, and truncation;
- cursor-based output reads;
- streaming with a p95 latency below 500 ms;
- cancellation that terminates descendants;
- reconnection without duplicate execution;
- a terminal state for every accepted execution;
- separate retention for metadata and output.

The plan's MCP section requires the system to distinguish denial, approval, offline, timeout, and failure. It also requires raw output to stay out of metadata.

ADR-002 reserves `execution.result` and `execution.cancel` but defines neither payload. Process output is arbitrary bytes, which may not be UTF-8, and it is untrusted and may contain secrets.

## Decision

### Messages

| Type | Direction | Sent |
|---|---|---|
| `execution.started` (new) | Mio to Caloris | at most once per execution ID, when the command's process starts |
| `execution.output` (new) | Mio to Caloris | for each output chunk, in order |
| `execution.result` (reserved) | Mio to Caloris | exactly once per execution ID that reaches a terminal state, including a rejection |
| `execution.cancel` (reserved) | Caloris to Mio | to ask Mio to stop an execution |

Adding the two new types amends ADR-002's closed set for version 1. Nothing is deployed, so no version bump is needed.

Freshness, replay protection, and authentication come from the envelope (ADR-002). Every payload follows ADR-007's strict rules:

- every field is required, and each object has exactly its keys;
- `null` is never allowed;
- decoded text equals transmitted text;
- integers stay below 2^53.

Mio encodes the payloads it sends with ADR-008's canonical writer.

### States

Mio's states for one execution ID are shown below. The cloud's own states, such as pending approval or machine offline, are not part of Messenger.

```text
received ──> rejected                       never started
         └─> running ──> succeeded          exit code 0
                     ├─> failed             exit code 1–255
                     ├─> timed_out          Mio killed it at limits.timeoutSeconds
                     ├─> cancelled          Mio killed it after execution.cancel
                     ├─> out_of_memory      the sandbox killed it at limits.memoryBytes
                     └─> errored            the sandbox or Mio failed
         └─> errored                        the command could not start
```

**When Mio sends a result**

- A request that decodes and authenticates but that Mio refuses gets a `rejected` result.
- A request that fails to decode or to authenticate gets no reply. Mio does not answer forgeries, and without a valid execution ID it cannot address a result.
- A repeated request with a known execution ID never starts a second execution. If that execution is terminal, Mio sends its stored result again; otherwise it does nothing.

**Which terminal state is reported**

- `timed_out` and `cancelled` mean that Mio initiated the kill, and the first kill wins.
- A process that ends from a signal Mio did not send reports the sandbox's exit code, which is 128 plus the signal number, as a `failed` result.
- A PID limit causes fork failures, not a kill. A CPU limit throttles.

**Output**

Mio terminates the whole process tree for `timed_out`, `cancelled`, and `out_of_memory`. Output beyond `limits.outputBytes` (stdout and stderr combined) is still read but is discarded and never sent. The process keeps running, and the result reports `truncated: true`.

### Payloads

`execution.started`:

```json
{"executionId": "exe_01HZX3", "actionHash": "b4f47c942fc6499aea49d76df1f89c630f8683bd69c98f292b9e03eed4dd44e2", "startedAt": "2026-10-16T12:00:01.250Z"}
```

`execution.output`:

```json
{"executionId": "exe_01HZX3", "sequence": 1, "stream": "stdout", "data": "b2sgIAlnaXRodWIuY29tL1N5bmVyZ3lGbG93TGFicy9zY29wcmEvcGFja2FnZXMvcHJvdG9jb2wJMC4wNjBzCg"}
```

`execution.result` has three common fields (`executionId`, `actionHash`, and `state`) and exactly the following further keys for each state:

| `state` | Further keys |
|---|---|
| `succeeded` | `startedAt`, `endedAt`, `exitCode` (0), `output` |
| `failed` | `startedAt`, `endedAt`, `exitCode` (1–255), `output` |
| `timed_out`, `cancelled`, `out_of_memory` | `startedAt`, `endedAt`, `output` |
| `errored` | `error`, `output` |
| `rejected` | `error` |

```json
{"executionId": "exe_01HZX3", "actionHash": "b4f47c942fc6499aea49d76df1f89c630f8683bd69c98f292b9e03eed4dd44e2", "state": "failed", "startedAt": "2026-10-16T12:00:01.250Z", "endedAt": "2026-10-16T12:00:09.004Z", "exitCode": 1, "output": {"stdoutBytes": 64, "stderrBytes": 2048, "truncated": false, "chunks": 3}}
```

`execution.cancel`:

```json
{"executionId": "exe_01HZX3", "actionHash": "b4f47c942fc6499aea49d76df1f89c630f8683bd69c98f292b9e03eed4dd44e2", "principals": {"userId": "usr_1", "agentId": "agt_1", "clientId": "cli_1"}, "reason": "The user stopped the run"}
```

### Field rules

| Field | Rule |
|---|---|
| `executionId`, `principals.*` | As in ADR-007. The principals in a cancel identify who asked for it, which may differ from who requested the execution. |
| `actionHash` | 64 lowercase hexadecimal characters: the ADR-008 action hash of the request. Caloris checks it against the request it dispatched. Mio ignores a cancel whose action hash does not match the running execution. |
| `startedAt`, `endedAt` | RFC 3339 in UTC with exactly three fractional digits (`YYYY-MM-DDTHH:MM:SS.sssZ`). `endedAt` is not before `startedAt`. |
| `sequence` | 1 to 2^30. Each execution's chunks are numbered 1, 2, 3, and so on with no gap, across both streams, in the order Mio read them. |
| `stream` | `stdout` or `stderr`. |
| `data` | Unpadded base64url (canonical, as for ADR-002 nonces) of 1 to 65536 bytes. Empty chunks are not sent. |
| `exitCode` | The exit status that the sandbox reports. |
| `output.stdoutBytes`, `output.stderrBytes` | 0 to 2^30: the bytes sent in chunks for each stream. Their sum is at most the request's `limits.outputBytes`. |
| `output.truncated` | Whether any output was discarded at the limit. |
| `output.chunks` | 0 to 2^30: the number of chunks sent, which is the last sequence number. A receiver can therefore tell when it holds all of them. |
| `error.code` | `[a-z][a-z_]*`, at most 32 bytes. Senders use the codes below. Receivers treat an unknown well-formed code as a generic error, as in ADR-006, so that codes can be added. |
| `error.message`, `reason` | 1 to 1024 bytes with no control, format, or line or paragraph separator characters (ADR-007). The text is untrusted and grants nothing. |

### Error codes

| Code | Used for | Meaning |
|---|---|---|
| `expired` | `rejected` | The request's validity window passed before Mio could start it. |
| `unauthorized` | `rejected` | The grant or approval checks failed, once those checks exist. |
| `policy_denied` | `rejected` | Rupes's local maximum refuses the request, for example limits above the local maximum or read-write access. |
| `workspace_unavailable` | `rejected` | The workspace is not registered, or it failed revalidation (ADR-004). |
| `containment_unavailable` | `rejected` | Rootless containment is not available, so Mio fails closed. |
| `busy` | `rejected` | Mio's concurrency limit is reached. Version 1 has no queue. |
| `start_failed` | `errored` | The command could not start, for example because the executable was not found in the sandbox. |
| `containment_failed` | `errored` | The sandbox failed while the command ran. |
| `internal` | `errored` | Any other failure. |

### Delivery

Retries and acknowledgements belong to the relay and reconnect ADR. This ADR fixes the keys that make repeated messages idempotent:

- `execution.started` and `execution.result` are keyed by execution ID;
- `execution.output` is keyed by execution ID and sequence number.

## Consequences

- The cloud can report each state that the MCP tools must distinguish: denial (`rejected`, with `unauthorized` or `policy_denied`), timeout (`timed_out`), and failure (`failed` or `errored`). Approval and offline remain cloud states.
- Results carry only metadata and byte counts. Output travels only in chunks, so it can be retained, redacted, and deleted separately, and it never enters logs or Catena metadata.
- Cursor-based reads can use the sequence number. Missing chunks are detectable from `output.chunks`.
- Per-state key sets keep decoding strict without optional fields. An `errored` result carries no timestamps.
- Mio must store each execution's result until delivery is acknowledged, so that a repeated request does not run again.

## Security Considerations

- Output and error messages are untrusted and may hold secrets. They must never be logged, never be used for authorization (PRD invariant 6), and always be displayed escaped.
- Base64url keeps arbitrary output bytes exact without breaking ADR-007's text rules.
- Not replying to undecodable or unauthenticated requests avoids becoming an oracle for forged envelopes.
- The action hash in every daemon-to-cloud message lets Caloris detect a result for a different request than the one it dispatched.
- Every bound stays under the envelope's 512 KiB payload limit. The largest chunk encodes to 87,382 characters.

## Alternatives

- **One `execution.result` message for both progress and the final result:** rejected. It makes "exactly once" ambiguous, and it mixes output into metadata.
- **Output as UTF-8 text:** rejected. Output is arbitrary bytes.
- **Optional fields instead of per-state key sets:** rejected for the same reason as ADR-007: they bring missing-versus-empty ambiguity.
- **Separate sequence numbers for each stream:** rejected. One sequence keeps the interleaving order needed to replay output faithfully.

## Implementation plan

Each step is test-first, as before:

1. Generalize ADR-007's strict-document helpers so that any payload can use them, with no change in behaviour. Then implement `execution.result`: its states, per-state key sets, and errors, with a strict decoder and a canonical encoder.
2. Implement `execution.started` and `execution.output`, and add both types to the envelope's closed set.
3. Implement `execution.cancel`.

**Implementation status (2026-10-17):** all three steps are implemented in `packages/protocol` (Steps 18 to 20 in `progress.md`). Mio does not yet produce or act on these messages; that needs the relay and containment.
