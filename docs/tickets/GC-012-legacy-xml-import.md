# GC-012 - Legacy Parameters.xml import

- **Epic:** B - Configuration & domain
- **Type:** Feature
- **Priority:** Medium
- **Status:** TODO
- **Estimate:** 1 day
- **Depends on:** GC-011
- **Blocks:** -

## Context

- XML attribute -> YAML mapping table:
  [docs/07-configuration.md](../07-configuration.md) §2.
- Original XML reader (incl. legacy `GbbVictronWeb_*` aliases):
  [`Plant.ReadFromXML`](../../../GbbEngine2/Configuration/Plant.cs),
  [`Parameters.ReadFromXML`](../../../GbbEngine2/Configuration/Parameters.cs),
  [`SubInverter.ReadFromXML`](../../../GbbEngine2/Configuration/SubInverter.cs).

## Description

Let existing users migrate without retyping config: import an official
`Parameters.xml` and produce the equivalent `gbbconnect.yaml`.

## Tasks

- Add `gbbconnect import-xml --in Parameters.xml --out gbbconnect.yaml`
  subcommand (or `config import`).
- Parse the XML attributes per the mapping table; accept both `GbbOptimizer_*`
  and legacy `GbbVictronWeb_*` attribute names.
- Map `DriverNo` 0/1/999 to driver strings; `IsDisabled` -> `enabled` (inverted).
- Emit YAML using the GC-010 model; warn (don't fail) on unknown attributes.

## Acceptance criteria

- The sample XML from the original [`README.md`](../../../README.md) (both with
  and without `SubInverter`) imports into a valid config that passes GC-011
  validation.
- Legacy `GbbVictronWeb_*` attributes are honored.
- Driver numbers and enabled/disabled map correctly.

## Test notes

- Fixtures: the two README samples + one using `GbbVictronWeb_*` aliases.
- Round-trip: import -> load -> assert expected plant fields.
