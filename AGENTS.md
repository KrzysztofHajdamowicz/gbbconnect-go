# Agent orientation

A two-minute map of this repository for contributors and AI coding agents.
Read this before exploring; it replaces the usual "figure out what this repo
is" pass.

## What this is

`gbbconnect-go` is an unofficial Go reimplementation of
[GbbConnect2](https://github.com/gbbsoft/GbbConnect2) (.NET): a daemon that
bridges solar inverters to the GbbOptimizer cloud over MQTT/TLS and polls the
inverters over several Modbus transports. **Byte-for-byte wire compatibility
with the original C# implementation is the prime directive** — protocol
changes must match the original, validated by golden test vectors.

## Module map

Dependency direction is strictly downward: protocol packages never import the
supervisor or CLI; transports never dial the cloud.

| Path | Purpose |
|---|---|
| `cmd/gbbconnect/` | Cobra CLI (`run`, `discover`, `config validate`, `import-xml`, `version`, Windows `service`), daemon bootstrap |
| `internal/protocol/` | Cloud JSON "Header/Line" application protocol codec |
| `internal/cloud/` | MQTT/TLS client, keepalive, request handler, remote log-level, log streaming |
| `internal/cloudtest/` | In-process TLS MQTT broker for tests |
| `internal/modbus/` | Modbus RTU framing, CRC, response parsing (in-house, must match original bytes) |
| `internal/driver/` | Driver facade, executor, transport factory |
| `internal/driver/{solarmanv5,modbustcp,modbusrtutcp,modbusserial,random}/` | Concrete transports; `random` is diagnostic-only |
| `internal/discovery/` | Solarman dongle discovery: UDP broadcast + bounded subnet scan |
| `internal/config/` | YAML/JSON config model, loading, env overrides, schema validation |
| `internal/config/xmlimport/` | Legacy GbbConnect2 `Parameters.xml` importer |
| `internal/state/`, `internal/supervisor/`, `internal/logbuf/` | Persistent state, per-plant workers/lifecycle, logging |
| `internal/testutil/`, `internal/invertertest/` | Golden fixtures + byte assertions, device-side transport mocks |
| `schema/gbbconnect.schema.json` | Canonical embedded JSON Schema for the config |
| `gbbconnect_go/` | Home Assistant add-on package — **must stay at repo root** (Supervisor discovery) and the directory name must equal the slug |
| `deploy/` | Main container image, systemd unit, Windows Service docs |
| `scripts/` | Cross-build, protocol-coverage gate, release packaging |
| `docs/` | Design docs `01`–`11`, `user-guide.md`, ticket history in `docs/tickets/` |

## Configuration model

Canonical config is YAML (`version: 1`, `runtime`, `logging`, `plants[]`
with nested `serial_port`, `cloud`, and top-level `sub_inverters[]`),
validated against `schema/gbbconnect.schema.json`. Env overrides use the
`GBB_` prefix (see `internal/config/loader.go`), including
`GBB_PLANT_<N>_CLOUD_PLANT_TOKEN` for secrets. `gbbconnect import-xml`
converts a legacy `Parameters.xml`.

The Home Assistant add-on exposes a **flattened** variant of the same model
(Supervisor UI cannot nest); `gbbconnect_go/render.jq` re-nests
`/data/options.json` into canonical YAML-equivalent JSON, then `run.sh`
validates it and execs the daemon. `internal/config/addon_test.go` enforces
that the add-on schema stays in sync with the native model — touch one, check
the other.

## CI and release traps

- Release tag `vX.Y.Z` **must equal** `version:` in
  `gbbconnect_go/config.yaml`, and the manifest `image:` must be
  `ghcr.io/<owner>/gbbconnect-go-addon` — the release workflow's `prepare`
  job fails otherwise.
- Any user-visible change to `gbbconnect_go/` contents requires a version
  bump + tag before Home Assistant users receive it.
- CI jobs: `quality` (build, vet, golangci-lint, race tests, protocol
  coverage gate via `scripts/check-protocol-coverage.sh`), `addon-lint`
  (frenck/action-addon-linter on `./gbbconnect_go`), `cross-build`
  (static binaries for linux amd64/arm64/armv7, windows, darwin), and a real
  Windows SCM service lifecycle test.
- Published images: `ghcr.io/krzysztofhajdamowicz/gbbconnect-go` (main) and
  `...-gbbconnect-go-addon` (+ per-arch `amd64-`/`aarch64-`/`armv7-`
  prefixes).

## Make targets

`build`, `build-all`, `test`, `coverage`, `coverage-protocol`, `lint`,
`tidy`.

## Conventions

- Compatibility first: golden vectors live in `internal/testutil/`; do not
  change protocol bytes without matching the original C# behaviour.
- New transports get a new driver name, never a new `DriverNo` — the concrete
  checklist is in [CONTRIBUTING.md](CONTRIBUTING.md) §6.
- Keep the JSON Schema, the HA options schema, and validation logic in sync.

## Where to go next

- Users / installation: [docs/user-guide.md](docs/user-guide.md)
- Workflow, CI contract: [CONTRIBUTING.md](CONTRIBUTING.md)
- Design details: [docs/01-architecture.md](docs/01-architecture.md) through
  [docs/11-glossary.md](docs/11-glossary.md)
- Why a decision was made: [docs/tickets/README.md](docs/tickets/README.md)
