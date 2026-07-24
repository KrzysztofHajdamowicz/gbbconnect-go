package modbustcp

import (
	"context"
	"encoding/binary"
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

// ErrConnectionLost matches the original zero-byte receive error.
//
//nolint:staticcheck // The compatibility contract requires this exact text.
var ErrConnectionLost = errors.New("Connection Lost (received 0 bytes)")

type traceControls interface {
	DriverTraceEnabled() bool
	DriverTraceRawEnabled() bool
}

type dialContextFunc func(
	ctx context.Context,
	network string,
	address string,
) (net.Conn, error)

// Transport implements Modbus TCP while preserving the original transaction
// ID byte order.
type Transport struct {
	mu          sync.Mutex
	address     string
	port        int
	logger      logbuf.Logger
	trace       traceControls
	conn        net.Conn
	sequence    sequenceGenerator
	dialContext dialContextFunc
	timeout     time.Duration
	retryDelay  time.Duration
}

// New constructs a Modbus TCP transport with lazy connection setup.
func New(plant config.Plant, logger logbuf.Logger) *Transport {
	port := plant.Port
	if port == 0 {
		port = defaultPort
	}
	transport := &Transport{
		address:    plant.Address,
		port:       port,
		logger:     logger,
		sequence:   newSequenceGenerator(),
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
		//nolint:staticcheck // The compatibility contract requires this exact text.
		return fmt.Errorf("GbbConnect2: Missing IP Address")
	}
	if transport.port < 1 || transport.port > 65535 {
		//nolint:staticcheck // The compatibility contract requires this exact text.
		return fmt.Errorf("GbbConnect2: Missing Port Number")
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
	return nil
}

// SendRTU sends one complete RTU frame as Modbus TCP and rebuilds the response
// CRC before returning it.
func (transport *Transport) SendRTU(
	ctx context.Context,
	rtu []byte,
) ([]byte, error) {
	transport.mu.Lock()
	defer transport.mu.Unlock()

	if err := transport.connectLocked(ctx); err != nil {
		return nil, err
	}

	transactionID := transport.sequence.next()
	request, err := BuildRequest(transactionID, rtu)
	if err != nil {
		return nil, err
	}
	transport.logDecoded("Send ModBus", rtu)
	transport.logRaw("Send ModBusTCP", request)

	response, err := transport.internalSendLocked(ctx, request)
	if err != nil {
		return nil, err
	}
	transport.logRaw("Received ModBusTCP", response)
	parsed, err := ParseResponse(transactionID, response)
	if err != nil {
		return nil, err
	}
	transport.logDecoded("Received ModBus", parsed)
	return parsed, nil
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
			return fmt.Errorf("write Modbus TCP frame: %w", err)
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

	header := make([]byte, mbapHeaderLength)
	read, err := io.ReadFull(transport.conn, header)
	if err != nil {
		if read == 0 && errors.Is(err, io.EOF) {
			return nil, ErrConnectionLost
		}
		return nil, fmt.Errorf("read Modbus TCP header: %w", err)
	}

	pduLength := int(binary.BigEndian.Uint16(header[4:6]))
	frameLength := mbapHeaderLength + pduLength
	if frameLength < mbapHeaderLength || frameLength > maxFrameSize {
		return nil, fmt.Errorf("invalid Modbus TCP frame length: %d", frameLength)
	}
	frame := make([]byte, frameLength)
	copy(frame, header)
	if _, err := io.ReadFull(transport.conn, frame[mbapHeaderLength:]); err != nil {
		return nil, fmt.Errorf("read Modbus TCP frame: %w", err)
	}
	return frame, nil
}

// Close closes the TCP connection and is safe to call repeatedly.
func (transport *Transport) Close() error {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	return transport.closeLocked()
}

func (transport *Transport) closeLocked() error {
	if transport.conn == nil {
		return nil
	}
	err := transport.conn.Close()
	transport.conn = nil
	if err != nil {
		return fmt.Errorf("close Modbus TCP connection: %w", err)
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
