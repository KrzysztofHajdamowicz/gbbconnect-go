# User guide

This guide takes a new installation from an empty host to one connected
GbbOptimizer plant. `gbbconnect-go` is unofficial software; keep the original
GbbConnect2 configuration available until the replacement has been verified
read-only against the same plant.

## 1. Before installation

Collect these values:

- the GbbOptimizer plant ID, plant token, MQTT hostname, and MQTT port;
- the inverter transport type;
- for network transports, the dongle/gateway IP address and port;
- for SolarmanV5, the logger serial number;
- for serial Modbus, the device path and RS485 line parameters.

An existing GbbConnect2 `Parameters.xml` contains the cloud, transport, and
sub-inverter values and can be imported as described in
[Migrating from GbbConnect2](#8-migrating-from-gbbconnect2). Solarman loggers
can also be found with [discovery](#7-discovering-a-dongle).

> **Keep the plant token secret.** Do not commit it, paste it into issue
> reports, or include it in screenshots. For a binary, Docker, or systemd
> installation, leave `plant_token: ""` in YAML and provide
> `GBB_PLANT_<NUMBER>_CLOUD_PLANT_TOKEN` through the process environment.
> Home Assistant stores the token as a password option; protect backups and
> diagnostic exports that contain add-on options.

The host must be able to:

- resolve and reach the configured MQTT hostname on TCP port `8883` (unless the
  existing configuration specifies another port);
- reach the inverter dongle/gateway address;
- keep accurate system time for TLS certificate validation (when TLS is
  enabled, which is the default).

## 2. Docker quick start

This is the shortest deployment path on a Linux host with Docker.

1. Create `gbbconnect.yaml` using the SolarmanV5 example in
   [Transport examples](#5-transport-examples). Replace the address, logger
   serial, plant ID, and MQTT hostname.
2. Validate it with the container image:

   ```bash
   export GBB_PLANT_1_CLOUD_PLANT_TOKEN='replace-with-the-real-token'
   docker run --rm \
     -e GBB_PLANT_1_CLOUD_PLANT_TOKEN \
     --mount type=bind,src="$PWD/gbbconnect.yaml",dst=/config/gbbconnect.yaml,readonly \
     ghcr.io/krzysztofhajdamowicz/gbbconnect-go:latest \
     config validate --config /config/gbbconnect.yaml
   ```

3. Start the bridge:

   ```bash
   docker volume create gbbconnect-state
   docker run -d \
     --name gbbconnect \
     --restart unless-stopped \
     -e GBB_PLANT_1_CLOUD_PLANT_TOKEN \
     --mount type=bind,src="$PWD/gbbconnect.yaml",dst=/config/gbbconnect.yaml,readonly \
     --mount type=volume,src=gbbconnect-state,dst=/data \
     ghcr.io/krzysztofhajdamowicz/gbbconnect-go:latest
   docker logs -f gbbconnect
   ```

The process runs as UID/GID `65532`. A bind-mounted configuration must be
readable by that user; keeping the token in the environment allows the YAML
file itself to be non-secret. The named volume holds state and daily logs.

For `modbus_serial`, add the host device:

```bash
docker run ... --device /dev/ttyUSB0:/dev/ttyUSB0 ...
```

The container normally reaches LAN devices through Docker routing. UDP
broadcast discovery is a setup command and is best run with the native binary
on the host; it is not required during normal daemon operation.

## 3. Home Assistant app/add-on quick start

This repository is a Home Assistant add-on repository: the add-on package
lives at the repository root in [`gbbconnect_go/`](../gbbconnect_go/), so you
can install it directly from the add-on store without copying any files.

### Install from the repository (recommended)

Click the button to open your Home Assistant instance with the repository URL
pre-filled:

[![Open your Home Assistant instance and show the add add-on repository dialog with a specific repository URL pre-filled.](https://my.home-assistant.io/badges/supervisor_add_addon_repository.svg)](https://my.home-assistant.io/redirect/supervisor_add_addon_repository/?repository_url=https%3A%2F%2Fgithub.com%2FKrzysztofHajdamowicz%2Fgbbconnect-go)

Or add the repository manually:

1. In Home Assistant, open **Settings → Add-ons → Add-on Store**.
2. Open the store menu (**⋮** in the top-right corner) and select
   **Repositories**.
3. Paste `https://github.com/KrzysztofHajdamowicz/gbbconnect-go` and click
   **Add**, then close the dialog.
4. Refresh the store page (or select **Check for updates** from the same
   menu) and install **gbbconnect-go** from the new repository section.
5. In its **Configuration** tab, edit the default disabled plant:
   select the driver, enter its transport fields, then enter
   `cloud_plant_id`, `cloud_plant_token`, and `cloud_mqtt_address`.
6. Set `enabled: true`, save, start the add-on, and inspect its **Log** tab.

> **Migrating from a local install:** if you previously copied the add-on to
> `/addons/gbbconnect_go`, that local copy (`local_gbbconnect_go`) is a
> different add-on identity from the repository install. Note down your
> options, uninstall the local add-on, remove `/addons/gbbconnect_go`, and
> install from the repository; state under `/data` does not carry over.

### Install as a local add-on (fallback)

Without internet access to GitHub from the add-on store, install the package
as a local add-on instead:

1. Use the Home Assistant SSH or Samba app to access `/addons`, following the
   [official local app instructions](https://developers.home-assistant.io/docs/apps/tutorial/).
2. Copy the contents of `gbbconnect_go` into
   `/addons/gbbconnect_go` so that
   `/addons/gbbconnect_go/config.yaml` exists.
3. Open the Home Assistant app store (called the add-on store in older
   releases), open its menu, and select **Check for updates**.
4. Install **gbbconnect-go** from the local apps/add-ons section, then
   configure it as in steps 5–6 above.

The form uses flat field names because of Home Assistant nesting limits:

| Native YAML | Home Assistant option |
|---|---|
| `serial_port.device` | `serial_device` |
| `serial_port.baud` | `serial_baud` |
| `cloud.plant_id` | `cloud_plant_id` |
| `cloud.plant_token` | `cloud_plant_token` |
| `cloud.mqtt_address` | `cloud_mqtt_address` |

Sub-inverters are entered in the separate top-level `sub_inverters` list. Its
`plant_number` selects the parent plant. Home Assistant persists configuration,
state, and logs below `/data`; the add-on exposes available UART devices for
the serial driver.

See [the add-on documentation](../gbbconnect_go/DOCS.md) for every form field,
serial access, VLAN guidance, and shutdown behaviour.

## 4. systemd quick start

Download the release archive for the host architecture from
[GitHub Releases](https://github.com/KrzysztofHajdamowicz/gbbconnect-go/releases).
Verify it against the release's `SHA256SUMS` before extracting it. For example,
for x86-64 Linux:

```bash
VERSION=v0.1.0
curl -LO "https://github.com/KrzysztofHajdamowicz/gbbconnect-go/releases/download/${VERSION}/gbbconnect_${VERSION}_linux_amd64.tar.gz"
curl -LO "https://github.com/KrzysztofHajdamowicz/gbbconnect-go/releases/download/${VERSION}/SHA256SUMS"
grep "gbbconnect_${VERSION}_linux_amd64.tar.gz" SHA256SUMS | sha256sum --check
tar -xzf "gbbconnect_${VERSION}_linux_amd64.tar.gz"
```

Use the current release tag in place of `v0.1.0`. Then follow the complete
[systemd installation procedure](../deploy/systemd/README.md): create the
unprivileged account, install the binary and supplied unit, place the
configuration at `/etc/gbbconnect/gbbconnect.yaml`, validate it, and enable the
service.

Store the token in a root-owned environment file instead of YAML:

```bash
install -o root -g root -m 0600 /dev/null /etc/gbbconnect/environment
```

Add this line to `/etc/gbbconnect/environment`:

```text
GBB_PLANT_1_CLOUD_PLANT_TOKEN=replace-with-the-real-token
```

Add a systemd drop-in with `systemctl edit gbbconnect.service`:

```ini
[Service]
EnvironmentFile=/etc/gbbconnect/environment
```

After changing the unit or configuration:

```bash
set -a
. /etc/gbbconnect/environment
set +a
/usr/local/bin/gbbconnect config validate \
  --config /etc/gbbconnect/gbbconnect.yaml
systemctl daemon-reload
systemctl restart gbbconnect.service
journalctl -u gbbconnect.service -f
```

For `modbus_serial`, also grant the service access to the group owning the
serial device, as documented in the systemd installation procedure.

The Windows release binary can run either in the foreground or as a native
Windows Service. Follow the
[elevated PowerShell installation guide](../deploy/windows/README.md) for
installation, Event Viewer logs, upgrades, and removal.

## 5. Transport examples

Every example is a complete `gbbconnect.yaml`. Set the token through
`GBB_PLANT_1_CLOUD_PLANT_TOKEN` before validation or startup. `number` must be
unique when multiple plants are combined in one file.

### SolarmanV5

Use this for a Solarman/LSW logger. Both the IP address and logger serial are
required.

```yaml
version: 1
logging:
  level: info
plants:
  - number: 1
    name: "Home Solarman"
    enabled: true
    driver: solarman_v5
    address: "192.168.1.100"
    port: 8899
    serial: 1720000000
    cloud:
      plant_id: "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
      plant_token: ""
      mqtt_address: "gbboptimizer1-mqtt.gbbsoft.pl"
      mqtt_port: 8883
      use_tls: true
      tls_insecure_skip_verify: false
```

### Modbus TCP

Use this when the inverter or Ethernet gateway exposes standard Modbus TCP
framing.

```yaml
version: 1
logging:
  level: info
plants:
  - number: 1
    name: "Home Modbus TCP"
    enabled: true
    driver: modbus_tcp
    address: "192.168.1.110"
    port: 502
    cloud:
      plant_id: "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
      plant_token: ""
      mqtt_address: "gbboptimizer1-mqtt.gbbsoft.pl"
      mqtt_port: 8883
      use_tls: true
      tls_insecure_skip_verify: false
```

### Modbus RTU over TCP

Use this for a transparent RS485-to-Ethernet gateway that carries complete
Modbus RTU frames, including CRC, over a TCP socket. This is not the same
framing as Modbus TCP.

```yaml
version: 1
logging:
  level: info
plants:
  - number: 1
    name: "Home RTU gateway"
    enabled: true
    driver: modbus_rtu_tcp
    address: "192.168.1.120"
    port: 8899
    cloud:
      plant_id: "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
      plant_token: ""
      mqtt_address: "gbboptimizer1-mqtt.gbbsoft.pl"
      mqtt_port: 8883
      use_tls: true
      tls_insecure_skip_verify: false
```

### Modbus serial

Use this for a directly attached RS485 adapter. Match baud, parity, data bits,
and stop bits to the inverter manual.

```yaml
version: 1
logging:
  level: info
plants:
  - number: 1
    name: "Home serial"
    enabled: true
    driver: modbus_serial
    serial_port:
      device: "/dev/ttyUSB0"
      baud: 9600
      data_bits: 8
      parity: none
      stop_bits: 1
    cloud:
      plant_id: "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
      plant_token: ""
      mqtt_address: "gbboptimizer1-mqtt.gbbsoft.pl"
      mqtt_port: 8883
      use_tls: true
      tls_insecure_skip_verify: false
```

### Optional runtime, logging, and sub-inverters

These top-level settings may be added to any example:

```yaml
runtime:
  debug: false
  clear_old_logs: true
  gbb_environment: ""
logging:
  level: info
  driver_trace: false
  driver_trace_raw: false
  directory: ""
```

For a sub-inverter routed through another logger, add this below its parent
plant:

```yaml
    sub_inverters:
      - serial: 123456
        dongle_serial: 654321
        address: "192.168.1.105"
        port: 8899
```

`serial` is the value received in cloud `SubInverterSN`;
`dongle_serial` is the logger serial used for Solarman framing.

The full field model, defaults, environment overrides, and config search order
are in [Configuration & State](07-configuration.md). The canonical editor
schema is [`schema/gbbconnect.schema.json`](../schema/gbbconnect.schema.json).

## 6. Validate and run a native binary

Always validate after editing:

```bash
export GBB_PLANT_1_CLOUD_PLANT_TOKEN='replace-with-the-real-token'
gbbconnect config validate --config ./gbbconnect.yaml
```

Run in the foreground before installing a service:

```bash
gbbconnect --config ./gbbconnect.yaml \
  --state-dir ./gbbconnect-state run
```

The root command without `run` starts the same daemon. Useful global overrides
are:

```text
--config PATH
--state-dir PATH
--log-level error|warn|info|debug
--dev
```

Environment overrides take precedence over the file:

| Variable | Purpose |
|---|---|
| `GBB_CONFIG` | Configuration path |
| `GBB_STATE_DIR` | Persistent state directory |
| `GBB_RUNTIME_DEBUG` | Development timing and debug behaviour |
| `GBB_LOGGING_LEVEL` | `error`, `warn`, `info`, or `debug` |
| `GBB_LOGGING_DRIVER_TRACE` | Decoded Modbus trace |
| `GBB_LOGGING_DRIVER_TRACE_RAW` | Raw transport trace |
| `GBB_PLANT_<NUMBER>_CLOUD_PLANT_ID` | Per-plant cloud ID |
| `GBB_PLANT_<NUMBER>_CLOUD_PLANT_TOKEN` | Per-plant token |

Stop a foreground process with `Ctrl-C`. SIGINT, SIGTERM, Home Assistant stop,
and Windows Service stop all use the graceful path: new requests are rejected,
in-flight work gets up to 30 seconds, MQTT disconnects, and state is saved.

## 7. Discovering a dongle

Discovery is local and does not contact GbbOptimizer.

Broadcast on the automatically selected local interface:

```bash
gbbconnect discover --timeout 5s
```

Select an interface by its local IPv4 address on a multi-homed host:

```bash
gbbconnect discover --interface 192.168.1.20
```

If broadcast is blocked or the dongle is behind a routed VLAN, scan a CIDR:

```bash
gbbconnect discover \
  --broadcast=false \
  --subnet 192.168.2.0/24 \
  --port 8899 \
  --timeout 5s
```

Machine-readable output:

```bash
gbbconnect discover --subnet 192.168.2.0/24 --json
```

Copy the resulting IP and serial into the SolarmanV5 plant entry. A row with an
IP but no serial or protocol only means the configured TCP port responded; it
can be an unrelated service. UDP broadcast normally stays within one subnet,
and some logger firmware answers only a request originating on that subnet.

## 8. Migrating from GbbConnect2

Stop GbbConnect2 first so the old and new clients do not use the same plant
credentials concurrently. Keep a backup of `Parameters.xml`, then run:

```bash
gbbconnect import-xml \
  --in /path/to/Parameters.xml \
  --out ./gbbconnect.yaml
```

The importer prints warnings for legacy values it cannot map. Inspect the
result, move every `plant_token` to an environment variable if practical, and
validate:

```bash
export GBB_PLANT_1_CLOUD_PLANT_TOKEN='replace-with-the-real-token'
gbbconnect config validate --config ./gbbconnect.yaml
```

Start `gbbconnect` in the foreground, confirm the MQTT connection and read-only
plant data, then install it as a service. Do not test register writes until
reads match the official client and the inverter's safe writable registers are
known.

## 9. Troubleshooting

### Configuration does not validate

Run `gbbconnect config validate --config PATH`. It reports all known problems
in one pass. Common causes are a duplicate plant number, a missing token
environment variable, the wrong transport name, or missing serial parameters.
Confirm that the environment variable uses the YAML plant `number`, not the
list position.

### MQTT or TLS connection fails

- Verify DNS, outbound TCP access to the configured MQTT port, and system time.
- Recheck the plant ID, token, MQTT hostname, and port against the working
  GbbConnect2 configuration.
- The production image includes public CA certificates. A private/intercepting
  proxy requires its CA to be trusted by the host/image.
- `tls_insecure_skip_verify: true` disables server identity verification. Use
  it only briefly to isolate a certificate problem, never as the permanent fix.
- `use_tls: false` switches the cloud connection to plaintext MQTT. It is meant
  only for brokers without a TLS endpoint, not as a workaround for certificate
  problems: the plant token and all traffic are then sent unencrypted.

After a failed cloud cycle, production mode waits five minutes before retrying.
`--dev` or `runtime.debug: true` shortens that backoff to ten seconds for
controlled troubleshooting.

### Inverter is unreachable or returns framing errors

- Test the configured network endpoint from the same host, for example
  `nc -vz 192.168.1.100 8899`.
- Distinguish `modbus_tcp` from `modbus_rtu_tcp`; they use different wire
  framing even when the TCP port is identical.
- For SolarmanV5, verify both the IP and logger serial.
- For serial, check the stable device path (`/dev/serial/by-id/...` is
  preferable when available), line parameters, container device mapping, and
  service group permissions.
- Do not power-cycle an inverter repeatedly while diagnosing a network issue.

### Responses seem delayed

Cloud keepalives are sent once per minute. Worker reconnect backoff is five
minutes in production, while transport-level retries are shorter. The local
driver API also preserves protective operation spacing where required. Search
the log for the first error instead of repeatedly restarting the daemon.

### Finding useful logs

```bash
docker logs -f gbbconnect
journalctl -u gbbconnect.service -f
```

Home Assistant shows stdout in the add-on **Log** tab. Windows Service messages
are under the `gbbconnect` provider in the Windows Application Event Log.
Daily files used for cloud log streaming live under the configured
`logging.directory`, or under the state directory's `logs` subdirectory by
default.

Start with `--log-level debug` or `GBB_LOGGING_LEVEL=debug`. Enable
`logging.driver_trace` for the cloud MQTT payloads (`Received MQTT` and
`Send MQTT`, matching the portal's own Send/Received entries) together with the
decoded Modbus frames they carry, and `logging.driver_trace_raw` only when raw
framing is needed; turn both off after the investigation. The traced `Send
MQTT` payload reports the size of the streamed `LastLog` chunk instead of its
text, so the daily log is never written back into itself. Cloud values map as
follows:

| Cloud `LogLevel` | Effect |
|---|---|
| `OnlyErrors` | Error-only logging |
| `Min` | Informational logging, driver traces off |
| `Max` | Informational logging, decoded and raw driver traces on |

The GbbOptimizer portal sends its plant setting *"Remotly change log level on
GbbConnect2"* in every request, so a portal value of `OnlyErrors` silences
informational logs and driver traces regardless of the local configuration.
Each change is announced with a `logging level overridden by remote side`
message before it takes effect. A valid remote cloud level is persisted in
`runtime.json` under the state directory and re-applied on start. If logging
unexpectedly quiets down or returns after restart, inspect that state file, the
portal setting, the YAML, and the command-line override.

`runtime.debug: true` (or `--dev`) pins logging to the local configuration:
remote `LogLevel` values are acknowledged but ignored, and the persisted
override in `runtime.json` is not restored.

### State or log permission errors

The state directory must be writable by the runtime user. Docker should use the
named volume shown above; systemd uses `/var/lib/gbbconnect`; Home Assistant
uses `/data/state`. Do not make the entire host filesystem writable to solve a
single ownership problem.

For protocol details and compatibility evidence, see
[Compatibility & Testing](10-compatibility-and-testing.md).
