package driver

import (
	"fmt"

	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/config"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/driver/modbusrtutcp"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/driver/modbusserial"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/driver/modbustcp"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/driver/random"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/driver/solarmanv5"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/logbuf"
)

// New creates a Driver facade with the transport selected by plant.Driver.
// Concrete transports establish their connection lazily on the first request.
func New(plant config.Plant, logger logbuf.Logger) (Driver, error) {
	var transport Transport

	switch plant.Driver {
	case config.DriverSolarmanV5:
		transport = solarmanv5.New(plant, logger)
	case config.DriverModbusTCP:
		transport = modbustcp.New(plant, logger)
	case config.DriverModbusRTUTCP:
		transport = modbusrtutcp.New(plant, logger)
	case config.DriverModbusSerial:
		transport = modbusserial.New(plant, logger)
	case config.DriverRandom:
		transport = random.New(plant, logger)
	default:
		//nolint:staticcheck // The compatibility contract requires this exact text.
		return nil, fmt.Errorf("Unknown driver: %s", plant.Driver)
	}

	return Wrap(transport), nil
}
