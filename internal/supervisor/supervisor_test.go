package supervisor

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/cloud"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/config"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/logbuf"
)

func TestSupervisorStartsEligiblePlantsConcurrentlyAndSkipsOthers(t *testing.T) {
	t.Parallel()

	configuration := config.Config{
		Runtime: config.Runtime{},
		Plants: []config.Plant{
			testPlant(1, "plant-one"),
			testPlant(2, "plant-two"),
			testPlant(3, "disabled"),
			testPlant(4, "no-token"),
		},
	}
	configuration.Plants[2].Enabled = false
	configuration.Plants[3].Cloud.PlantToken = ""

	started := make(chan string, len(configuration.Plants))
	deps := testDependencies()
	deps.newLifecycle = func(
		plantID string,
		_ cloud.KeepaliveClient,
		_ logbuf.Logger,
		_ cloud.KeepaliveOptions,
	) (workerLifecycle, error) {
		return lifecycleFunc(func(ctx context.Context) error {
			started <- plantID
			<-ctx.Done()
			return nil
		}), nil
	}

	service := newTestSupervisor(t, configuration, deps)
	ctx, cancel := context.WithCancel(context.Background())
	done := runAsync(service, ctx)

	got := map[string]bool{
		receive(t, started): true,
		receive(t, started): true,
	}
	if !got["plant-one"] || !got["plant-two"] || len(got) != 2 {
		t.Fatalf("started plants = %v", got)
	}
	select {
	case extra := <-started:
		t.Fatalf("unexpected worker started for %q", extra)
	case <-time.After(25 * time.Millisecond):
	}

	cancel()
	if err := receive(t, done); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestSupervisorRoutesMessagesToPlantHandler(t *testing.T) {
	t.Parallel()

	client := &fakePlantClient{}
	handled := make(chan []byte, 1)
	started := make(chan struct{}, 1)
	deps := testDependencies()
	deps.newClient = func(config.Cloud, logbuf.Logger) (plantClient, error) {
		return client, nil
	}
	deps.newHandler = func(
		_ config.Plant,
		_ cloud.ResponsePublisher,
		_ logbuf.Logger,
		options cloud.HandlerOptions,
	) (requestProcessor, error) {
		if options.Version != "test-version" ||
			options.LogLevelController == nil ||
			options.LastLogStreamer == nil {
			t.Fatalf("handler options = %#v", options)
		}
		return processorFunc(func(_ context.Context, payload []byte) error {
			handled <- append([]byte(nil), payload...)
			return nil
		}), nil
	}
	deps.newLifecycle = func(
		string,
		cloud.KeepaliveClient,
		logbuf.Logger,
		cloud.KeepaliveOptions,
	) (workerLifecycle, error) {
		return lifecycleFunc(func(ctx context.Context) error {
			started <- struct{}{}
			<-ctx.Done()
			return nil
		}), nil
	}

	service := newTestSupervisor(t, config.Config{
		Plants: []config.Plant{testPlant(1, "plant-one")},
	}, deps)
	ctx, cancel := context.WithCancel(context.Background())
	done := runAsync(service, ctx)
	receive(t, started)

	const payload = `{"Lines":[]}`
	client.deliver([]byte(payload))
	if got := string(receive(t, handled)); got != payload {
		t.Fatalf("handled payload = %q", got)
	}

	cancel()
	if err := receive(t, done); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestSupervisorKeepsPlantsIndependentDuringSlowRequest(t *testing.T) {
	t.Parallel()

	type identifiedClient struct {
		id     string
		client *fakePlantClient
	}
	clients := make(chan identifiedClient, 2)
	started := make(chan string, 2)
	slowStarted := make(chan struct{}, 1)
	releaseSlow := make(chan struct{})
	fastHandled := make(chan struct{}, 1)
	deps := testDependencies()
	deps.newClient = func(
		configuration config.Cloud,
		_ logbuf.Logger,
	) (plantClient, error) {
		client := &fakePlantClient{}
		clients <- identifiedClient{
			id:     configuration.PlantID,
			client: client,
		}
		return client, nil
	}
	deps.newHandler = func(
		plant config.Plant,
		_ cloud.ResponsePublisher,
		_ logbuf.Logger,
		_ cloud.HandlerOptions,
	) (requestProcessor, error) {
		if plant.Cloud.PlantID == "slow" {
			return processorFunc(func(context.Context, []byte) error {
				slowStarted <- struct{}{}
				<-releaseSlow
				return nil
			}), nil
		}
		return processorFunc(func(context.Context, []byte) error {
			fastHandled <- struct{}{}
			return nil
		}), nil
	}
	deps.newLifecycle = func(
		plantID string,
		_ cloud.KeepaliveClient,
		_ logbuf.Logger,
		_ cloud.KeepaliveOptions,
	) (workerLifecycle, error) {
		return lifecycleFunc(func(ctx context.Context) error {
			started <- plantID
			<-ctx.Done()
			return nil
		}), nil
	}

	service := newTestSupervisor(t, config.Config{
		Plants: []config.Plant{
			testPlant(1, "slow"),
			testPlant(2, "fast"),
		},
	}, deps)
	ctx, cancel := context.WithCancel(context.Background())
	done := runAsync(service, ctx)

	clientByID := make(map[string]*fakePlantClient, 2)
	for range 2 {
		identified := receive(t, clients)
		clientByID[identified.id] = identified.client
	}
	receive(t, started)
	receive(t, started)

	go clientByID["slow"].deliver([]byte(`{"Lines":[]}`))
	receive(t, slowStarted)
	clientByID["fast"].deliver([]byte(`{"Lines":[]}`))
	receive(t, fastHandled)

	close(releaseSlow)
	cancel()
	if err := receive(t, done); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestSupervisorRecoversLifecyclePanicAndUsesDebugBackoff(t *testing.T) {
	t.Parallel()

	var lifecycleCount int
	var lifecycleMu sync.Mutex
	restarted := make(chan struct{}, 1)
	backoffs := make(chan time.Duration, 1)
	deps := testDependencies()
	deps.newLifecycle = func(
		string,
		cloud.KeepaliveClient,
		logbuf.Logger,
		cloud.KeepaliveOptions,
	) (workerLifecycle, error) {
		lifecycleMu.Lock()
		lifecycleCount++
		count := lifecycleCount
		lifecycleMu.Unlock()
		if count == 1 {
			return lifecycleFunc(func(context.Context) error {
				panic("broken worker")
			}), nil
		}
		return lifecycleFunc(func(ctx context.Context) error {
			restarted <- struct{}{}
			<-ctx.Done()
			return nil
		}), nil
	}
	deps.wait = func(_ context.Context, duration time.Duration) error {
		backoffs <- duration
		return nil
	}

	service := newTestSupervisor(t, config.Config{
		Runtime: config.Runtime{Debug: true},
		Plants:  []config.Plant{testPlant(1, "plant-one")},
	}, deps)
	ctx, cancel := context.WithCancel(context.Background())
	done := runAsync(service, ctx)

	if got := receive(t, backoffs); got != cloud.DebugReconnectBackoff {
		t.Fatalf("backoff = %s, want %s", got, cloud.DebugReconnectBackoff)
	}
	receive(t, restarted)

	cancel()
	if err := receive(t, done); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestSupervisorRecoversMessageHandlerPanicAndRestartsWorker(t *testing.T) {
	t.Parallel()

	clients := make(chan *fakePlantClient, 2)
	started := make(chan struct{}, 2)
	backoff := make(chan struct{}, 1)
	deps := testDependencies()
	deps.newClient = func(config.Cloud, logbuf.Logger) (plantClient, error) {
		client := &fakePlantClient{}
		clients <- client
		return client, nil
	}
	deps.newHandler = func(
		config.Plant,
		cloud.ResponsePublisher,
		logbuf.Logger,
		cloud.HandlerOptions,
	) (requestProcessor, error) {
		return processorFunc(func(context.Context, []byte) error {
			panic("broken handler")
		}), nil
	}
	deps.newLifecycle = func(
		string,
		cloud.KeepaliveClient,
		logbuf.Logger,
		cloud.KeepaliveOptions,
	) (workerLifecycle, error) {
		return lifecycleFunc(func(ctx context.Context) error {
			started <- struct{}{}
			<-ctx.Done()
			return nil
		}), nil
	}
	deps.wait = func(context.Context, time.Duration) error {
		backoff <- struct{}{}
		return nil
	}

	service := newTestSupervisor(t, config.Config{
		Plants: []config.Plant{testPlant(1, "plant-one")},
	}, deps)
	ctx, cancel := context.WithCancel(context.Background())
	done := runAsync(service, ctx)

	firstClient := receive(t, clients)
	receive(t, started)
	firstClient.deliver([]byte(`{"Lines":[]}`))
	receive(t, backoff)
	_ = receive(t, clients)
	receive(t, started)

	cancel()
	if err := receive(t, done); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestSupervisorValidatesOptionsAndContext(t *testing.T) {
	t.Parallel()

	logger, err := logbuf.New(logbuf.Options{Output: &bytes.Buffer{}})
	if err != nil {
		t.Fatalf("create logger: %v", err)
	}
	t.Cleanup(func() {
		_ = logger.Close()
	})
	options := Options{
		StateDir: t.TempDir(),
		LogDir:   t.TempDir(),
		Logger:   logger,
	}
	if _, err := newSupervisor(config.Config{}, Options{}, testDependencies()); err == nil {
		t.Fatal("newSupervisor() without options error = nil")
	}
	service, err := newSupervisor(config.Config{}, options, testDependencies())
	if err != nil {
		t.Fatalf("newSupervisor() error = %v", err)
	}
	//nolint:staticcheck // Exercise the public API's defensive nil-context guard.
	if err := service.Run(nil); err == nil {
		t.Fatal("Run(nil) error = nil")
	}
}

func newTestSupervisor(
	t *testing.T,
	configuration config.Config,
	deps dependencies,
) *Supervisor {
	t.Helper()
	logger, err := logbuf.New(logbuf.Options{
		Output: &bytes.Buffer{},
		LogDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("create logger: %v", err)
	}
	t.Cleanup(func() {
		_ = logger.Close()
	})
	service, err := newSupervisor(configuration, Options{
		Version:  "test-version",
		StateDir: t.TempDir(),
		LogDir:   t.TempDir(),
		Logger:   logger,
	}, deps)
	if err != nil {
		t.Fatalf("newSupervisor() error = %v", err)
	}
	return service
}

func testDependencies() dependencies {
	return dependencies{
		newClient: func(config.Cloud, logbuf.Logger) (plantClient, error) {
			return &fakePlantClient{}, nil
		},
		newHandler: func(
			config.Plant,
			cloud.ResponsePublisher,
			logbuf.Logger,
			cloud.HandlerOptions,
		) (requestProcessor, error) {
			return processorFunc(func(context.Context, []byte) error {
				return nil
			}), nil
		},
		newLifecycle: func(
			string,
			cloud.KeepaliveClient,
			logbuf.Logger,
			cloud.KeepaliveOptions,
		) (workerLifecycle, error) {
			return lifecycleFunc(func(ctx context.Context) error {
				<-ctx.Done()
				return nil
			}), nil
		},
		wait: waitContext,
	}
}

func testPlant(number int, plantID string) config.Plant {
	plant := config.DefaultPlant()
	plant.Number = number
	plant.Name = plantID
	plant.Driver = config.DriverRandom
	plant.Cloud.PlantID = plantID
	plant.Cloud.PlantToken = "secret"
	return plant
}

func runAsync(service *Supervisor, ctx context.Context) <-chan error {
	done := make(chan error, 1)
	go func() {
		done <- service.Run(ctx)
	}()
	return done
}

func receive[T any](t *testing.T, channel <-chan T) T {
	t.Helper()
	select {
	case value := <-channel:
		return value
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for test event")
		var zero T
		return zero
	}
}

type fakePlantClient struct {
	mu          sync.Mutex
	handler     cloud.MessageHandler
	connected   bool
	disconnects int
}

func (client *fakePlantClient) Connect(context.Context) error {
	client.mu.Lock()
	defer client.mu.Unlock()
	client.connected = true
	return nil
}

func (client *fakePlantClient) IsConnected() bool {
	client.mu.Lock()
	defer client.mu.Unlock()
	return client.connected
}

func (client *fakePlantClient) PublishContext(
	context.Context,
	string,
	[]byte,
	byte,
) error {
	return nil
}

func (client *fakePlantClient) Disconnect() {
	client.mu.Lock()
	defer client.mu.Unlock()
	client.connected = false
	client.disconnects++
}

func (client *fakePlantClient) SubscribeContext(
	_ context.Context,
	handler cloud.MessageHandler,
) error {
	if handler == nil {
		return errors.New("handler is nil")
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	client.handler = handler
	return nil
}

func (client *fakePlantClient) deliver(payload []byte) {
	client.mu.Lock()
	handler := client.handler
	client.mu.Unlock()
	if handler == nil {
		panic("message delivered before subscription")
	}
	handler(cloud.ToDeviceTopic("test"), payload)
}

type processorFunc func(context.Context, []byte) error

func (function processorFunc) Handle(ctx context.Context, payload []byte) error {
	return function(ctx, payload)
}

type lifecycleFunc func(context.Context) error

func (function lifecycleFunc) Run(ctx context.Context) error {
	return function(ctx)
}
