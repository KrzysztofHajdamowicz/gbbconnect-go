package config

import (
	"encoding/json"
	"fmt"
	"slices"

	"go.yaml.in/yaml/v3"
)

const (
	CurrentVersion  = 1
	DefaultPort     = 8899
	DefaultMQTTPort = 8883
	DefaultBaud     = 9600
	redactedSecret  = "[REDACTED]"
)

// DriverType identifies an inverter transport.
type DriverType string

const (
	DriverSolarmanV5   DriverType = "solarman_v5"
	DriverModbusTCP    DriverType = "modbus_tcp"
	DriverModbusRTUTCP DriverType = "modbus_rtu_tcp"
	DriverModbusSerial DriverType = "modbus_serial"
	DriverRandom       DriverType = "random"
)

// LogLevel is a configured application logging severity.
type LogLevel string

const (
	LogLevelError LogLevel = "error"
	LogLevelWarn  LogLevel = "warn"
	LogLevelInfo  LogLevel = "info"
	LogLevelDebug LogLevel = "debug"
)

// Parity is a serial-port parity mode.
type Parity string

const (
	ParityNone Parity = "none"
	ParityEven Parity = "even"
	ParityOdd  Parity = "odd"
)

// Config is the complete application configuration.
type Config struct {
	Version int     `yaml:"version" json:"version"`
	Runtime Runtime `yaml:"runtime" json:"runtime"`
	Logging Logging `yaml:"logging" json:"logging"`
	Plants  []Plant `yaml:"plants" json:"plants"`
}

// Runtime contains process-wide behaviour controls.
type Runtime struct {
	Debug          bool   `yaml:"debug" json:"debug"`
	ClearOldLogs   bool   `yaml:"clear_old_logs" json:"clear_old_logs"`
	GBBEnvironment string `yaml:"gbb_environment" json:"gbb_environment"`
}

// Logging contains process-wide logging controls.
type Logging struct {
	Level          LogLevel `yaml:"level" json:"level"`
	DriverTrace    bool     `yaml:"driver_trace" json:"driver_trace"`
	DriverTraceRaw bool     `yaml:"driver_trace_raw" json:"driver_trace_raw"`
	Directory      string   `yaml:"directory" json:"directory"`
}

// Plant describes one inverter site and its cloud connection.
type Plant struct {
	Number       int           `yaml:"number" json:"number"`
	Name         string        `yaml:"name" json:"name"`
	Enabled      bool          `yaml:"enabled" json:"enabled"`
	Driver       DriverType    `yaml:"driver" json:"driver"`
	Address      string        `yaml:"address" json:"address"`
	Port         int           `yaml:"port" json:"port"`
	Serial       int64         `yaml:"serial" json:"serial"`
	SerialPort   SerialPort    `yaml:"serial_port" json:"serial_port"`
	Cloud        Cloud         `yaml:"cloud" json:"cloud"`
	SubInverters []SubInverter `yaml:"sub_inverters" json:"sub_inverters"`
}

// Cloud contains the per-plant GbbOptimizer MQTT credentials and endpoint.
type Cloud struct {
	PlantID               string `yaml:"plant_id" json:"plant_id"`
	PlantToken            string `yaml:"plant_token" json:"plant_token"`
	MQTTAddress           string `yaml:"mqtt_address" json:"mqtt_address"`
	MQTTPort              int    `yaml:"mqtt_port" json:"mqtt_port"`
	UseTLS                bool   `yaml:"use_tls" json:"use_tls"`
	TLSInsecureSkipVerify bool   `yaml:"tls_insecure_skip_verify" json:"tls_insecure_skip_verify"`
}

// SerialPort contains line settings for the Modbus serial transport.
type SerialPort struct {
	Device   string `yaml:"device" json:"device"`
	Baud     int    `yaml:"baud" json:"baud"`
	DataBits int    `yaml:"data_bits" json:"data_bits"`
	Parity   Parity `yaml:"parity" json:"parity"`
	StopBits int    `yaml:"stop_bits" json:"stop_bits"`
}

// SubInverter describes an inverter routed through another logger.
type SubInverter struct {
	Serial       int64  `yaml:"serial" json:"serial"`
	DongleSerial int64  `yaml:"dongle_serial" json:"dongle_serial"`
	Address      string `yaml:"address" json:"address"`
	Port         int    `yaml:"port" json:"port"`
}

// Default returns a configuration populated with global defaults.
func Default() Config {
	return Config{
		Version: CurrentVersion,
		Runtime: Runtime{
			ClearOldLogs: true,
		},
		Logging: Logging{
			Level: LogLevelInfo,
		},
		Plants: []Plant{},
	}
}

// DefaultPlant returns a plant populated with connection defaults.
func DefaultPlant() Plant {
	return Plant{
		Enabled:      true,
		Port:         DefaultPort,
		SerialPort:   DefaultSerialPort(),
		Cloud:        DefaultCloud(),
		SubInverters: []SubInverter{},
	}
}

