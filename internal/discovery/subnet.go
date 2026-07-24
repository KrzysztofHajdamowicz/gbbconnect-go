package discovery

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

const subnetConnectTimeout = 500 * time.Millisecond
const solarmanFingerprintTimeout = 300 * time.Millisecond
const solarmanStatusTimeout = 750 * time.Millisecond
const maximumStatusSize = 64 * 1024

var (
	solarmanStatusSerialPattern = regexp.MustCompile(
		`\bcover_mid\s*=\s*["']([0-9]{6,10})["']`,
	)
	solarmanStatusMACPattern = regexp.MustCompile(
		`\bcover_sta_mac\s*=\s*["']([^"']+)["']`,
	)
)

type hostProber func(ctx context.Context, ip string, port int) (Dongle, bool)

// ScanSubnet probes every usable IPv4 host in a CIDR for a reachable dongle
// TCP port. It fingerprints passive SolarmanV5 frames and the logger's
// read-only HTTP status page where available. Reachable hosts are returned even
// when their serial is unavailable.
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
	defer func() {
		_ = connection.Close()
	}()

	dongle := Dongle{IP: ip}
	if err := connection.SetReadDeadline(
		time.Now().Add(solarmanFingerprintTimeout),
	); err != nil {
		return dongle, true
	}
	buffer := make([]byte, 1024)
	received := make([]byte, 0, len(buffer))
	for len(received) < 4096 {
		count, readErr := connection.Read(buffer)
		received = append(received, buffer[:count]...)
		if serial, ok := extractSolarmanSerial(received); ok {
			dongle.Serial = serial
			dongle.Protocol = "solarman_v5"
			return dongle, true
		}
		if count > 0 && !bytes.Contains(received, []byte{0xA5}) {
			break
		}
		if readErr != nil {
			break
		}
	}
	if statusDongle, ok := probeSolarmanStatus(ctx, ip); ok {
		return statusDongle, true
	}
	return dongle, true
}

func probeSolarmanStatus(ctx context.Context, ip string) (Dongle, bool) {
	dialer := net.Dialer{Timeout: solarmanStatusTimeout}
	client := &http.Client{
		Transport: &http.Transport{
			DialContext:       dialer.DialContext,
			DisableKeepAlives: true,
		},
		Timeout: solarmanStatusTimeout,
		CheckRedirect: func(
			_ *http.Request,
			_ []*http.Request,
		) error {
			return http.ErrUseLastResponse
		},
	}

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		"http://"+net.JoinHostPort(ip, "80")+"/status.html",
		nil,
	)
	if err != nil {
		return Dongle{}, false
	}
	response, err := client.Do(request)
	if err != nil {
		return Dongle{}, false
	}
	defer func() {
		_ = response.Body.Close()
	}()
	if response.StatusCode != http.StatusOK {
		return Dongle{}, false
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, maximumStatusSize+1))
	if err != nil || len(body) > maximumStatusSize {
		return Dongle{}, false
	}
	dongle, ok := parseSolarmanStatus(body)
	if !ok {
		return Dongle{}, false
	}
	dongle.IP = ip
	return dongle, true
}

func parseSolarmanStatus(body []byte) (Dongle, bool) {
	match := solarmanStatusSerialPattern.FindSubmatch(body)
	if len(match) != 2 {
		return Dongle{}, false
	}
	serial, err := strconv.ParseUint(string(match[1]), 10, 32)
	if err != nil || serial == 0 {
		return Dongle{}, false
	}

	dongle := Dongle{
		Serial:   int64(serial),
		Protocol: "solarman_v5",
	}
	if match = solarmanStatusMACPattern.FindSubmatch(body); len(match) == 2 {
		if mac, parseErr := net.ParseMAC(
			strings.TrimSpace(string(match[1])),
		); parseErr == nil {
			dongle.MAC = strings.ToUpper(mac.String())
		}
	}
	return dongle, true
}

func extractSolarmanSerial(data []byte) (int64, bool) {
	for offset := 0; offset+12 <= len(data); offset++ {
		if data[offset] != 0xA5 {
			continue
		}
		frameLength := int(binary.LittleEndian.Uint16(data[offset+1:offset+3])) + 13
		if frameLength < 13 || offset+frameLength > len(data) {
			continue
		}
		frame := data[offset : offset+frameLength]
		if frame[3] != 0x10 ||
			frame[4] != 0x15 ||
			frame[11] != 0x02 ||
			frame[len(frame)-1] != 0x15 {
			continue
		}
		serial := binary.LittleEndian.Uint32(frame[7:11])
		if serial != 0 {
			return int64(serial), true
		}
	}
	return 0, false
}

func parseIPv4Prefix(cidr string) (netip.Prefix, error) {
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil || !prefix.Addr().Is4() {
		return netip.Prefix{}, fmt.Errorf(
			"discovery subnet must be an IPv4 CIDR: %q",
			cidr,
		)
	}
	return prefix, nil
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
