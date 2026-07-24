package main

import (
	"bytes"
	"strings"
	"testing"
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
