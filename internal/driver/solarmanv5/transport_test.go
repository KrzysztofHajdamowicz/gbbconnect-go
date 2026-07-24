package solarmanv5

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strconv"
	"sync"
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
			request, readErr := readTestFrame(connection)
			if readErr != nil {
				serverErrors <- readErr
				return
			}
			response, responseErr := responseForRequest(request, testSerial)
			if responseErr != nil {
				serverErrors <- responseErr
				return
			}
			middle := len(response) / 2
			if _, writeErr := connection.Write(response[:middle]); writeErr != nil {
				serverErrors <- writeErr
				return
			}
			if _, writeErr := connection.Write(response[middle:]); writeErr != nil {
				serverErrors <- writeErr
				return
			}
		}
		serverErrors <- nil
	}()

	transport := newLoopbackTransport(t, listener)
	readRequest := modbus.BuildReadHoldingRegisters(1, 0x0204, 2)
	readResponse, err := transport.SendRTU(testContext(t), readRequest)
	if err != nil {
		t.Fatalf("read SendRTU() error = %v", err)
	}
	wantRead := modbus.AppendCRC([]byte{0x01, 0x03, 0x04, 0x00, 0x01, 0x00, 0x02})
	if !bytes.Equal(readResponse, wantRead) {
		t.Fatalf("read SendRTU() = %X, want %X", readResponse, wantRead)
	}

	writeRequest := modbus.BuildWriteMultipleRegisters(
		1,
		0x0010,
		[]byte{0x12, 0x34, 0x56, 0x78},
	)
	writeResponse, err := transport.SendRTU(testContext(t), writeRequest)
	if err != nil {
		t.Fatalf("write SendRTU() error = %v", err)
	}
	wantWrite := modbus.AppendCRC([]byte{0x01, 0x10, 0x00, 0x10, 0x00, 0x02})
	if !bytes.Equal(writeResponse, wantWrite) {
		t.Fatalf("write SendRTU() = %X, want %X", writeResponse, wantWrite)
	}

	if err := <-serverErrors; err != nil {
		t.Fatalf("loopback server error = %v", err)
	}
	if err := transport.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := transport.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}

func TestTransportResendsOnWrongSequence(t *testing.T) {
	t.Parallel()

	listener := listenTCP(t)
	requests := make(chan int, 1)
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

		first, err := readTestFrame(connection)
		if err != nil {
			serverErrors <- err
			return
		}
		wrong, err := responseForRequest(first, testSerial)
		if err != nil {
			serverErrors <- err
			return
		}
		wrong[5]++
		if _, err := connection.Write(wrong); err != nil {
			serverErrors <- err
			return
		}

		second, err := readTestFrame(connection)
		if err != nil {
			serverErrors <- err
			return
		}
		if !bytes.Equal(first, second) {
			serverErrors <- errors.New("sequence retry changed the request")
			return
		}
		correct, err := responseForRequest(second, testSerial)
		if err != nil {
			serverErrors <- err
			return
		}
		if _, err := connection.Write(correct); err != nil {
			serverErrors <- err
			return
		}
		requests <- 2
		serverErrors <- nil
	}()

	transport := newLoopbackTransport(t, listener)
	request := modbus.BuildReadHoldingRegisters(1, 0, 1)
	if _, err := transport.SendRTU(testContext(t), request); err != nil {
		t.Fatalf("SendRTU() error = %v", err)
	}
	if got := <-requests; got != 2 {
		t.Fatalf("server request count = %d, want 2", got)
	}
	if err := <-serverErrors; err != nil {
		t.Fatalf("loopback server error = %v", err)
	}
}

