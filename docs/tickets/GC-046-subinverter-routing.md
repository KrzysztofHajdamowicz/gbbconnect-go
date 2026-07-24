# GC-046 - Sub-inverter routing

- **Epic:** E - Cloud gateway
- **Type:** Feature
- **Priority:** Medium
- **Status:** DONE
- **Estimate:** 0.5 day
- **Depends on:** GC-043
- **Blocks:** -

## Context

- Routing rule: [docs/03-protocol-json-app.md](../03-protocol-json-app.md) §4
  step 4.
- Original: the `SubInverterSN` block in
  [`JobManager-mqtt.cs`](../../../GbbEngine2/Server/JobManager-mqtt.cs);
  config [`SubInverter.cs`](../../../GbbEngine2/Configuration/SubInverter.cs).

## Description

When a request specifies `SubInverterSN`, route the transaction to the matching
sub-inverter instead of the plant's primary target.

## Tasks

- If `Header.SubInverterSN` is set and non-empty (trim whitespace), search the
  plant's `sub_inverters` for a matching `serial`.
- On match: use that sub-inverter's `address`, `port`, and `dongle_serial`
  (the latter is the logger serial used by SolarmanV5 framing) to build the
  driver.
- On no match: global error with the exact message
  `"Inverter SerialNumber not found: {SubInverterSN} on Slave Inverters list!"`
  (null all lines per error cascading).

## Acceptance criteria

- A request targeting a configured sub-inverter uses its address/port/dongle
  serial.
- An unknown `SubInverterSN` yields the exact error string and nulls all lines.
- Absent/empty `SubInverterSN` uses the plant's primary target.

## Test notes

- Unit test target resolution with a mock driver factory capturing the params it
  was constructed with.
- Negative test for the not-found message.
