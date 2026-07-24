package discovery

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

func TestDiscoverUDPCollectsResponsesAndIgnoresEcho(t *testing.T) {
	t.Parallel()

	responder, err := net.ListenUDP("udp4", &net.UDPAddr{
		IP:   net.ParseIP("127.0.0.1"),
		Port: 0,
	})
	if err != nil {
		t.Fatalf("listen mock responder: %v", err)
	}
	defer func() {
		_ = responder.Close()
	}()

	responderDone := make(chan error, 1)
	go func() {
		buffer := make([]byte, 1024)
		count, client, readErr := responder.ReadFromUDP(buffer)
		if readErr != nil {
			responderDone <- readErr
			return
		}
		if got := string(buffer[:count]); got != solarmanDiscoveryRequest {
			responderDone <- errors.New("unexpected discovery request: " + got)
			return
		}
		for _, response := range []string{
			solarmanDiscoveryRequest,
			"192.168.1.100,AC:1F:0B:AA:BB:CC,1720000000",
			"firmware-specific response",
		} {
			if _, writeErr := responder.WriteToUDP([]byte(response), client); writeErr != nil {
				responderDone <- writeErr
				return
			}
		}
		responderDone <- nil
	}()

	dongles, err := discoverUDP(
		context.Background(),
		"127.0.0.1",
		100*time.Millisecond,
		udpDiscoveryOptions{
			bindPort:    0,
			destination: responder.LocalAddr().(*net.UDPAddr),
		},
	)
	if err != nil {
		t.Fatalf("discoverUDP() error = %v", err)
	}
	if err := <-responderDone; err != nil {
		t.Fatalf("mock responder error = %v", err)
	}
	if len(dongles) != 2 {
		t.Fatalf("dongle count = %d, want 2: %#v", len(dongles), dongles)
	}
	if dongles[0] != (Dongle{
		IP:     "192.168.1.100",
		MAC:    "AC:1F:0B:AA:BB:CC",
		Serial: 1720000000,
		Raw:    "192.168.1.100,AC:1F:0B:AA:BB:CC,1720000000",
	}) {
		t.Fatalf("parsed dongle = %#v", dongles[0])
	}
	if dongles[1] != (Dongle{Raw: "firmware-specific response"}) {
		t.Fatalf("unparseable dongle = %#v", dongles[1])
	}
}

func TestDiscoverUDPReturnsPromptlyOnCancellation(t *testing.T) {
	t.Parallel()

	responder, err := net.ListenUDP("udp4", &net.UDPAddr{
		IP:   net.ParseIP("127.0.0.1"),
		Port: 0,
	})
	if err != nil {
		t.Fatalf("listen mock responder: %v", err)
	}
	defer func() {
		_ = responder.Close()
	}()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		buffer := make([]byte, 1024)
		_, _, _ = responder.ReadFromUDP(buffer)
		cancel()
	}()

	start := time.Now()
	dongles, err := discoverUDP(
		ctx,
		"",
		5*time.Second,
		udpDiscoveryOptions{
			bindPort:    0,
			destination: responder.LocalAddr().(*net.UDPAddr),
		},
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("discoverUDP() error = %v, want context.Canceled", err)
	}
	if len(dongles) != 0 {
		t.Fatalf("dongles = %#v, want empty", dongles)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("cancellation took %s", elapsed)
	}
}

func TestParseDongleToleratesFieldOrderAndUnknownData(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want Dongle
	}{
		{
			name: "reordered and padded",
			raw:  " 4012345678 , 192.168.1.105 , ac-1f-0b-01-02-03 ",
			want: Dongle{
				IP:     "192.168.1.105",
				MAC:    "ac-1f-0b-01-02-03",
				Serial: 4012345678,
				Raw:    " 4012345678 , 192.168.1.105 , ac-1f-0b-01-02-03 ",
			},
		},
		{
			name: "short numbers are not serials",
			raw:  "192.0.2.1,8899,other",
			want: Dongle{
				IP:  "192.0.2.1",
				Raw: "192.0.2.1,8899,other",
			},
		},
		{
			name: "unparseable",
			raw:  "opaque",
			want: Dongle{Raw: "opaque"},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := parseDongle(test.raw); got != test.want {
				t.Fatalf("parseDongle() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestDiscoverUDPValidatesArguments(t *testing.T) {
	t.Parallel()

	//nolint:staticcheck // Exercise the public API's defensive nil-context guard.
	if _, err := DiscoverUDP(nil, "", time.Second); err == nil {
		t.Fatal("DiscoverUDP(nil context) error = nil")
	}
	if _, err := DiscoverUDP(context.Background(), "", 0); err == nil {
		t.Fatal("DiscoverUDP(zero timeout) error = nil")
	}
	if _, err := DiscoverUDP(
		context.Background(),
		"2001:db8::1",
		time.Second,
	); err == nil {
		t.Fatal("DiscoverUDP(IPv6 interface) error = nil")
	}
	if _, err := DiscoverUDP(
		context.Background(),
		"not-an-ip",
		time.Second,
	); err == nil {
		t.Fatal("DiscoverUDP(invalid interface) error = nil")
	}
}
