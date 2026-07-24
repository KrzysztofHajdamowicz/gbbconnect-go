package cloud

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/config"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/driver"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/logbuf"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/protocol"
)

func TestRequestHandlerHappyPath(t *testing.T) {
	t.Parallel()

	publisher := &fakeResponsePublisher{}
	inverterDriver := &fakeHandlerDriver{
		responses: [][]byte{
			{0x01, 0x03, 0x02, 0x00, 0x2A, 0x39, 0x9B},
			{0x01, 0x10, 0x00, 0x94, 0x00, 0x01, 0x01, 0xED},
		},
	}
	handler := mustRequestHandler(t, publisher, func(
		config.Plant,
		logbuf.Logger,
	) (driver.Driver, error) {
		return inverterDriver, nil
	})

	payload := []byte(`{
		"OrderId":"batch-1",
		"Lines":[
			{"LineNo":0,"Timestamp":1784761247,"Modbus":"010300000001840a"},
			{"LineNo":1,"Tag":"write","Modbus":null},
			{"LineNo":2,"Modbus":"0110009400010200012f5c"}
		]
	}`)
	if err := handler.Handle(context.Background(), payload); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	if inverterDriver.connectCalls != 1 {
		t.Errorf("Connect() calls = %d, want 1", inverterDriver.connectCalls)
	}
	if inverterDriver.closeCalls != 1 {
		t.Errorf("Close() calls = %d, want 1", inverterDriver.closeCalls)
	}
	if len(inverterDriver.requests) != 2 {
		t.Fatalf("driver request count = %d, want 2", len(inverterDriver.requests))
	}
	if !bytes.Equal(inverterDriver.requests[0], []byte{
		0x01, 0x03, 0x00, 0x00, 0x00, 0x01, 0x84, 0x0A,
	}) {
		t.Errorf("request 0 = %X", inverterDriver.requests[0])
	}

	message := publisher.singleMessage(t)
	if message.topic != FromDeviceTopic(testPlantID) {
		t.Errorf("publish topic = %q", message.topic)
	}
	if message.qos != 2 {
		t.Errorf("publish QoS = %d, want 2", message.qos)
	}
	response := decodePublishedHeader(t, message.payload)
	if response.OrderID == nil || *response.OrderID != "batch-1" {
		t.Errorf("response OrderID = %v", response.OrderID)
	}
	if response.GBBVersion == nil || *response.GBBVersion != "1.3.0-go" {
		t.Errorf("response GBBVersion = %v", response.GBBVersion)
	}
	if response.GBBEnvironment == nil || *response.GBBEnvironment != "Test" {
		t.Errorf("response GBBEnvironment = %v", response.GBBEnvironment)
	}
	if got := dereference(response.Lines[0].Modbus); got != "010302002A399B" {
		t.Errorf("line 0 Modbus = %q", got)
	}
	if response.Lines[1].Modbus != nil || dereference(response.Lines[1].Tag) != "write" {
		t.Errorf("line 1 = %+v", response.Lines[1])
	}
	if got := dereference(response.Lines[2].Modbus); got != "01100094000101ED" {
		t.Errorf("line 2 Modbus = %q", got)
	}
}

func TestRequestHandlerCascadesLineFailure(t *testing.T) {
	t.Parallel()

	publisher := &fakeResponsePublisher{}
	wantErr := errors.New("line two failed")
	inverterDriver := &fakeHandlerDriver{
		responses: [][]byte{{0x01, 0x03, 0x02, 0x00, 0x01, 0x79, 0x84}},
		sendErrAt: 1,
		sendErr:   wantErr,
	}
	handler := mustRequestHandler(t, publisher, func(
		config.Plant,
		logbuf.Logger,
	) (driver.Driver, error) {
		return inverterDriver, nil
	})

	if err := handler.Handle(context.Background(), threeLinePayload()); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	published := publisher.singleMessage(t).payload
	if bytes.Contains(published, []byte(`"Modbus":null`)) {
		t.Errorf("response contains explicit null Modbus: %s", published)
	}
	response := decodePublishedHeader(t, published)
	if got := dereference(response.Lines[0].Modbus); got != "01030200017984" {
		t.Errorf("line 0 Modbus = %q", got)
	}
	if response.Lines[0].Error != nil {
		t.Errorf("line 0 Error = %v", response.Lines[0].Error)
	}
	if got := dereference(response.Lines[1].Error); got != wantErr.Error() {
		t.Errorf("line 1 Error = %q", got)
	}
	if response.Lines[1].Modbus != nil || response.Lines[2].Modbus != nil {
		t.Errorf("cascaded Modbus values = %v / %v", response.Lines[1].Modbus, response.Lines[2].Modbus)
	}
	if len(inverterDriver.requests) != 2 {
		t.Errorf("driver request count = %d, want 2", len(inverterDriver.requests))
	}
	if inverterDriver.closeCalls != 1 {
		t.Errorf("Close() calls = %d, want 1", inverterDriver.closeCalls)
	}
}

