package cloud

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/config"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/driver"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/logbuf"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/protocol"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/state"
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

func TestRequestHandlerRoutesToSubInverter(t *testing.T) {
	t.Parallel()

	plant := testHandlerPlant()
	plant.SubInverters = []config.SubInverter{
		{
			Serial:       456,
			DongleSerial: 654321,
			Address:      "192.0.2.46",
			Port:         18899,
		},
	}
	var captured config.Plant
	publisher := &fakeResponsePublisher{}
	handler, err := NewRequestHandler(
		plant,
		publisher,
		nil,
		HandlerOptions{
			Version:            "1.3.0-go",
			Environment:        "Test",
			LogLevelController: noopLogLevelController{},
			LastLogStreamer:    noopLastLogStreamer{},
			DriverFactory: func(target config.Plant, _ logbuf.Logger) (driver.Driver, error) {
				captured = target
				return &fakeHandlerDriver{}, nil
			},
		},
	)
	if err != nil {
		t.Fatalf("NewRequestHandler() error = %v", err)
	}

	if err := handler.Handle(
		context.Background(),
		[]byte(`{"SubInverterSN":" 456 "}`),
	); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if captured.Address != "192.0.2.46" ||
		captured.Port != 18899 ||
		captured.Serial != 654321 {
		t.Fatalf(
			"captured target = address %q, port %d, serial %d",
			captured.Address,
			captured.Port,
			captured.Serial,
		)
	}
	if captured.Driver != plant.Driver ||
		captured.Cloud != plant.Cloud ||
		captured.Number != plant.Number {
		t.Fatalf("captured target lost plant settings: %#v", captured)
	}
}

func TestRequestHandlerUnknownSubInverterSetsExactGlobalError(t *testing.T) {
	t.Parallel()

	plant := testHandlerPlant()
	plant.SubInverters = []config.SubInverter{{
		Serial:       456,
		DongleSerial: 654321,
		Address:      "192.0.2.46",
		Port:         18899,
	}}
	factoryCalls := 0
	publisher := &fakeResponsePublisher{}
	handler, err := NewRequestHandler(
		plant,
		publisher,
		nil,
		HandlerOptions{
			Version:            "1.3.0-go",
			Environment:        "Test",
			LogLevelController: noopLogLevelController{},
			LastLogStreamer:    noopLastLogStreamer{},
			DriverFactory: func(config.Plant, logbuf.Logger) (driver.Driver, error) {
				factoryCalls++
				return &fakeHandlerDriver{}, nil
			},
		},
	)
	if err != nil {
		t.Fatalf("NewRequestHandler() error = %v", err)
	}

	payload := []byte(`{"SubInverterSN":" 999 ","Lines":[
		{"LineNo":0,"Modbus":"010300000001840A"},
		{"LineNo":1,"Modbus":"010300010001D5CA"}
	]}`)
	if err := handler.Handle(context.Background(), payload); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if factoryCalls != 0 {
		t.Fatalf("driver factory calls = %d, want 0", factoryCalls)
	}

	response := decodePublishedHeader(t, publisher.singleMessage(t).payload)
	const wantError = "Inverter SerialNumber not found:  999  on Slave Inverters list!"
	if got := dereference(response.Error); got != wantError {
		t.Fatalf("Header.Error = %q, want %q", got, wantError)
	}
	for index, line := range response.Lines {
		if line.Modbus != nil {
			t.Errorf("line %d Modbus = %v, want nil", index, line.Modbus)
		}
	}
}

func TestRequestHandlerEmptySubInverterUsesPrimaryTarget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload []byte
	}{
		{name: "absent", payload: []byte(`{}`)},
		{name: "empty", payload: []byte(`{"SubInverterSN":""}`)},
		{name: "whitespace", payload: []byte(`{"SubInverterSN":" \t "}`)},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			plant := testHandlerPlant()
			var captured config.Plant
			handler, err := NewRequestHandler(
				plant,
				&fakeResponsePublisher{},
				nil,
				HandlerOptions{
					Version:            "1.3.0-go",
					Environment:        "Test",
					LogLevelController: noopLogLevelController{},
					LastLogStreamer:    noopLastLogStreamer{},
					DriverFactory: func(
						target config.Plant,
						_ logbuf.Logger,
					) (driver.Driver, error) {
						captured = target
						return &fakeHandlerDriver{}, nil
					},
				},
			)
			if err != nil {
				t.Fatalf("NewRequestHandler() error = %v", err)
			}

			if err := handler.Handle(context.Background(), test.payload); err != nil {
				t.Fatalf("Handle() error = %v", err)
			}
			if captured.Address != plant.Address ||
				captured.Port != plant.Port ||
				captured.Serial != plant.Serial {
				t.Fatalf(
					"captured target = address %q, port %d, serial %d",
					captured.Address,
					captured.Port,
					captured.Serial,
				)
			}
		})
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

