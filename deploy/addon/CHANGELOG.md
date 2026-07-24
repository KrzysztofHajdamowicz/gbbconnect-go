# Changelog

## 0.1.1

- Fix start-up failure for serial-port plants: Supervisor stored
  `serial_stop_bits` as a string (`list(1|2)` schema), which the rendered
  configuration rejected. The option now uses an `int(1,2)` schema and the
  renderer coerces legacy string values back to numbers.

## 0.1.0

- Initial Home Assistant add-on package.
- Native options mapping for all gbbconnect-go drivers.
- Persistent state/log directories and serial-device support.
