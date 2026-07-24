# 11 - Glossary

| Term | Meaning |
|------|---------|
| **GbbOptimizer** | The cloud service (gbbsoft) that this bridge talks to over MQTT/TLS. |
| **GbbConnect2** | The official .NET application this project reimplements. |
| **gbbconnect-go** | This unofficial Go reimplementation. |
| **Plant** | A configured inverter site: one cloud connection + one inverter/dongle target (plus optional sub-inverters). Original config element `Plant`. |
| **Dongle / Logger** | The communication stick attached to the inverter. WiFi dongles (LSW3/Solarman) speak SolarmanV5; wired/Ethernet dongles speak Modbus TCP. |
| **Logger serial** | The dongle's serial number (e.g. `17xxxxxxx`, `21xxxxxxx`, `40xxxxxxx`). Used in SolarmanV5 framing. NOT the inverter's serial. |
| **Inverter serial** | The inverter's own serial; used for sub-inverter routing (`SubInverterSN`). |
| **Sub-inverter** | An additional inverter reachable behind a plant, addressed by its serial; has its own address/port/dongle serial. Original `SubInverter`. |
| **SolarmanV5** | Proprietary framing that wraps Modbus RTU for WiFi dongles. TCP port 8899. Original `DriverNo=0`. |
| **Modbus RTU** | The base inverter protocol: `unit | function | data | CRC16`. |
| **Modbus TCP** | Modbus over TCP using an MBAP header; inner CRC is stripped. Original `DriverNo=1`. Used by wired dongles. |
| **Modbus RTU over TCP** | Raw RTU frames (with CRC) sent verbatim over a TCP socket; used by transparent gateways (e.g. Waveshare). New transport. |
| **Modbus serial** | RTU over a physical serial/RS485 line. New transport. |
| **MBAP** | Modbus Application Protocol header: transaction id, protocol id, length (the Modbus TCP wrapper). |
| **CRC-16 (Modbus)** | Cyclic check using polynomial 0xA001; stored little-endian (lo, hi). |
| **Transaction id** | 16-bit correlation id in Modbus TCP requests/responses. |
| **Sequence number** | 1-byte (on the wire) correlation value in SolarmanV5 frames. |
| **Keepalive** | Empty MQTT message published every 60 s so the cloud sees the device online. |
| **toDevice / fromDevice** | MQTT subtopics under `{PlantId}/ModbusInMqtt/` for requests (in) and responses (out). |
| **Header / Line** | The JSON application protocol objects (a request/response batch and its individual Modbus commands). |
| **OrderId / Tag** | Echo-back tracking identifiers in the JSON protocol. |
| **LogLevel** | Remote verbosity control (`OnlyErrors` / `Min` / `Max`). |
| **SendLastLog / LastLog** | Optional incremental log-streaming request/response fields. |
| **PlantState** | Per-plant persisted runtime state (log streaming position). |
| **Transport** | Medium-specific raw RTU send/receive (this project's abstraction). |
| **Driver** | Transport + Modbus helpers + timing (this project's abstraction). |
| **Transaction executor** | Per-plant serializer that enforces one in-flight Modbus transaction and inter-command timing. |
| **HA Add-on** | Home Assistant Add-on packaging (Docker image + options schema). |
