// Package invertertest provides reusable inverter-side transport mocks.
package invertertest

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/config"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/modbus"
	serial "go.bug.st/serial"
)

// Protocol selects the device-side framing used by a mock.
type Protocol string

const (
	ProtocolSolarmanV5 Protocol = "solarman_v5"
	ProtocolModbusTCP  Protocol = "modbus_tcp"
	ProtocolRTUOverTCP Protocol = "modbus_rtu_tcp"
	ProtocolSerial     Protocol = "modbus_serial"
)

// Scenario selects deterministic transport behavior.
type Scenario string

const (
	ScenarioNormal            Scenario = "normal"
	ScenarioFragmented        Scenario = "fragmented"
	ScenarioCoalesced         Scenario = "coalesced"
	ScenarioCloseOnce         Scenario = "close_once"
	ScenarioShortResponseOnce Scenario = "short_response_once"
	ScenarioMalformed         Scenario = "malformed"
	ScenarioWrongSequenceOnce Scenario = "wrong_sequence_once"
	ScenarioWrongTransaction  Scenario = "wrong_transaction"
	ScenarioException         Scenario = "exception"
)

// DongleSerial is the fixed Solarman serial exposed by the network harness.
const DongleSerial int64 = 0x12345678

// RegistryEntry describes the scenarios supported for one transport.
type RegistryEntry struct {
	Protocol  Protocol
	Scenarios []Scenario
}

// Registry returns an independent list suitable for integration-test matrices.
func Registry() []RegistryEntry {
	entries := []RegistryEntry{
		{
			Protocol: ProtocolSolarmanV5,
			Scenarios: []Scenario{
				ScenarioNormal,
				ScenarioFragmented,
				ScenarioCloseOnce,
				ScenarioShortResponseOnce,
				ScenarioMalformed,
				ScenarioWrongSequenceOnce,
			},
		},
		{
			Protocol: ProtocolModbusTCP,
			Scenarios: []Scenario{
				ScenarioNormal,
				ScenarioFragmented,
				ScenarioCloseOnce,
				ScenarioShortResponseOnce,
				ScenarioMalformed,
				ScenarioWrongTransaction,
				ScenarioException,
			},
		},
		{
			Protocol: ProtocolRTUOverTCP,
			Scenarios: []Scenario{
				ScenarioNormal,
				ScenarioFragmented,
				ScenarioCoalesced,
				ScenarioCloseOnce,
				ScenarioShortResponseOnce,
				ScenarioMalformed,
			},
		},
		{
			Protocol: ProtocolSerial,
			Scenarios: []Scenario{
				ScenarioNormal,
				ScenarioFragmented,
				ScenarioMalformed,
			},
		},
	}
	for index := range entries {
		entries[index].Scenarios = append(
			[]Scenario(nil),
			entries[index].Scenarios...,
		)
	}
	return entries
}

// Harness is an ephemeral TCP inverter mock.
type Harness struct {
	protocol Protocol
	scenario Scenario
	listener *net.TCPListener

	mu          sync.Mutex
	connections map[net.Conn]struct{}
	faultUsed   bool
	err         error
	wg          sync.WaitGroup
}

// Start starts a network mock on an ephemeral loopback port.
func Start(t *testing.T, protocol Protocol, scenario Scenario) *Harness {
	t.Helper()
	if protocol == ProtocolSerial {
		t.Fatal("serial mock must be created with NewSerial")
	}
	if !supports(protocol, scenario) {
		t.Fatalf("scenario %q is not supported for %q", scenario, protocol)
	}

	listener, err := net.ListenTCP(
		"tcp",
		&net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)},
	)
	if err != nil {
		t.Fatalf("listen for %s inverter mock: %v", protocol, err)
	}
	harness := &Harness{
		protocol:    protocol,
		scenario:    scenario,
		listener:    listener,
		connections: make(map[net.Conn]struct{}),
	}
	harness.wg.Add(1)
	go harness.serve()
	t.Cleanup(func() {
		harness.close(t)
	})
	return harness
}

// Plant returns transport configuration pointing at the mock.
func (harness *Harness) Plant() config.Plant {
	driverType := config.DriverType(harness.protocol)
	return config.Plant{
		Driver:  driverType,
		Address: "127.0.0.1",
		Port:    harness.listener.Addr().(*net.TCPAddr).Port,
		Serial:  DongleSerial,
	}
}