// DefaultCloud returns cloud endpoint defaults inherited from GbbConnect2.
func DefaultCloud() Cloud {
	return Cloud{
		MQTTAddress: "gbboptimizerX-mqtt.gbbsoft.pl",
		MQTTPort:    DefaultMQTTPort,
		UseTLS:      true,
	}
}

// DefaultSerialPort returns the conventional Modbus RTU line settings.
func DefaultSerialPort() SerialPort {
	return SerialPort{
		Baud:     DefaultBaud,
		DataBits: 8,
		Parity:   ParityNone,
		StopBits: 1,
	}
}

// DefaultSubInverter returns a sub-inverter populated with network defaults.
func DefaultSubInverter() SubInverter {
	return SubInverter{Port: DefaultPort}
}

// DriverTypeFromLegacy converts an original GbbConnect2 DriverNo.
func DriverTypeFromLegacy(number int) (DriverType, error) {
	switch number {
	case 0:
		return DriverSolarmanV5, nil
	case 1:
		return DriverModbusTCP, nil
	case 999:
		return DriverRandom, nil
	default:
		return "", fmt.Errorf("unknown legacy driver number: %d", number)
	}
}

// LegacyNumber converts a driver type supported by GbbConnect2 to DriverNo.
func (driver DriverType) LegacyNumber() (int, error) {
	switch driver {
	case DriverSolarmanV5:
		return 0, nil
	case DriverModbusTCP:
		return 1, nil
	case DriverRandom:
		return 999, nil
	case DriverModbusRTUTCP, DriverModbusSerial:
		return 0, fmt.Errorf("driver %q has no legacy driver number", driver)
	default:
		return 0, fmt.Errorf("unknown driver %q", driver)
	}
}

// DriverTypes returns every supported configuration value.
func DriverTypes() []DriverType {
	return []DriverType{
		DriverSolarmanV5,
		DriverModbusTCP,
		DriverModbusRTUTCP,
		DriverModbusSerial,
		DriverRandom,
	}
}

// LogLevels returns every supported logging level.
func LogLevels() []LogLevel {
	return []LogLevel{LogLevelError, LogLevelWarn, LogLevelInfo, LogLevelDebug}
}

// Parities returns every supported serial parity.
func Parities() []Parity {
	return []Parity{ParityNone, ParityEven, ParityOdd}
}

// Redacted returns an independent copy safe for diagnostic output.
func (config Config) Redacted() Config {
	redacted := config
	redacted.Plants = slices.Clone(config.Plants)
	for index := range redacted.Plants {
		redacted.Plants[index].SubInverters = slices.Clone(config.Plants[index].SubInverters)
		if redacted.Plants[index].Cloud.PlantToken != "" {
			redacted.Plants[index].Cloud.PlantToken = redactedSecret
		}
	}
	return redacted
}

// String returns a redacted YAML representation.
func (config Config) String() string {
	data, err := yaml.Marshal(config.Redacted())
	if err != nil {
		return "<invalid config>"
	}
	return string(data)
}

// UnmarshalJSON applies defaults before decoding JSON.
func (config *Config) UnmarshalJSON(data []byte) error {
	type plain Config
	decoded := plain(Default())
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*config = Config(decoded)
	return nil
}

// UnmarshalYAML applies defaults before decoding YAML.
func (config *Config) UnmarshalYAML(node *yaml.Node) error {
	type plain Config
	decoded := plain(Default())
	if err := node.Decode(&decoded); err != nil {
		return err
	}
	*config = Config(decoded)
	return nil
}

// UnmarshalJSON applies plant defaults before decoding JSON.
func (plant *Plant) UnmarshalJSON(data []byte) error {
	type plain Plant
	decoded := plain(DefaultPlant())
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*plant = Plant(decoded)
	return nil
}

// UnmarshalYAML applies plant defaults before decoding YAML.
func (plant *Plant) UnmarshalYAML(node *yaml.Node) error {
	type plain Plant
	decoded := plain(DefaultPlant())
	if err := node.Decode(&decoded); err != nil {
		return err
	}
	*plant = Plant(decoded)
	return nil
}

// UnmarshalJSON applies sub-inverter defaults before decoding JSON.
func (subInverter *SubInverter) UnmarshalJSON(data []byte) error {
	type plain SubInverter
	decoded := plain(DefaultSubInverter())
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*subInverter = SubInverter(decoded)
	return nil
}

// UnmarshalYAML applies sub-inverter defaults before decoding YAML.
func (subInverter *SubInverter) UnmarshalYAML(node *yaml.Node) error {
	type plain SubInverter
	decoded := plain(DefaultSubInverter())
	if err := node.Decode(&decoded); err != nil {
		return err
	}
	*subInverter = SubInverter(decoded)
	return nil
}
