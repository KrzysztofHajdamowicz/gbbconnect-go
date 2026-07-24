// Package modbusrtutcp implements raw Modbus RTU over TCP.
package modbusrtutcp

import (
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/config"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/driver/transportstub"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/logbuf"
)

// Transport is the raw Modbus RTU-over-TCP transport implementation.
type Transport struct {
	*transportstub.Transport
}

// New constructs a raw Modbus RTU-over-TCP transport.
func New(plant config.Plant, logger logbuf.Logger) *Transport {
	return &Transport{
		Transport: transportstub.New(config.DriverModbusRTUTCP, plant, logger),
	}
}
