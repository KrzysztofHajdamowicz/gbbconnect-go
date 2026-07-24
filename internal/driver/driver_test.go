package driver

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/modbus"
)

type fakeClock struct {
	mu    sync.Mutex
	now   time.Time
	waits []time.Duration
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Unix(1_000, 0)}
}

func (clock *fakeClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}

func (clock *fakeClock) Wait(ctx context.Context, duration time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	clock.mu.Lock()
	defer clock.mu.Unlock()
	clock.waits = append(clock.waits, duration)
	clock.now = clock.now.Add(duration)
	return nil
}

func (clock *fakeClock) recordedWaits() []time.Duration {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return append([]time.Duration(nil), clock.waits...)
}

type fakeTransport struct {
	mu         sync.Mutex
	send       func(context.Context, []byte) ([]byte, error)
	connectErr error
	connects   int
	requests   [][]byte
	active     int
	maxActive  int
	closeCalls int
}

func (transport *fakeTransport) Connect(ctx context.Context) error {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	transport.connects++
	if err := ctx.Err(); err != nil {
		return err
	}
	return transport.connectErr
}

func TestDriverConnectDelegatesToTransport(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("connect failed")
	transport := &fakeTransport{connectErr: wantErr}
	inverterDriver := newFacade(transport, newFakeClock())

	if err := inverterDriver.Connect(context.Background()); !errors.Is(err, wantErr) {
		t.Fatalf("Connect() error = %v, want %v", err, wantErr)
	}
	transport.mu.Lock()
	connects := transport.connects
	transport.mu.Unlock()
	if connects != 1 {
		t.Errorf("transport Connect() calls = %d, want 1", connects)
	}
}

func (transport *fakeTransport) SendRTU(
	ctx context.Context,
	request []byte,
) ([]byte, error) {
	transport.mu.Lock()
	transport.active++
	if transport.active > transport.maxActive {
		transport.maxActive = transport.active
	}
	transport.requests = append(transport.requests, bytes.Clone(request))
	send := transport.send
	transport.mu.Unlock()

	var (
		response []byte
		err      error
	)
	if send != nil {
		response, err = send(ctx, request)
	}

	transport.mu.Lock()
	transport.active--
	transport.mu.Unlock()
	return response, err
}

func (transport *fakeTransport) Close() error {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	transport.closeCalls++
	return nil
}

func (transport *fakeTransport) snapshot() (requests [][]byte, maxActive int) {
	transport.mu.Lock()
	defer transport.mu.Unlock()

	requests = make([][]byte, len(transport.requests))
	for index := range transport.requests {
		requests[index] = bytes.Clone(transport.requests[index])
	}
	return requests, transport.maxActive
}

func modbusResponse(_ context.Context, request []byte) ([]byte, error) {
	switch request[1] {
	case 0x03:
		count := int(request[4])<<8 | int(request[5])
		payload := make([]byte, 3+count*2)
		payload[0] = request[0]
		payload[1] = request[1]
		payload[2] = byte(count * 2)
		for index := 0; index < count; index++ {
			payload[3+index*2] = byte(index + 1)
			payload[4+index*2] = byte(index + 2)
		}
		return modbus.AppendCRC(payload), nil
	case 0x10:
		return modbus.AppendCRC(request[:6]), nil
	default:
		return nil, errors.New("unexpected request")
	}
}

func TestDriverSerializesConcurrentRawCalls(t *testing.T) {
	t.Parallel()

	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	transport := &fakeTransport{
		send: func(ctx context.Context, _ []byte) ([]byte, error) {
			entered <- struct{}{}
			select {
			case <-release:
				return []byte{0x01}, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	}
	driver := newFacade(transport, newFakeClock())

	start := make(chan struct{})
	errs := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			_, err := driver.SendDataToDevice(context.Background(), []byte{0x01})
			errs <- err
		}()
	}
	close(start)

	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("first transport call did not start")
	}
	select {
	case <-entered:
		t.Fatal("concurrent transport calls overlapped")
	case <-time.After(25 * time.Millisecond):
	}

	close(release)
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatalf("SendDataToDevice() error = %v", err)
		}
	}
	_, maxActive := transport.snapshot()
	if maxActive != 1 {
		t.Fatalf("maximum active transport calls = %d, want 1", maxActive)
	}
}

