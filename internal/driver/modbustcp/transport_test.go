package modbustcp

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/config"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/logbuf"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/modbus"
)

func TestTransportReadAndWriteRoundTrip(t *testing.T) {
	t.Parallel()

	listener := listenTCP(t)
	serverErrors := make(chan error, 1)
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			serverErrors <- err
			return
		}
		defer func() {
			_ = connection.Close()
		}()

		for range 2 {
			request, err := readTestFrame(connection)
			if err != nil {
				serverErrors <- err
				return
			}
			response, err := responseForRequest(request)
			if err != nil {
				serverErrors <- err
				return
			}
			middle := len(response) / 2
			if _, err := connection.Write(response[:middle]); err != nil {
				serverErrors <- err
				return
			}
			if _, err := connection.Write(response[middle:]); err != nil {
				serverErrors <- err
				return
			}
		}
		serverErrors <- nil
	}()

	transport := newLoopbackTransport(t, listener)
	readRequest := modbus.BuildReadHoldingRegisters(1, 0x009C, 1)
	readResponse, err := transport.SendRTU(testContext(t), readRequest)
	if err != nil {
		t.Fatalf("read SendRTU() error = %v", err)
	}
	wantRead := modbus.AppendCRC([]byte{0x01, 0x03, 0x02, 0x00, 0xFF})
	if !bytes.Equal(readResponse, wantRead) {
		t.Fatalf("read SendRTU() = %X, want %X", readResponse, wantRead)
	}

	writeRequest := modbus.BuildWriteMultipleRegisters(
		1,
		0x0010,
		[]byte{0x12, 0x34},
	)
	writeResponse, err := transport.SendRTU(testContext(t), writeRequest)
	if err != nil {
		t.Fatalf("write SendRTU() error = %v", err)
	}
	wantWrite := modbus.AppendCRC([]byte{0x01, 0x10, 0x00, 0x10, 0x00, 0x01})
	if !bytes.Equal(writeResponse, wantWrite) {
		t.Fatalf("write SendRTU() = %X, want %X", writeResponse, wantWrite)
	}
	if err := <-serverErrors; err != nil {
		t.Fatalf("loopback server error = %v", err)
	}
}

func TestTransportReconnectsAfterDroppedConnection(t *testing.T) {
	t.Parallel()

	listener := listenTCP(t)
	serverErrors := make(chan error, 1)
	go func() {
		first, err := listener.Accept()
		if err != nil {
			serverErrors <- err
			return
		}
		if _, err := readTestFrame(first); err != nil {
			serverErrors <- err
			return
		}
		if err := first.Close(); err != nil {
			serverErrors <- err
			return
		}

		second, err := listener.Accept()
		if err != nil {
			serverErrors <- err
			return
		}
		defer func() {
			_ = second.Close()
		}()
		request, err := readTestFrame(second)
		if err != nil {
			serverErrors <- err
			return
		}
		response, err := responseForRequest(request)
		if err != nil {
			serverErrors <- err
			return
		}
		if _, err := second.Write(response); err != nil {
			serverErrors <- err
			return
		}
		serverErrors <- nil
	}()

	transport := newLoopbackTransport(t, listener)
	transport.retryDelay = 0
	request := modbus.BuildReadHoldingRegisters(1, 0, 1)
	if _, err := transport.SendRTU(testContext(t), request); err != nil {
		t.Fatalf("SendRTU() error = %v", err)
	}
	if err := <-serverErrors; err != nil {
		t.Fatalf("loopback server error = %v", err)
	}
}

func TestTransportRethrowsAfterElevenAttempts(t *testing.T) {
	t.Parallel()

	transport := New(
		config.Plant{Address: "127.0.0.1", Port: defaultPort},
		nil,
	)
	transport.retryDelay = 0
	var dials atomic.Int32
	transport.dialContext = func(
		context.Context,
		string,
		string,
	) (net.Conn, error) {
		dials.Add(1)
		return writeErrorConnection{}, nil
	}

	request := modbus.BuildReadHoldingRegisters(1, 0, 1)
	_, err := transport.SendRTU(context.Background(), request)
	if err == nil || err.Error() != "write Modbus TCP frame: injected write error" {
		t.Fatalf("SendRTU() error = %v", err)
	}
	if got := dials.Load(); got != connectionAttempts {
		t.Fatalf("dial attempts = %d, want %d", got, connectionAttempts)
	}
}

