package cloud

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"sync"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/config"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/logbuf"
)

const (
	testPlantID    = "11111111-2222-3333-4444-555555555555"
	testPlantToken = "super-secret-plant-token"
)

func TestTopics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		got  string
		want string
	}{
		{
			name: "to device",
			got:  ToDeviceTopic(testPlantID),
			want: testPlantID + "/ModbusInMqtt/toDevice",
		},
		{
			name: "from device",
			got:  FromDeviceTopic(testPlantID),
			want: testPlantID + "/ModbusInMqtt/fromDevice",
		},
		{
			name: "keepalive",
			got:  KeepaliveTopic(testPlantID),
			want: testPlantID + "/keepalive",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if test.got != test.want {
				t.Fatalf("topic = %q, want %q", test.got, test.want)
			}
		})
	}
}

func TestNewClientBuildsCompatibleOptions(t *testing.T) {
	t.Parallel()

	backend := newFakeBackend()
	client, err := newClient(testCloud(), nil, backend.factory)
	if err != nil {
		t.Fatalf("newClient() error = %v", err)
	}
	if client == nil {
		t.Fatal("newClient() returned nil")
	}

	reader := mqtt.NewOptionsReader(backend.options)
	if got, want := reader.ClientID(), "GbbConnect2_"+testPlantID; got != want {
		t.Errorf("ClientID = %q, want %q", got, want)
	}
	if got := reader.Username(); got != testPlantID {
		t.Errorf("Username = %q, want %q", got, testPlantID)
	}
	if got := reader.Password(); got != testPlantToken {
		t.Errorf("Password = %q, want configured token", got)
	}
	if got := reader.ProtocolVersion(); got != 4 {
		t.Errorf("ProtocolVersion = %d, want 4", got)
	}
	if !reader.AutoReconnect() {
		t.Error("AutoReconnect = false, want true")
	}
	if reader.ConnectRetry() {
		t.Error("ConnectRetry = true, want false")
	}
	if !reader.CleanSession() {
		t.Error("CleanSession = false, want Paho default true")
	}

	servers := reader.Servers()
	if len(servers) != 1 || servers[0].String() != "tls://mqtt.example.test:8883" {
		t.Fatalf("Servers = %v, want [tls://mqtt.example.test:8883]", servers)
	}
	tlsConfig := reader.TLSConfig()
	if tlsConfig == nil {
		t.Fatal("TLSConfig = nil")
	}
	if tlsConfig.InsecureSkipVerify {
		t.Error("InsecureSkipVerify = true, want false")
	}
	if tlsConfig.ServerName != "mqtt.example.test" {
		t.Errorf("ServerName = %q, want mqtt.example.test", tlsConfig.ServerName)
	}
	if tlsConfig.MinVersion != tls.VersionTLS12 {
		t.Errorf("MinVersion = %#x, want TLS 1.2", tlsConfig.MinVersion)
	}
}

func TestNewClientInsecureTLSWarnsWithoutLeakingToken(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	runtime, err := logbuf.New(logbuf.Options{Output: &output})
	if err != nil {
		t.Fatalf("logbuf.New() error = %v", err)
	}
	defer func() {
		if closeErr := runtime.Close(); closeErr != nil {
			t.Errorf("logger close error = %v", closeErr)
		}
	}()

	cloud := testCloud()
	cloud.TLSInsecureSkipVerify = true
	backend := newFakeBackend()
	_, err = newClient(cloud, runtime, backend.factory)
	if err != nil {
		t.Fatalf("newClient() error = %v", err)
	}

	reader := mqtt.NewOptionsReader(backend.options)
	if !reader.TLSConfig().InsecureSkipVerify {
		t.Error("InsecureSkipVerify = false, want explicit true")
	}
	logged := output.String()
	if !bytes.Contains([]byte(logged), []byte("certificate verification is disabled")) {
		t.Errorf("warning log missing: %q", logged)
	}
	if bytes.Contains([]byte(logged), []byte(testPlantToken)) {
		t.Errorf("logs contain plant token: %q", logged)
	}
}

func TestNewClientValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*config.Cloud)
	}{
		{name: "plant id", mutate: func(cloud *config.Cloud) { cloud.PlantID = "" }},
		{name: "plant token", mutate: func(cloud *config.Cloud) { cloud.PlantToken = "" }},
		{name: "address", mutate: func(cloud *config.Cloud) { cloud.MQTTAddress = "" }},
		{name: "low port", mutate: func(cloud *config.Cloud) { cloud.MQTTPort = 0 }},
		{name: "high port", mutate: func(cloud *config.Cloud) { cloud.MQTTPort = 65536 }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			cloud := testCloud()
			test.mutate(&cloud)
			if _, err := newClient(cloud, nil, newFakeBackend().factory); err == nil {
				t.Fatal("newClient() error = nil, want validation error")
			}
		})
	}

	if _, err := newClient(testCloud(), nil, nil); err == nil {
		t.Fatal("newClient() with nil factory error = nil")
	}
	backend := newFakeBackend()
	backend.returnNil = true
	if _, err := newClient(testCloud(), nil, backend.factory); err == nil {
		t.Fatal("newClient() with nil backend error = nil")
	}
}

func TestConnectSubscribesAndReconnectRestoresSubscription(t *testing.T) {
	t.Parallel()

	backend := newFakeBackend()
	client, err := newClient(testCloud(), nil, backend.factory)
	if err != nil {
		t.Fatalf("newClient() error = %v", err)
	}
	if err := client.Subscribe(func(string, []byte) {}); err != nil {
		t.Fatalf("Subscribe() before connect error = %v", err)
	}
	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	subscriptions := backend.subscriptionsSnapshot()
	assertSubscription(t, subscriptions, 1)

	backend.options.OnConnect(nil)
	subscriptions = backend.subscriptionsSnapshot()
	assertSubscription(t, subscriptions, 2)

	if !client.IsConnected() {
		t.Error("IsConnected() = false after connect")
	}
	client.Disconnect()
	if client.IsConnected() {
		t.Error("IsConnected() = true after disconnect")
	}
	if got := backend.disconnectQuiesce; got != disconnectQuiesceMillis {
		t.Errorf("disconnect quiesce = %d, want %d", got, disconnectQuiesceMillis)
	}
}

func TestSubscribeAfterConnectAndMessageDelivery(t *testing.T) {
	t.Parallel()

	backend := newFakeBackend()
	client, err := newClient(testCloud(), nil, backend.factory)
	if err != nil {
		t.Fatalf("newClient() error = %v", err)
	}
	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	delivered := make(chan receivedMessage, 1)
	if err := client.Subscribe(func(topic string, payload []byte) {
		delivered <- receivedMessage{topic: topic, payload: payload}
	}); err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	assertSubscription(t, backend.subscriptionsSnapshot(), 1)

	payload := []byte("request")
	backend.deliver(ToDeviceTopic(testPlantID), payload)
	payload[0] = 'X'

	select {
	case message := <-delivered:
		if message.topic != ToDeviceTopic(testPlantID) {
			t.Errorf("handler topic = %q", message.topic)
		}
		if string(message.payload) != "request" {
			t.Errorf("handler payload = %q, want copied request", message.payload)
		}
	case <-time.After(time.Second):
		t.Fatal("message handler was not called")
	}
}

func TestSubscribeValidationAndFailure(t *testing.T) {
	t.Parallel()

	backend := newFakeBackend()
	client, err := newClient(testCloud(), nil, backend.factory)
	if err != nil {
		t.Fatalf("newClient() error = %v", err)
	}
	if err := client.Subscribe(nil); err == nil {
		t.Fatal("Subscribe(nil) error = nil")
	}
	//nolint:staticcheck // Exercise the public API's defensive nil-context guard.
	if err := client.SubscribeContext(nil, func(string, []byte) {}); err == nil {
		t.Fatal("SubscribeContext(nil, handler) error = nil")
	}

	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	backend.subscribeErr = errors.New("subscription rejected")
	if err := client.Subscribe(func(string, []byte) {}); !errors.Is(err, backend.subscribeErr) {
		t.Fatalf("Subscribe() error = %v, want subscription error", err)
	}
}

