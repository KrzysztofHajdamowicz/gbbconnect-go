# GC-011 - YAML loader, env overrides, validation

- **Epic:** B - Configuration & domain
- **Type:** Feature
- **Priority:** High
- **Status:** DONE
- **Estimate:** 1.5 days
- **Depends on:** GC-010
- **Blocks:** GC-040, GC-060, GC-061, GC-014

## Context

- Resolution order, env overrides, validation rules:
  [docs/07-configuration.md](../07-configuration.md) §1, §3, §4, §6.
- Original load + version check:
  [`Parameters.Load` / `ReadFromXML`](../../../GbbEngine2/Configuration/Parameters.cs).

## Description

Load configuration from YAML (or HA `/data/options.json`), apply environment
variable overrides, and validate it with actionable error messages.

## Tasks

- Config path resolution per [07](../07-configuration.md) §1 (`--config`,
  `GBB_CONFIG`, cwd, OS dir, HA `/data/options.json`).
- Parse YAML; also accept JSON when the file is `options.json` (HA).
- Apply env overrides (precedence env > file > default); at minimum the
  per-plant secret vars in [07](../07-configuration.md) §3.
- Validate per [07](../07-configuration.md) §4 and return aggregated, readable
  errors. Refuse `version` newer than supported (mirror original message intent).
- On missing config, return a clear error and print a sample (mirror
  [`Program.cs`](../../../GbbConnect2Console/Program.cs) "No parameters.xml").

## Acceptance criteria

- Valid YAML loads into the GC-010 model with defaults applied.
- Env var overrides win over file values.
- Each validation rule produces a specific error; invalid driver, duplicate plant
  number, missing SolarmanV5 serial, missing serial device, etc.
- Unsupported `version` is rejected.
- Loading `/data/options.json` works (HA path).

## Test notes

- Fixtures: minimal valid, full valid, each invalid case (one per rule).
- Env override test using `t.Setenv`.
- HA options.json fixture loads identically to the YAML equivalent.
