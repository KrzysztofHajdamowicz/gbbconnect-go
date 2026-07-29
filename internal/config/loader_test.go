package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestLoadYAMLAndJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		extension string
		content   string
	}{
		{name: "YAML", extension: ".yaml", content: minimalConfigYAML},
		{
			name:      "Home Assistant JSON",
			extension: ".json",
			content: `{
				"version": 1,
				"plants": [{
					"number": 1,
					"name": "Home",
					"driver": "solarman_v5",
					"address": "192.168.1.100",
					"serial": 1720000000,
					"cloud": {
						"plant_id": "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
						"plant_token": "your-token-here",
						"mqtt_address": "gbboptimizer1-mqtt.gbbsoft.pl"
					}
				}]
			}`,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			path := writeConfig(t, test.extension, test.content)
			config, err := Load(LoadOptions{
				Path:      path,
				LookupEnv: emptyEnvironment,
			})
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if config.Plants[0].Port != DefaultPort ||
				config.Plants[0].Cloud.MQTTPort != DefaultMQTTPort ||
				!config.Plants[0].Enabled {
				t.Fatalf("defaults were not applied: %#v", config.Redacted())
			}
		})
	}
}

func TestResolvePathPrecedence(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	workingDirectory := filepath.Join(root, "working")
	if err := os.MkdirAll(workingDirectory, 0o750); err != nil {
		t.Fatalf("create working directory: %v", err)
	}

	explicit := writeNamedConfig(t, filepath.Join(root, "explicit.yaml"))
	fromEnvironment := writeNamedConfig(t, filepath.Join(root, "environment.yaml"))
	fromWorkingDirectory := writeNamedConfig(t, filepath.Join(workingDirectory, "gbbconnect.yaml"))
	system := writeNamedConfig(t, filepath.Join(root, "system.yaml"))
	ha := writeNamedConfig(t, filepath.Join(root, "options.json"))

	options := LoadOptions{
		Path:             explicit,
		WorkingDirectory: workingDirectory,
		SystemConfigPath: system,
		HAOptionsPath:    ha,
		LookupEnv: mapEnvironment(map[string]string{
			"GBB_CONFIG": fromEnvironment,
		}),
	}
	assertResolvedPath(t, options, explicit)

	options.Path = ""
	assertResolvedPath(t, options, fromEnvironment)

	options.LookupEnv = emptyEnvironment
	assertResolvedPath(t, options, fromWorkingDirectory)

	if err := os.Remove(fromWorkingDirectory); err != nil {
		t.Fatalf("remove working-directory config: %v", err)
	}
	assertResolvedPath(t, options, system)

	if err := os.Remove(system); err != nil {
		t.Fatalf("remove system config: %v", err)
	}
	assertResolvedPath(t, options, ha)
}