func TestRequestHandlerCascadesHexDecodeFailure(t *testing.T) {
	t.Parallel()

	publisher := &fakeResponsePublisher{}
	inverterDriver := &fakeHandlerDriver{
		responses: [][]byte{{0x01, 0x03, 0x02, 0x00, 0x01, 0x79, 0x84}},
	}
	handler := mustRequestHandler(t, publisher, func(
		config.Plant,
		logbuf.Logger,
	) (driver.Driver, error) {
		return inverterDriver, nil
	})

	payload := []byte(`{"Lines":[
		{"LineNo":0,"Modbus":"010300000001840A"},
		{"LineNo":1,"Modbus":"GG"},
		{"LineNo":2,"Modbus":"01030002000125CA"}
	]}`)
	if err := handler.Handle(context.Background(), payload); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	response := decodePublishedHeader(t, publisher.singleMessage(t).payload)
	if response.Lines[1].Error == nil ||
		!stringsContain(*response.Lines[1].Error, "decode hex string") {
		t.Errorf("line 1 Error = %v", response.Lines[1].Error)
	}
	if response.Lines[1].Modbus != nil || response.Lines[2].Modbus != nil {
		t.Error("hex error did not clear this and subsequent Modbus values")
	}
	if len(inverterDriver.requests) != 1 {
		t.Errorf("driver request count = %d, want 1", len(inverterDriver.requests))
	}
}

func TestRequestHandlerGlobalFactoryFailure(t *testing.T) {
	t.Parallel()

	publisher := &fakeResponsePublisher{}
	wantErr := errors.New("create driver failed")
	handler := mustRequestHandler(t, publisher, func(
		config.Plant,
		logbuf.Logger,
	) (driver.Driver, error) {
		return nil, wantErr
	})

	if err := handler.Handle(context.Background(), threeLinePayload()); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	response := decodePublishedHeader(t, publisher.singleMessage(t).payload)
	if got := dereference(response.Error); got != wantErr.Error() {
		t.Errorf("Header.Error = %q", got)
	}
	for index := range response.Lines {
		if response.Lines[index].Modbus != nil {
			t.Errorf("line %d Modbus = %v, want nil", index, response.Lines[index].Modbus)
		}
	}
}

func TestRequestHandlerTreatsNilDriverAsGlobalFailure(t *testing.T) {
	t.Parallel()

	publisher := &fakeResponsePublisher{}
	handler := mustRequestHandler(t, publisher, func(
		config.Plant,
		logbuf.Logger,
	) (driver.Driver, error) {
		return nil, nil
	})

	if err := handler.Handle(context.Background(), singleLinePayload()); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	response := decodePublishedHeader(t, publisher.singleMessage(t).payload)
	if got := dereference(response.Error); got != "driver factory returned nil" {
		t.Errorf("Header.Error = %q", got)
	}
	if response.Lines[0].Modbus != nil {
		t.Errorf("line Modbus = %v, want nil", response.Lines[0].Modbus)
	}
}

func TestRequestHandlerGlobalConnectFailureClosesDriver(t *testing.T) {
	t.Parallel()

	publisher := &fakeResponsePublisher{}
	wantErr := errors.New("connect driver failed")
	inverterDriver := &fakeHandlerDriver{connectErr: wantErr}
	handler := mustRequestHandler(t, publisher, func(
		config.Plant,
		logbuf.Logger,
	) (driver.Driver, error) {
		return inverterDriver, nil
	})

	if err := handler.Handle(context.Background(), threeLinePayload()); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	response := decodePublishedHeader(t, publisher.singleMessage(t).payload)
	if got := dereference(response.Error); got != wantErr.Error() {
		t.Errorf("Header.Error = %q", got)
	}
	if len(inverterDriver.requests) != 0 {
		t.Errorf("driver request count = %d, want 0", len(inverterDriver.requests))
	}
	if inverterDriver.closeCalls != 1 {
		t.Errorf("Close() calls = %d, want 1", inverterDriver.closeCalls)
	}
}

