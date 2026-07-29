# 07 - Configuration & State

`gbbconnect-go` configuration must be easy to edit by hand, by automation tools,
and via the Home Assistant Add-on options UI. Primary format is **YAML**; legacy
`Parameters.xml` can be imported.

Authoritative source for the fields:
[`Parameters.cs`](../../GbbEngine2/Configuration/Parameters.cs),
[`Plant.cs`](../../GbbEngine2/Configuration/Plant.cs),
[`SubInverter.cs`](../../GbbEngine2/Configuration/SubInverter.cs).

## 1. Config file resolution

Search order (first match wins), overridable by `--config <path>` /
`GBB_CONFIG`:
1. `--config` flag.
2. `GBB_CONFIG` env var.
3. `./gbbconnect.yaml` (current dir).
4. OS-appropriate config dir (e.g. `/etc/gbbconnect/gbbconnect.yaml` on Linux,
   `%ProgramData%\gbbconnect\gbbconnect.yaml` on Windows, `/data/options.json`
   when running as a HA add-on — see §6).

If no config exists, print a clear error and a sample (mirrors the original's
"ERROR: No parameters.xml file!").

## 2. YAML schema

```yaml
# gbbconnect.yaml
version: 1

# Global runtime + logging options (map from Parameters.* attributes)
runtime:
  debug: false              # 10s backoff instead of 5min; verbose
  clear_old_logs: true      # delete logs older than ~2 months (daily)
  gbb_environment: ""       # optional override; default = OS name

logging:
  level: info               # error | warn | info | debug  (see LogLevel mapping)
  driver_trace: false       # decoded Modbus trace (orig IsDriverLog)
  driver_trace_raw: false   # raw frame hex trace   (orig IsDriverLog2)
  directory: ""             # daily log files dir (empty = <state-dir>/logs); used by log streaming

# One entry per plant (inverter site)
plants:
  - number: 1               # unique id; groups state/data (orig "No")
    name: "My Main Plant"
    enabled: true           # orig IsDisabled inverted
    driver: solarman_v5     # solarman_v5 | modbus_tcp | modbus_rtu_tcp | modbus_serial

    # Inverter / dongle connection (network transports)
    address: "192.168.1.100"   # orig AddressIP
    port: 8899                  # orig PortNo (default 8899)
    serial: 1720000000          # dongle serial (orig SerialNumber); SolarmanV5 requires it

    # Serial transport only (driver: modbus_serial)
    serial_port:
      device: "/dev/ttyUSB0"
      baud: 9600
      data_bits: 8
      parity: none            # none | even | odd
      stop_bits: 1

    # GbbOptimizer cloud (per plant)
    cloud:
      plant_id: "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"   # UUID (MQTT user)
      plant_token: "your-token-here"                     # secret (MQTT pass)
      mqtt_address: "gbboptimizer1-mqtt.gbbsoft.pl"
      mqtt_port: 8883
      use_tls: true                                      # plaintext when false
      tls_insecure_skip_verify: false                    # troubleshooting only

    # Optional sub-inverters reachable behind this plant
    sub_inverters:
      - serial: 123              # inverter SN used in SubInverterSN routing
        dongle_serial: 321       # logger SN for SolarmanV5 framing
        address: "192.168.1.105"
        port: 8899
```

### Field mapping (XML attribute -> YAML)

| XML (`Parameters`/`Plant`/`SubInverter`) | YAML | Notes |
|------------------------------------------|------|-------|
| `Parameters@Server_AutoStart` | (dropped) | GUI-only concept; daemon always runs |
| `Parameters@IsVerboseLog` | `logging.level` (info vs error) | see LogLevel mapping |
| `Parameters@IsDriverLog` | `logging.driver_trace` | |
| `Parameters@IsDriverLog2` | `logging.driver_trace_raw` | |
| `Parameters@ClearOldLogs` | `runtime.clear_old_logs` | |
| `Plant@Number` | `plants[].number` | |
| `Plant@Name` | `plants[].name` | |
| `Plant@DriverNo` | `plants[].driver` | 0->solarman_v5, 1->modbus_tcp, 999->random |
| `Plant@IsDisabled` | `plants[].enabled` | inverted (`IsDisabled=0` -> `enabled: true`) |
| `Plant@AddressIP` | `plants[].address` | |
| `Plant@PortNo` | `plants[].port` | default 8899 |
| `Plant@SerialNumber` | `plants[].serial` | dongle SN |
| `Plant@GbbOptimizer_PlantId` | `plants[].cloud.plant_id` | also accept legacy `GbbVictronWeb_PlantId` |
| `Plant@GbbOptimizer_PlantToken` | `plants[].cloud.plant_token` | also `GbbVictronWeb_PlantToken` |
| `Plant@GbbOptimizer_Mqtt_Address` | `plants[].cloud.mqtt_address` | also `GbbVictronWeb_Mqtt_Address` |
| `Plant@GbbOptimizer_Mqtt_Port` | `plants[].cloud.mqtt_port` | default 8883 |
| `SubInverter@SerialNumber` | `sub_inverters[].serial` | |
| `SubInverter@DongleSerialNumber` | `sub_inverters[].dongle_serial` | |
| `SubInverter@AddressIP` | `sub_inverters[].address` | |
| `SubInverter@PortNo` | `sub_inverters[].port` | |

