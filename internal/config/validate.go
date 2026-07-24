package config

import (
	"fmt"
	"strings"
)

// ValidationError contains every configuration problem found in one pass.
type ValidationError struct {
	Problems []string
}

func (err *ValidationError) Error() string {
	var message strings.Builder
	message.WriteString("invalid configuration:")
	for _, problem := range err.Problems {
		message.WriteString("\n- ")
		message.WriteString(problem)
	}
	return message.String()
}

// Validate checks a configuration without modifying it.
func Validate(config Config) error {
	var problems []string

	switch {
	case config.Version > CurrentVersion:
		problems = append(problems, fmt.Sprintf(
			"version %d is newer than supported version %d",
			config.Version,
			CurrentVersion,
		))
	case config.Version != CurrentVersion:
		problems = append(problems, fmt.Sprintf(
			"version %d is not supported; expected %d",
			config.Version,
			CurrentVersion,
		))
	}

	if !contains(LogLevels(), config.Logging.Level) {
		problems = append(problems, fmt.Sprintf(
			"logging.level %q is not supported",
			config.Logging.Level,
		))
	}

	plantNumbers := make(map[int]int, len(config.Plants))
	for index, plant := range config.Plants {
		path := fmt.Sprintf("plants[%d]", index)

		if previous, exists := plantNumbers[plant.Number]; exists {
			problems = append(problems, fmt.Sprintf(
				"%s.number %d duplicates plants[%d].number",
				path,
				plant.Number,
				previous,
			))
		} else {
			plantNumbers[plant.Number] = index
		}

		if strings.TrimSpace(plant.Name) == "" {
			problems = append(problems, path+".name must not be empty")
		}
		if !contains(DriverTypes(), plant.Driver) {
			problems = append(problems, fmt.Sprintf(
				"%s.driver %q is not supported",
				path,
				plant.Driver,
			))
		} else {
			problems = append(problems, validateTransport(path, plant)...)
		}

		if plant.Enabled {
			if strings.TrimSpace(plant.Cloud.PlantID) == "" {
				problems = append(problems, path+".cloud.plant_id is required for an enabled plant")
			}
			if strings.TrimSpace(plant.Cloud.PlantToken) == "" {
				problems = append(problems, path+".cloud.plant_token is required for an enabled plant")
			}
		}
		if plant.Cloud.MQTTPort < 1 || plant.Cloud.MQTTPort > 65535 {
			problems = append(problems, fmt.Sprintf(
				"%s.cloud.mqtt_port must be between 1 and 65535",
				path,
			))
		}

		for subIndex, subInverter := range plant.SubInverters {
			subPath := fmt.Sprintf("%s.sub_inverters[%d]", path, subIndex)
			if subInverter.Serial == 0 {
				problems = append(problems, subPath+".serial is required")
			}
			if subInverter.DongleSerial == 0 {
				problems = append(problems, subPath+".dongle_serial is required")
			}
			if subInverter.Port < 1 || subInverter.Port > 65535 {
				problems = append(problems, subPath+".port must be between 1 and 65535")
			}
		}
	}

	if len(problems) > 0 {
		return &ValidationError{Problems: problems}
	}
	return nil
}

func validateTransport(path string, plant Plant) []string {
	var problems []string

	switch plant.Driver {
	case DriverSolarmanV5:
		if strings.TrimSpace(plant.Address) == "" {
			problems = append(problems, path+".address is required for solarman_v5")
		}
		if plant.Serial == 0 {
			problems = append(problems, path+".serial is required for solarman_v5")
		}
		problems = append(problems, validateNetworkPort(path+".port", plant.Port)...)
	case DriverModbusTCP, DriverModbusRTUTCP:
		if strings.TrimSpace(plant.Address) == "" {
			problems = append(problems, path+".address is required for "+string(plant.Driver))
		}
		problems = append(problems, validateNetworkPort(path+".port", plant.Port)...)
	case DriverModbusSerial:
		if strings.TrimSpace(plant.SerialPort.Device) == "" {
			problems = append(problems, path+".serial_port.device is required for modbus_serial")
		}
		if plant.SerialPort.Baud < 1 {
			problems = append(problems, path+".serial_port.baud must be positive")
		}
		if plant.SerialPort.DataBits < 5 || plant.SerialPort.DataBits > 8 {
			problems = append(problems, path+".serial_port.data_bits must be between 5 and 8")
		}
		if !contains(Parities(), plant.SerialPort.Parity) {
			problems = append(problems, fmt.Sprintf(
				"%s.serial_port.parity %q is not supported",
				path,
				plant.SerialPort.Parity,
			))
		}
		if plant.SerialPort.StopBits != 1 && plant.SerialPort.StopBits != 2 {
			problems = append(problems, path+".serial_port.stop_bits must be 1 or 2")
		}
	case DriverRandom:
	}

	return problems
}

func validateNetworkPort(path string, port int) []string {
	if port < 1 || port > 65535 {
		return []string{path + " must be between 1 and 65535"}
	}
	return nil
}

func contains[T comparable](values []T, target T) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

var _ error = (*ValidationError)(nil)
