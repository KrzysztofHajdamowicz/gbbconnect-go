package config

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

const fullConfigYAML = `version: 1
runtime:
  debug: false
  clear_old_logs: true
  gbb_environment: ""
logging:
  level: info
  driver_trace: false
  driver_trace_raw: false
  directory: ""
plants:
  - number: 1
    name: "My Main Plant"
    enabled: true
    driver: solarman_v5
    address: "192.168.1.100"
    port: 8899
    serial: 1720000000
    serial_port:
      device: "/dev/ttyUSB0"
      baud: 9600
      data_bits: 8
      parity: none
      stop_bits: 1
    cloud:
      plant_id: "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
      plant_token: "your-token-here"
      mqtt_address: "gbboptimizer1-mqtt.gbbsoft.pl"
      mqtt_port: 8883
      use_tls: true
      tls_insecure_skip_verify: false
    sub_inverters:
      - serial: 123
        dongle_serial: 321
        address: "192.168.1.105"
        port: 8899
`

const minimalConfigYAML = `version: 1
plants:
  - number: 1
    name: "Home"
    driver: solarman_v5
    address: "192.168.1.100"
    serial: 1720000000
    cloud:
      plant_id: "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
      plant_token: "your-token-here"
      mqtt_address: "gbboptimizer1-mqtt.gbbsoft.pl"
`

func TestYAMLRoundTrip(t *testing.T) {
	t.Parallel()

	var original Config
	if err := yaml.Unmarshal([]byte(fullConfigYAML), &original); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}
	assertFullConfig(t, original)

	encoded, err := yaml.Marshal(original)
	if err != nil {
		t.Fatalf("yaml.Marshal() error = %v", err)
	}

	var decoded Config
	if err := yaml.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("round-trip yaml.Unmarshal() error = %v", err)
	}
	if !reflect.DeepEqual(decoded, original) {
		t.Fatalf("round-trip mismatch:\n got: %#v\nwant: %#v", decoded.Redacted(), original.Redacted())
	}
}

func TestJSONRoundTrip(t *testing.T) {
	t.Parallel()

	var fromYAML Config
	if err := yaml.Unmarshal([]byte(fullConfigYAML), &fromYAML); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}

	encoded, err := json.Marshal(fromYAML)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var decoded Config
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if !reflect.DeepEqual(decoded, fromYAML) {
		t.Fatalf("round-trip mismatch:\n got: %#v\nwant: %#v", decoded.Redacted(), fromYAML.Redacted())
	}

	var fields map[string]any
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatalf("json.Unmarshal(fields) error = %v", err)
	}
	for _, field := range []string{"version", "runtime", "logging", "plants"} {
		if _, ok := fields[field]; !ok {
			t.Errorf("JSON is missing documented field %q", field)
		}
	}
}

func TestDefaultsForOmittedFields(t *testing.T) {
	t.Parallel()

	var config Config
	if err := yaml.Unmarshal([]byte(minimalConfigYAML), &config); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}

	if config.Version != CurrentVersion {
		t.Errorf("Version = %d, want %d", config.Version, CurrentVersion)
	}
	if !config.Runtime.ClearOldLogs {
		t.Error("Runtime.ClearOldLogs = false, want true")
	}
	if config.Logging.Level != LogLevelInfo {
		t.Errorf("Logging.Level = %q, want %q", config.Logging.Level, LogLevelInfo)
	}
	if len(config.Plants) != 1 {
		t.Fatalf("len(Plants) = %d, want 1", len(config.Plants))
	}

	plant := config.Plants[0]
	if !plant.Enabled {
		t.Error("Plant.Enabled = false, want true")
	}
	if plant.Port != DefaultPort {
		t.Errorf("Plant.Port = %d, want %d", plant.Port, DefaultPort)
	}
	if plant.Cloud.MQTTPort != DefaultMQTTPort {
		t.Errorf("Cloud.MQTTPort = %d, want %d", plant.Cloud.MQTTPort, DefaultMQTTPort)
	}
	if !plant.Cloud.UseTLS {
		t.Error("Cloud.UseTLS = false, want default true")
	}
	if plant.SerialPort.Baud != DefaultBaud ||
		plant.SerialPort.DataBits != 8 ||
		plant.SerialPort.Parity != ParityNone ||
		plant.SerialPort.StopBits != 1 {
		t.Errorf("SerialPort defaults = %#v", plant.SerialPort)
	}
}

func TestExplicitFalseOverridesDefaults(t *testing.T) {
	t.Parallel()

	const input = `runtime:
  clear_old_logs: false
plants:
  - enabled: false
`

	var config Config
	if err := yaml.Unmarshal([]byte(input), &config); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}
	if config.Runtime.ClearOldLogs {
		t.Error("Runtime.ClearOldLogs = true, want explicit false")
	}
	if config.Plants[0].Enabled {
		t.Error("Plant.Enabled = true, want explicit false")
	}

	const plaintextInput = `plants:
  - cloud:
      use_tls: false
`
	var plaintext Config
	if err := yaml.Unmarshal([]byte(plaintextInput), &plaintext); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}
	if plaintext.Plants[0].Cloud.UseTLS {
		t.Error("Cloud.UseTLS = true, want explicit false")
	}
}

