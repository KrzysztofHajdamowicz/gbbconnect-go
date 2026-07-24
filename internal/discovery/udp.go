package discovery

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

const (
	solarmanDiscoveryPort    = 48899
	solarmanDiscoveryRequest = "WIFIKIT-214028-READ"
)

// Dongle is one discovered inverter logger.
type Dongle struct {
	IP     string `json:"ip,omitempty"`
	MAC    string `json:"mac,omitempty"`
	Serial int64  `json:"serial,omitempty"`
	Raw    string `json:"raw"`
}

type udpDiscoveryOptions struct {
	bindPort    int
	destination *net.UDPAddr
}

// DiscoverUDP broadcasts a Solarman discovery request and collects responses
// until timeout. An empty ifaceIP lets the operating system choose the local
// IPv4 interface.
func DiscoverUDP(
	ctx context.Context,
	ifaceIP string,
	timeout time.Duration,
) ([]Dongle, error) {
	return discoverUDP(ctx, ifaceIP, timeout, udpDiscoveryOptions{
		bindPort: solarmanDiscoveryPort,
		destination: &net.UDPAddr{
			IP:   net.IPv4bcast,
			Port: solarmanDiscoveryPort,
		},
	})
}

func discoverUDP(
	ctx context.Context,
	ifaceIP string,
	timeout time.Duration,
	options udpDiscoveryOptions,
) ([]Dongle, error) {
	if ctx == nil {
		return nil, errors.New("discovery context is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if timeout <= 0 {
		return nil, fmt.Errorf("discovery timeout must be positive: %s", timeout)
	}
	localIP, err := parseInterfaceIPv4(ifaceIP)
	if err != nil {
		return nil, err
	}
	if options.destination == nil {
		return nil, errors.New("discovery destination is required")
	}

	// Go enables SO_BROADCAST by default when creating an IPv4 UDP socket.
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{
		IP:   localIP,
		Port: options.bindPort,
	})
	if err != nil {
		return nil, fmt.Errorf(
			"bind UDP discovery on %s:%d: %w",
			localIP,
			options.bindPort,
			err,
		)
	}
	defer func() {
		_ = conn.Close()
	}()

	discoveryCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	deadline, _ := discoveryCtx.Deadline()
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, fmt.Errorf("set UDP discovery deadline: %w", err)
	}
	stopDeadline := context.AfterFunc(discoveryCtx, func() {
		_ = conn.SetReadDeadline(time.Now())
	})
	defer stopDeadline()

	if _, err := conn.WriteToUDP(
		[]byte(solarmanDiscoveryRequest),
		options.destination,
	); err != nil {
		return nil, fmt.Errorf("send UDP discovery request: %w", err)
	}

	dongles := make([]Dongle, 0)
	buffer := make([]byte, 65535)
	for {
		count, _, err := conn.ReadFromUDP(buffer)
		if err != nil {
			if discoveryCtx.Err() != nil {
				if ctx.Err() != nil {
					return dongles, ctx.Err()
				}
				return dongles, nil
			}
			var networkError net.Error
			if errors.As(err, &networkError) && networkError.Timeout() {
				return dongles, nil
			}
			return dongles, fmt.Errorf("receive UDP discovery response: %w", err)
		}

		raw := string(buffer[:count])
		if isDiscoveryEcho(raw) {
			continue
		}
		dongles = append(dongles, parseDongle(raw))
	}
}

func parseInterfaceIPv4(value string) (net.IP, error) {
	if strings.TrimSpace(value) == "" {
		return net.IPv4zero, nil
	}
	ip := net.ParseIP(strings.TrimSpace(value))
	if ip == nil || ip.To4() == nil {
		return nil, fmt.Errorf("discovery interface must be an IPv4 address: %q", value)
	}
	return ip.To4(), nil
}

func isDiscoveryEcho(raw string) bool {
	normalized := strings.TrimSpace(strings.TrimRight(raw, "\x00"))
	return normalized == solarmanDiscoveryRequest
}

func parseDongle(raw string) Dongle {
	dongle := Dongle{Raw: raw}
	for _, field := range strings.Split(raw, ",") {
		field = strings.TrimSpace(strings.Trim(field, "\x00"))
		if field == "" {
			continue
		}
		if dongle.IP == "" {
			if ip := net.ParseIP(field); ip != nil && ip.To4() != nil {
				dongle.IP = ip.To4().String()
				continue
			}
		}
		if dongle.MAC == "" {
			if _, err := net.ParseMAC(field); err == nil {
				dongle.MAC = field
				continue
			}
		}
		if dongle.Serial == 0 && isDecimal(field) && len(field) >= 6 {
			serial, err := strconv.ParseInt(field, 10, 64)
			if err == nil {
				dongle.Serial = serial
			}
		}
	}
	return dongle
}

func isDecimal(value string) bool {
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return value != ""
}
