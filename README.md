# gbbconnect-go

Connect your solar inverter (Deye, Goodwe, Sofar, …) to the
[GbbOptimizer](https://gbboptimizer.gbbsoft.pl/) cloud — an unofficial,
single-binary reimplementation of
[GbbConnect2](https://github.com/gbbsoft/GbbConnect2) written in Go.

[![CI](https://github.com/KrzysztofHajdamowicz/gbbconnect-go/actions/workflows/ci.yml/badge.svg)](https://github.com/KrzysztofHajdamowicz/gbbconnect-go/actions/workflows/ci.yml)
[![Release](https://github.com/KrzysztofHajdamowicz/gbbconnect-go/actions/workflows/release.yml/badge.svg)](https://github.com/KrzysztofHajdamowicz/gbbconnect-go/actions/workflows/release.yml)
[![Latest release](https://img.shields.io/github/v/release/KrzysztofHajdamowicz/gbbconnect-go)](https://github.com/KrzysztofHajdamowicz/gbbconnect-go/releases)
[![Go version](https://img.shields.io/github/go-mod/go-version/KrzysztofHajdamowicz/gbbconnect-go)](go.mod)
[![Container image](https://img.shields.io/badge/ghcr.io-gbbconnect--go-blue?logo=docker)](https://github.com/KrzysztofHajdamowicz/gbbconnect-go/pkgs/container/gbbconnect-go)

## What it does

`gbbconnect` runs 24/7 next to your solar installation. It polls your
inverter over the local network (or a serial cable), forwards the readings to
the GbbOptimizer cloud over MQTT/TLS, and passes control commands from the
cloud back to the inverter. It is wire-compatible with the official
GbbConnect2 v1.3.0, so an existing plant keeps working unchanged — you only
swap the bridge software.

It ships as a Home Assistant add-on, a small multi-arch Docker image, and
static binaries for Linux (amd64/arm64/armv7, e.g. Raspberry Pi), Windows,
and macOS.

This is an independent, unofficial project. It is not affiliated with or
endorsed by gbbsoft.

## Install

### Home Assistant add-on (recommended)

Click the button to add this repository to your Home Assistant add-on store:

[![Open your Home Assistant instance and show the add add-on repository dialog with a specific repository URL pre-filled.](https://my.home-assistant.io/badges/supervisor_add_addon_repository.svg)](https://my.home-assistant.io/redirect/supervisor_add_addon_repository/?repository_url=https%3A%2F%2Fgithub.com%2FKrzysztofHajdamowicz%2Fgbbconnect-go)

Or add it manually:

1. In Home Assistant, open **Settings → Add-ons → Add-on Store**.
2. Open the store menu (**⋮** in the top-right corner) and select
   **Repositories**.
3. Paste `https://github.com/KrzysztofHajdamowicz/gbbconnect-go`, click
   **Add**, and close the dialog.
4. Install **gbbconnect-go** from the new repository section and configure
   your plant in the add-on's **Configuration** tab.

The add-on is currently marked **experimental**. Setup details, every
configuration field, and troubleshooting are in the
[user guide §3](docs/user-guide.md#3-home-assistant-appadd-on-quick-start).

### Docker

```bash
docker run -d --name gbbconnect \
  -v /path/to/config:/config:ro \
  -v gbbconnect-data:/data \
  -e GBB_PLANT_1_CLOUD_PLANT_TOKEN \
  ghcr.io/krzysztofhajdamowicz/gbbconnect-go:latest
```

Put your `gbbconnect.yaml` in the config mount and pass the plant token via
the environment so it never touches the YAML file. Full walkthrough in the
[user guide §2](docs/user-guide.md#2-docker-quick-start).

### systemd (Linux)

Download a static binary from the
[releases page](https://github.com/KrzysztofHajdamowicz/gbbconnect-go/releases),
verify its checksum, and install the provided
[systemd unit](deploy/systemd/). Step-by-step instructions:
[user guide §4](docs/user-guide.md#4-systemd-quick-start).

### Windows Service

The same binary registers itself as a native Windows Service
(`gbbconnect service install`). See [deploy/windows](deploy/windows/README.md).

## Features

Everything the original GbbConnect2 does, plus:

- **More inverter transports** — beyond the original's SolarmanV5 (WiFi
  logger) and Modbus TCP, it also speaks **Modbus RTU over TCP** (e.g.
  Waveshare RS485-to-Ethernet gateways) and **Modbus over a local serial
  port** (directly attached RS485).
- **Autodiscovery** — `gbbconnect discover` finds Solarman WiFi dongles on
  your LAN via UDP broadcast or a subnet scan and prints their serial
  numbers.
- **Friendly configuration** — a single YAML file with a published JSON
  Schema, environment-variable overrides for secrets, and a one-command
  import of your existing GbbConnect2 `Parameters.xml`
  (`gbbconnect import-xml`).
- **Easy to run anywhere** — one static binary per platform, small container
  images, Home Assistant add-on packaging, systemd unit, Windows Service.

Compatibility with the GbbOptimizer cloud is the project's prime directive:
the MQTT topics, JSON protocol, and Modbus framing match the original
byte-for-byte, validated by golden test vectors.

## Documentation

- **[User guide](docs/user-guide.md)** — installation, configuration for
  every transport, GbbConnect2 migration, discovery, secret handling, and
  troubleshooting. Start here.
- **[CONTRIBUTING.md](CONTRIBUTING.md)** — development workflow, CI contract,
  and the checklist for adding a new transport.
- **[AGENTS.md](AGENTS.md)** — a two-minute orientation to the codebase for
  contributors and AI coding agents.

<details>
<summary>Design documentation map</summary>

| Doc | Purpose |
|-----|---------|
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

</details>

## Project status

The bridge is functional and packaged, but young: the Home Assistant add-on
is published at `stage: experimental`, and keeping your GbbConnect2 setup
available as a fallback during the first weeks is sensible. Development
history is tracked ticket-by-ticket under
[docs/tickets/](docs/tickets/README.md).

## Relationship to the original

The official [GbbConnect2](https://github.com/gbbsoft/GbbConnect2) (.NET) is
the authoritative reference for cloud behaviour. Where this project's
documentation refers to original source files, such as
[`GbbEngine2/Server/JobManager-mqtt.cs`](https://github.com/gbbsoft/GbbConnect2/blob/master/GbbEngine2/Server/JobManager-mqtt.cs),
they live in that repository. Any behavioural discrepancies that were found
during reimplementation are recorded in
[docs/10-compatibility-and-testing.md](docs/10-compatibility-and-testing.md).