func (harness *Harness) serve() {
	defer harness.wg.Done()
	for {
		connection, err := harness.listener.Accept()
		if err != nil {
			if !errors.Is(err, net.ErrClosed) {
				harness.recordError(err)
			}
			return
		}
		harness.mu.Lock()
		harness.connections[connection] = struct{}{}
		harness.mu.Unlock()

		err = harness.handleConnection(connection)
		harness.mu.Lock()
		delete(harness.connections, connection)
		harness.mu.Unlock()
		_ = connection.Close()
		if err != nil &&
			!errors.Is(err, io.EOF) &&
			!errors.Is(err, net.ErrClosed) {
			harness.recordError(err)
			return
		}
	}
}

func (harness *Harness) handleConnection(connection net.Conn) error {
	presend := 0
	for {
		_, metadata, err := harness.readRequest(connection)
		if err != nil {
			return err
		}
		response, err := buildRTUResponse(metadata.rtu)
		if err != nil {
			return err
		}

		if harness.takeOneShotFault(ScenarioCloseOnce) {
			return nil
		}
		wireResponse := harness.wrapResponse(metadata, response)
		if harness.takeOneShotFault(ScenarioShortResponseOnce) {
			_, err = connection.Write(wireResponse[:max(1, len(wireResponse)/2)])
			return err
		}

		if harness.scenario == ScenarioMalformed {
			corruptResponse(harness.protocol, wireResponse)
		}
		if harness.scenario == ScenarioWrongTransaction {
			wireResponse[0]++
		}
		if harness.takeOneShotFault(ScenarioWrongSequenceOnce) {
			wireResponse[5]++
		}
		if harness.scenario == ScenarioException {
			wireResponse = modbusTCPException(metadata.transactionID)
		}
		if presend > 0 {
			presend--
			continue
		}
		if harness.scenario == ScenarioCoalesced {
			next := modbus.AppendCRC([]byte{1, 0x10, 0, 0x10, 0, 1})
			wireResponse = append(wireResponse, next...)
			presend++
		}
		if harness.scenario == ScenarioFragmented && len(wireResponse) > 1 {
			middle := len(wireResponse) / 2
			if _, err := connection.Write(wireResponse[:middle]); err != nil {
				return err
			}
			if _, err := connection.Write(wireResponse[middle:]); err != nil {
				return err
			}
			continue
		}
		if _, err := connection.Write(wireResponse); err != nil {
			return err
		}
	}
}

type requestMetadata struct {
	rtu           []byte
	sequence      byte
	serial        uint32
	transactionID uint16
}

func (harness *Harness) readRequest(
	connection net.Conn,
) ([]byte, requestMetadata, error) {
	switch harness.protocol {
	case ProtocolSolarmanV5:
		header := make([]byte, 3)
		if _, err := io.ReadFull(connection, header); err != nil {
			return nil, requestMetadata{}, err
		}
		length := int(binary.LittleEndian.Uint16(header[1:3])) + 13
		if length < 28 || length > 1024 {
			return nil, requestMetadata{}, fmt.Errorf(
				"invalid Solarman request length: %d",
				length,
			)
		}
		frame := make([]byte, length)
		copy(frame, header)
		if _, err := io.ReadFull(connection, frame[3:]); err != nil {
			return nil, requestMetadata{}, err
		}
		if frame[0] != 0xA5 || frame[3] != 0x10 || frame[4] != 0x45 ||
			frame[11] != 0x02 || frame[len(frame)-1] != 0x15 {
			return nil, requestMetadata{}, errors.New(
				"invalid Solarman request wrapper",
			)
		}
		rtu := bytes.Clone(frame[26 : len(frame)-2])
		if !modbus.ValidateCRC(rtu) {
			return nil, requestMetadata{}, errors.New(
				"solarman request has invalid RTU CRC",
			)
		}
		return frame, requestMetadata{
			rtu:      rtu,
			sequence: frame[5],
			serial:   binary.LittleEndian.Uint32(frame[7:11]),
		}, nil

	case ProtocolModbusTCP:
		header := make([]byte, 6)
		if _, err := io.ReadFull(connection, header); err != nil {
			return nil, requestMetadata{}, err
		}
		length := int(binary.BigEndian.Uint16(header[4:6]))
		if length < 2 || length > 1024 {
			return nil, requestMetadata{}, fmt.Errorf(
				"invalid Modbus TCP request length: %d",
				length,
			)
		}
		frame := make([]byte, 6+length)
		copy(frame, header)
		if _, err := io.ReadFull(connection, frame[6:]); err != nil {
			return nil, requestMetadata{}, err
		}
		rtu := modbus.AppendCRC(frame[6:])
		return frame, requestMetadata{
			rtu:           rtu,
			transactionID: binary.LittleEndian.Uint16(frame[0:2]),
		}, nil

	case ProtocolRTUOverTCP:
		rtu, err := readRTURequest(connection)
		return rtu, requestMetadata{rtu: rtu}, err
	default:
		return nil, requestMetadata{}, fmt.Errorf(
			"unsupported inverter mock protocol %q",
			harness.protocol,
		)
	}
}

