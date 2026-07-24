package cloud

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestKeepaliveLoopPublishesEveryMinuteFromCycleStart(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.July, 24, 12, 0, 0, 0, time.UTC)
	clock := newFakeLoopClock(start)
	client := newFakeKeepaliveClient(clock)
	client.connected = true
	ctx, cancel := context.WithCancel(context.Background())
	clock.afterWait = func(waitCount int) {
		if waitCount == 3 {
			cancel()
		}
	}

	loop := mustKeepaliveLoop(t, client, clock, false)
	if err := loop.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	publications := client.publicationsSnapshot()
	if len(publications) != 3 {
		t.Fatalf("publication count = %d, want 3", len(publications))
	}
	for index, publication := range publications {
		wantTime := start.Add(time.Duration(index) * KeepaliveInterval)
		if !publication.at.Equal(wantTime) {
			t.Errorf("publication %d time = %s, want %s", index, publication.at, wantTime)
		}
		if publication.topic != KeepaliveTopic(testPlantID) {
			t.Errorf("publication %d topic = %q", index, publication.topic)
		}
		if publication.qos != 1 {
			t.Errorf("publication %d QoS = %d, want 1", index, publication.qos)
		}
		if len(publication.payload) != 0 {
			t.Errorf("publication %d payload = %q, want empty", index, publication.payload)
		}
	}
	for index, duration := range clock.waitsSnapshot() {
		if duration != KeepaliveInterval {
			t.Errorf("wait %d = %s, want %s", index, duration, KeepaliveInterval)
		}
	}
	if client.disconnectCallsSnapshot() != 1 {
		t.Errorf("Disconnect() calls = %d, want 1", client.disconnectCallsSnapshot())
	}
}

func TestKeepaliveLoopSkipsDelayWhenCycleExceedsMinute(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.July, 24, 12, 0, 0, 0, time.UTC)
	clock := newFakeLoopClock(start)
	client := newFakeKeepaliveClient(clock)
	client.connected = true
	ctx, cancel := context.WithCancel(context.Background())
	client.afterPublish = func(publishCount int) {
		clock.Advance(75 * time.Second)
		if publishCount == 2 {
			cancel()
		}
	}

	loop := mustKeepaliveLoop(t, client, clock, false)
	if err := loop.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	publications := client.publicationsSnapshot()
	if len(publications) != 2 {
		t.Fatalf("publication count = %d, want 2", len(publications))
	}
	if got, want := publications[1].at.Sub(publications[0].at), 75*time.Second; got != want {
		t.Errorf("time between long-cycle publishes = %s, want %s", got, want)
	}
	if waits := clock.waitsSnapshot(); len(waits) != 0 {
		t.Errorf("waits after overlong cycles = %v, want none", waits)
	}
}

func TestKeepaliveLoopReconnectsAfterDrop(t *testing.T) {
	t.Parallel()

	clock := newFakeLoopClock(time.Now())
	client := newFakeKeepaliveClient(clock)
	ctx, cancel := context.WithCancel(context.Background())
	clock.afterWait = func(waitCount int) {
		switch waitCount {
		case 1:
			client.dropConnection()
		case 2:
			cancel()
		}
	}

	loop := mustKeepaliveLoop(t, client, clock, false)
	if err := loop.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if got := client.connectCallsSnapshot(); got != 2 {
		t.Errorf("Connect() calls = %d, want 2", got)
	}
	if got := len(client.publicationsSnapshot()); got != 2 {
		t.Errorf("publication count = %d, want 2", got)
	}
}

