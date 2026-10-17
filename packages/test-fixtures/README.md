# test-fixtures

Reference data shared by every Scopra implementation. Implementation tasks are defined in `docs/plans/00-full-implementation-plan.md`.

## Messenger v1 reference payloads

`messenger/v1/` holds reference payloads for every Messenger v1 payload type that has strict rules: `execution.request` (ADR-007), its canonical form and action hash (ADR-008), and `execution.result`, `execution.started`, `execution.output`, and `execution.cancel` (ADR-009).

- `manifest.json` lists every fixture. Each entry has a `name`, a `messageType`, and a `payload` path relative to the manifest. A valid fixture also has:
  - `canonical`: the exact RFC 8785 canonical form, as a JSON string;
  - `actionHash`: for requests only, the ADR-008 action hash.

  An invalid fixture has instead an `invalidField`: text that the rejection must mention, such as `command.shell`, or `payload` for document-level errors.
- `valid/` holds payloads that must decode. They include:
  - every example from ADR-007 and ADR-009, byte for byte;
  - both ADR-008 vectors;
  - a request whose arguments hold terminal control characters;
  - all seven result states;
  - an output chunk with every byte value.
- `invalid/` holds payloads that must be rejected:
  - every strictness class: `null`, duplicate keys, unknown and missing fields, invalid UTF-8 (one file holds a raw `0xFF` byte), lone surrogates, escaped NUL, and trailing data;
  - key field rules for each payload type.

An implementation conforms when it meets three conditions:

- it decodes every valid payload to a value whose canonical form is exactly `canonical`, whose action hash is `actionHash` for requests, and whose canonical form decodes back to itself;
- it rejects every invalid payload;
- it reports an error that names `invalidField`.

The Go package `packages/protocol` checks all three in `fixtures_test.go`, which also requires the manifest to list every file exactly once and to cover each payload type with valid and invalid fixtures.

### Provenance

The expected values do not come from the Go code they test:

- payloads quoted from an ADR are extracted from the ADR text;
- canonical forms were rendered with Python's `json` module (sorted keys, compact separators, `ensure_ascii=False`), which matches RFC 8785 for this value domain;
- action hashes were computed with Python's `hashlib`, and the two ADR-008 vectors match the values published in that ADR.

### Changing fixtures

A changed `canonical` or `actionHash`, a valid payload that becomes invalid, or the reverse is a wire-format change. Before the alpha freeze it needs an amendment to the ADR concerned; after the freeze it needs a protocol-version decision (ADR-002). Adding a fixture for behaviour that is already specified needs no decision. Every file under `valid/` and `invalid/` must be listed in the manifest.

`.gitattributes` disables line-ending conversion here, so every checkout keeps the exact bytes.
