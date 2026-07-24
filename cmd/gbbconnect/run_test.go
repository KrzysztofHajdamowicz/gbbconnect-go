package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/config"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/logbuf"
)

const runTestConfig = `version: 1
runtime:
  debug: false
logging:
  level: info
plants:
  - number: 1
    name: "Test"
    enabled: false
    driver: random
    cloud: {}
`

func TestRunCommandBootstrapsApplicationAndAppliesGlobalOverrides(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	configPath := filepath.Join(directory, "gbbconnect.yaml")
	stateDir := filepath.Join(directory, "state")
	if err := os.WriteFile(configPath, []byte(runTestConfig), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var received serviceOptions
	dependencies := defaultRunDependencies()
	dependencies.runService = func(
		ctx context.Context,
		options serviceOptions,
	) error {
		if ctx == nil {
			t.Fatal("service context is nil")
		}
		received = options
		return nil
	}

	var output bytes.Buffer
	command := newRootCommandWithDependencies(
		"1.2.3-test",
		defaultDiscoveryDependencies(),
		dependencies,
	)
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{
		"run",
		"--config", configPath,
		"--state-dir", stateDir,
		"--log-level", "debug",
		"--dev",
	})
	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if received.Version != "1.2.3-test" ||
		received.StateDir != stateDir ||
		received.LogDir != filepath.Join(stateDir, "logs") {
		t.Fatalf("service options = %#v", received)
	}
	if !received.Config.Runtime.Debug ||
		received.Config.Logging.Level != config.LogLevelDebug {
		t.Fatalf("global overrides were not applied: %#v", received.Config)
	}
	if received.Logger == nil ||
		received.Logger.Level() != logbuf.LevelDebug {
		t.Fatal("logging runtime was not initialized at debug level")
	}

	banner := output.String()
	for _, fragment := range []string{
		"gbbconnect 1.2.3-test starting:",
		"config=" + configPath,
		"state-dir=" + stateDir,
		"plants=1",
	} {
		if !strings.Contains(banner, fragment) {
			t.Fatalf("banner %q does not contain %q", banner, fragment)
		}
	}
	if _, err := os.Stat(filepath.Join(stateDir, "logs")); err != nil {
		t.Fatalf("default log directory was not created: %v", err)
	}
}

func TestRootCommandRunsDaemonByDefault(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "gbbconnect.yaml")
	if err := os.WriteFile(configPath, []byte(runTestConfig), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	called := false
	dependencies := defaultRunDependencies()
	dependencies.runService = func(context.Context, serviceOptions) error {
		called = true
		return nil
	}
	command := newRootCommandWithDependencies(
		"test",
		defaultDiscoveryDependencies(),
		dependencies,
	)
	command.SetOut(&bytes.Buffer{})
	command.SetArgs([]string{
		"--config", configPath,
		"--state-dir", filepath.Join(t.TempDir(), "state"),
	})
	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !called {
		t.Fatal("root command did not run the daemon")
	}
}

func TestRunCommandMissingExplicitConfigIncludesSample(t *testing.T) {
	t.Parallel()

	command := newRootCommand("test")
	command.SetArgs([]string{
		"run",
		"--config", filepath.Join(t.TempDir(), "missing.yaml"),
	})
	err := command.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil")
	}
	if !errors.Is(err, os.ErrNotExist) ||
		!strings.Contains(err.Error(), "Example gbbconnect.yaml:") ||
		!strings.Contains(err.Error(), "driver: solarman_v5") {
		t.Fatalf("missing config error is not actionable: %v", err)
	}
}

func TestRunCommandRejectsUnknownLogLevel(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "gbbconnect.yaml")
	if err := os.WriteFile(configPath, []byte(runTestConfig), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	command := newRootCommand("test")
	command.SetArgs([]string{
		"run",
		"--config", configPath,
		"--log-level", "verbose",
	})
	err := command.Execute()
	if err == nil || !strings.Contains(err.Error(), `unsupported log level "verbose"`) {
		t.Fatalf("Execute() error = %v", err)
	}
}
