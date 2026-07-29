# gbbconnect-go add-on

This add-on runs the unofficial `gbbconnect-go` GbbOptimizer-to-Modbus bridge.
The Home Assistant form keeps plant fields flat to stay within the Supervisor
UI nesting limit. At startup, the entrypoint renders and validates the native
configuration in private, container-only `/tmp`.

## First setup

1. Open the add-on **Configuration** tab.
2. Edit the default disabled plant.
3. Select the driver and fill the fields required for it.
4. Enter the GbbOptimizer plant ID, token, and MQTT broker hostname.
5. Set `enabled: true`, save, and start the add-on.
6. Check the **Log** tab for the startup banner and MQTT connection.

The default plant is disabled so a fresh installation starts safely before
credentials and an inverter transport are configured.

## Connection methods and their fields

The `driver` field selects how the inverter is reached. The form shows every
field regardless of the selected method (the Home Assistant UI cannot hide
fields conditionally); fields not used by the selected method are ignored —
leave them at their defaults.

- `solarman_v5` — through a Solarman Wi-Fi dongle: set `address` (the
  dongle's IP), `port` (normally `8899`), and `serial` (the serial number
  printed on the dongle, e.g. `17xxxxxxx` — **not** the inverter's serial;
  `gbbconnect discover` can find both the IP and the serial).
- `modbus_tcp` — Modbus TCP: set `address` and `port` (usually `502`).
- `modbus_rtu_tcp` — Modbus RTU over TCP, for transparent RS485-to-Ethernet
  gateways: set `address` and `port`.
- `modbus_serial` — Modbus over a serial port (requires an RS485 adapter):
  set `serial_device` (for example `/dev/ttyUSB0` or a stable
  `/dev/serial/by-id/...` path) and the `serial_baud`, `serial_data_bits`,
  `serial_parity`, and `serial_stop_bits` line parameters. The add-on
  declares `uart: true`, so Home Assistant maps available serial devices
  into the container.
- `random` — diagnostics only; it returns no data. Do not use it for a
  production plant.

Each enabled plant also requires `cloud.plant_id`, `cloud.plant_token`, and
`cloud.mqtt_address`; these appear in the add-on form as `cloud_plant_id`,
`cloud_plant_token`, and `cloud_mqtt_address`. Keep TLS verification enabled
except for controlled troubleshooting, and keep `cloud_use_tls` on unless
your broker has no TLS endpoint — disabling it sends the plant token
unencrypted.

Sub-inverters are configured in the separate `sub_inverters` list. Set
`plant_number` to the number of the parent plant; the add-on groups matching
entries into that plant's native `sub_inverters` configuration.

## State and logs

Home Assistant persists `/data` across add-on restarts and backups:

- runtime and per-plant state: `/data/state`;
- daily logs used by cloud log streaming: `/data/logs`;
- UI-managed configuration: `/data/options.json`.

## Discovery and VLANs

The daemon itself only needs outbound MQTT and access to inverter addresses, so
the shipped manifest keeps `host_network: false`. UDP broadcast discovery does
not cross VLANs; use `gbbconnect discover --subnet <CIDR>` from a suitable host
for routed discovery. If you intentionally modify a local add-on installation
to run broadcast discovery inside it, set `host_network: true` in the manifest
and review the wider network exposure first.

## Shutdown

Home Assistant stops the add-on with SIGTERM. The process stops accepting new
requests, allows in-flight work up to 30 seconds to finish, disconnects MQTT,
and saves plant state. The manifest timeout is 40 seconds to accommodate that
grace period.
