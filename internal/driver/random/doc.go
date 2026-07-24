// Package random implements the debug-only random inverter transport.
package random

import (
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/config"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/driver/transportstub"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/logbuf"
)

// Transport is the debug-only random transport implementation.
type Transport struct {
	*transportstub.Transport
}

// New constructs a debug-only random transport.
func New(plant config.Plant, logger logbuf.Logger) *Transport {
	return &Transport{
		Transport: transportstub.New(config.DriverRandom, plant, logger),
	}
}
