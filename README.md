# gbbconnect-go

> Unofficial, modern reimplementation of [GbbConnect2](https://github.com/gbbsoft/GbbConnect2) in Go.
> Wire-compatible with the GbbOptimizer cloud (MQTT over TLS), with a cleanly
> engineered "core" and additional inverter transports.

This repository contains the working application, deployment packages, design
documentation, and implementation history. Development is tracked
incrementally in [`docs/tickets/`](docs/tickets/).

To install and configure a plant, start with the
**[end-user guide](docs/user-guide.md)**. It includes Docker, Home Assistant,
systemd, every supported transport, discovery, GbbConnect2 migration, secret
handling, and troubleshooting.

---

## 1. Why this project exists

The official GbbConnect2 is a .NET application that bridges solar inverters
(Deye, Goodwe, Sofar, etc.) to the GbbOptimizer cloud service. It works, but for
the use cases below it is awkward to deploy:

- Home Assistant Add-on (multi-arch, small image).
- Plain Docker container.
- Long-running service (systemd on Linux, Windows Service).

`gbbconnect-go` reimplements the same cloud protocol in Go to get:

- **Single static binary**, trivial multi-arch cross-compilation (amd64, arm64,
  armv7) for Raspberry Pi / Home Assistant OS.
- **Small container images** (scratch/distroless).
- **First-class config** that is friendly to hand-editing, automation tools, and
  the Home Assistant Add-on options UI.
- **More transports** than the original (see below).
- A **clean, testable core** with golden test vectors validating byte-for-byte
  compatibility with the original.

This is an independent, unofficial project. It is not affiliated with or endorsed
by gbbsoft. It must remain compatible with the GbbOptimizer cloud so that
existing plants keep working.

## 2. Scope

### Must keep (compatibility with GbbOptimizer cloud)

- MQTT over TLS connection to the GbbOptimizer broker (port 8883).
- The JSON "Header/Line" application protocol over MQTT.
- The exact topic names, QoS levels, and keepalive behaviour.
- SolarmanV5 framing for WiFi loggers (the original's `DriverNo=0`).
- Modbus TCP for wired/Ethernet dongles (the original's `DriverNo=1`).
- Error-cascading semantics, remote log-level control, optional log streaming,
  and sub-inverter routing.

### New functionality (beyond the original)

- **Modbus RTU over TCP** ("raw" RTU frames over a TCP socket, e.g. Waveshare
  RS485-to-Ethernet gateways).
- **Modbus over serial port** (RS485 directly attached, typically on Linux).
- **Autodiscovery CLI**: discover Solarman WiFi dongles on the LAN (UDP
  broadcast) or scan a provided subnet, and print discovered inverter/dongle
  serial numbers.
- **YAML configuration** (with legacy `Parameters.xml` import) and a published
  JSON Schema for Home Assistant Add-on options.
- Packaging for Home Assistant Add-on, multi-arch Docker, systemd, and Windows
  Service.

### Non-goals (at least initially)

- No GUI (the original has a WinForms app). Configuration is file/CLI/HA-options
  based.
- No attempt to reverse new/undocumented cloud features beyond what the official
  GbbConnect2 v1.3.0 implements.
- Not a generic Modbus dashboard; it is a focused cloud bridge.

## 3. Chosen technology

- **Language: Go.** Static binaries, simple `GOOS`/`GOARCH` cross-compilation,
  good standard library for TCP/TLS/UDP, and mature ecosystem libraries for the
  parts we need (MQTT, Modbus, serial).
- **Libraries:** `eclipse/paho.mqtt.golang` for MQTT and `go.bug.st/serial` for
  serial ports. Modbus framing is implemented in-house because it is small and
  must match the original byte-for-byte.
- Working name: module `gbbconnect-go`, binary `gbbconnect`. Easy to rename.

## 4. Documentation map

Read in this order:

| Doc | Purpose |
|-----|---------|
| [docs/user-guide.md](docs/user-guide.md) | Installation, configuration, migration, discovery, and troubleshooting. |
| [docs/01-architecture.md](docs/01-architecture.md) | Target Go architecture, concurrency, lifecycle. |
| [docs/02-protocol-cloud-mqtt.md](docs/02-protocol-cloud-mqtt.md) | MQTT/TLS transport: broker, auth, topics, QoS, keepalive. |
| [docs/03-protocol-json-app.md](docs/03-protocol-json-app.md) | JSON Header/Line application protocol. |
| [docs/04-protocol-solarmanv5.md](docs/04-protocol-solarmanv5.md) | SolarmanV5 frame format, sequence, retries. |
| [docs/05-protocol-modbus.md](docs/05-protocol-modbus.md) | Modbus RTU/CRC, Modbus TCP, RTU-over-TCP, serial. |
| [docs/06-driver-interface.md](docs/06-driver-interface.md) | The driver/transport abstraction. |
| [docs/07-configuration.md](docs/07-configuration.md) | YAML schema, env overrides, XML import, HA mapping, state. |
| [docs/08-discovery.md](docs/08-discovery.md) | Dongle autodiscovery and the discovery CLI. |
| [docs/09-deployment.md](docs/09-deployment.md) | HA add-on, Docker, systemd, Windows service. |
| [docs/10-compatibility-and-testing.md](docs/10-compatibility-and-testing.md) | Golden vectors, mocks, acceptance matrix. |
| [docs/11-glossary.md](docs/11-glossary.md) | Domain terminology. |

## 5. Implementation history

The work is broken into epics and tickets under
[docs/tickets/](docs/tickets/). Start at
[docs/tickets/README.md](docs/tickets/README.md), which lists every ticket, its
epic, dependencies, and suggested order.

Each ticket is self-contained: it links to the relevant official source and
design docs, states acceptance criteria, and includes test notes, so an
implementer (human or AI agent) can pick it up with minimal extra context.

## 6. Relationship to the original source

The `../` parent repository is the official GbbConnect2 .NET source. Throughout
these docs, file references like
[`GbbEngine2/Server/JobManager-mqtt.cs`](../GbbEngine2/Server/JobManager-mqtt.cs)
point at the authoritative behaviour we must match. The reverse-engineered
protocol notes in
[`docs-donotanalyze/GbbConnect2-Architecture.md`](../docs-donotanalyze/GbbConnect2-Architecture.md)
were also reviewed; where this documentation and that file disagree, the actual
C# source is authoritative (and any such discrepancies are called out in
[docs/10-compatibility-and-testing.md](docs/10-compatibility-and-testing.md)).
