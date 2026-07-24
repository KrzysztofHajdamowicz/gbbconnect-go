package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/config"
	"go.yaml.in/yaml/v3"
)

func TestDefaultVersion(t *testing.T) {
	t.Parallel()

	if version != "dev" {
		t.Fatalf("default version = %q, want dev", version)
	}
}

func TestHelpCommands(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "root", args: []string{}, want: "Usage:\n  gbbconnect"},
		{name: "root explicit", args: []string{"--help"}, want: "Usage:\n  gbbconnect"},
		{name: "run", args: []string{"run", "--help"}, want: "Usage:\n  gbbconnect run"},
		{name: "discover", args: []string{"discover", "--help"}, want: "Usage:\n  gbbconnect discover"},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var output bytes.Buffer
			command := newRootCommand("test")
			command.SetOut(&output)
			command.SetErr(&output)
			command.SetArgs(test.args)

			if err := command.Execute(); err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			if !strings.Contains(output.String(), test.want) {
				t.Fatalf("output %q does not contain %q", output.String(), test.want)
			}
		})
	}
}

func TestVersionCommand(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	command := newRootCommand("1.2.3-test")
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{"version"})

	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	const want = "gbbconnect version 1.2.3-test\n"
	if output.String() != want {
		t.Fatalf("output = %q, want %q", output.String(), want)
	}
}

func TestUnknownCommand(t *testing.T) {
	t.Parallel()

	command := newRootCommand("test")
	command.SetArgs([]string{"does-not-exist"})

	err := command.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want an error")
	}
	if !strings.Contains(err.Error(), `unknown command "does-not-exist"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestImportXMLCommand(t *testing.T) {
	t.Parallel()

	const input = `<Parameters Version="1">
	  <Plant Version="1" Number="1" Name="Home" DriverNo="0" IsDisabled="0"
	    AddressIP="192.168.1.100" SerialNumber="1720000000"
	    GbbOptimizer_PlantId="plant-id" GbbOptimizer_PlantToken="plant-token"/>
	</Parameters>`

	directory := t.TempDir()
	inputPath := filepath.Join(directory, "Parameters.xml")
	outputPath := filepath.Join(directory, "gbbconnect.yaml")
	if err := os.WriteFile(inputPath, []byte(input), 0o600); err != nil {
		t.Fatalf("write input XML: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command := newRootCommand("test")
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	command.SetArgs([]string{"import-xml", "--in", inputPath, "--out", outputPath})
	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "Imported") {
		t.Fatalf("stdout = %q, want import confirmation", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output YAML: %v", err)
	}
	var imported config.Config
	if err := yaml.Unmarshal(data, &imported); err != nil {
		t.Fatalf("decode output YAML: %v", err)
	}
	if err := config.Validate(imported); err != nil {
		t.Fatalf("output YAML does not validate: %v", err)
	}
	if imported.Plants[0].Cloud.PlantToken != "plant-token" {
		t.Fatal("output YAML did not preserve the token")
	}
}

func TestImportXMLCommandRequiresPaths(t *testing.T) {
	t.Parallel()

	command := newRootCommand("test")
	command.SetArgs([]string{"import-xml"})
	err := command.Execute()
	if err == nil || !strings.Contains(err.Error(), "--in is required") {
		t.Fatalf("Execute() error = %v, want missing --in", err)
	}
}

func TestConfigValidateCommand(t *testing.T) {
	t.Parallel()

	const input = `version: 1
plants:
  - number: 1
    name: "Test"
    enabled: false
    driver: random
    cloud: {}
`
	path := filepath.Join(t.TempDir(), "gbbconnect.yaml")
	if err := os.WriteFile(path, []byte(input), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var output bytes.Buffer
	command := newRootCommand("test")
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{"config", "validate", "--config", path})
	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(output.String(), "is valid") {
		t.Fatalf("output = %q, want validation confirmation", output.String())
	}
}

func TestConfigValidateCommandRejectsInvalidConfig(t *testing.T) {
	t.Parallel()

	const input = `version: 1
plants:
  - number: 1
    name: ""
    driver: invalid
    cloud: {}
`
	path := filepath.Join(t.TempDir(), "invalid.yaml")
	if err := os.WriteFile(path, []byte(input), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	command := newRootCommand("test")
	command.SetArgs([]string{"config", "validate", "--config", path})
	err := command.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil")
	}
	if !strings.Contains(err.Error(), "name must not be empty") ||
		!strings.Contains(err.Error(), `driver "invalid" is not supported`) {
		t.Fatalf("validation error was not actionable: %v", err)
	}
}