func TestRequestHandlerIgnoresNilPayloadAndRejectsMalformedJSON(t *testing.T) {
	t.Parallel()

	publisher := &fakeResponsePublisher{}
	factoryCalls := 0
	handler := mustRequestHandler(t, publisher, func(
		config.Plant,
		logbuf.Logger,
	) (driver.Driver, error) {
		factoryCalls++
		return &fakeHandlerDriver{}, nil
	})

	for _, payload := range [][]byte{nil, []byte(" "), []byte("null")} {
		if err := handler.Handle(context.Background(), payload); err != nil {
			t.Errorf("Handle(%q) error = %v", payload, err)
		}
	}
	if factoryCalls != 0 || publisher.messageCount() != 0 {
		t.Errorf("nil payload performed factory=%d publish=%d", factoryCalls, publisher.messageCount())
	}
	if err := handler.Handle(context.Background(), []byte(`{"Lines":[}`)); err == nil {
		t.Fatal("Handle(malformed) error = nil")
	}
	if factoryCalls != 0 || publisher.messageCount() != 0 {
		t.Errorf("malformed payload performed factory=%d publish=%d", factoryCalls, publisher.messageCount())
	}
}

func TestRequestHandlerReturnsPublishFailure(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("publish failed")
	publisher := &fakeResponsePublisher{err: wantErr}
	inverterDriver := &fakeHandlerDriver{}
	handler := mustRequestHandler(t, publisher, func(
		config.Plant,
		logbuf.Logger,
	) (driver.Driver, error) {
		return inverterDriver, nil
	})

	if err := handler.Handle(context.Background(), []byte(`{}`)); !errors.Is(err, wantErr) {
		t.Fatalf("Handle() error = %v, want publish error", err)
	}
	if inverterDriver.closeCalls != 1 {
		t.Errorf("Close() calls = %d, want 1", inverterDriver.closeCalls)
	}
}

