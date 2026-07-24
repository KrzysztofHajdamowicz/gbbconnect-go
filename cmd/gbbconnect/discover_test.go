package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/discovery"
)

func TestDiscoverCommandMergesBroadcastAndSubnetResults(t *testing.T) {
	t.Parallel()

	var gotInterface string
	var gotTimeout time.Duration
	var gotSubnet string
	var gotUDPSubnet string
	var gotPort int
	var gotConcurrency int
	dependencies := discoveryDependencies{
		discoverUDP: func(
			_ context.Context,
			ifaceIP string,
			timeout time.Duration,
		) ([]discovery.Dongle, error) {
			gotInterface = ifaceIP
			gotTimeout = timeout
			return []discovery.Dongle{
				{
					IP:     "192.168.1.100",
					MAC:    "AC:1F:0B:AA:BB:CC",
					Serial: 1720000000,
					Raw:    "udp-one",
				},
				{IP: "192.168.1.105", Serial: 4012345678, Raw: "udp-two"},
			}, nil
		},
		discoverUDPSubnet: func(
			_ context.Context,
			_ string,
			cidr string,
			_ time.Duration,
		) ([]discovery.Dongle, error) {
			gotUDPSubnet = cidr
			return []discovery.Dongle{{
				IP:     "192.168.1.110",
				MAC:    "AA:BB:CC:DD:EE:FF",
				Serial: 2112345678,
				Raw:    "udp-unicast",
			}}, nil
		},
		scanSubnet: func(
			_ context.Context,
			cidr string,
			port int,
			concurrency int,
		) ([]discovery.Dongle, error) {
			gotSubnet = cidr
			gotPort = port
			gotConcurrency = concurrency
			return []discovery.Dongle{
				{IP: "192.168.1.100"},
				{IP: "192.168.1.110"},
			}, nil
		},
	}

	var output bytes.Buffer
	command := newRootCommandWithDiscovery("test", dependencies)
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{
		"discover",
		"--interface", "192.168.1.2",
		"--subnet", "192.168.1.0/24",
		"--port", "18899",
		"--timeout", "250ms",
	})
	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if gotInterface != "192.168.1.2" || gotTimeout != 250*time.Millisecond {
		t.Fatalf("UDP arguments = interface %q, timeout %s", gotInterface, gotTimeout)
	}
	if gotUDPSubnet != "192.168.1.0/24" ||
		gotSubnet != "192.168.1.0/24" ||
		gotPort != 18899 ||
		gotConcurrency != defaultDiscoveryConcurrency {
		t.Fatalf(
			"subnet arguments = %q, port %d, concurrency %d",
			gotSubnet,
			gotPort,
			gotConcurrency,
		)
	}
	text := output.String()
	for _, fragment := range []string{
		"Discovered Solarman dongles:",
		"192.168.1.100",
		"AC:1F:0B:AA:BB:CC",
		"1720000000",
		"192.168.1.105",
		"192.168.1.110",
		"2112345678",
		"3 dongle(s) found.",
	} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("output %q does not contain %q", text, fragment)
		}
	}
	if strings.Count(text, "192.168.1.100") != 1 {
		t.Fatalf("deduplicated IP appears more than once: %q", text)
	}
}

func TestDiscoverCommandJSONAndBroadcastDisable(t *testing.T) {
	t.Parallel()

	udpCalls := 0
	dependencies := discoveryDependencies{
		discoverUDP: func(
			context.Context,
			string,
			time.Duration,
		) ([]discovery.Dongle, error) {
			udpCalls++
			return nil, nil
		},
		discoverUDPSubnet: func(
			context.Context,
			string,
			string,
			time.Duration,
		) ([]discovery.Dongle, error) {
			return nil, nil
		},
		scanSubnet: func(
			context.Context,
			string,
			int,
			int,
		) ([]discovery.Dongle, error) {
			return []discovery.Dongle{{
				IP: "192.0.2.10",
			}}, nil
		},
	}

	var output bytes.Buffer
	command := newRootCommandWithDiscovery("test", dependencies)
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{
		"discover",
		"--broadcast=false",
		"--subnet", "192.0.2.0/28",
		"--json",
	})
	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if udpCalls != 0 {
		t.Fatalf("UDP calls = %d, want 0", udpCalls)
	}

	var decoded struct {
		Dongles []discovery.Dongle `json:"dongles"`
	}
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatalf("decode JSON output: %v; output %q", err, output.String())
	}
	if len(decoded.Dongles) != 1 ||
		decoded.Dongles[0].IP != "192.0.2.10" {
		t.Fatalf("JSON dongles = %#v", decoded.Dongles)
	}
}

func TestDiscoverCommandEmptyJSONUsesArray(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	command := newRootCommandWithDiscovery("test", discoveryDependencies{
		discoverUDP: func(
			context.Context,
			string,
			time.Duration,
		) ([]discovery.Dongle, error) {
			return nil, nil
		},
	})
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{"discover", "--json"})
	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(output.String(), `"dongles": []`) {
		t.Fatalf("JSON output = %q, want empty array", output.String())
	}
}

func TestDiscoverCommandValidatesMethodsAndWrapsErrors(t *testing.T) {
	t.Parallel()

	command := newRootCommandWithDiscovery("test", discoveryDependencies{})
	command.SetArgs([]string{"discover", "--broadcast=false"})
	if err := command.Execute(); err == nil ||
		!strings.Contains(err.Error(), "enable --broadcast or provide --subnet") {
		t.Fatalf("no-method Execute() error = %v", err)
	}

	wantErr := errors.New("broadcast failed")
	command = newRootCommandWithDiscovery("test", discoveryDependencies{
		discoverUDP: func(
			context.Context,
			string,
			time.Duration,
		) ([]discovery.Dongle, error) {
			return nil, wantErr
		},
	})
	command.SetArgs([]string{"discover"})
	if err := command.Execute(); !errors.Is(err, wantErr) ||
		!strings.Contains(err.Error(), "UDP discovery") {
		t.Fatalf("UDP failure Execute() error = %v", err)
	}
}

func TestMergeDonglesUsesSerialOrIPAndFillsMissingFields(t *testing.T) {
	t.Parallel()

	got := mergeDongles(
		[]discovery.Dongle{
			{Serial: 123, Raw: "first"},
			{IP: "192.0.2.2"},
		},
		[]discovery.Dongle{
			{IP: "192.0.2.1", MAC: "AA:BB:CC:DD:EE:FF", Serial: 123},
			{IP: "192.0.2.2", Serial: 456},
		},
	)
	if len(got) != 2 {
		t.Fatalf("merged length = %d, want 2: %#v", len(got), got)
	}
	if got[0] != (discovery.Dongle{
		IP:     "192.0.2.1",
		MAC:    "AA:BB:CC:DD:EE:FF",
		Serial: 123,
		Raw:    "first",
	}) {
		t.Fatalf("serial merge = %#v", got[0])
	}
	if got[1] != (discovery.Dongle{IP: "192.0.2.2", Serial: 456}) {
		t.Fatalf("IP merge = %#v", got[1])
	}
}