> Legacy alias note: the original `Plant.ReadFromXML` accepts both
> `GbbOptimizer_*` and older `GbbVictronWeb_*` attribute names. The XML importer
> must accept both.

## 3. Environment variable overrides

For automation/containers, allow overriding scalar fields via env vars. Suggested
convention (documented per ticket GC-011):
- Global: `GBB_RUNTIME_DEBUG`, `GBB_LOGGING_LEVEL`, etc.
- Per-plant secrets are the common case; support at least:
  `GBB_PLANT_<NUMBER>_CLOUD_PLANT_TOKEN`,
  `GBB_PLANT_<NUMBER>_CLOUD_PLANT_ID`.
  This lets users keep secrets out of the YAML file.
- `GBB_PLANT_<NUMBER>_CLOUD_USE_TLS` toggles TLS for the cloud connection
  (boolean, default `true`).

Precedence: env var > config file > built-in default.

## 4. Validation rules

On load, validate and fail fast with actionable messages:
- `version` must be supported (refuse newer than known, mirroring the original's
  "Can't read Parameters from newer program version!").
- Each plant `number` unique.
- `name` non-empty (original `OurCheckDataForUI`).
- `driver` is one of the known strings.
- SolarmanV5: `address`, `port`, `serial` present.
- Modbus TCP / RTU-over-TCP: `address`, `port` present.
- Serial: `serial_port.device` + `baud` present.
- Enabled plants must have `cloud.plant_id` and `cloud.plant_token` (otherwise
  the original simply never connects them — a warning is acceptable instead of a
  hard error, to match "disabled-ish" behaviour).
- Sub-inverter: `serial` and `dongle_serial` required (original
  `OurCheckDataForUI`).

## 5. Secrets handling

- `plant_token` is a secret: never log it; redact in any diagnostic dumps.
- The original stored a (commented-out) AES helper
  ([`GbbLibSmall/Encryption.cs`](../../GbbLibSmall/Encryption.cs)) but does not
  actually encrypt tokens in the XML. `gbbconnect-go` keeps tokens in plaintext
  config by default but strongly recommends env-var injection for secrets
  (see §3). Do not invent an incompatible encryption scheme.

## 6. Home Assistant Add-on options

HA add-ons present options via `config.yaml` schema and write user values to
`/data/options.json`. Plan:
- Provide a HA `config.yaml` declaring an options schema that mirrors the YAML
  above (a list of plants with typed fields). See
  [09-deployment.md](09-deployment.md) and ticket GC-071.
- On startup, if `/data/options.json` exists (HA), load it directly (it is JSON
  but structurally identical to the YAML model). Otherwise use the normal YAML
  resolution (§1).
- Publish a **JSON Schema** (ticket GC-014) describing the config model, used
  both to validate YAML and to generate/verify the HA options schema. This keeps
  hand-editing safe (editors with schema support) and automation-friendly.

The canonical schema is [`../schema/gbbconnect.schema.json`](../schema/gbbconnect.schema.json).
The Home Assistant options schema in GC-071 must be derived from these same
field names, required values, and enums; automated tests keep the driver,
logging-level, and parity enums synchronized with the Go model.

## 7. State persistence

The original persists per-plant runtime state to
`~/Documents/GbbConnect2/PlantStates/{Number:00000}.json` with two fields
(`lastLog_Date`, `lastLog_Pos`) used by log streaming (see
[03-protocol-json-app.md](03-protocol-json-app.md) §8).

`gbbconnect-go` replacement:
- A state directory (default OS data dir, or `/data` under HA, configurable via
  `--state-dir` / `GBB_STATE_DIR`).
- Per-plant state file `state/plant-<number>.json`:

```json
{ "last_log_date": "2026-03-10", "last_log_pos": 12345 }
```

- Global runtime override file `state/runtime.json`:

```json
{ "log_level": "Max" }
```

  This stores the last valid cloud `LogLevel` (`OnlyErrors`, `Min`, or `Max`)
  without rewriting the source YAML/JSON configuration. It is restored when
  the logging runtime is initialized.
- Saved after each response publish and on shutdown (original
  `OurSaveState` / `OurStopJobs`).
- Writes must be atomic (write temp + rename), matching the original
  `Save` pattern (`.tmp` then move).

## 8. Old-log cleanup

Mirror `Parameters.DoClearOldLogs`: once per day, if `clear_old_logs` is true,
delete daily log files from ~2 months ago (`now.AddMonths(-2)`, glob
`yyyy-MM*.txt`) in the log directory. Log a line with the count deleted.

## 9. Sample minimal config

```yaml
version: 1
plants:
  - number: 1
    name: "Home"
    driver: solarman_v5
    address: "192.168.1.100"
    port: 8899
    serial: 1720000000
    cloud:
      plant_id: "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
      plant_token: "your-token-here"
      mqtt_address: "gbboptimizer1-mqtt.gbbsoft.pl"
      mqtt_port: 8883
```
