package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	configschema "github.com/KrzysztofHajdamowicz/gbbconnect-go/schema"
	"go.yaml.in/yaml/v3"
)

func TestDocumentedConfigsValidateAgainstSchema(t *testing.T) {
	t.Parallel()

	for name, source := range map[string]string{
		"full":    fullConfigYAML,
		"minimal": minimalConfigYAML,
	} {
		name := name
		source := source
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var config Config
			if err := yaml.Unmarshal([]byte(source), &config); err != nil {
				t.Fatalf("yaml.Unmarshal() error = %v", err)
			}
			if err := Validate(config); err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
			if err := ValidateSchema(config); err != nil {
				t.Fatalf("ValidateSchema() error = %v", err)
			}

			path := filepath.Join(t.TempDir(), name+".yaml")
			if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
				t.Fatalf("write documented config: %v", err)
			}
			if err := ValidateSchemaFile(path); err != nil {
				t.Fatalf("ValidateSchemaFile() error = %v", err)
			}
		})
	}
}

func TestSchemaRejectsBrokenValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value map[string]any
	}{
		{
			name: "unknown property",
			value: map[string]any{
				"version": 1,
				"plants":  []any{},
				"typo":    true,
			},
		},
		{
			name: "unknown driver",
			value: map[string]any{
				"version": 1,
				"plants": []any{
					map[string]any{
						"number": 1,
						"name":   "Home",
						"driver": "invalid",
						"cloud":  map[string]any{},
					},
				},
			},
		},
		{
			name: "wrong port type",
			value: map[string]any{
				"version": 1,
				"plants": []any{
					map[string]any{
						"number": 1,
						"name":   "Home",
						"driver": "random",
						"port":   "8899",
						"cloud":  map[string]any{},
					},
				},
			},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if err := ValidateSchema(test.value); err == nil {
				t.Fatal("ValidateSchema() error = nil")
			}
		})
	}
}

func TestValidateSchemaFileChecksRawFields(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.yaml")
	const input = `version: 1
plants: []
unknown_field: true
`
	if err := os.WriteFile(path, []byte(input), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := ValidateSchemaFile(path); err == nil {
		t.Fatal("ValidateSchemaFile() error = nil")
	}
}

func TestSchemaEnumsMatchCode(t *testing.T) {
	t.Parallel()

	var document struct {
		Definitions struct {
			Plant struct {
				Properties struct {
					Driver struct {
						Enum []DriverType `json:"enum"`
					} `json:"driver"`
				} `json:"properties"`
			} `json:"plant"`
			Logging struct {
				Properties struct {
					Level struct {
						Enum []LogLevel `json:"enum"`
					} `json:"level"`
				} `json:"properties"`
			} `json:"logging"`
			SerialPort struct {
				Properties struct {
					Parity struct {
						Enum []Parity `json:"enum"`
					} `json:"parity"`
				} `json:"properties"`
			} `json:"serial_port"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(configschema.ConfigJSON, &document); err != nil {
		t.Fatalf("json.Unmarshal(schema) error = %v", err)
	}

	if !reflect.DeepEqual(document.Definitions.Plant.Properties.Driver.Enum, DriverTypes()) {
		t.Errorf(
			"schema driver enum = %v, code = %v",
			document.Definitions.Plant.Properties.Driver.Enum,
			DriverTypes(),
		)
	}
	if !reflect.DeepEqual(document.Definitions.Logging.Properties.Level.Enum, LogLevels()) {
		t.Errorf(
			"schema log level enum = %v, code = %v",
			document.Definitions.Logging.Properties.Level.Enum,
			LogLevels(),
		)
	}
	if !reflect.DeepEqual(document.Definitions.SerialPort.Properties.Parity.Enum, Parities()) {
		t.Errorf(
			"schema parity enum = %v, code = %v",
			document.Definitions.SerialPort.Properties.Parity.Enum,
			Parities(),
		)
	}
}