func TestPublishHonorsQoSAndPayload(t *testing.T) {
	t.Parallel()

	backend := newFakeBackend()
	client, err := newClient(testCloud(), nil, backend.factory)
	if err != nil {
		t.Fatalf("newClient() error = %v", err)
	}
	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	response := []byte("response")
	if err := client.Publish(FromDeviceTopic(testPlantID), response, 2); err != nil {
		t.Fatalf("response Publish() error = %v", err)
	}
	response[0] = 'X'
	if err := client.Publish(KeepaliveTopic(testPlantID), nil, 1); err != nil {
		t.Fatalf("keepalive Publish() error = %v", err)
	}

	published := backend.publicationsSnapshot()
	if len(published) != 2 {
		t.Fatalf("publish count = %d, want 2", len(published))
	}
	if got := published[0]; got.topic != FromDeviceTopic(testPlantID) ||
		got.qos != 2 || got.retained || string(got.payload) != "response" {
		t.Errorf("response publish = %+v", got)
	}
	if got := published[1]; got.topic != KeepaliveTopic(testPlantID) ||
		got.qos != 1 || got.retained || len(got.payload) != 0 {
		t.Errorf("keepalive publish = %+v", got)
	}
}

func TestPublishValidationAndErrors(t *testing.T) {
	t.Parallel()

	backend := newFakeBackend()
	client, err := newClient(testCloud(), nil, backend.factory)
	if err != nil {
		t.Fatalf("newClient() error = %v", err)
	}
	if err := client.Publish("topic", nil, 1); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("Publish() disconnected error = %v, want ErrNotConnected", err)
	}
	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if err := client.Publish("", nil, 1); err == nil {
		t.Fatal("Publish() empty topic error = nil")
	}
	if err := client.Publish("topic", nil, 3); !errors.Is(err, ErrInvalidQoS) {
		t.Fatalf("Publish() QoS error = %v, want ErrInvalidQoS", err)
	}
	//nolint:staticcheck // Exercise the public API's defensive nil-context guard.
	if err := client.PublishContext(nil, "topic", nil, 1); err == nil {
		t.Fatal("PublishContext(nil, ...) error = nil")
	}

	backend.publishErr = errors.New("broker rejected publish")
	if err := client.Publish("topic", nil, 1); !errors.Is(err, backend.publishErr) {
		t.Fatalf("Publish() error = %v, want broker error", err)
	}
}

func TestConnectAndTokenCancellation(t *testing.T) {
	t.Parallel()

	backend := newFakeBackend()
	backend.connectToken = pendingToken()
	client, err := newClient(testCloud(), nil, backend.factory)
	if err != nil {
		t.Fatalf("newClient() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := client.Connect(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Connect() error = %v, want context.Canceled", err)
	}
	//nolint:staticcheck // Exercise the public API's defensive nil-context guard.
	if err := client.Connect(nil); err == nil {
		t.Fatal("Connect(nil) error = nil")
	}
	if err := waitToken(context.Background(), nil); err == nil {
		t.Fatal("waitToken(nil) error = nil")
	}
}

func TestConnectAndInitialSubscriptionErrors(t *testing.T) {
	t.Parallel()

	connectBackend := newFakeBackend()
	connectBackend.connectToken = completedToken(errors.New("authentication failed"))
	connectClient, err := newClient(testCloud(), nil, connectBackend.factory)
	if err != nil {
		t.Fatalf("newClient() error = %v", err)
	}
	if err := connectClient.Connect(context.Background()); !errors.Is(err, connectBackend.connectToken.err) {
		t.Fatalf("Connect() error = %v, want connect token error", err)
	}

	subscribeBackend := newFakeBackend()
	subscribeBackend.subscribeErr = errors.New("subscribe failed")
	subscribeClient, err := newClient(testCloud(), nil, subscribeBackend.factory)
	if err != nil {
		t.Fatalf("newClient() error = %v", err)
	}
	if err := subscribeClient.Subscribe(func(string, []byte) {}); err != nil {
		t.Fatalf("Subscribe() before connect error = %v", err)
	}
	if err := subscribeClient.Connect(context.Background()); !errors.Is(err, subscribeBackend.subscribeErr) {
		t.Fatalf("Connect() subscription error = %v, want subscribe error", err)
	}
}