func TestRequestHandlerAttachesLastLogAndCommitsAfterPublish(t *testing.T) {
	t.Parallel()

	const day = "2026-07-24"
	logDirectory := t.TempDir()
	writeLogFile(t, logDirectory, day, "existing\n")
	store := newTestStateStore(t)
	streamer := newTestLogStreamer(t, store, logDirectory, day)
	publisher := &fakeResponsePublisher{}
	handler, err := NewRequestHandler(
		testHandlerPlant(),
		publisher,
		nil,
		HandlerOptions{
			Version:            "1.3.0-go",
			Environment:        "Test",
			LogLevelController: noopLogLevelController{},
			LastLogStreamer:    streamer,
			DriverFactory: func(config.Plant, logbuf.Logger) (driver.Driver, error) {
				return &fakeHandlerDriver{}, nil
			},
		},
	)
	if err != nil {
		t.Fatalf("NewRequestHandler() error = %v", err)
	}

	request := []byte(`{"SendLastLog":1}`)
	if err := handler.Handle(context.Background(), request); err != nil {
		t.Fatalf("first Handle() error = %v", err)
	}
	first := decodePublishedHeader(t, publisher.messages[0].payload)
	if first.LastLog != nil {
		t.Fatalf("first LastLog = %q, want nil", dereference(first.LastLog))
	}

	appendLogFile(t, logDirectory, day, "incremental\n")
	if err := handler.Handle(context.Background(), request); err != nil {
		t.Fatalf("second Handle() error = %v", err)
	}
	second := decodePublishedHeader(t, publisher.messages[1].payload)
	if got := dereference(second.LastLog); got != "incremental\n" {
		t.Fatalf("second LastLog = %q", got)
	}

	persisted, err := store.Load(testHandlerPlant().Number)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if persisted.LastLogDate != day ||
		persisted.LastLogPos != int64(len("existing\nincremental\n")) {
		t.Fatalf("persisted cursor = %#v", persisted)
	}
}

func TestRequestHandlerDoesNotCommitLastLogWhenPublishFails(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("publish failed")
	streamer := &recordingLastLogStreamer{
		prepared: LastLogRead{
			Text: stringPointer("new log\n"),
			State: state.PlantState{
				LastLogDate: "2026-07-24",
				LastLogPos:  8,
			},
		},
	}
	handler, err := NewRequestHandler(
		testHandlerPlant(),
		&fakeResponsePublisher{err: wantErr},
		nil,
		HandlerOptions{
			Version:            "1.3.0-go",
			Environment:        "Test",
			LogLevelController: noopLogLevelController{},
			LastLogStreamer:    streamer,
			DriverFactory: func(config.Plant, logbuf.Logger) (driver.Driver, error) {
				return &fakeHandlerDriver{}, nil
			},
		},
	)
	if err != nil {
		t.Fatalf("NewRequestHandler() error = %v", err)
	}

	if err := handler.Handle(
		context.Background(),
		[]byte(`{"SendLastLog":1}`),
	); !errors.Is(err, wantErr) {
		t.Fatalf("Handle() error = %v, want publish error", err)
	}
	if streamer.prepareCalls != 1 {
		t.Fatalf("Prepare() calls = %d, want 1", streamer.prepareCalls)
	}
	if streamer.commitCalls != 0 {
		t.Fatalf("Commit() calls = %d, want 0", streamer.commitCalls)
	}
}