func TestJSONDefaults(t *testing.T) {
	t.Parallel()

	const input = `{"plants":[{"name":"Home","cloud":{}}]}`
	var config Config
	if err := json.Unmarshal([]byte(input), &config); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	plant := config.Plants[0]
	if !config.Runtime.ClearOldLogs || config.Logging.Level != LogLevelInfo {
		t.Fatalf("global defaults were not applied: %#v", config.Redacted())
	}
	if !plant.Enabled || plant.Port != DefaultPort || plant.Cloud.MQTTPort != DefaultMQTTPort || !plant.Cloud.UseTLS {
		t.Fatalf("plant defaults were not applied: %#v", config.Redacted().Plants[0])
	}
}

func TestLegacyDriverMapping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		number int
		driver DriverType
	}{
		{number: 0, driver: DriverSolarmanV5},
		{number: 1, driver: DriverModbusTCP},
		{number: 999, driver: DriverRandom},
	}

	for _, test := range tests {
		driver, err := DriverTypeFromLegacy(test.number)
		if err != nil {
			t.Fatalf("DriverTypeFromLegacy(%d) error = %v", test.number, err)
		}
		if driver != test.driver {
			t.Errorf("DriverTypeFromLegacy(%d) = %q, want %q", test.number, driver, test.driver)
		}

		number, err := driver.LegacyNumber()
		if err != nil {
			t.Fatalf("%q.LegacyNumber() error = %v", driver, err)
		}
		if number != test.number {
			t.Errorf("%q.LegacyNumber() = %d, want %d", driver, number, test.number)
		}
	}

	if _, err := DriverTypeFromLegacy(42); err == nil {
		t.Fatal("DriverTypeFromLegacy(42) error = nil")
	}
	for _, driver := range []DriverType{DriverModbusRTUTCP, DriverModbusSerial, "unknown"} {
		if _, err := driver.LegacyNumber(); err == nil {
			t.Errorf("%q.LegacyNumber() error = nil", driver)
		}
	}
}

func TestConfigRedaction(t *testing.T) {
	t.Parallel()

	const token = "top-secret-token"
	config := Default()
	config.Plants = []Plant{DefaultPlant(), DefaultPlant()}
	config.Plants[0].Cloud.PlantToken = token

	for name, output := range map[string]string{
		"String":     config.String(),
		"fmt.Sprint": fmt.Sprint(config),
	} {
		if strings.Contains(output, token) {
			t.Errorf("%s output contains token: %q", name, output)
		}
		if !strings.Contains(output, redactedSecret) {
			t.Errorf("%s output does not contain redaction marker: %q", name, output)
		}
	}

	redacted := config.Redacted()
	if redacted.Plants[0].Cloud.PlantToken != redactedSecret {
		t.Errorf("Redacted token = %q, want %q", redacted.Plants[0].Cloud.PlantToken, redactedSecret)
	}
	if config.Plants[0].Cloud.PlantToken != token {
		t.Error("Redacted mutated the original config")
	}
	if redacted.Plants[1].Cloud.PlantToken != "" {
		t.Error("Redacted replaced an empty token")
	}
}

func TestEnumListsAreIndependent(t *testing.T) {
	t.Parallel()

	drivers := DriverTypes()
	drivers[0] = "changed"
	if DriverTypes()[0] != DriverSolarmanV5 {
		t.Error("DriverTypes returned shared mutable state")
	}

	if got := LogLevels(); !reflect.DeepEqual(got, []LogLevel{
		LogLevelError, LogLevelWarn, LogLevelInfo, LogLevelDebug,
	}) {
		t.Errorf("LogLevels() = %v", got)
	}
	if got := Parities(); !reflect.DeepEqual(got, []Parity{
		ParityNone, ParityEven, ParityOdd,
	}) {
		t.Errorf("Parities() = %v", got)
	}
}

func assertFullConfig(t *testing.T, config Config) {
	t.Helper()

	if config.Version != 1 ||
		config.Runtime.ClearOldLogs != true ||
		config.Logging.Level != LogLevelInfo ||
		len(config.Plants) != 1 {
		t.Fatalf("unexpected top-level config: %#v", config.Redacted())
	}

	plant := config.Plants[0]
	if plant.Number != 1 ||
		plant.Name != "My Main Plant" ||
		!plant.Enabled ||
		plant.Driver != DriverSolarmanV5 ||
		plant.Address != "192.168.1.100" ||
		plant.Port != 8899 ||
		plant.Serial != 1720000000 {
		t.Fatalf("unexpected plant: %#v", config.Redacted().Plants[0])
	}
	if plant.SerialPort != (SerialPort{
		Device: "/dev/ttyUSB0", Baud: 9600, DataBits: 8, Parity: ParityNone, StopBits: 1,
	}) {
		t.Errorf("unexpected serial port: %#v", plant.SerialPort)
	}
	if plant.Cloud.PlantID != "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx" ||
		plant.Cloud.PlantToken != "your-token-here" ||
		plant.Cloud.MQTTAddress != "gbboptimizer1-mqtt.gbbsoft.pl" ||
		plant.Cloud.MQTTPort != 8883 ||
		!plant.Cloud.UseTLS ||
		plant.Cloud.TLSInsecureSkipVerify {
		t.Errorf("unexpected cloud config: %#v", config.Redacted().Plants[0].Cloud)
	}
	if len(plant.SubInverters) != 1 || plant.SubInverters[0] != (SubInverter{
		Serial: 123, DongleSerial: 321, Address: "192.168.1.105", Port: 8899,
	}) {
		t.Errorf("unexpected sub-inverters: %#v", plant.SubInverters)
	}
}
