package modbusrtutcp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/config"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/logbuf"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/modbus"
)

const (
	defaultPort        = 8899
	ioTimeout          = time.Second
	reconnectDelay     = 500 * time.Millisecond
	connectionAttempts = 11
	maxFrameSize       = 1024
)

var (
	// ErrConnectionLost reports a zero-byte read from the gateway.
	ErrConnectionLost = errors.New("connection lost while reading RTU-over-TCP")
	// ErrWrongCRC reports a complete response with an invalid Modbus CRC.
	ErrWrongCRC = errors.New("wrong Modbus RTU CRC")
)

type traceControls interface {
	DriverTraceEnabled() bool
	DriverTraceRawEnabled() bool
}

type dialContextFunc func(
	ctx context.Context,
	network string,
	address string,
) (net.Conn, error)

// Transport sends raw Modbus RTU frames through a transparent TCP gateway.
type Transport struct {
	mu          sync.Mutex
	address     string
	port        int
	logger      logbuf.Logger
	trace       traceControls
	conn        net.Conn
	pending     []byte
	dialContext dialContextFunc
	timeout     time.Duration
	retryDelay  time.Duration
}

// New constructs an RTU-over-TCP transport with lazy connection setup.
func New(plant config.Plant, logger logbuf.Logger) *Transport {
	port := plant.Port
	if port == 0 {
		port = defaultPort
	}
	transport := &Transport{
		address:    plant.Address,
		port:       port,
		logger:     logger,
		timeout:    ioTimeout,
		retryDelay: reconnectDelay,
	}
	if controls, ok := logger.(traceControls); ok {
		transport.trace = controls
	}
	dialer := &net.Dialer{Timeout: transport.timeout}
	transport.dialContext = dialer.DialContext
	return transport
}

// Connect establishes the TCP connection if needed.
func (transport *Transport) Connect(ctx context.Context) error {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	return transport.connectLocked(ctx)
}

func (transport *Transport) connectLocked(ctx context.Context) error {
	if transport.conn != nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if transport.address == "" {
		return fmt.Errorf("RTU-over-TCP address is required")
	}
	if transport.port < 1 || transport.port > 65535 {
		return fmt.Errorf("RTU-over-TCP port must be between 1 and 65535")
	}

	host, err := resolveFirst(ctx, transport.address)
	if err != nil {
		return fmt.Errorf("resolve %q: %w", transport.address, err)
	}
	address := net.JoinHostPort(host, strconv.Itoa(transport.port))
	connection, err := transport.dialContext(ctx, "tcp", address)
	if err != nil {
		return fmt.Errorf("connect %s: %w", address, err)
	}
	if tcpConnection, ok := connection.(*net.TCPConn); ok {
		if err := tcpConnection.SetNoDelay(true); err != nil {
			_ = connection.Close()
			return fmt.Errorf("set TCP no-delay: %w", err)
		}
	}
	transport.conn = connection
	transport.pending = nil
	return nil
}

// SendRTU sends a complete RTU frame verbatim and returns one complete,
// CRC-validated RTU response. Coalesced bytes after the first response are
// retained for the next serialized exchange.
func (transport *Transport) SendRTU(
	ctx context.Context,
	rtu []byte,
) ([]byte, error) {
	transport.mu.Lock()
	defer transport.mu.Unlock()

	if err := transport.connectLocked(ctx); err != nil {
		return nil, err
	}
	transport.logDecoded("Send ModBus", rtu)
	transport.logRaw("Send RTU-over-TCP", rtu)

	response, err := transport.internalSendLocked(ctx, rtu)
	if err != nil {
		return nil, err
	}
	if !modbus.ValidateCRC(response) {
		return nil, ErrWrongCRC
	}
	transport.logRaw("Received RTU-over-TCP", response)
	transport.logDecoded("Received ModBus", response)
	return response, nil
}

