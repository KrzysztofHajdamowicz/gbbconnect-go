package config

import (
	"errors"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestValidateDocumentedFullConfig(t *testing.T) {
	t.Parallel()

	var config Config
	if err := yaml.Unmarshal([]byte(fullConfigYAML), &config); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}
	if err := Validate(config); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidationRules(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*Config)
		want   string
	}{
		{
			name: "newer version",
			mutate: func(config *Config) {
				config.Version = CurrentVersion + 1
			},
			want: "newer than supported",
		},
		{
			name: "duplicate plant number",
			mutate: func(config *Config) {
				config.Plants = append(config.Plants, config.Plants[0])
			},
			want: "duplicates plants[0].number",
		},
		{
			name: "empty name",
			mutate: func(config *Config) {
				config.Plants[0].Name = " "
			},
			want: "name must not be empty",
		},
		{
			name: "unknown driver",
			mutate: func(config *Config) {
				config.Plants[0].Driver = "unknown"
			},
			want: `driver "unknown" is not supported`,
		},
		{
			name: "missing Solarman address",
			mutate: func(config *Config) {
				config.Plants[0].Address = ""
			},
			want: "address is required for solarman_v5",
		},
		{
			name: "missing Solarman serial",
			mutate: func(config *Config) {
				config.Plants[0].Serial = 0
			},
			want: "serial is required for solarman_v5",
		},
		{
			name: "missing Modbus TCP address",
			mutate: func(config *Config) {
				config.Plants[0].Driver = DriverModbusTCP
				config.Plants[0].Address = ""
			},
			want: "address is required for modbus_tcp",
		},
		{
			name: "missing serial device",
			mutate: func(config *Config) {
				config.Plants[0].Driver = DriverModbusSerial
				config.Plants[0].SerialPort.Device = ""
			},
			want: "serial_port.device is required",
		},
		{
			name: "missing cloud id",
			mutate: func(config *Config) {
				config.Plants[0].Cloud.PlantID = ""
			},
			want: "cloud.plant_id is required",
		},
		{
			name: "missing cloud token",
			mutate: func(config *Config) {
				config.Plants[0].Cloud.PlantToken = ""
			},
			want: "cloud.plant_token is required",
		},
		{
			name: "missing sub-inverter serials",
			mutate: func(config *Config) {
				config.Plants[0].SubInverters[0].Serial = 0
				config.Plants[0].SubInverters[0].DongleSerial = 0
			},
			want: "sub_inverters[0].serial is required",
		},
		{
			name: "unknown log level",
			mutate: func(config *Config) {
				config.Logging.Level = "trace"
			},
			want: `logging.level "trace" is not supported`,
		},
		{
			name: "unknown parity",
			mutate: func(config *Config) {
				config.Plants[0].Driver = DriverModbusSerial
				config.Plants[0].SerialPort.Parity = "mark"
			},
			want: `serial_port.parity "mark" is not supported`,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			config := validConfig(t)
			test.mutate(&config)
			err := Validate(config)
			if err == nil {
				t.Fatal("Validate() error = nil")
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Validate() error = %q, want substring %q", err, test.want)
			}
		})
	}
}

func TestValidationAggregatesProblemsWithoutSecrets(t *testing.T) {
	t.Parallel()

	config := validConfig(t)
	const token = "never-print-me"
	config.Plants[0].Cloud.PlantToken = token
	config.Version = 99
	config.Logging.Level = "trace"
	config.Plants[0].Name = ""
	config.Plants[0].Driver = "invalid"

	err := Validate(config)
	if err == nil {
		t.Fatal("Validate() error = nil")
	}

	var validation *ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("error type = %T, want *ValidationError", err)
	}
	if len(validation.Problems) < 4 {
		t.Fatalf("len(Problems) = %d, want at least 4: %v", len(validation.Problems), err)
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("validation error contains token: %v", err)
	}
}

func TestDisabledPlantDoesNotRequireCloudCredentials(t *testing.T) {
	t.Parallel()

	config := validConfig(t)
	config.Plants[0].Enabled = false
	config.Plants[0].Cloud.PlantID = ""
	config.Plants[0].Cloud.PlantToken = ""

	if err := Validate(config); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func validConfig(t *testing.T) Config {
	t.Helper()

	var config Config
	if err := yaml.Unmarshal([]byte(fullConfigYAML), &config); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}
	return config
}
