package modbusserial

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"testing"
	"time"

	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/config"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/invertertest"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/modbus"
	serial "go.bug.st/serial"
)

func TestSharedHarnessReadWriteAndFaults(t *testing.T) {
	t.Parallel()

	t.Run("fragmented read and write", func(t *testing.T) {
		mock, transport := sharedSerialHarness(
			t,
			invertertest.ScenarioFragmented,
		)
		read, err := transport.SendRTU(
			context.Background(),
			modbus.BuildReadHoldingRegisters(1, 0x0204, 2),
		)
		if err != nil {
			t.Fatalf("read SendRTU() error = %v", err)
		}
		wantRead := modbus.AppendCRC([]byte{1, 3, 4, 0, 1, 0, 2})
		if !bytes.Equal(read, wantRead) {
			t.Fatalf("read SendRTU() = %X, want %X", read, wantRead)
		}

		write, err := transport.SendRTU(
			context.Background(),
			modbus.BuildWriteMultipleRegisters(
				1,
				0x0010,
				[]byte{0x12, 0x34},
			),
		)
		if err != nil {
			t.Fatalf("write SendRTU() error = %v", err)
		}
		wantWrite := modbus.AppendCRC([]byte{1, 16, 0, 16, 0, 1})
		if !bytes.Equal(write, wantWrite) {
			t.Fatalf("write SendRTU() = %X, want %X", write, wantWrite)
		}
		if mock.Requests() != 2 {
			t.Fatalf("mock handled %d requests, want 2", mock.Requests())
		}
	})

	t.Run("malformed CRC", func(t *testing.T) {
		_, transport := sharedSerialHarness(
			t,
			invertertest.ScenarioMalformed,
		)
		_, err := transport.SendRTU(
			context.Background(),
			modbus.BuildReadHoldingRegisters(1, 0, 1),
		)
		if !errors.Is(err, ErrWrongCRC) {
			t.Fatalf("SendRTU() error = %v, want ErrWrongCRC", err)
		}
	})
}

func sharedSerialHarness(
	t *testing.T,
	scenario invertertest.Scenario,
) (*invertertest.Serial, *Transport) {
	t.Helper()
	mock := invertertest.NewSerial(t, scenario)
	settings := config.DefaultSerialPort()
	settings.Device = "/dev/invertertest"
	transport := New(config.Plant{SerialPort: settings}, nil)
	transport.openPort = func(
		device string,
		mode *serial.Mode,
	) (port, error) {
		mock.CaptureOpen(device, mode)
		return mock, nil
	}
	t.Cleanup(func() {
		_ = transport.Close()
	})
	return mock, transport
}

func TestTransportReadRoundTripAndSettings(t *testing.T) {
	t.Parallel()

	request := modbus.BuildReadHoldingRegisters(1, 0x0204, 2)
	response := modbus.AppendCRC([]byte{1, 3, 4, 0, 1, 0, 2})
	fake := &fakePort{
		readChunks: [][]byte{
			response[:1],
			response[1:3],
			response[3:5],
			response[5:],
		},
		maxWrite: 3,
	}
	settings := config.SerialPort{
		Device:   "/dev/mock0",
		Baud:     9600,
		DataBits: 8,
		Parity:   config.ParityEven,
		StopBits: 2,
	}
	transport := newFakeTransport(settings, fake)

	got, err := transport.SendRTU(context.Background(), request)
	if err != nil {
		t.Fatalf("SendRTU() error = %v", err)
	}
	if !bytes.Equal(got, response) {
		t.Fatalf("SendRTU() = %X, want %X", got, response)
	}
	if !bytes.Equal(fake.written, request) {
		t.Fatalf("serial write = %X, want %X", fake.written, request)
	}
	if fake.resetCalls != 1 || fake.drainCalls != 1 {
		t.Fatalf(
			"ResetInputBuffer calls=%d Drain calls=%d, want 1 each",
			fake.resetCalls,
			fake.drainCalls,
		)
	}
	if fake.timeout != responseTimeout(settings, len(response)) {
		t.Fatalf(
			"read timeout = %v, want %v",
			fake.timeout,
			responseTimeout(settings, len(response)),
		)
	}

	wantMode := &serial.Mode{
		BaudRate: 9600,
		DataBits: 8,
		Parity:   serial.EvenParity,
		StopBits: serial.TwoStopBits,
	}
	if !reflect.DeepEqual(fake.mode, wantMode) {
		t.Fatalf("serial mode = %#v, want %#v", fake.mode, wantMode)
	}
	if fake.device != settings.Device {
		t.Fatalf("opened device = %q, want %q", fake.device, settings.Device)
	}
}

