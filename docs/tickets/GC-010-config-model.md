# GC-010 - Config domain model

- **Epic:** B - Configuration & domain
- **Type:** Feature
- **Priority:** High
- **Status:** DONE
- **Estimate:** 1 day
- **Depends on:** GC-001
- **Blocks:** GC-011, GC-012, GC-013, GC-030, GC-043, GC-061

## Context

- Full schema & field mapping: [docs/07-configuration.md](../07-configuration.md).
- Original model: [`Parameters.cs`](../../../GbbEngine2/Configuration/Parameters.cs),
  [`Plant.cs`](../../../GbbEngine2/Configuration/Plant.cs),
  [`SubInverter.cs`](../../../GbbEngine2/Configuration/SubInverter.cs).
- Driver string<->number mapping:
  [docs/06-driver-interface.md](../06-driver-interface.md) §3.

## Description

Define the in-memory configuration types in `internal/config` with explicit YAML
and JSON tags (the same struct is used for YAML files and HA `/data/options.json`).
This is data only; loading/validation is GC-011.

## Tasks

- Define structs: `Config`, `Runtime`, `Logging`, `Plant`, `Cloud`,
  `SerialPort`, `SubInverter` matching [07](../07-configuration.md) §2.
- Define a `DriverType` string enum: `solarman_v5`, `modbus_tcp`,
  `modbus_rtu_tcp`, `modbus_serial`, `random`; with helpers to/from legacy
  numeric `DriverNo` (0/1/999).
- Sensible zero-value defaults (port 8899, mqtt_port 8883, baud 9600, parity
  none, enabled true, clear_old_logs true).
- A `Redacted()` / `String()` that never prints `plant_token`.

## Acceptance criteria

- Structs marshal/unmarshal round-trip in YAML and JSON with the documented field
  names.
- `DriverType` converts 0->solarman_v5, 1->modbus_tcp, 999->random and back.
- Defaults applied when fields are omitted.
- Printing a `Config` never reveals a token.

## Test notes

- Round-trip the sample YAML from [07](../07-configuration.md) §9 and the full
  example §2.
- Table test for driver type<->number mapping including unknown -> error.