func TestRequestHandlerAppliesRemoteLogLevelBeforeDriverWork(t *testing.T) {
	t.Parallel()

	logRuntime, err := logbuf.New(logbuf.Options{
		Level:  logbuf.LevelError,
		Output: &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("logbuf.New() error = %v", err)
	}
	store, err := state.New(t.TempDir())
	if err != nil {
		t.Fatalf("state.New() error = %v", err)
	}
	controller, err := NewPersistentLogLevelController(logRuntime, store, LogLevelOptions{})
	if err != nil {
		t.Fatalf("NewPersistentLogLevelController() error = %v", err)
	}

	driverObservedLevel := false
	handler, err := NewRequestHandler(
		testHandlerPlant(),
		&fakeResponsePublisher{},
		logRuntime,
		HandlerOptions{
			Version:            "1.3.0-go",
			Environment:        "Test",
			LogLevelController: controller,
			LastLogStreamer:    noopLastLogStreamer{},
			DriverFactory: func(config.Plant, logbuf.Logger) (driver.Driver, error) {
				driverObservedLevel = logRuntime.Level() == logbuf.LevelInfo &&
					logRuntime.DriverTraceEnabled() &&
					logRuntime.DriverTraceRawEnabled()
				return &fakeHandlerDriver{}, nil
			},
		},
	)
	if err != nil {
		t.Fatalf("NewRequestHandler() error = %v", err)
	}

	if err := handler.Handle(context.Background(), []byte(`{"LogLevel":"mAx"}`)); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if !driverObservedLevel {
		t.Fatal("driver factory did not observe Max logging controls")
	}

	persisted, err := store.LoadRuntime()
	if err != nil {
		t.Fatalf("LoadRuntime() error = %v", err)
	}
	if persisted.LogLevel != "Max" {
		t.Fatalf("persisted LogLevel = %q, want Max", persisted.LogLevel)
	}
}

func TestRequestHandlerWarnsAndIgnoresUnknownRemoteLogLevel(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	logRuntime, err := logbuf.New(logbuf.Options{
		Level:  logbuf.LevelDebug,
		Output: &output,
	})
	if err != nil {
		t.Fatalf("logbuf.New() error = %v", err)
	}
	logRuntime.SetDriverTrace(true, false)
	store, err := state.New(t.TempDir())
	if err != nil {
		t.Fatalf("state.New() error = %v", err)
	}
	controller, err := NewPersistentLogLevelController(logRuntime, store, LogLevelOptions{})
	if err != nil {
		t.Fatalf("NewPersistentLogLevelController() error = %v", err)
	}
	publisher := &fakeResponsePublisher{}
	handler, err := NewRequestHandler(
		testHandlerPlant(),
		publisher,
		logRuntime,
		HandlerOptions{
			Version:            "1.3.0-go",
			Environment:        "Test",
			LogLevelController: controller,
			LastLogStreamer:    noopLastLogStreamer{},
			DriverFactory: func(config.Plant, logbuf.Logger) (driver.Driver, error) {
				return &fakeHandlerDriver{}, nil
			},
		},
	)
	if err != nil {
		t.Fatalf("NewRequestHandler() error = %v", err)
	}

	if err := handler.Handle(
		context.Background(),
		[]byte(`{"LogLevel":"everything"}`),
	); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if logRuntime.Level() != logbuf.LevelDebug ||
		!logRuntime.DriverTraceEnabled() ||
		logRuntime.DriverTraceRawEnabled() {
		t.Fatal("unknown LogLevel changed runtime logging controls")
	}
	if !stringsContain(output.String(), "unknown cloud log level") ||
		!stringsContain(output.String(), "everything") {
		t.Fatalf("warning output = %q", output.String())
	}
	persisted, err := store.LoadRuntime()
	if err != nil {
		t.Fatalf("LoadRuntime() error = %v", err)
	}
	if persisted != (state.RuntimeState{}) {
		t.Fatalf("unknown LogLevel persisted state = %#v", persisted)
	}
	if publisher.messageCount() != 1 {
		t.Fatalf("unknown LogLevel published messages = %d, want 1", publisher.messageCount())
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
		HandlerOptions{LogLevelController: noopLogLevelController{}},
	); err == nil {
		t.Fatal("NewRequestHandler() nil publisher error = nil")
	}
	if _, err := NewRequestHandler(
		plant,
		publisher,
		nil,
		HandlerOptions{},
	); err == nil {
		t.Fatal("NewRequestHandler() nil log level controller error = nil")
	}
	if _, err := NewRequestHandler(
		plant,
		publisher,
		nil,
		HandlerOptions{LogLevelController: noopLogLevelController{}},
	); err == nil {
		t.Fatal("NewRequestHandler() nil last log streamer error = nil")
	}

	handler, err := NewRequestHandler(plant, publisher, nil, HandlerOptions{
		LogLevelController: noopLogLevelController{},
		LastLogStreamer:    noopLastLogStreamer{},
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
			Version:            "1.3.0-go",
			Environment:        "Test",
			DriverFactory:      factory,
			LogLevelController: noopLogLevelController{},
			LastLogStreamer:    noopLastLogStreamer{},
		},
	)
	if err != nil {
		t.Fatalf("NewRequestHandler() error = %v", err)
	}
	return handler
}

func TestRequestHandlerTracesMQTTPayloadsWithDriverTrace(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		trace bool
	}{
		{name: "enabled", trace: true},
		{name: "disabled"},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var output bytes.Buffer
			handler, _ := newTracingTestHandler(t, &output, test.trace, nil)

			request := `{"OrderId":"abc","Lines":[{"LineNo":0,"Modbus":"010300000001840A"}]}`
			if err := handler.Handle(context.Background(), []byte(request)); err != nil {
				t.Fatalf("Handle() error = %v", err)
			}

			logged := output.String()
			gotReceived := strings.Contains(logged, "Received MQTT")
			gotSent := strings.Contains(logged, "Send MQTT")
			if gotReceived != test.trace || gotSent != test.trace {
				t.Fatalf(
					"received=%t sent=%t, want %t for both\nlog: %s",
					gotReceived,
					gotSent,
					test.trace,
					logged,
				)
			}
			if !test.trace {
				return
			}
			if !strings.Contains(logged, `OrderId`) {
				t.Fatalf("traced payload is missing the request body\nlog: %s", logged)
			}
		})
	}
}

