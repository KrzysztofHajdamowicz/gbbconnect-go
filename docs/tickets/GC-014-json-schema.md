# GC-014 - JSON Schema for config / HA options

- **Epic:** B - Configuration & domain
- **Type:** Feature
- **Priority:** Medium
- **Status:** TODO
- **Estimate:** 0.5 day
- **Depends on:** GC-011
- **Blocks:** GC-071

## Context

- HA options + schema sync: [docs/07-configuration.md](../07-configuration.md) §6.
- Field reference: [docs/07-configuration.md](../07-configuration.md) §2.

## Description

Publish a JSON Schema for the configuration model so hand-editing is safe
(editor validation) and automation/HA options stay in sync.

## Tasks

- Author `schema/gbbconnect.schema.json` (Draft 2020-12) describing the GC-010
  model with types, enums (driver, parity, log level), required fields, and
  descriptions.
- Add a `gbbconnect config validate --config x.yaml` command that validates the
  file against the schema (in addition to the GC-011 programmatic validation).
- Document how the HA add-on options schema (GC-071) derives from / matches this
  schema.

## Acceptance criteria

- The sample configs from [07](../07-configuration.md) validate against the
  schema; deliberately broken ones fail with useful messages.
- `config validate` exit code is non-zero on invalid config.
- Schema enums match the code enums (a test asserts they stay in sync).

## Test notes

- Validate the doc's example YAMLs against the schema in a unit test.
- Sync test: enumerate driver/parity/level constants and assert each appears in
  the schema enums.