func (harness *Harness) wrapResponse(
	metadata requestMetadata,
	rtu []byte,
) []byte {
	switch harness.protocol {
	case ProtocolSolarmanV5:
		frame := make([]byte, 25+len(rtu)+2)
		frame[0] = 0xA5
		binary.LittleEndian.PutUint16(frame[1:3], uint16(len(frame)-13))
		frame[3] = 0x10
		frame[4] = 0x15
		frame[5] = metadata.sequence
		binary.LittleEndian.PutUint32(frame[7:11], metadata.serial)
		frame[11] = 0x02
		copy(frame[25:], rtu)
		frame[len(frame)-2] = checksum(frame[1 : len(frame)-2])
		frame[len(frame)-1] = 0x15
		return frame
	case ProtocolModbusTCP:
		pdu := rtu[:len(rtu)-2]
		frame := make([]byte, 6+len(pdu))
		binary.LittleEndian.PutUint16(frame[0:2], metadata.transactionID)
		binary.BigEndian.PutUint16(frame[4:6], uint16(len(pdu)))
		copy(frame[6:], pdu)
		return frame
	default:
		return bytes.Clone(rtu)
	}
}

func (harness *Harness) takeOneShotFault(scenario Scenario) bool {
	harness.mu.Lock()
	defer harness.mu.Unlock()
	if harness.scenario != scenario || harness.faultUsed {
		return false
	}
	harness.faultUsed = true
	return true
}

func (harness *Harness) recordError(err error) {
	harness.mu.Lock()
	defer harness.mu.Unlock()
	if harness.err == nil {
		harness.err = err
	}
}

func (harness *Harness) close(t *testing.T) {
	t.Helper()
	_ = harness.listener.Close()
	harness.mu.Lock()
	for connection := range harness.connections {
		_ = connection.Close()
	}
	harness.mu.Unlock()

	done := make(chan struct{})
	go func() {
		harness.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("timed out stopping inverter mock")
	}

	harness.mu.Lock()
	defer harness.mu.Unlock()
	if harness.err != nil {
		t.Errorf("%s inverter mock error: %v", harness.protocol, harness.err)
	}
}

func supports(protocol Protocol, scenario Scenario) bool {
	for _, entry := range Registry() {
		if entry.Protocol != protocol {
			continue
		}
		for _, candidate := range entry.Scenarios {
			if candidate == scenario {
				return true
			}
		}
	}
	return false
}

func readRTURequest(reader io.Reader) ([]byte, error) {
	header := make([]byte, 2)
	if _, err := io.ReadFull(reader, header); err != nil {
		return nil, err
	}
	var length int
	switch header[1] {
	case 0x03:
		length = 8
	case 0x10:
		body := make([]byte, 5)
		if _, err := io.ReadFull(reader, body); err != nil {
			return nil, err
		}
		length = 9 + int(body[4])
		frame := make([]byte, length)
		copy(frame, header)
		copy(frame[2:], body)
		if _, err := io.ReadFull(reader, frame[7:]); err != nil {
			return nil, err
		}
		if !modbus.ValidateCRC(frame) {
			return nil, errors.New("request has invalid Modbus RTU CRC")
		}
		return frame, nil
	default:
		return nil, fmt.Errorf(
			"unsupported Modbus function 0x%02X",
			header[1],
		)
	}
	frame := make([]byte, length)
	copy(frame, header)
	if _, err := io.ReadFull(reader, frame[2:]); err != nil {
		return nil, err
	}
	if !modbus.ValidateCRC(frame) {
		return nil, errors.New("request has invalid Modbus RTU CRC")
	}
	return frame, nil
}

func buildRTUResponse(request []byte) ([]byte, error) {
	if !modbus.ValidateCRC(request) || len(request) < 6 {
		return nil, errors.New("invalid Modbus RTU request")
	}
	switch request[1] {
	case 0x03:
		count := int(binary.BigEndian.Uint16(request[4:6]))
		payload := make([]byte, 3+count*2)
		payload[0] = request[0]
		payload[1] = request[1]
		payload[2] = byte(count * 2)
		for index := range count {
			binary.BigEndian.PutUint16(
				payload[3+index*2:],
				uint16(index+1),
			)
		}
		return modbus.AppendCRC(payload), nil
	case 0x10:
		return modbus.AppendCRC(request[:6]), nil
	default:
		return nil, fmt.Errorf(
			"unsupported Modbus function 0x%02X",
			request[1],
		)
	}
}