func TestRequestHandlerTraceElidesLastLogText(t *testing.T) {
	t.Parallel()

	logText := "2026-07-25 20:00:00: a previously written log line"
	var output bytes.Buffer
	handler, publisher := newTracingTestHandler(t, &output, true, &LastLogRead{
		Text: &logText,
	})

	if err := handler.Handle(
		context.Background(),
		[]byte(`{"OrderId":"abc","SendLastLog":1}`),
	); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	logged := output.String()
	if strings.Contains(logged, "a previously written log line") {
		t.Fatalf("traced payload leaked LastLog text back into the log\nlog: %s", logged)
	}
	if !strings.Contains(logged, fmt.Sprintf("[%d bytes]", len(logText))) {
		t.Fatalf("traced payload is missing the LastLog size marker\nlog: %s", logged)
	}

	published := publisher.singleMessage(t)
	if !strings.Contains(string(published.payload), logText) {
		t.Fatal("published payload lost the real LastLog text")
	}
}

func newTracingTestHandler(
	t *testing.T,
	output *bytes.Buffer,
	trace bool,
	lastLog *LastLogRead,
) (*RequestHandler, *fakeResponsePublisher) {
	t.Helper()

	logRuntime, err := logbuf.New(logbuf.Options{
		Level:  logbuf.LevelInfo,
		Output: output,
	})
	if err != nil {
		t.Fatalf("logbuf.New() error = %v", err)
	}
	logRuntime.SetDriverTrace(trace, false)

	store, err := state.New(t.TempDir())
	if err != nil {
		t.Fatalf("state.New() error = %v", err)
	}
	controller, err := NewPersistentLogLevelController(logRuntime, store, LogLevelOptions{})
	if err != nil {
		t.Fatalf("NewPersistentLogLevelController() error = %v", err)
	}

	var streamer LastLogStreamer = noopLastLogStreamer{}
	if lastLog != nil {
		streamer = &recordingLastLogStreamer{prepared: *lastLog}
	}

	publisher := &fakeResponsePublisher{}
	handler, err := NewRequestHandler(
		testHandlerPlant(),
		publisher,
		logRuntime,
		HandlerOptions{
			Version:            "1.3.0-go",
			Environment:        "Test",
			LogLevelController: controller,
			LastLogStreamer:    streamer,
			DriverFactory: func(config.Plant, logbuf.Logger) (driver.Driver, error) {
				return &fakeHandlerDriver{}, nil
			},
		},
	)
	if err != nil {
		t.Fatalf("NewRequestHandler() error = %v", err)
	}
	return handler, publisher
}

func testHandlerPlant() config.Plant {
	return config.Plant{
		Number:  1,
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

type noopLogLevelController struct{}

func (noopLogLevelController) ApplyCloudLevel(string) error {
	return nil
}

type noopLastLogStreamer struct{}

func (noopLastLogStreamer) Prepare(int) (LastLogRead, error) {
	return LastLogRead{}, nil
}

func (noopLastLogStreamer) Commit(int, state.PlantState) error {
	return nil
}

type recordingLastLogStreamer struct {
	prepared LastLogRead
	err      error

	prepareCalls int
	commitCalls  int
}

func (streamer *recordingLastLogStreamer) Prepare(int) (LastLogRead, error) {
	streamer.prepareCalls++
	return streamer.prepared, streamer.err
}

func (streamer *recordingLastLogStreamer) Commit(int, state.PlantState) error {
	streamer.commitCalls++
	return streamer.err
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