func TestTransportTraceFlags(t *testing.T) {
	t.Parallel()

	logger := &traceLogger{}
	transport := New(config.Plant{}, logger)
	frame := []byte{0x01, 0x03}

	transport.logDecoded("decoded", frame)
	transport.logRaw("raw", frame)
	if got := logger.snapshot(); len(got) != 0 {
		t.Fatalf("messages with trace disabled = %v", got)
	}

	logger.decoded = true
	logger.raw = true
	transport.logDecoded("decoded", frame)
	transport.logRaw("raw", frame)
	if got := logger.snapshot(); !equalStrings(got, []string{"decoded", "raw"}) {
		t.Fatalf("messages with trace enabled = %v", got)
	}
}

type writeErrorConnection struct{}

func (writeErrorConnection) Read([]byte) (int, error) {
	return 0, io.EOF
}
func (writeErrorConnection) Write([]byte) (int, error) {
	return 0, errors.New("injected write error")
}
func (writeErrorConnection) Close() error                     { return nil }
func (writeErrorConnection) LocalAddr() net.Addr              { return nil }
func (writeErrorConnection) RemoteAddr() net.Addr             { return nil }
func (writeErrorConnection) SetDeadline(time.Time) error      { return nil }
func (writeErrorConnection) SetReadDeadline(time.Time) error  { return nil }
func (writeErrorConnection) SetWriteDeadline(time.Time) error { return nil }

type traceLogger struct {
	mu       sync.Mutex
	decoded  bool
	raw      bool
	messages []string
}

func (logger *traceLogger) Debug(string, ...any) {}
func (logger *traceLogger) Info(message string, _ ...any) {
	logger.mu.Lock()
	defer logger.mu.Unlock()
	logger.messages = append(logger.messages, message)
}
func (logger *traceLogger) Warn(string, ...any)  {}
func (logger *traceLogger) Error(string, ...any) {}
func (logger *traceLogger) With(...any) logbuf.Logger {
	return logger
}
func (logger *traceLogger) DriverTraceEnabled() bool    { return logger.decoded }
func (logger *traceLogger) DriverTraceRawEnabled() bool { return logger.raw }
func (logger *traceLogger) snapshot() []string {
	logger.mu.Lock()
	defer logger.mu.Unlock()
	return append([]string(nil), logger.messages...)
}

func listenTCP(t *testing.T) *net.TCPListener {
	t.Helper()
	listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = listener.Close()
	})
	return listener
}

func newLoopbackTransport(t *testing.T, listener *net.TCPListener) *Transport {
	t.Helper()
	transport := New(
		config.Plant{
			Address: "127.0.0.1",
			Port:    listener.Addr().(*net.TCPAddr).Port,
		},
		nil,
	)
	transport.timeout = time.Second
	transport.retryDelay = 5 * time.Millisecond
	t.Cleanup(func() {
		_ = transport.Close()
	})
	return transport
}

func readTestFrame(connection net.Conn) ([]byte, error) {
	header := make([]byte, mbapHeaderLength)
	if _, err := io.ReadFull(connection, header); err != nil {
		return nil, err
	}
	length := int(binary.BigEndian.Uint16(header[4:6]))
	frame := make([]byte, mbapHeaderLength+length)
	copy(frame, header)
	if _, err := io.ReadFull(connection, frame[mbapHeaderLength:]); err != nil {
		return nil, err
	}
	return frame, nil
}

func responseForRequest(request []byte) ([]byte, error) {
	if len(request) < 8 {
		return nil, errors.New("request too short")
	}
	var pdu []byte
	switch request[7] {
	case 0x03:
		pdu = []byte{request[6], 0x03, 0x02, 0x00, 0xFF}
	case 0x10:
		if len(request) < 12 {
			return nil, errors.New("write request too short")
		}
		pdu = bytes.Clone(request[6:12])
	default:
		return nil, errors.New("unsupported request function")
	}

	response := make([]byte, mbapHeaderLength+len(pdu))
	copy(response[0:2], request[0:2])
	binary.BigEndian.PutUint16(response[4:6], uint16(len(pdu)))
	copy(response[mbapHeaderLength:], pdu)
	return response, nil
}

func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
