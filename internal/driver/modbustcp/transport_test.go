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
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/invertertest"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/logbuf"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/modbus"
)

func TestSharedHarnessReadWriteAndFaults(t *testing.T) {
	t.Parallel()

	t.Run("fragmented read and write", func(t *testing.T) {
		transport := sharedHarnessTransport(
			t,
			invertertest.ScenarioFragmented,
		)
		assertHarnessReadWrite(t, transport)
	})

	for _, scenario := range []invertertest.Scenario{
		invertertest.ScenarioCloseOnce,
		invertertest.ScenarioShortResponseOnce,
	} {
		t.Run(string(scenario), func(t *testing.T) {
			transport := sharedHarnessTransport(t, scenario)
			got, err := transport.SendRTU(
				testContext(t),
				modbus.BuildReadHoldingRegisters(1, 0, 1),
			)
			if err != nil {
				t.Fatalf("SendRTU() error = %v", err)
			}
			want := modbus.AppendCRC([]byte{1, 3, 2, 0, 1})
			if !bytes.Equal(got, want) {
				t.Fatalf("SendRTU() = %X, want %X", got, want)
			}
		})
	}

	errorScenarios := []struct {
		scenario invertertest.Scenario
		want     string
	}{
		{
			scenario: invertertest.ScenarioWrongTransaction,
			want:     "ModBusTCP: Wrong TransactionId!",
		},
		{
			scenario: invertertest.ScenarioException,
			want:     "Error response: 2=Illegal Data Address",
		},
	}
	for _, test := range errorScenarios {
		t.Run(string(test.scenario), func(t *testing.T) {
			transport := sharedHarnessTransport(t, test.scenario)
			_, err := transport.SendRTU(
				testContext(t),
				modbus.BuildReadHoldingRegisters(1, 0, 1),
			)
			if err == nil || err.Error() != test.want {
				t.Fatalf("SendRTU() error = %v, want %q", err, test.want)
			}
		})
	}
}

func sharedHarnessTransport(
	t *testing.T,
	scenario invertertest.Scenario,
) *Transport {
	t.Helper()
	harness := invertertest.Start(
		t,
		invertertest.ProtocolModbusTCP,
		scenario,
	)
	transport := New(harness.Plant(), nil)
	transport.timeout = 100 * time.Millisecond
	transport.retryDelay = 0
	t.Cleanup(func() {
		_ = transport.Close()
	})
	return transport
}

func assertHarnessReadWrite(t *testing.T, transport *Transport) {
	t.Helper()
	read, err := transport.SendRTU(
		testContext(t),
		modbus.BuildReadHoldingRegisters(1, 0x009C, 2),
	)
	if err != nil {
		t.Fatalf("read SendRTU() error = %v", err)
	}
	wantRead := modbus.AppendCRC([]byte{1, 3, 4, 0, 1, 0, 2})
	if !bytes.Equal(read, wantRead) {
		t.Fatalf("read SendRTU() = %X, want %X", read, wantRead)
	}

	write, err := transport.SendRTU(
		testContext(t),
		modbus.BuildWriteMultipleRegisters(
			1,
			0x0010,
			[]byte{0x12, 0x34, 0x56, 0x78},
		),
	)
	if err != nil {
		t.Fatalf("write SendRTU() error = %v", err)
	}
	wantWrite := modbus.AppendCRC([]byte{1, 16, 0, 16, 0, 2})
	if !bytes.Equal(write, wantWrite) {
		t.Fatalf("write SendRTU() = %X, want %X", write, wantWrite)
	}
}

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

func TestTransportConnectContextAndExistingConnection(t *testing.T) {
	t.Parallel()

	transport := New(
		config.Plant{Address: "127.0.0.1", Port: defaultPort},
		nil,
	)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := transport.Connect(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Connect(canceled) error = %v, want context.Canceled", err)
	}

	transport.conn = &scriptedConnection{}
	if err := transport.Connect(context.Background()); err != nil {
		t.Fatalf("Connect(existing connection) error = %v", err)
	}
}

func TestTransportConstructionValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		plant config.Plant
		want  string
	}{
		{
			name:  "missing address",
			plant: config.Plant{Port: defaultPort},
			want:  "GbbConnect2: Missing IP Address",
		},
		{
			name: "invalid port",
			plant: config.Plant{
				Address: "127.0.0.1",
				Port:    -1,
			},
			want: "GbbConnect2: Missing Port Number",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := New(test.plant, nil).Connect(context.Background())
			if err == nil || err.Error() != test.want {
				t.Fatalf("Connect() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestTransportFrameIOErrors(t *testing.T) {
	t.Parallel()

	injected := errors.New("injected failure")
	newTransport := func(connection net.Conn) *Transport {
		transport := New(
			config.Plant{Address: "127.0.0.1", Port: defaultPort},
			nil,
		)
		transport.conn = connection
		transport.timeout = time.Second
		return transport
	}

	t.Run("read deadline", func(t *testing.T) {
		transport := newTransport(&scriptedConnection{
			readDeadlineError: injected,
		})
		_, err := transport.readFrameLocked(context.Background())
		if !errors.Is(err, injected) {
			t.Fatalf("readFrameLocked() error = %v", err)
		}
	})
	t.Run("partial header", func(t *testing.T) {
		transport := newTransport(&scriptedConnection{
			reader: bytes.NewReader([]byte{0x01}),
		})
		_, err := transport.readFrameLocked(context.Background())
		if err == nil || err.Error() != "read Modbus TCP header: unexpected EOF" {
			t.Fatalf("readFrameLocked() error = %v", err)
		}
	})
	t.Run("connection lost", func(t *testing.T) {
		transport := newTransport(&scriptedConnection{
			reader: bytes.NewReader(nil),
		})
		_, err := transport.readFrameLocked(context.Background())
		if !errors.Is(err, ErrConnectionLost) {
			t.Fatalf("readFrameLocked() error = %v, want ErrConnectionLost", err)
		}
	})
	t.Run("invalid length", func(t *testing.T) {
		header := make([]byte, mbapHeaderLength)
		binary.BigEndian.PutUint16(header[4:6], maxFrameSize)
		transport := newTransport(&scriptedConnection{
			reader: bytes.NewReader(header),
		})
		_, err := transport.readFrameLocked(context.Background())
		if err == nil || err.Error() != "invalid Modbus TCP frame length: 1030" {
			t.Fatalf("readFrameLocked() error = %v", err)
		}
	})
	t.Run("partial body", func(t *testing.T) {
		header := make([]byte, mbapHeaderLength)
		binary.BigEndian.PutUint16(header[4:6], 5)
		transport := newTransport(&scriptedConnection{
			reader: bytes.NewReader(append(header, 0x01)),
		})
		_, err := transport.readFrameLocked(context.Background())
		if err == nil || err.Error() != "read Modbus TCP frame: unexpected EOF" {
			t.Fatalf("readFrameLocked() error = %v", err)
		}
	})
	t.Run("write deadline", func(t *testing.T) {
		transport := newTransport(&scriptedConnection{
			writeDeadlineError: injected,
		})
		err := transport.writeFrameLocked(context.Background(), []byte{0x01})
		if !errors.Is(err, injected) {
			t.Fatalf("writeFrameLocked() error = %v", err)
		}
	})
	t.Run("zero-byte write", func(t *testing.T) {
		transport := newTransport(&scriptedConnection{
			writer: writerFunc(func([]byte) (int, error) { return 0, nil }),
		})
		err := transport.writeFrameLocked(context.Background(), []byte{0x01})
		if !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatalf("writeFrameLocked() error = %v, want unexpected EOF", err)
		}
	})
	t.Run("close", func(t *testing.T) {
		transport := newTransport(&scriptedConnection{closeError: injected})
		if err := transport.Close(); !errors.Is(err, injected) {
			t.Fatalf("Close() error = %v", err)
		}
		if transport.conn != nil {
			t.Fatal("Close() did not clear connection after an error")
		}
	})
}

func TestTransportTimingAndAddressHelpers(t *testing.T) {
	t.Parallel()

	if got, err := resolveFirst(context.Background(), "127.0.0.1"); err != nil ||
		got != "127.0.0.1" {
		t.Fatalf("resolveFirst() = %q, %v", got, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waitContext(ctx, time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("waitContext(canceled) error = %v", err)
	}
	if err := waitContext(context.Background(), 0); err != nil {
		t.Fatalf("waitContext(zero) error = %v", err)
	}
	if err := waitContext(context.Background(), time.Millisecond); err != nil {
		t.Fatalf("waitContext(timer) error = %v", err)
	}

	contextDeadline := time.Now().Add(time.Second)
	deadlineContext, stop := context.WithDeadline(
		context.Background(),
		contextDeadline,
	)
	defer stop()
	got := deadline(deadlineContext, time.Hour)
	if got.Sub(contextDeadline) > time.Millisecond ||
		contextDeadline.Sub(got) > time.Millisecond {
		t.Fatalf("deadline() = %v, want %v", got, contextDeadline)
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

type writerFunc func([]byte) (int, error)

func (write writerFunc) Write(data []byte) (int, error) {
	return write(data)
}

type scriptedConnection struct {
	reader             io.Reader
	writer             io.Writer
	closeError         error
	readDeadlineError  error
	writeDeadlineError error
}

func (connection *scriptedConnection) Read(data []byte) (int, error) {
	if connection.reader == nil {
		return 0, io.EOF
	}
	return connection.reader.Read(data)
}

func (connection *scriptedConnection) Write(data []byte) (int, error) {
	if connection.writer == nil {
		return len(data), nil
	}
	return connection.writer.Write(data)
}

func (connection *scriptedConnection) Close() error {
	return connection.closeError
}

func (*scriptedConnection) LocalAddr() net.Addr  { return nil }
func (*scriptedConnection) RemoteAddr() net.Addr { return nil }
func (*scriptedConnection) SetDeadline(time.Time) error {
	return nil
}
func (connection *scriptedConnection) SetReadDeadline(time.Time) error {
	return connection.readDeadlineError
}
func (connection *scriptedConnection) SetWriteDeadline(time.Time) error {
	return connection.writeDeadlineError
}

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