func TestDriverQueuedCallHonorsCancellation(t *testing.T) {
	t.Parallel()

	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	transport := &fakeTransport{
		send: func(ctx context.Context, _ []byte) ([]byte, error) {
			entered <- struct{}{}
			select {
			case <-release:
				return []byte{0x01}, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	}
	driver := newFacade(transport, newFakeClock())

	firstDone := make(chan error, 1)
	go func() {
		_, err := driver.SendDataToDevice(context.Background(), []byte{0x01})
		firstDone <- err
	}()
	<-entered

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := driver.SendDataToDevice(ctx, []byte{0x02}); !errors.Is(err, context.Canceled) {
		t.Fatalf("queued SendDataToDevice() error = %v, want context.Canceled", err)
	}

	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first SendDataToDevice() error = %v", err)
	}
	requests, _ := transport.snapshot()
	if len(requests) != 1 {
		t.Fatalf("transport calls = %d, want 1", len(requests))
	}
}

func TestDriverLocalTimingAndRawPath(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	transport := &fakeTransport{send: modbusResponse}
	driver := newFacade(transport, clock)

	rawResponse := []byte{0xAA}
	transport.send = func(ctx context.Context, request []byte) ([]byte, error) {
		if bytes.Equal(request, []byte{0xFF}) {
			return rawResponse, nil
		}
		return modbusResponse(ctx, request)
	}

	gotRaw, err := driver.SendDataToDevice(context.Background(), []byte{0xFF})
	if err != nil {
		t.Fatalf("SendDataToDevice() error = %v", err)
	}
	if !bytes.Equal(gotRaw, rawResponse) {
		t.Fatalf("SendDataToDevice() = %X, want %X", gotRaw, rawResponse)
	}

	firstRead, err := driver.ReadHoldingRegisters(context.Background(), 1, 0x10, 1)
	if err != nil {
		t.Fatalf("first ReadHoldingRegisters() error = %v", err)
	}
	if !bytes.Equal(firstRead, []byte{0x01, 0x02}) {
		t.Fatalf("first ReadHoldingRegisters() = %X", firstRead)
	}
	if _, err := driver.ReadHoldingRegisters(context.Background(), 1, 0x11, 1); err != nil {
		t.Fatalf("second ReadHoldingRegisters() error = %v", err)
	}
	if err := driver.WriteMultipleRegisters(
		context.Background(),
		1,
		0x20,
		[]byte{0x12, 0x34},
	); err != nil {
		t.Fatalf("WriteMultipleRegisters() error = %v", err)
	}
	if _, err := driver.SendDataToDevice(context.Background(), []byte{0xFF}); err != nil {
		t.Fatalf("second SendDataToDevice() error = %v", err)
	}
	if _, err := driver.ReadHoldingRegisters(context.Background(), 1, 0x12, 1); err != nil {
		t.Fatalf("third ReadHoldingRegisters() error = %v", err)
	}

	wantWaits := []time.Duration{readDelay, writeDelay, readDelay}
	if waits := clock.recordedWaits(); !equalDurations(waits, wantWaits) {
		t.Fatalf("clock waits = %v, want %v", waits, wantWaits)
	}

	requests, _ := transport.snapshot()
	wantRequests := [][]byte{
		{0xFF},
		modbus.BuildReadHoldingRegisters(1, 0x10, 1),
		modbus.BuildReadHoldingRegisters(1, 0x11, 1),
		modbus.BuildWriteMultipleRegisters(1, 0x20, []byte{0x12, 0x34}),
		{0xFF},
		modbus.BuildReadHoldingRegisters(1, 0x12, 1),
	}
	if !reflectByteSlicesEqual(requests, wantRequests) {
		t.Fatalf("transport requests = %X, want %X", requests, wantRequests)
	}
}

func TestDriverDoesNotRecordFailedLocalSend(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	call := 0
	transport := &fakeTransport{
		send: func(ctx context.Context, request []byte) ([]byte, error) {
			call++
			if call == 1 {
				return nil, errors.New("send failed")
			}
			return modbusResponse(ctx, request)
		},
	}
	driver := newFacade(transport, clock)

	if _, err := driver.ReadHoldingRegisters(context.Background(), 1, 0, 1); err == nil {
		t.Fatal("first ReadHoldingRegisters() error = nil")
	}
	if _, err := driver.ReadHoldingRegisters(context.Background(), 1, 0, 1); err != nil {
		t.Fatalf("second ReadHoldingRegisters() error = %v", err)
	}
	if waits := clock.recordedWaits(); len(waits) != 0 {
		t.Fatalf("clock waits after failed send = %v, want none", waits)
	}
}

func TestDriverBuilderLimitsAndClose(t *testing.T) {
	t.Parallel()

	transport := &fakeTransport{send: modbusResponse}
	driver := newFacade(transport, newFakeClock())

	if _, err := driver.ReadHoldingRegisters(context.Background(), 1, 0, 126); !errors.Is(
		err,
		ErrTooManyRegistersToRead,
	) {
		t.Fatalf("ReadHoldingRegisters(126) error = %v", err)
	}
	if err := driver.WriteMultipleRegisters(
		context.Background(),
		1,
		0,
		make([]byte, 251),
	); !errors.Is(err, ErrTooManyRegistersToWrite) {
		t.Fatalf("WriteMultipleRegisters(251) error = %v", err)
	}
	if err := driver.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	requests, _ := transport.snapshot()
	if len(requests) != 0 {
		t.Fatalf("transport calls = %d, want 0", len(requests))
	}
	transport.mu.Lock()
	closeCalls := transport.closeCalls
	transport.mu.Unlock()
	if closeCalls != 1 {
		t.Fatalf("transport Close() calls = %d, want 1", closeCalls)
	}
}

func TestSystemClockWaitHonorsCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := (systemClock{}).Wait(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("systemClock.Wait() error = %v, want context.Canceled", err)
	}
}

func equalDurations(left, right []time.Duration) bool {
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

func reflectByteSlicesEqual(left, right [][]byte) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if !bytes.Equal(left[index], right[index]) {
			return false
		}
	}
	return true
}