func (transport *Transport) internalSendLocked(
	ctx context.Context,
	request []byte,
) ([]byte, error) {
	for attempt := 1; attempt <= connectionAttempts; attempt++ {
		var (
			response []byte
			err      error
		)
		if transport.conn == nil {
			err = transport.connectLocked(ctx)
		}
		if err == nil {
			err = transport.writeFrameLocked(ctx, request)
		}
		if err == nil {
			response, err = transport.readFrameLocked(ctx)
		}
		if err == nil {
			return response, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		transport.logInfo("Send error", "error", err)
		if attempt == connectionAttempts {
			return nil, err
		}
		transport.logInfo("Retry", "attempt", attempt)
		if closeErr := transport.closeLocked(); closeErr != nil {
			transport.logInfo("Close error", "error", closeErr)
		}
		if err := waitContext(ctx, transport.retryDelay); err != nil {
			return nil, err
		}
	}
	panic("unreachable")
}

func (transport *Transport) writeFrameLocked(
	ctx context.Context,
	frame []byte,
) error {
	if err := transport.conn.SetWriteDeadline(deadline(ctx, transport.timeout)); err != nil {
		return fmt.Errorf("set write deadline: %w", err)
	}
	written := 0
	for written < len(frame) {
		count, err := transport.conn.Write(frame[written:])
		written += count
		if err != nil {
			return fmt.Errorf("write RTU-over-TCP frame: %w", err)
		}
		if count == 0 {
			return io.ErrUnexpectedEOF
		}
	}
	return nil
}

func (transport *Transport) readFrameLocked(ctx context.Context) ([]byte, error) {
	if err := transport.conn.SetReadDeadline(deadline(ctx, transport.timeout)); err != nil {
		return nil, fmt.Errorf("set read deadline: %w", err)
	}

	buffer := transport.pending
	transport.pending = nil
	for {
		expected, err := ExpectedResponseLength(buffer)
		if err != nil {
			return nil, err
		}
		if expected > 0 && len(buffer) >= expected {
			frame := make([]byte, expected)
			copy(frame, buffer[:expected])
			transport.pending = append(transport.pending, buffer[expected:]...)
			return frame, nil
		}
		if len(buffer) >= maxFrameSize {
			return nil, fmt.Errorf("modbus RTU response exceeds %d bytes", maxFrameSize)
		}

		chunk := make([]byte, maxFrameSize-len(buffer))
		read, readErr := transport.conn.Read(chunk)
		if read > 0 {
			buffer = append(buffer, chunk[:read]...)
		}
		if readErr != nil {
			if len(buffer) == 0 && errors.Is(readErr, io.EOF) {
				return nil, ErrConnectionLost
			}
			return nil, fmt.Errorf("read RTU-over-TCP frame: %w", readErr)
		}
		if read == 0 {
			return nil, ErrConnectionLost
		}
	}
}

// Close closes the TCP connection and clears any buffered response bytes.
func (transport *Transport) Close() error {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	return transport.closeLocked()
}

func (transport *Transport) closeLocked() error {
	transport.pending = nil
	if transport.conn == nil {
		return nil
	}
	err := transport.conn.Close()
	transport.conn = nil
	if err != nil {
		return fmt.Errorf("close RTU-over-TCP connection: %w", err)
	}
	return nil
}

func (transport *Transport) logDecoded(message string, frame []byte) {
	if transport.logger != nil && transport.trace != nil &&
		transport.trace.DriverTraceEnabled() {
		transport.logger.Info(message, "frame", modbus.EncodeHex(frame))
	}
}

func (transport *Transport) logRaw(message string, frame []byte) {
	if transport.logger != nil && transport.trace != nil &&
		transport.trace.DriverTraceRawEnabled() {
		transport.logger.Info(message, "frame", modbus.EncodeHex(frame))
	}
}

func (transport *Transport) logInfo(message string, attrs ...any) {
	if transport.logger != nil {
		transport.logger.Info(message, attrs...)
	}
}

func resolveFirst(ctx context.Context, address string) (string, error) {
	if parsed := net.ParseIP(address); parsed != nil {
		return parsed.String(), nil
	}
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, address)
	if err != nil {
		return "", err
	}
	if len(addresses) == 0 {
		return "", fmt.Errorf("no addresses found")
	}
	return addresses[0].IP.String(), nil
}

func deadline(ctx context.Context, timeout time.Duration) time.Time {
	result := time.Now().Add(timeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(result) {
		return contextDeadline
	}
	return result
}

func waitContext(ctx context.Context, duration time.Duration) error {
	if duration <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