func TestRequestHandlerSerializesConcurrentMessagesAndQueuedCancellation(t *testing.T) {
	t.Parallel()

	publisher := &fakeResponsePublisher{}
	entered := make(chan struct{})
	release := make(chan struct{})
	firstDriver := &fakeHandlerDriver{
		send: func(ctx context.Context, _ []byte) ([]byte, error) {
			close(entered)
			select {
			case <-release:
				return []byte{0x01, 0x03}, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	}
	factoryCalls := 0
	var factoryMu sync.Mutex
	handler := mustRequestHandler(t, publisher, func(
		config.Plant,
		logbuf.Logger,
	) (driver.Driver, error) {
		factoryMu.Lock()
		defer factoryMu.Unlock()
		factoryCalls++
		return firstDriver, nil
	})

	firstDone := make(chan error, 1)
	go func() {
		firstDone <- handler.Handle(context.Background(), singleLinePayload())
	}()
	<-entered

	queuedCtx, cancelQueued := context.WithCancel(context.Background())
	secondDone := make(chan error, 1)
	go func() {
		secondDone <- handler.Handle(queuedCtx, singleLinePayload())
	}()
	cancelQueued()

	select {
	case err := <-secondDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("queued Handle() error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("queued Handle() did not honor cancellation")
	}
	factoryMu.Lock()
	gotFactoryCalls := factoryCalls
	factoryMu.Unlock()
	if gotFactoryCalls != 1 {
		t.Errorf("factory calls while first request active = %d, want 1", gotFactoryCalls)
	}

	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first Handle() error = %v", err)
	}
}

func TestNewRequestHandlerValidationAndDefaults(t *testing.T) {
	t.Parallel()

	plant := testHandlerPlant()
	publisher := &fakeResponsePublisher{}
	if _, err := NewRequestHandler(
		config.Plant{},
		publisher,
		nil,
		HandlerOptions{},
	); err == nil {
		t.Fatal("NewRequestHandler() empty PlantID error = nil")
	}
	if _, err := NewRequestHandler(
		plant,
		nil,
		nil,
		HandlerOptions{},
	); err == nil {
		t.Fatal("NewRequestHandler() nil publisher error = nil")
	}

	handler, err := NewRequestHandler(plant, publisher, nil, HandlerOptions{
		DriverFactory: func(config.Plant, logbuf.Logger) (driver.Driver, error) {
			return &fakeHandlerDriver{}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewRequestHandler() error = %v", err)
	}
	if handler.version != "dev" {
		t.Errorf("default version = %q, want dev", handler.version)
	}
	if handler.environment == "" {
		t.Error("default environment is empty")
	}
	for goos, want := range map[string]string{
		"linux":   "Linux",
		"windows": "Windows",
		"darwin":  "Darwin",
		"":        "gbbconnect-go",
	} {
		if got := defaultEnvironment(goos); got != want {
			t.Errorf("defaultEnvironment(%q) = %q, want %q", goos, got, want)
		}
	}
	//nolint:staticcheck // Exercise the public API's defensive nil-context guard.
	if err := handler.Handle(nil, nil); err == nil {
		t.Fatal("Handle(nil, ...) error = nil")
	}
}

func mustRequestHandler(
	t *testing.T,
	publisher ResponsePublisher,
	factory DriverFactory,
) *RequestHandler {
	t.Helper()
	handler, err := NewRequestHandler(
		testHandlerPlant(),
		publisher,
		nil,
		HandlerOptions{
			Version:       "1.3.0-go",
			Environment:   "Test",
			DriverFactory: factory,
		},
	)
	if err != nil {
		t.Fatalf("NewRequestHandler() error = %v", err)
	}
	return handler
}

func testHandlerPlant() config.Plant {
	return config.Plant{
		Name:    "test plant",
		Enabled: true,
		Driver:  config.DriverSolarmanV5,
		Address: "192.0.2.1",
		Port:    8899,
		Serial:  12345,
		Cloud: config.Cloud{
			PlantID:    testPlantID,
			PlantToken: testPlantToken,
		},
	}
}

func threeLinePayload() []byte {
	return []byte(`{"OrderId":"batch","Lines":[
		{"LineNo":0,"Modbus":"010300000001840A"},
		{"LineNo":1,"Modbus":"010300010001D5CA"},
		{"LineNo":2,"Modbus":"01030002000125CA"}
	]}`)
}

func singleLinePayload() []byte {
	return []byte(`{"Lines":[{"LineNo":0,"Modbus":"010300000001840A"}]}`)
}

func decodePublishedHeader(t *testing.T, payload []byte) *protocol.Header {
	t.Helper()
	header, err := protocol.Decode(payload)
	if err != nil {
		t.Fatalf("decode published response error = %v", err)
	}
	if header == nil {
		t.Fatal("published response header = nil")
	}
	return header
}

func dereference(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func stringsContain(value, fragment string) bool {
	return bytes.Contains([]byte(value), []byte(fragment))
}

type responseMessage struct {
	topic   string
	payload []byte
	qos     byte
}

type fakeResponsePublisher struct {
	mu       sync.Mutex
	messages []responseMessage
	err      error
}

func (publisher *fakeResponsePublisher) PublishContext(
	_ context.Context,
	topic string,
	payload []byte,
	qos byte,
) error {
	publisher.mu.Lock()
	defer publisher.mu.Unlock()
	publisher.messages = append(publisher.messages, responseMessage{
		topic:   topic,
		payload: bytes.Clone(payload),
		qos:     qos,
	})
	return publisher.err
}

func (publisher *fakeResponsePublisher) singleMessage(t *testing.T) responseMessage {
	t.Helper()
	publisher.mu.Lock()
	defer publisher.mu.Unlock()
	if len(publisher.messages) != 1 {
		t.Fatalf("published message count = %d, want 1", len(publisher.messages))
	}
	return publisher.messages[0]
}

func (publisher *fakeResponsePublisher) messageCount() int {
	publisher.mu.Lock()
	defer publisher.mu.Unlock()
	return len(publisher.messages)
}

type fakeHandlerDriver struct {
	connectErr error
	closeErr   error
	responses  [][]byte
	sendErrAt  int
	sendErr    error
	send       func(context.Context, []byte) ([]byte, error)

	connectCalls int
	closeCalls   int
	requests     [][]byte
}

func (inverter *fakeHandlerDriver) Connect(context.Context) error {
	inverter.connectCalls++
	return inverter.connectErr
}

func (inverter *fakeHandlerDriver) SendDataToDevice(
	ctx context.Context,
	request []byte,
) ([]byte, error) {
	inverter.requests = append(inverter.requests, bytes.Clone(request))
	index := len(inverter.requests) - 1
	if inverter.send != nil {
		return inverter.send(ctx, request)
	}
	if inverter.sendErr != nil && index == inverter.sendErrAt {
		return nil, inverter.sendErr
	}
	if index < len(inverter.responses) {
		return bytes.Clone(inverter.responses[index]), nil
	}
	return nil, nil
}

func (*fakeHandlerDriver) ReadHoldingRegisters(
	context.Context,
	byte,
	uint16,
	uint16,
) ([]byte, error) {
	return nil, errors.New("not implemented in handler test")
}

func (*fakeHandlerDriver) WriteMultipleRegisters(
	context.Context,
	byte,
	uint16,
	[]byte,
) error {
	return errors.New("not implemented in handler test")
}

func (inverter *fakeHandlerDriver) Close() error {
	inverter.closeCalls++
	return inverter.closeErr
}
