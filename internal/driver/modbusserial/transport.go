package modbusserial

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/config"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/driver/modbusrtutcp"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/logbuf"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/modbus"
	serial "go.bug.st/serial"
)

const maxFrameSize = 1024

var (
	// ErrTimeout reports that no more serial bytes arrived before the configured
	// response deadline.
	ErrTimeout = errors.New("serial response timeout")
	// ErrWrongCRC reports a complete response with an invalid Modbus CRC.
	ErrWrongCRC = errors.New("wrong Modbus RTU CRC")
)

type port interface {
	io.ReadWriteCloser
	Drain() error
	ResetInputBuffer() error
	SetReadTimeout(time.Duration) error
}

type openPortFunc func(device string, mode *serial.Mode) (port, error)

type traceControls interface {
	DriverTraceEnabled() bool
	DriverTraceRawEnabled() bool
}

// Transport implements Modbus RTU over a physical serial port.
type Transport struct {
	mu       sync.Mutex
	settings config.SerialPort
	logger   logbuf.Logger
	trace    traceControls
	port     port
	openPort openPortFunc
}

// New constructs a serial transport. The device is opened lazily.
func New(plant config.Plant, logger logbuf.Logger) *Transport {
	transport := &Transport{
		settings: plant.SerialPort,
		logger:   logger,
	}
	if controls, ok := logger.(traceControls); ok {
		transport.trace = controls
	}
	transport.openPort = func(device string, mode *serial.Mode) (port, error) {
		return serial.Open(device, mode)
	}
	return transport
}

// Connect opens and configures the serial port if needed.
func (transport *Transport) Connect(ctx context.Context) error {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	return transport.connectLocked(ctx)
}

func (transport *Transport) connectLocked(ctx context.Context) error {
	if transport.port != nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	mode, err := serialMode(transport.settings)
	if err != nil {
		return err
	}

	opened, err := transport.openPort(transport.settings.Device, mode)
	if err != nil {
		return fmt.Errorf("open serial device %q: %w", transport.settings.Device, err)
	}
	transport.port = opened
	return nil
}

// SendRTU flushes stale input, sends one complete RTU frame, and returns a
// length-aware, CRC-validated response.
func (transport *Transport) SendRTU(
	ctx context.Context,
	rtu []byte,
) ([]byte, error) {
	transport.mu.Lock()
	defer transport.mu.Unlock()

	if err := transport.connectLocked(ctx); err != nil {
		return nil, err
	}

	timeout := responseTimeout(
		transport.settings,
		expectedResponseLength(rtu),
	)
	if err := transport.port.SetReadTimeout(timeout); err != nil {
		return nil, fmt.Errorf("set serial read timeout: %w", err)
	}
	if err := transport.port.ResetInputBuffer(); err != nil {
		return nil, fmt.Errorf("flush serial input: %w", err)
	}

	transport.logDecoded("Send ModBus", rtu)
	transport.logRaw("Send serial RTU", rtu)
	if err := writeAll(transport.port, rtu); err != nil {
		transport.closeAfterError()
		return nil, fmt.Errorf("write serial RTU frame: %w", err)
	}
	if err := transport.port.Drain(); err != nil {
		transport.closeAfterError()
		return nil, fmt.Errorf("drain serial output: %w", err)
	}

	response, err := transport.readFrameLocked(ctx)
	if err != nil {
		transport.closeAfterError()
		return nil, err
	}
	if !modbus.ValidateCRC(response) {
		return nil, ErrWrongCRC
	}
	transport.logRaw("Received serial RTU", response)
	transport.logDecoded("Received ModBus", response)
	return response, nil
}

func (transport *Transport) readFrameLocked(ctx context.Context) ([]byte, error) {
	frame := make([]byte, 0, 256)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		expected, err := modbusrtutcp.ExpectedResponseLength(frame)
		if err != nil {
			return nil, err
		}
		if expected > 0 && len(frame) == expected {
			return frame, nil
		}

		readSize := 1
		if expected > 0 {
			readSize = expected - len(frame)
		} else if len(frame) < 2 {
			readSize = 2 - len(frame)
		}
		if readSize < 1 || len(frame)+readSize > maxFrameSize {
			return nil, fmt.Errorf("serial Modbus response exceeds %d bytes", maxFrameSize)
		}

		chunk := make([]byte, readSize)
		read, readErr := transport.port.Read(chunk)
		if read > 0 {
			frame = append(frame, chunk[:read]...)
		}
		if readErr != nil {
			return nil, fmt.Errorf("read serial RTU frame: %w", readErr)
		}
		if read == 0 {
			return nil, ErrTimeout
		}
	}
}

// Close closes the serial port and is safe to call repeatedly.
func (transport *Transport) Close() error {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	if transport.port == nil {
		return nil
	}
	err := transport.port.Close()
	transport.port = nil
	if err != nil {
		return fmt.Errorf("close serial device: %w", err)
	}
	return nil
}

func (transport *Transport) closeAfterError() {
	if transport.port != nil {
		_ = transport.port.Close()
		transport.port = nil
	}
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

func serialMode(settings config.SerialPort) (*serial.Mode, error) {
	if settings.Device == "" {
		return nil, fmt.Errorf("serial device is required")
	}
	if settings.Baud < 1 {
		return nil, fmt.Errorf("serial baud must be positive")
	}
	if settings.DataBits < 5 || settings.DataBits > 8 {
		return nil, fmt.Errorf("serial data bits must be between 5 and 8")
	}

	var parity serial.Parity
	switch settings.Parity {
	case config.ParityNone:
		parity = serial.NoParity
	case config.ParityEven:
		parity = serial.EvenParity
	case config.ParityOdd:
		parity = serial.OddParity
	default:
		return nil, fmt.Errorf("unsupported serial parity %q", settings.Parity)
	}

	var stopBits serial.StopBits
	switch settings.StopBits {
	case 1:
		stopBits = serial.OneStopBit
	case 2:
		stopBits = serial.TwoStopBits
	default:
		return nil, fmt.Errorf("serial stop bits must be 1 or 2")
	}

	return &serial.Mode{
		BaudRate: settings.Baud,
		DataBits: settings.DataBits,
		Parity:   parity,
		StopBits: stopBits,
	}, nil
}

func expectedResponseLength(request []byte) int {
	if len(request) < 2 {
		return 256
	}
	function := request[1]
	if function >= 5 && function != 23 {
		return 8
	}
	if len(request) >= 6 {
		registers := int(binary.BigEndian.Uint16(request[4:6]))
		length := 3 + registers*2 + 2
		if length <= maxFrameSize {
			return length
		}
	}
	return 256
}

func responseTimeout(settings config.SerialPort, length int) time.Duration {
	if settings.Baud < 1 {
		return 5 * time.Second
	}
	bitsPerCharacter := 1 + settings.DataBits + settings.StopBits
	if settings.Parity != config.ParityNone {
		bitsPerCharacter++
	}
	transmission := time.Duration(
		int64(length*bitsPerCharacter) * int64(time.Second) / int64(settings.Baud),
	)
	timeout := 100*time.Millisecond + 2*transmission
	if timeout < 250*time.Millisecond {
		return 250 * time.Millisecond
	}
	if timeout > 5*time.Second {
		return 5 * time.Second
	}
	return timeout
}

func writeAll(writer io.Writer, data []byte) error {
	written := 0
	for written < len(data) {
		count, err := writer.Write(data[written:])
		written += count
		if err != nil {
			return err
		}
		if count == 0 {
			return io.ErrUnexpectedEOF
		}
	}
	return nil
}