func corruptResponse(protocol Protocol, response []byte) {
	switch protocol {
	case ProtocolSolarmanV5:
		response[4] = 0
	case ProtocolModbusTCP:
		binary.BigEndian.PutUint16(response[4:6], uint16(len(response)))
	default:
		response[len(response)-1] ^= 0xFF
	}
}

func modbusTCPException(transactionID uint16) []byte {
	response := make([]byte, 10)
	binary.LittleEndian.PutUint16(response[0:2], transactionID)
	binary.BigEndian.PutUint16(response[4:6], 4)
	response[8] = 0x83
	response[9] = 0x02
	return response
}

func checksum(data []byte) byte {
	var result byte
	for _, value := range data {
		result += value
	}
	return result
}

// Serial is an in-memory implementation of the serial transport's port
// contract.
type Serial struct {
	scenario Scenario

	mu            sync.Mutex
	request       []byte
	response      []byte
	maxRead       int
	closed        bool
	mode          *serial.Mode
	device        string
	readTimeout   time.Duration
	resetCalls    int
	drainCalls    int
	completeCount int
}

// NewSerial creates a serial mock selected from the shared registry.
func NewSerial(t *testing.T, scenario Scenario) *Serial {
	t.Helper()
	if !supports(ProtocolSerial, scenario) {
		t.Fatalf("scenario %q is not supported for serial", scenario)
	}
	mock := &Serial{scenario: scenario}
	if scenario == ScenarioFragmented {
		mock.maxRead = 2
	}
	return mock
}

// CaptureOpen records the settings passed to a serial open seam.
func (mock *Serial) CaptureOpen(device string, mode *serial.Mode) {
	mock.mu.Lock()
	defer mock.mu.Unlock()
	mock.device = device
	modeCopy := *mode
	mock.mode = &modeCopy
}

// Read implements io.Reader.
func (mock *Serial) Read(buffer []byte) (int, error) {
	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.response) == 0 {
		return 0, nil
	}
	count := len(mock.response)
	if mock.maxRead > 0 && count > mock.maxRead {
		count = mock.maxRead
	}
	if count > len(buffer) {
		count = len(buffer)
	}
	copy(buffer, mock.response[:count])
	mock.response = mock.response[count:]
	return count, nil
}

// Write implements io.Writer and generates a response after a complete RTU
// request arrives.
func (mock *Serial) Write(data []byte) (int, error) {
	mock.mu.Lock()
	defer mock.mu.Unlock()
	if mock.closed {
		return 0, net.ErrClosed
	}
	mock.request = append(mock.request, data...)
	expected, complete := expectedRTURequestLength(mock.request)
	if complete && len(mock.request) >= expected {
		response, err := buildRTUResponse(mock.request[:expected])
		if err != nil {
			return 0, err
		}
		if mock.scenario == ScenarioMalformed {
			response[len(response)-1] ^= 0xFF
		}
		mock.response = append(mock.response, response...)
		mock.request = mock.request[expected:]
		mock.completeCount++
	}
	return len(data), nil
}

// Close implements io.Closer.
func (mock *Serial) Close() error {
	mock.mu.Lock()
	defer mock.mu.Unlock()
	mock.closed = true
	return nil
}

// Drain implements the serial port contract.
func (mock *Serial) Drain() error {
	mock.mu.Lock()
	defer mock.mu.Unlock()
	mock.drainCalls++
	return nil
}

// ResetInputBuffer implements the serial port contract.
func (mock *Serial) ResetInputBuffer() error {
	mock.mu.Lock()
	defer mock.mu.Unlock()
	mock.response = nil
	mock.resetCalls++
	return nil
}

// SetReadTimeout implements the serial port contract.
func (mock *Serial) SetReadTimeout(timeout time.Duration) error {
	mock.mu.Lock()
	defer mock.mu.Unlock()
	mock.readTimeout = timeout
	return nil
}

// Requests returns the number of complete requests handled by the mock.
func (mock *Serial) Requests() int {
	mock.mu.Lock()
	defer mock.mu.Unlock()
	return mock.completeCount
}

func expectedRTURequestLength(data []byte) (int, bool) {
	if len(data) < 2 {
		return 0, false
	}
	switch data[1] {
	case 0x03:
		return 8, true
	case 0x10:
		if len(data) < 7 {
			return 0, false
		}
		return 9 + int(data[6]), true
	default:
		return 0, false
	}
}

var _ io.ReadWriteCloser = (*Serial)(nil)