func TestExplicitMissingPathDoesNotFallBack(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	fallback := writeNamedConfig(t, filepath.Join(root, "gbbconnect.yaml"))
	_, err := ResolvePath(LoadOptions{
		Path:             filepath.Join(root, "missing.yaml"),
		WorkingDirectory: filepath.Dir(fallback),
		LookupEnv:        emptyEnvironment,
	})
	if err == nil {
		t.Fatal("ResolvePath() error = nil")
	}
	if !strings.Contains(err.Error(), "explicit configuration") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMissingConfigErrorIncludesSample(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	_, err := ResolvePath(LoadOptions{
		WorkingDirectory: root,
		SystemConfigPath: filepath.Join(root, "system.yaml"),
		HAOptionsPath:    filepath.Join(root, "options.json"),
		LookupEnv:        emptyEnvironment,
	})
	if err == nil {
		t.Fatal("ResolvePath() error = nil")
	}

	var missing *MissingConfigError
	if !errors.As(err, &missing) {
		t.Fatalf("error type = %T, want *MissingConfigError", err)
	}
	if len(missing.Paths) != 3 {
		t.Fatalf("len(Paths) = %d, want 3", len(missing.Paths))
	}
	if !strings.Contains(err.Error(), "Example gbbconnect.yaml") ||
		!strings.Contains(err.Error(), "driver: solarman_v5") {
		t.Fatalf("missing error does not contain sample: %v", err)
	}

	var sample Config
	if err := yaml.Unmarshal([]byte(Sample()), &sample); err != nil {
		t.Fatalf("Sample() is not valid YAML: %v", err)
	}
	if err := Validate(sample); err != nil {
		t.Fatalf("Sample() does not validate: %v", err)
	}
}

func TestEnvironmentOverrides(t *testing.T) {
	t.Parallel()

	path := writeConfig(t, ".yaml", minimalConfigYAML)
	config, err := Load(LoadOptions{
		Path: path,
		LookupEnv: mapEnvironment(map[string]string{
			"GBB_RUNTIME_DEBUG":             "true",
			"GBB_RUNTIME_CLEAR_OLD_LOGS":    "false",
			"GBB_RUNTIME_GBB_ENVIRONMENT":   "HomeAssistant",
			"GBB_LOGGING_LEVEL":             "debug",
			"GBB_LOGGING_DRIVER_TRACE":      "true",
			"GBB_LOGGING_DRIVER_TRACE_RAW":  "true",
			"GBB_LOGGING_DIRECTORY":         "/logs",
			"GBB_PLANT_1_CLOUD_PLANT_ID":    "overridden-id",
			"GBB_PLANT_1_CLOUD_PLANT_TOKEN": "overridden-token",
			"GBB_PLANT_1_CLOUD_USE_TLS":     "false",
		}),
	})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if !config.Runtime.Debug ||
		config.Runtime.ClearOldLogs ||
		config.Runtime.GBBEnvironment != "HomeAssistant" ||
		config.Logging.Level != LogLevelDebug ||
		!config.Logging.DriverTrace ||
		!config.Logging.DriverTraceRaw ||
		config.Logging.Directory != "/logs" ||
		config.Plants[0].Cloud.PlantID != "overridden-id" ||
		config.Plants[0].Cloud.PlantToken != "overridden-token" ||
		config.Plants[0].Cloud.UseTLS {
		t.Fatalf("unexpected overrides: %#v", config.Redacted())
	}
}

func TestInvalidEnvironmentOverride(t *testing.T) {
	t.Parallel()

	path := writeConfig(t, ".yaml", minimalConfigYAML)
	_, err := Load(LoadOptions{
		Path: path,
		LookupEnv: mapEnvironment(map[string]string{
			"GBB_RUNTIME_DEBUG":         "sometimes",
			"GBB_LOGGING_DRIVER_TRACE":  "perhaps",
			"GBB_PLANT_1_CLOUD_USE_TLS": "banana",
		}),
	})
	if err == nil {
		t.Fatal("Load() error = nil")
	}
	if !strings.Contains(err.Error(), "GBB_RUNTIME_DEBUG") ||
		!strings.Contains(err.Error(), "GBB_LOGGING_DRIVER_TRACE") ||
		!strings.Contains(err.Error(), "GBB_PLANT_1_CLOUD_USE_TLS") {
		t.Fatalf("environment errors were not aggregated: %v", err)
	}
}

func writeConfig(t *testing.T, extension, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config"+extension)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func writeNamedConfig(t *testing.T, path string) string {
	t.Helper()

	if err := os.WriteFile(path, []byte(minimalConfigYAML), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func assertResolvedPath(t *testing.T, options LoadOptions, want string) {
	t.Helper()

	got, err := ResolvePath(options)
	if err != nil {
		t.Fatalf("ResolvePath() error = %v", err)
	}
	if got != want {
		t.Fatalf("ResolvePath() = %q, want %q", got, want)
	}
}

func mapEnvironment(values map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		value, ok := values[name]
		return value, ok
	}
}

func emptyEnvironment(string) (string, bool) {
	return "", false
}
