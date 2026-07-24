package discovery

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"sync"
	"time"
)

const subnetConnectTimeout = 500 * time.Millisecond

type hostProber func(ctx context.Context, ip string, port int) (Dongle, bool)

// ScanSubnet probes every usable IPv4 host in a CIDR for a reachable dongle
// TCP port. Reachable hosts are returned even when their serial is unavailable.
func ScanSubnet(
	ctx context.Context,
	cidr string,
	port int,
	concurrency int,
) ([]Dongle, error) {
	return scanSubnet(ctx, cidr, port, concurrency, probeTCP)
}

func scanSubnet(
	ctx context.Context,
	cidr string,
	port int,
	concurrency int,
	probe hostProber,
) ([]Dongle, error) {
	if ctx == nil {
		return nil, errors.New("subnet scan context is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil || !prefix.Addr().Is4() {
		return nil, fmt.Errorf("subnet scan requires an IPv4 CIDR: %q", cidr)
	}
	if port < 1 || port > 65535 {
		return nil, fmt.Errorf("subnet scan port must be between 1 and 65535: %d", port)
	}
	if concurrency < 1 {
		return nil, fmt.Errorf("subnet scan concurrency must be positive: %d", concurrency)
	}
	if probe == nil {
		return nil, errors.New("subnet host prober is required")
	}

	jobs := make(chan string)
	results := make(chan Dongle)
	var workers sync.WaitGroup
	for range concurrency {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for ip := range jobs {
				dongle, reachable := probe(ctx, ip, port)
				if !reachable {
					continue
				}
				select {
				case results <- dongle:
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	go func() {
		defer close(jobs)
		forEachUsableIPv4(prefix, func(ip string) bool {
			select {
			case jobs <- ip:
				return true
			case <-ctx.Done():
				return false
			}
		})
	}()
	go func() {
		workers.Wait()
		close(results)
	}()

	dongles := make([]Dongle, 0)
	for dongle := range results {
		dongles = append(dongles, dongle)
	}
	slices.SortFunc(dongles, func(left, right Dongle) int {
		leftIP, _ := netip.ParseAddr(left.IP)
		rightIP, _ := netip.ParseAddr(right.IP)
		return leftIP.Compare(rightIP)
	})
	if err := ctx.Err(); err != nil {
		return dongles, err
	}
	return dongles, nil
}

func probeTCP(ctx context.Context, ip string, port int) (Dongle, bool) {
	dialer := net.Dialer{Timeout: subnetConnectTimeout}
	connection, err := dialer.DialContext(
		ctx,
		"tcp",
		net.JoinHostPort(ip, strconv.Itoa(port)),
	)
	if err != nil {
		return Dongle{}, false
	}
	_ = connection.Close()
	return Dongle{IP: ip}, true
}

func forEachUsableIPv4(prefix netip.Prefix, visit func(ip string) bool) {
	prefix = prefix.Masked()
	address := prefix.Addr().As4()
	network := binary.BigEndian.Uint32(address[:])
	hostBits := 32 - prefix.Bits()
	addressCount := uint64(1) << hostBits

	firstOffset := uint64(0)
	endOffset := addressCount
	if hostBits >= 2 {
		firstOffset = 1
		endOffset--
	}
	for offset := firstOffset; offset < endOffset; offset++ {
		value := network + uint32(offset)
		var candidate [4]byte
		binary.BigEndian.PutUint32(candidate[:], value)
		if !visit(netip.AddrFrom4(candidate).String()) {
			return
		}
	}
}