func TestKeepaliveLoopUsesConfiguredReconnectBackoff(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		debug bool
		want  time.Duration
	}{
		{name: "production", debug: false, want: ProductionReconnectBackoff},
		{name: "debug", debug: true, want: DebugReconnectBackoff},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			clock := newFakeLoopClock(time.Now())
			client := newFakeKeepaliveClient(clock)
			client.connectErr = errors.New("broker unavailable")
			ctx, cancel := context.WithCancel(context.Background())
			clock.afterWait = func(int) {
				cancel()
			}

			loop := mustKeepaliveLoop(t, client, clock, test.debug)
			if err := loop.Run(ctx); err != nil {
				t.Fatalf("Run() error = %v", err)
			}

			waits := clock.waitsSnapshot()
			if len(waits) != 1 || waits[0] != test.want {
				t.Errorf("backoff waits = %v, want [%s]", waits, test.want)
			}
		})
	}
}

func TestKeepaliveLoopKeepsRetryingWhenConnectionNeverSucceeds(t *testing.T) {
	t.Parallel()

	clock := newFakeLoopClock(time.Now())
	client := newFakeKeepaliveClient(clock)
	client.connectErr = errors.New("broker unavailable")
	ctx, cancel := context.WithCancel(context.Background())
	clock.afterWait = func(waitCount int) {
		if waitCount == 3 {
			cancel()
		}
	}

	loop := mustKeepaliveLoop(t, client, clock, true)
	if err := loop.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got := client.connectCallsSnapshot(); got != 3 {
		t.Errorf("Connect() calls = %d, want 3", got)
	}
	if got := len(client.publicationsSnapshot()); got != 0 {
		t.Errorf("publication count = %d, want 0", got)
	}
}

func TestKeepaliveLoopRetriesInactiveSuccessfulConnection(t *testing.T) {
	t.Parallel()

	clock := newFakeLoopClock(time.Now())
	client := newFakeKeepaliveClient(clock)
	client.stayDisconnected = true
	ctx, cancel := context.WithCancel(context.Background())
	clock.afterWait = func(int) {
		cancel()
	}

	loop := mustKeepaliveLoop(t, client, clock, false)
	if err := loop.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got := clock.waitsSnapshot(); len(got) != 1 || got[0] != ProductionReconnectBackoff {
		t.Errorf("waits = %v, want production reconnect backoff", got)
	}
}

func TestKeepaliveLoopPublishFailureKeepsMinuteCadence(t *testing.T) {
	t.Parallel()

	clock := newFakeLoopClock(time.Now())
	client := newFakeKeepaliveClient(clock)
	client.connected = true
	client.publishErr = errors.New("publish failed")
	ctx, cancel := context.WithCancel(context.Background())
	clock.afterWait = func(int) {
		cancel()
	}

	loop := mustKeepaliveLoop(t, client, clock, false)
	if err := loop.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got := clock.waitsSnapshot(); len(got) != 1 || got[0] != KeepaliveInterval {
		t.Errorf("waits = %v, want keepalive interval", got)
	}
}

func TestKeepaliveLoopValidationAndCancellation(t *testing.T) {
	t.Parallel()

	clock := newFakeLoopClock(time.Now())
	client := newFakeKeepaliveClient(clock)

	if _, err := newKeepaliveLoop("", client, nil, KeepaliveOptions{}, clock); err == nil {
		t.Fatal("newKeepaliveLoop() empty plant id error = nil")
	}
	if _, err := newKeepaliveLoop(testPlantID, nil, nil, KeepaliveOptions{}, clock); err == nil {
		t.Fatal("newKeepaliveLoop() nil client error = nil")
	}
	if _, err := newKeepaliveLoop(testPlantID, client, nil, KeepaliveOptions{}, nil); err == nil {
		t.Fatal("newKeepaliveLoop() nil clock error = nil")
	}

	loop := mustKeepaliveLoop(t, client, clock, false)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := loop.Run(ctx); err != nil {
		t.Fatalf("Run(cancelled) error = %v", err)
	}
	//nolint:staticcheck // Exercise the public API's defensive nil-context guard.
	if err := loop.Run(nil); err == nil {
		t.Fatal("Run(nil) error = nil")
	}
}

