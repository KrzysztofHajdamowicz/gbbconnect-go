# Changelog

## 0.1.3

- Add English and Polish translations for the configuration form: every
  option now has a readable label and a description that explains what to
  enter and which connection methods (`driver` values) actually use it —
  including the often-confused `serial` (Solarman dongle serial number, not
  the inverter's) and `serial_device` (RS485 serial port path, e.g.
  `/dev/ttyUSB0`).
- Rewrite the "Driver fields" section of the documentation to match the
  form's flat field names (`serial_device` instead of the native
  `serial_port.device`) and to state that fields unused by the selected
  connection method are ignored.

## 0.1.2

- The add-on package moved from `deploy/addon/` to `gbbconnect_go/` at the
  repository root, so this repository can be added directly as a Home
  Assistant add-on repository. If you previously installed the add-on as a
  local copy under `/addons/gbbconnect_go`, that install is a separate add-on
  identity: note down your options, uninstall the local add-on, and install
  from the repository (state under `/data` does not carry over). Existing
  users of the repository URL may need **Check for updates** before the
  add-on appears.

## 0.1.1

- Fix start-up failure for serial-port plants: Supervisor stored
  `serial_stop_bits` as a string (`list(1|2)` schema), which the rendered
  configuration rejected. The option now uses an `int(1,2)` schema and the
  renderer coerces legacy string values back to numbers.

## 0.1.0

- Initial Home Assistant add-on package.
- Native options mapping for all gbbconnect-go drivers.
- Persistent state/log directories and serial-device support.
