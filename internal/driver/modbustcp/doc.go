// Package modbustcp implements the Modbus TCP transport.
package modbustcp

import (
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/config"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/driver/transportstub"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/logbuf"
)

// Transport is the Modbus TCP transport implementation.
type Transport struct {
	*transportstub.Transport
}

// New constructs a Modbus TCP transport.
func New(plant config.Plant, logger logbuf.Logger) *Transport {
	return &Transport{
		Transport: transportstub.New(config.DriverModbusTCP, plant, logger),
	}
}