func testCloud() config.Cloud {
	return config.Cloud{
		PlantID:     testPlantID,
		PlantToken:  testPlantToken,
		MQTTAddress: "mqtt.example.test",
		MQTTPort:    8883,
	}
}

func assertSubscription(t *testing.T, subscriptions []subscription, wantCount int) {
	t.Helper()
	if len(subscriptions) != wantCount {
		t.Fatalf("subscription count = %d, want %d", len(subscriptions), wantCount)
	}
	got := subscriptions[len(subscriptions)-1]
	if got.topic != ToDeviceTopic(testPlantID) || got.qos != 1 {
		t.Errorf("subscription = %+v, want toDevice QoS 1", got)
	}
}

type fakeToken struct {
	done chan struct{}
	err  error
}

func completedToken(err error) *fakeToken {
	done := make(chan struct{})
	close(done)
	return &fakeToken{done: done, err: err}
}

func pendingToken() *fakeToken {
	return &fakeToken{done: make(chan struct{})}
}

func (token *fakeToken) Done() <-chan struct{} {
	return token.done
}

func (token *fakeToken) Error() error {
	return token.err
}

type subscription struct {
	topic   string
	qos     byte
	handler MessageHandler
}

type publication struct {
	topic    string
	qos      byte
	retained bool
	payload  []byte
}

type receivedMessage struct {
	topic   string
	payload []byte
}

type fakeBackend struct {
	mu sync.Mutex

	options *mqtt.ClientOptions

	connected          bool
	returnNil          bool
	connectToken       *fakeToken
	subscribeErr       error
	publishErr         error
	subscriptions      []subscription
	publications       []publication
	disconnectQuiesce  uint
	onConnectCallCount int
}

func newFakeBackend() *fakeBackend {
	return &fakeBackend{connectToken: completedToken(nil)}
}

func (backend *fakeBackend) factory(options *mqtt.ClientOptions) mqttBackend {
	if backend.returnNil {
		return nil
	}
	backend.options = options
	return backend
}

func (backend *fakeBackend) IsConnected() bool {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	return backend.connected
}

func (backend *fakeBackend) Connect() mqttToken {
	backend.mu.Lock()
	backend.connected = backend.connectToken.err == nil
	backend.onConnectCallCount++
	connected := backend.connected
	onConnect := backend.options.OnConnect
	token := backend.connectToken
	backend.mu.Unlock()

	if connected && onConnect != nil {
		onConnect(nil)
	}
	return token
}

func (backend *fakeBackend) Subscribe(
	topic string,
	qos byte,
	handler MessageHandler,
) mqttToken {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	backend.subscriptions = append(backend.subscriptions, subscription{
		topic:   topic,
		qos:     qos,
		handler: handler,
	})
	return completedToken(backend.subscribeErr)
}

func (backend *fakeBackend) Publish(
	topic string,
	qos byte,
	retained bool,
	payload []byte,
) mqttToken {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	backend.publications = append(backend.publications, publication{
		topic:    topic,
		qos:      qos,
		retained: retained,
		payload:  append([]byte(nil), payload...),
	})
	return completedToken(backend.publishErr)
}

func (backend *fakeBackend) Disconnect(quiesce uint) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	backend.connected = false
	backend.disconnectQuiesce = quiesce
}

func (backend *fakeBackend) subscriptionsSnapshot() []subscription {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	return append([]subscription(nil), backend.subscriptions...)
}

func (backend *fakeBackend) publicationsSnapshot() []publication {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	return append([]publication(nil), backend.publications...)
}

func (backend *fakeBackend) deliver(topic string, payload []byte) {
	backend.mu.Lock()
	subscriptions := append([]subscription(nil), backend.subscriptions...)
	backend.mu.Unlock()

	if len(subscriptions) == 0 {
		return
	}
	subscriptions[len(subscriptions)-1].handler(topic, payload)
}
