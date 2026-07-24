// Package modbusserial implements Modbus RTU over a serial port.
package modbusserial

import (
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/config"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/driver/transportstub"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/logbuf"
)

// Transport is the serial Modbus RTU transport implementation.
type Transport struct {
	*transportstub.Transport
}

// New constructs a serial Modbus RTU transport.
func New(plant config.Plant, logger logbuf.Logger) *Transport {
	return &Transport{
		Transport: transportstub.New(config.DriverModbusSerial, plant, logger),
	}
}
