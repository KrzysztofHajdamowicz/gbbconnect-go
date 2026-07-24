# GC-042 - JSON Header/Line types

- **Epic:** E - Cloud gateway
- **Type:** Feature
- **Priority:** High
- **Status:** TODO
- **Estimate:** 0.5 day
- **Depends on:** GC-001
- **Blocks:** GC-043

## Context

- Wire types + serialization rules:
  [docs/03-protocol-json-app.md](../03-protocol-json-app.md) §1, golden vectors
  in [docs/10-compatibility-and-testing.md](../10-compatibility-and-testing.md) §6.
- Original types: [`GbbConnect2Protocol/Protocol.cs`](../../../GbbConnect2Protocol/Protocol.cs).

## Description

Define the `Header` and `Line` Go types with exact PascalCase JSON field names and
null-omitting serialization, plus lenient decoding.

## Tasks

- `internal/protocol`: `Header` and `Line` structs with explicit JSON tags
  matching [03](../03-protocol-json-app.md) §1 (`OrderId`, `LogLevel`,
  `SendLastLog`, `SubInverterSN`, `Lines`, `GbbVersion`, `GbbEnvironment`,
  `LastLog`, `Error`; `LineNo`, `Tag`, `Timestamp`, `Modbus`, `Error`).
- Pointers / `omitempty` so null/absent optional fields are omitted on encode.
- `Decode([]byte) (*Header, error)` tolerant of trailing commas; case-insensitive
  handling deferred to the LogLevel/hex consumers.
- LogLevel constants `OnlyErrors` / `Min` / `Max`.

## Acceptance criteria

- Decoding the request example in [03](../03-protocol-json-app.md) §2 yields 2
  lines with the right fields.
- Encoding a response with nil `Error`/`Tag` omits those keys.
- Field names are exactly PascalCase.

## Test notes

- Round-trip + null-omission tests from
  [10](../10-compatibility-and-testing.md) §6.
- Decode tolerates trailing commas.