func TestTransportReconnectsAfterDroppedConnection(t *testing.T) {
	t.Parallel()

	listener := listenTCP(t)
	connections := make(chan int, 1)
	serverErrors := make(chan error, 1)
	go func() {
		firstConnection, err := listener.Accept()
		if err != nil {
			serverErrors <- err
			return
		}
		if _, err := readTestFrame(firstConnection); err != nil {
			serverErrors <- err
			return
		}
		if err := firstConnection.Close(); err != nil {
			serverErrors <- err
			return
		}

		secondConnection, err := listener.Accept()
		if err != nil {
			serverErrors <- err
			return
		}
		defer func() {
			_ = secondConnection.Close()
		}()
		request, err := readTestFrame(secondConnection)
		if err != nil {
			serverErrors <- err
			return
		}
		response, err := responseForRequest(request, testSerial)
		if err != nil {
			serverErrors <- err
			return
		}
		if _, err := secondConnection.Write(response); err != nil {
			serverErrors <- err
			return
		}
		connections <- 2
		serverErrors <- nil
	}()

	transport := newLoopbackTransport(t, listener)
	transport.retryDelay = 0
	request := modbus.BuildReadHoldingRegisters(1, 0, 1)
	if _, err := transport.SendRTU(testContext(t), request); err != nil {
		t.Fatalf("SendRTU() error = %v", err)
	}
	if got := <-connections; got != 2 {
		t.Fatalf("connection count = %d, want 2", got)
	}
	if err := <-serverErrors; err != nil {
		t.Fatalf("loopback server error = %v", err)
	}
}