func TestTransportWriteRoundTrip(t *testing.T) {
	t.Parallel()

	request := modbus.BuildWriteMultipleRegisters(
		1,
		0x0010,
		[]byte{0x12, 0x34},
	)
	response := modbus.AppendCRC([]byte{1, 16, 0, 16, 0, 1})
	fake := &fakePort{readChunks: [][]byte{response}}
	transport := newFakeTransport(config.DefaultSerialPort(), fake)
	transport.settings.Device = "/dev/mock1"

	got, err := transport.SendRTU(context.Background(), request)
	if err != nil {
		t.Fatalf("SendRTU() error = %v", err)
	}
	if !bytes.Equal(got, response) {
		t.Fatalf("SendRTU() = %X, want %X", got, response)
	}
	if fake.timeout != 250*time.Millisecond {
		t.Fatalf("write response timeout = %v, want 250ms", fake.timeout)
	}
}

func TestTransportCRCAndTimeoutErrors(t *testing.T) {
	t.Parallel()

	request := modbus.BuildReadHoldingRegisters(1, 0, 1)
	badCRC := modbus.AppendCRC([]byte{1, 3, 2, 0, 1})
	badCRC[len(badCRC)-1] ^= 0xFF

	tests := []struct {
		name string
		port *fakePort
		want error
	}{
		{
			name: "CRC",
			port: &fakePort{readChunks: [][]byte{badCRC}},
			want: ErrWrongCRC,
		},
		{
			name: "timeout",
			port: &fakePort{},
			want: ErrTimeout,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			transport := newFakeTransport(config.DefaultSerialPort(), test.port)
			transport.settings.Device = "/dev/mock"
			got, err := transport.SendRTU(context.Background(), request)
			if got != nil {
				t.Fatalf("SendRTU() = %X, want nil", got)
			}
			if !errors.Is(err, test.want) {
				t.Fatalf("SendRTU() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestTransportFlushErrorPreventsWrite(t *testing.T) {
	t.Parallel()

	fake := &fakePort{resetErr: errors.New("flush failed")}
	transport := newFakeTransport(config.DefaultSerialPort(), fake)
	transport.settings.Device = "/dev/mock"

	_, err := transport.SendRTU(
		context.Background(),
		modbus.BuildReadHoldingRegisters(1, 0, 1),
	)
	if err == nil || err.Error() != "flush serial input: flush failed" {
		t.Fatalf("SendRTU() error = %v", err)
	}
	if len(fake.written) != 0 {
		t.Fatalf("serial write after flush failure = %X", fake.written)
	}
}

func TestSerialModeMappingsAndValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		parity config.Parity
		stop   int
		wantP  serial.Parity
		wantS  serial.StopBits
	}{
		{config.ParityNone, 1, serial.NoParity, serial.OneStopBit},
		{config.ParityEven, 2, serial.EvenParity, serial.TwoStopBits},
		{config.ParityOdd, 1, serial.OddParity, serial.OneStopBit},
	}
	for _, test := range tests {
		mode, err := serialMode(config.SerialPort{
			Device:   "/dev/mock",
			Baud:     19200,
			DataBits: 7,
			Parity:   test.parity,
			StopBits: test.stop,
		})
		if err != nil {
			t.Fatalf("serialMode(%q) error = %v", test.parity, err)
		}
		if mode.Parity != test.wantP || mode.StopBits != test.wantS {
			t.Fatalf(
				"serialMode(%q, %d) = parity %v stop %v",
				test.parity,
				test.stop,
				mode.Parity,
				mode.StopBits,
			)
		}
	}

	invalid := []config.SerialPort{
		{Baud: 9600, DataBits: 8, Parity: config.ParityNone, StopBits: 1},
		{Device: "x", DataBits: 8, Parity: config.ParityNone, StopBits: 1},
		{Device: "x", Baud: 9600, DataBits: 4, Parity: config.ParityNone, StopBits: 1},
		{Device: "x", Baud: 9600, DataBits: 8, Parity: "mark", StopBits: 1},
		{Device: "x", Baud: 9600, DataBits: 8, Parity: config.ParityNone, StopBits: 3},
	}
	for index, settings := range invalid {
		if _, err := serialMode(settings); err == nil {
			t.Fatalf("serialMode(invalid %d) error = nil", index)
		}
	}
}

func TestResponseTimeoutBounds(t *testing.T) {
	t.Parallel()

	defaults := config.DefaultSerialPort()
	if got := responseTimeout(defaults, 8); got != 250*time.Millisecond {
		t.Fatalf("short response timeout = %v, want 250ms", got)
	}
	if got := responseTimeout(defaults, 255); got != 631250*time.Microsecond {
		t.Fatalf("125-register timeout = %v, want 631.25ms", got)
	}
	slow := defaults
	slow.Baud = 300
	slow.Parity = config.ParityEven
	slow.StopBits = 2
	if got := responseTimeout(slow, maxFrameSize); got != 5*time.Second {
		t.Fatalf("slow response timeout = %v, want 5s", got)
	}
}

func TestTransportCloseIsIdempotent(t *testing.T) {
	t.Parallel()

	fake := &fakePort{}
	transport := newFakeTransport(config.DefaultSerialPort(), fake)
	transport.settings.Device = "/dev/mock"
	if err := transport.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if err := transport.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := transport.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if fake.closeCalls != 1 {
		t.Fatalf("port Close() calls = %d, want 1", fake.closeCalls)
	}
}

type fakePort struct {
	readChunks [][]byte
	written    []byte
	maxWrite   int
	timeout    time.Duration
	resetCalls int
	drainCalls int
	closeCalls int
	resetErr   error
	device     string
	mode       *serial.Mode
}

func (port *fakePort) Read(buffer []byte) (int, error) {
	if len(port.readChunks) == 0 {
		return 0, nil
	}
	chunk := port.readChunks[0]
	count := copy(buffer, chunk)
	if count == len(chunk) {
		port.readChunks = port.readChunks[1:]
	} else {
		port.readChunks[0] = chunk[count:]
	}
	return count, nil
}

func (port *fakePort) Write(data []byte) (int, error) {
	count := len(data)
	if port.maxWrite > 0 && count > port.maxWrite {
		count = port.maxWrite
	}
	port.written = append(port.written, data[:count]...)
	return count, nil
}

func (port *fakePort) Close() error {
	port.closeCalls++
	return nil
}

func (port *fakePort) Drain() error {
	port.drainCalls++
	return nil
}

func (port *fakePort) ResetInputBuffer() error {
	port.resetCalls++
	return port.resetErr
}

func (port *fakePort) SetReadTimeout(timeout time.Duration) error {
	port.timeout = timeout
	return nil
}

func newFakeTransport(settings config.SerialPort, fake *fakePort) *Transport {
	transport := New(config.Plant{SerialPort: settings}, nil)
	transport.openPort = func(device string, mode *serial.Mode) (port, error) {
		fake.device = device
		modeCopy := *mode
		fake.mode = &modeCopy
		return fake, nil
	}
	return transport
}

var _ port = (*fakePort)(nil)
var _ io.ReadWriteCloser = (*fakePort)(nil)
