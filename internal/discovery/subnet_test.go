package discovery

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"sync/atomic"
	"testing"
	"time"
)

func TestScanSubnetFindsReachableHost(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen mock dongle: %v", err)
	}
	defer func() {
		_ = listener.Close()
	}()
	accepted := make(chan error, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			accepted <- acceptErr
			return
		}
		accepted <- connection.Close()
	}()

	port := listener.Addr().(*net.TCPAddr).Port
	dongles, err := ScanSubnet(
		context.Background(),
		"127.0.0.0/30",
		port,
		2,
	)
	if err != nil {
		t.Fatalf("ScanSubnet() error = %v", err)
	}
	if err := <-accepted; err != nil {
		t.Fatalf("mock dongle error = %v", err)
	}
	if len(dongles) != 1 || dongles[0] != (Dongle{IP: "127.0.0.1"}) {
		t.Fatalf("ScanSubnet() = %#v, want 127.0.0.1", dongles)
	}
}

func TestScanSubnetBoundsConcurrency(t *testing.T) {
	t.Parallel()

	const concurrency = 3
	var active atomic.Int32
	var maximum atomic.Int32
	entered := make(chan struct{}, 32)
	release := make(chan struct{})
	probe := func(context.Context, string, int) (Dongle, bool) {
		current := active.Add(1)
		for {
			previous := maximum.Load()
			if current <= previous || maximum.CompareAndSwap(previous, current) {
				break
			}
		}
		entered <- struct{}{}
		<-release
		active.Add(-1)
		return Dongle{}, false
	}

	done := make(chan error, 1)
	go func() {
		_, err := scanSubnet(
			context.Background(),
			"192.0.2.0/28",
			8899,
			concurrency,
			probe,
		)
		done <- err
	}()

	for range concurrency {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("workers did not reach requested concurrency")
		}
	}
	select {
	case <-entered:
		t.Fatal("probe exceeded concurrency limit")
	case <-time.After(25 * time.Millisecond):
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("scanSubnet() error = %v", err)
	}
	if got := maximum.Load(); got != concurrency {
		t.Fatalf("maximum concurrency = %d, want %d", got, concurrency)
	}
}

func TestScanSubnetCancellationStopsWorkers(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	entered := make(chan struct{}, 1)
	probe := func(ctx context.Context, _ string, _ int) (Dongle, bool) {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return Dongle{}, false
	}

	done := make(chan error, 1)
	go func() {
		_, err := scanSubnet(ctx, "192.0.2.0/24", 8899, 4, probe)
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("subnet probe did not start")
	}
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("scanSubnet() error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("subnet scan did not stop after cancellation")
	}
}

func TestForEachUsableIPv4(t *testing.T) {
	t.Parallel()

	tests := []struct {
		cidr string
		want []string
	}{
		{
			cidr: "192.0.2.0/30",
			want: []string{"192.0.2.1", "192.0.2.2"},
		},
		{
			cidr: "192.0.2.0/31",
			want: []string{"192.0.2.0", "192.0.2.1"},
		},
		{
			cidr: "192.0.2.7/32",
			want: []string{"192.0.2.7"},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.cidr, func(t *testing.T) {
			t.Parallel()

			prefix := mustPrefix(t, test.cidr)
			var got []string
			forEachUsableIPv4(prefix, func(ip string) bool {
				got = append(got, ip)
				return true
			})
			if len(got) != len(test.want) {
				t.Fatalf("hosts = %v, want %v", got, test.want)
			}
			for index := range got {
				if got[index] != test.want[index] {
					t.Fatalf("hosts = %v, want %v", got, test.want)
				}
			}
		})
	}
}

func TestScanSubnetValidatesArguments(t *testing.T) {
	t.Parallel()

	//nolint:staticcheck // Exercise the public API's defensive nil-context guard.
	if _, err := ScanSubnet(nil, "192.0.2.0/30", 8899, 1); err == nil {
		t.Fatal("ScanSubnet(nil context) error = nil")
	}
	for _, test := range []struct {
		cidr        string
		port        int
		concurrency int
	}{
		{cidr: "invalid", port: 8899, concurrency: 1},
		{cidr: "2001:db8::/126", port: 8899, concurrency: 1},
		{cidr: "192.0.2.0/30", port: 0, concurrency: 1},
		{cidr: "192.0.2.0/30", port: 65536, concurrency: 1},
		{cidr: "192.0.2.0/30", port: 8899, concurrency: 0},
	} {
		if _, err := ScanSubnet(
			context.Background(),
			test.cidr,
			test.port,
			test.concurrency,
		); err == nil {
			t.Errorf(
				"ScanSubnet(%q, %d, %d) error = nil",
				test.cidr,
				test.port,
				test.concurrency,
			)
		}
	}
}

func mustPrefix(t *testing.T, value string) netip.Prefix {
	t.Helper()
	prefix, err := netip.ParsePrefix(value)
	if err != nil {
		t.Fatalf("parse prefix: %v", err)
	}
	return prefix
}