func TestTransportTraceFlags(t *testing.T) {
	t.Parallel()

	logger := &traceLogger{}
	transport := New(config.Plant{}, logger)
	frame := []byte{0x01, 0x03}

	transport.logDecoded("decoded", frame)
	transport.logRaw("raw", frame)
	if got := logger.messageSnapshot(); len(got) != 0 {
		t.Fatalf("messages with trace disabled = %v", got)
	}

	logger.decoded = true
	transport.logDecoded("decoded", frame)
	transport.logRaw("raw", frame)
	if got := logger.messageSnapshot(); !equalStrings(got, []string{"decoded"}) {
		t.Fatalf("messages with decoded trace = %v", got)
	}

	logger.raw = true
	transport.logRaw("raw", frame)
	if got := logger.messageSnapshot(); !equalStrings(got, []string{"decoded", "raw"}) {
		t.Fatalf("messages with raw trace = %v", got)
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
			name: "missing address",
			plant: config.Plant{
				Port:   defaultPort,
				Serial: testSerial,
			},
			want: "Missing IP Address",
		},
		{
			name: "missing port",
			plant: config.Plant{
				Address: "127.0.0.1",
				Port:    -1,
				Serial:  testSerial,
			},
			want: "Missing Port Number",
		},
		{
			name: "missing serial",
			plant: config.Plant{
				Address: "127.0.0.1",
				Port:    defaultPort,
			},
			want: "Missing SerialNumber",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			transport := New(test.plant, nil)
			err := transport.Connect(context.Background())
			if err == nil || err.Error() != test.want {
				t.Fatalf("Connect() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestReadFrameReportsConnectionLost(t *testing.T) {
	t.Parallel()

	transport := New(
		config.Plant{Address: "127.0.0.1", Port: defaultPort, Serial: testSerial},
		nil,
	)
	transport.conn = eofConnection{}
	transport.timeout = time.Second
	_, err := transport.readFrameLocked(context.Background())
	if !errors.Is(err, ErrConnectionLost) {
		t.Fatalf("readFrameLocked() error = %v, want ErrConnectionLost", err)
	}
}

func TestTransportConnectContextAndExistingConnection(t *testing.T) {
	t.Parallel()

	transport := New(
		config.Plant{
			Address: "127.0.0.1",
			Port:    defaultPort,
			Serial:  testSerial,
		},
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

func TestTransportFrameIOErrors(t *testing.T) {
	t.Parallel()

	injected := errors.New("injected failure")
	newTransport := func(connection net.Conn) *Transport {
		transport := New(
			config.Plant{
				Address: "127.0.0.1",
				Port:    defaultPort,
				Serial:  testSerial,
			},
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
			reader: bytes.NewReader([]byte{startByte}),
		})
		_, err := transport.readFrameLocked(context.Background())
		if err == nil || err.Error() != "read SolarmanV5 header: unexpected EOF" {
			t.Fatalf("readFrameLocked() error = %v", err)
		}
	})
	t.Run("invalid length", func(t *testing.T) {
		header := []byte{startByte, 0xFF, 0xFF}
		transport := newTransport(&scriptedConnection{
			reader: bytes.NewReader(header),
		})
		_, err := transport.readFrameLocked(context.Background())
		if err == nil ||
			err.Error() != "invalid SolarmanV5 frame length: 65548" {
			t.Fatalf("readFrameLocked() error = %v", err)
		}
	})
	t.Run("partial body", func(t *testing.T) {
		header := []byte{startByte, 0x0D, 0x00}
		transport := newTransport(&scriptedConnection{
			reader: bytes.NewReader(append(header, 0x01)),
		})
		_, err := transport.readFrameLocked(context.Background())
		if err == nil || err.Error() != "read SolarmanV5 frame: unexpected EOF" {
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

type eofConnection struct{}

func (eofConnection) Read([]byte) (int, error)         { return 0, io.EOF }
func (eofConnection) Write(data []byte) (int, error)   { return len(data), nil }
func (eofConnection) Close() error                     { return nil }
func (eofConnection) LocalAddr() net.Addr              { return nil }
func (eofConnection) RemoteAddr() net.Addr             { return nil }
func (eofConnection) SetDeadline(time.Time) error      { return nil }
func (eofConnection) SetReadDeadline(time.Time) error  { return nil }
func (eofConnection) SetWriteDeadline(time.Time) error { return nil }

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
func (logger *traceLogger) DriverTraceEnabled() bool {
	return logger.decoded
}
func (logger *traceLogger) DriverTraceRawEnabled() bool {
	return logger.raw
}
func (logger *traceLogger) messageSnapshot() []string {
	logger.mu.Lock()
	defer logger.mu.Unlock()
	return append([]string(nil), logger.messages...)
}

func newLoopbackTransport(t *testing.T, listener *net.TCPListener) *Transport {
	t.Helper()

	port := listener.Addr().(*net.TCPAddr).Port
	transport := New(
		config.Plant{
			Address: "127.0.0.1",
			Port:    port,
			Serial:  testSerial,
		},
		nil,
	)
	transport.timeout = time.Second
	transport.retryDelay = 5 * time.Millisecond
	t.Cleanup(func() {
		_ = transport.Close()
		_ = listener.Close()
	})
	return transport
}

func listenTCP(t *testing.T) *net.TCPListener {
	t.Helper()

	listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	return listener
}

func readTestFrame(connection net.Conn) ([]byte, error) {
	header := make([]byte, 3)
	if _, err := io.ReadFull(connection, header); err != nil {
		return nil, err
	}
	length := int(binary.LittleEndian.Uint16(header[1:3])) + frameLengthOverhead
	frame := make([]byte, length)
	copy(frame, header)
	if _, err := io.ReadFull(connection, frame[3:]); err != nil {
		return nil, err
	}
	return frame, nil
}

func responseForRequest(request []byte, serial int64) ([]byte, error) {
	if len(request) < requestRTUOffset+trailerLength {
		return nil, errors.New("request wrapper too short")
	}
	rtu := request[requestRTUOffset : len(request)-trailerLength]
	if !modbus.ValidateCRC(rtu) {
		return nil, errors.New("request RTU has invalid CRC")
	}

	var response []byte
	switch rtu[1] {
	case 0x03:
		count := int(binary.BigEndian.Uint16(rtu[4:6]))
		payload := make([]byte, 3+count*2)
		payload[0] = rtu[0]
		payload[1] = rtu[1]
		payload[2] = byte(count * 2)
		for index := 0; index < count; index++ {
			binary.BigEndian.PutUint16(payload[3+index*2:], uint16(index+1))
		}
		response = modbus.AppendCRC(payload)
	case 0x10:
		response = modbus.AppendCRC(rtu[:6])
	default:
		return nil, errors.New("unsupported test function " + strconv.Itoa(int(rtu[1])))
	}
	return buildResponseFrame(request[5], serial, response), nil
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
