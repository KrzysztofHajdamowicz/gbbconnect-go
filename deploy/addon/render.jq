. as $root
| {
    version: .version,
    runtime: .runtime,
    logging: .logging,
    plants: [
      .plants[]
      | . as $plant
      | {
          number: .number,
          name: .name,
          enabled: .enabled,
          driver: .driver,
          address: .address,
          port: .port,
          serial: .serial,
          serial_port: {
            device: .serial_device,
            baud: .serial_baud,
            data_bits: .serial_data_bits,
            parity: .serial_parity,
            stop_bits: .serial_stop_bits
          },
          cloud: {
            plant_id: .cloud_plant_id,
            plant_token: .cloud_plant_token,
            mqtt_address: .cloud_mqtt_address,
            mqtt_port: .cloud_mqtt_port,
            tls_insecure_skip_verify: .cloud_tls_insecure_skip_verify
          },
          sub_inverters: [
            $root.sub_inverters[]
            | select(.plant_number == $plant.number)
            | {
                serial: .serial,
                dongle_serial: .dongle_serial,
                address: .address,
                port: .port
              }
          ]
        }
    ]
  }
