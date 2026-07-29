# Changelog

## 0.1.8

- Add the `cloud_use_tls` option (enabled by default) to allow plaintext MQTT
  connections to brokers without a TLS endpoint, matching the `UseTls` flag of
  the original GbbConnect2. Disabling it sends the plant token and all cloud
  traffic unencrypted, so keep it on unless your broker offers no TLS.

## 0.1.7

- Re-release of the 0.1.6 content, which Home Assistant installations did not
  pick up. Nothing changed in the add-on itself: see the 0.1.6 entry for the
  logging fixes this delivers.

## 0.1.6

- Fix the add-on log going quiet for no visible reason. The GbbOptimizer plant
  setting *"Remotly change log level on GbbConnect2"* is sent with every cloud
  request, so a portal value of `Only errors` silenced the log and the driver
  traces without leaving a trace of its own — the add-on looked hung while it
  was in fact polling the inverter normally. Every remote change is now
  reported as `logging level overridden by remote side` before it takes
  effect, including the value restored from `/data/state/runtime.json` on
  start.
- `debug` now pins logging to the add-on options: remote log levels are
  reported but ignored, and the persisted override is not restored, so a
  debugging session can no longer be silenced from the portal.
- `driver_trace` now also logs the cloud MQTT payloads as `Received MQTT` and
  `Send MQTT`, mirroring the Send/Received entries in the portal's own log
  view, so a request can be followed end to end with one option. The traced
  outgoing payload reports the size of the streamed log chunk instead of its
  text, which keeps the daily log from being written back into itself.

## 0.1.5

- Fix configuration form translations not being applied: fields nested in
  sections and lists (`plants`, `sub_inverters`, `runtime`, `logging`) must
  live under their parent's `fields:` key in the translation files — the
  0.1.3 files used flat keys, which the Home Assistant frontend never looks
  up for nested fields. As a bonus, `serial` now has distinct descriptions
  for plants (Solarman dongle serial) and sub-inverters (inverter serial).

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