func mustKeepaliveLoop(
	t *testing.T,
	client KeepaliveClient,
	clock loopClock,
	debug bool,
) *KeepaliveLoop {
	t.Helper()
	loop, err := newKeepaliveLoop(
		testPlantID,
		client,
		nil,
		KeepaliveOptions{Debug: debug},
		clock,
	)
	if err != nil {
		t.Fatalf("newKeepaliveLoop() error = %v", err)
	}
	return loop
}

type fakeLoopClock struct {
	mu        sync.Mutex
	now       time.Time
	waits     []time.Duration
	afterWait func(waitCount int)
}

func newFakeLoopClock(now time.Time) *fakeLoopClock {
	return &fakeLoopClock{now: now}
}

func (clock *fakeLoopClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}

func (clock *fakeLoopClock) Wait(ctx context.Context, duration time.Duration) error {
	clock.mu.Lock()
	clock.waits = append(clock.waits, duration)
	clock.now = clock.now.Add(duration)
	waitCount := len(clock.waits)
	afterWait := clock.afterWait
	clock.mu.Unlock()

	if afterWait != nil {
		afterWait(waitCount)
	}
	return ctx.Err()
}

func (clock *fakeLoopClock) Advance(duration time.Duration) {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	clock.now = clock.now.Add(duration)
}

func (clock *fakeLoopClock) waitsSnapshot() []time.Duration {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return append([]time.Duration(nil), clock.waits...)
}

type keepalivePublication struct {
	at      time.Time
	topic   string
	payload []byte
	qos     byte
}

type fakeKeepaliveClient struct {
	mu sync.Mutex

	clock            loopClock
	connected        bool
	stayDisconnected bool
	connectErr       error
	publishErr       error
	connectCalls     int
	disconnectCalls  int
	publications     []keepalivePublication
	afterPublish     func(publishCount int)
}

func newFakeKeepaliveClient(clock loopClock) *fakeKeepaliveClient {
	return &fakeKeepaliveClient{clock: clock}
}

func (client *fakeKeepaliveClient) Connect(context.Context) error {
	client.mu.Lock()
	defer client.mu.Unlock()
	client.connectCalls++
	if client.connectErr == nil && !client.stayDisconnected {
		client.connected = true
	}
	return client.connectErr
}

func (client *fakeKeepaliveClient) IsConnected() bool {
	client.mu.Lock()
	defer client.mu.Unlock()
	return client.connected
}

func (client *fakeKeepaliveClient) PublishContext(
	_ context.Context,
	topic string,
	payload []byte,
	qos byte,
) error {
	client.mu.Lock()
	client.publications = append(client.publications, keepalivePublication{
		at:      client.clock.Now(),
		topic:   topic,
		payload: append([]byte(nil), payload...),
		qos:     qos,
	})
	publishCount := len(client.publications)
	afterPublish := client.afterPublish
	err := client.publishErr
	client.mu.Unlock()

	if afterPublish != nil {
		afterPublish(publishCount)
	}
	return err
}

func (client *fakeKeepaliveClient) Disconnect() {
	client.mu.Lock()
	defer client.mu.Unlock()
	client.connected = false
	client.disconnectCalls++
}

func (client *fakeKeepaliveClient) dropConnection() {
	client.mu.Lock()
	defer client.mu.Unlock()
	client.connected = false
}

func (client *fakeKeepaliveClient) connectCallsSnapshot() int {
	client.mu.Lock()
	defer client.mu.Unlock()
	return client.connectCalls
}

func (client *fakeKeepaliveClient) disconnectCallsSnapshot() int {
	client.mu.Lock()
	defer client.mu.Unlock()
	return client.disconnectCalls
}

func (client *fakeKeepaliveClient) publicationsSnapshot() []keepalivePublication {
	client.mu.Lock()
	defer client.mu.Unlock()
	return append([]keepalivePublication(nil), client.publications...)
}
