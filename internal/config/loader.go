package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

const sampleConfig = `version: 1
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

// LoadOptions controls configuration-file resolution.
type LoadOptions struct {
	Path             string
	WorkingDirectory string
	SystemConfigPath string
	HAOptionsPath    string
	LookupEnv        func(string) (string, bool)
	OperatingSystem  string
}

// MissingConfigError reports every automatic location that was checked.
type MissingConfigError struct {
	Paths []string
}

func (err *MissingConfigError) Error() string {
	return fmt.Sprintf(
		"configuration file not found; checked: %s\n\nExample gbbconnect.yaml:\n%s",
		strings.Join(err.Paths, ", "),
		Sample(),
	)
}

// Sample returns a minimal valid YAML configuration.
func Sample() string {
	return sampleConfig
}

// Load resolves, parses, overrides, and validates a configuration.
func Load(options LoadOptions) (Config, error) {
	path, err := ResolvePath(options)
	if err != nil {
		return Config{}, err
	}

	config, err := loadFile(path)
	if err != nil {
		return Config{}, err
	}

	if err := applyEnvironment(&config, lookupEnvironment(options)); err != nil {
		return Config{}, err
	}
	if err := Validate(config); err != nil {
		return Config{}, err
	}
	return config, nil
}

// ResolvePath returns the first configuration path according to documented
// precedence.
func ResolvePath(options LoadOptions) (string, error) {
	lookupEnv := lookupEnvironment(options)

	if options.Path != "" {
		return requireFile("explicit configuration", options.Path)
	}
	if path, ok := lookupEnv("GBB_CONFIG"); ok && strings.TrimSpace(path) != "" {
		return requireFile("GBB_CONFIG", path)
	}

	workingDirectory := options.WorkingDirectory
	if workingDirectory == "" {
		var err error
		workingDirectory, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("get working directory: %w", err)
		}
	}

	operatingSystem := options.OperatingSystem
	if operatingSystem == "" {
		operatingSystem = runtime.GOOS
	}

	systemPath := options.SystemConfigPath
	if systemPath == "" {
		systemPath = defaultSystemConfigPath(operatingSystem, lookupEnv)
	}
	haPath := options.HAOptionsPath
	if haPath == "" {
		haPath = "/data/options.json"
	}

	candidates := uniquePaths([]string{
		filepath.Join(workingDirectory, "gbbconnect.yaml"),
		systemPath,
		haPath,
	})
	for _, candidate := range candidates {
		if isFile(candidate) {
			return candidate, nil
		}
	}
	return "", &MissingConfigError{Paths: candidates}
}

func loadFile(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read configuration %q: %w", path, err)
	}

	var config Config
	if strings.EqualFold(filepath.Ext(path), ".json") {
		err = json.Unmarshal(data, &config)
	} else {
		err = yaml.Unmarshal(data, &config)
	}
	if err != nil {
		return Config{}, fmt.Errorf("parse configuration %q: %w", path, err)
	}
	return config, nil
}

func applyEnvironment(config *Config, lookupEnv func(string) (string, bool)) error {
	var problems []string

	applyBool := func(name string, target *bool) {
		value, ok := lookupEnv(name)
		if !ok {
			return
		}
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s must be a boolean: %q", name, value))
			return
		}
		*target = parsed
	}

	applyBool("GBB_RUNTIME_DEBUG", &config.Runtime.Debug)
	applyBool("GBB_RUNTIME_CLEAR_OLD_LOGS", &config.Runtime.ClearOldLogs)
	applyBool("GBB_LOGGING_DRIVER_TRACE", &config.Logging.DriverTrace)
	applyBool("GBB_LOGGING_DRIVER_TRACE_RAW", &config.Logging.DriverTraceRaw)

	if value, ok := lookupEnv("GBB_RUNTIME_GBB_ENVIRONMENT"); ok {
		config.Runtime.GBBEnvironment = value
	}
	if value, ok := lookupEnv("GBB_LOGGING_LEVEL"); ok {
		config.Logging.Level = LogLevel(value)
	}
	if value, ok := lookupEnv("GBB_LOGGING_DIRECTORY"); ok {
		config.Logging.Directory = value
	}

	for index := range config.Plants {
		prefix := fmt.Sprintf("GBB_PLANT_%d_CLOUD_", config.Plants[index].Number)
		if value, ok := lookupEnv(prefix + "PLANT_ID"); ok {
			config.Plants[index].Cloud.PlantID = value
		}
		if value, ok := lookupEnv(prefix + "PLANT_TOKEN"); ok {
			config.Plants[index].Cloud.PlantToken = value
		}
	}

	if len(problems) > 0 {
		return fmt.Errorf("invalid environment overrides: %s", strings.Join(problems, "; "))
	}
	return nil
}

func lookupEnvironment(options LoadOptions) func(string) (string, bool) {
	if options.LookupEnv != nil {
		return options.LookupEnv
	}
	return os.LookupEnv
}

func defaultSystemConfigPath(
	operatingSystem string,
	lookupEnv func(string) (string, bool),
) string {
	switch operatingSystem {
	case "linux":
		return "/etc/gbbconnect/gbbconnect.yaml"
	case "windows":
		if programData, ok := lookupEnv("ProgramData"); ok && programData != "" {
			return filepath.Join(programData, "gbbconnect", "gbbconnect.yaml")
		}
		return filepath.Join(`C:\ProgramData`, "gbbconnect", "gbbconnect.yaml")
	default:
		configDirectory, err := os.UserConfigDir()
		if err != nil {
			return ""
		}
		return filepath.Join(configDirectory, "gbbconnect", "gbbconnect.yaml")
	}
}

func requireFile(source, path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("%s path %q: %w", source, path, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s path %q is not a regular file", source, path)
	}
	return path, nil
}

func isFile(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func uniquePaths(paths []string) []string {
	result := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		if path == "" {
			continue
		}
		clean := filepath.Clean(path)
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		result = append(result, clean)
	}
	return result
}

var _ error = (*MissingConfigError)(nil)
