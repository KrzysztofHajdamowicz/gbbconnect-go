package cloud

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/config"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/logbuf"
)

const (
	toDeviceQoS             byte = 1
	defaultPublishTimeout        = 30 * time.Second
	disconnectQuiesceMillis      = 250
)

var (
	// ErrNotConnected is returned when publishing without a broker connection.
	ErrNotConnected = errors.New("MQTT client is not connected")
	// ErrInvalidQoS is returned when a QoS value is outside the MQTT range.
	ErrInvalidQoS = errors.New("MQTT QoS must be 0, 1, or 2")
)

// MessageHandler receives a copy of an incoming MQTT message.
//
// Paho invokes the wrapper asynchronously so the handler may perform blocking
// driver work without stalling the MQTT network reader.
type MessageHandler func(topic string, payload []byte)

// Client is a per-plant MQTT/TLS connection to GbbOptimizer.
type Client struct {
	plantID string
	logger  logbuf.Logger
	backend mqttBackend

	handlerMu sync.RWMutex
	handler   MessageHandler

	connectionCount atomic.Uint64
}

// NewClient creates a disconnected MQTT client for one plant.
func NewClient(cloud config.Cloud, logger logbuf.Logger) (*Client, error) {
	return newClient(cloud, logger, newPahoBackend)
}

func newClient(cloud config.Cloud, logger logbuf.Logger, factory backendFactory) (*Client, error) {
	if strings.TrimSpace(cloud.PlantID) == "" {
		return nil, errors.New("MQTT plant id is required")
	}
	if strings.TrimSpace(cloud.PlantToken) == "" {
		return nil, errors.New("MQTT plant token is required")
	}
	if strings.TrimSpace(cloud.MQTTAddress) == "" {
		return nil, errors.New("MQTT broker address is required")
	}
	if cloud.MQTTPort < 1 || cloud.MQTTPort > 65535 {
		return nil, fmt.Errorf("MQTT broker port %d is outside 1..65535", cloud.MQTTPort)
	}
	if factory == nil {
		return nil, errors.New("MQTT backend factory is required")
	}
	if logger == nil {
		logger = noopLogger{}
	}

	client := &Client{
		plantID: cloud.PlantID,
		logger: logger.With(
			"component", "mqtt",
			"plant_id", cloud.PlantID,
		),
	}

	tlsConfig := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		ServerName:         strings.Trim(cloud.MQTTAddress, "[]"),
		InsecureSkipVerify: cloud.TLSInsecureSkipVerify, //nolint:gosec // Explicit troubleshooting setting from validated config.
	}
	if cloud.TLSInsecureSkipVerify {
		client.logger.Warn("MQTT TLS certificate verification is disabled")
	}

	brokerAddress := net.JoinHostPort(
		strings.Trim(cloud.MQTTAddress, "[]"),
		fmt.Sprintf("%d", cloud.MQTTPort),
	)
	options := mqtt.NewClientOptions().
		AddBroker("tls://" + brokerAddress).
		SetClientID("GbbConnect2_" + cloud.PlantID).
		SetUsername(cloud.PlantID).
		SetPassword(cloud.PlantToken).
		SetTLSConfig(tlsConfig).
		SetProtocolVersion(4).
		SetAutoReconnect(true).
		SetConnectRetry(false)

	options.SetOnConnectHandler(func(_ mqtt.Client) {
		client.handleConnected()
	})
	options.SetConnectionLostHandler(func(_ mqtt.Client, err error) {
		client.logger.Warn("MQTT connection lost", "error", err)
	})

	client.backend = factory(options)
	if client.backend == nil {
		return nil, errors.New("MQTT backend factory returned nil")
	}
	return client, nil
}

// Connect establishes the TLS/MQTT session and completes any handler
// subscription registered before the call.
func (c *Client) Connect(ctx context.Context) error {
	if ctx == nil {
		return errors.New("MQTT connect context is nil")
	}
	if c.backend.IsConnected() {
		return nil
	}

	c.logger.Info("connecting to MQTT broker")
	if err := waitToken(ctx, c.backend.Connect()); err != nil {
		return fmt.Errorf("connect to MQTT broker: %w", err)
	}

	// Paho runs OnConnect after completing the connect token. The first
	// callback deliberately skips subscription; doing it here makes initial
	// Connect deterministic and lets subscription errors reach the caller.
	if err := c.subscribeCurrent(ctx); err != nil {
		return err
	}
	c.logger.Info("connected to MQTT broker")
	return nil
}

// Subscribe registers the incoming request handler and, when already
// connected, subscribes immediately at QoS 1.
func (c *Client) Subscribe(handler MessageHandler) error {
	return c.SubscribeContext(context.Background(), handler)
}

// SubscribeContext is Subscribe with caller-controlled cancellation.
func (c *Client) SubscribeContext(ctx context.Context, handler MessageHandler) error {
	if ctx == nil {
		return errors.New("MQTT subscribe context is nil")
	}
	if handler == nil {
		return errors.New("MQTT message handler is required")
	}

	c.handlerMu.Lock()
	c.handler = handler
	c.handlerMu.Unlock()

	if !c.backend.IsConnected() {
		return nil
	}
	return c.subscribeCurrent(ctx)
}

// Publish sends a non-retained MQTT message and waits for the requested QoS
// acknowledgement for at most 30 seconds.
func (c *Client) Publish(topic string, payload []byte, qos byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), defaultPublishTimeout)
	defer cancel()
	return c.PublishContext(ctx, topic, payload, qos)
}

// PublishContext is Publish with caller-controlled cancellation.
func (c *Client) PublishContext(ctx context.Context, topic string, payload []byte, qos byte) error {
	if ctx == nil {
		return errors.New("MQTT publish context is nil")
	}
	if strings.TrimSpace(topic) == "" {
		return errors.New("MQTT publish topic is required")
	}
	if qos > 2 {
		return ErrInvalidQoS
	}
	if !c.backend.IsConnected() {
		return ErrNotConnected
	}

	payloadCopy := append([]byte(nil), payload...)
	if err := waitToken(ctx, c.backend.Publish(topic, qos, false, payloadCopy)); err != nil {
		return fmt.Errorf("publish MQTT message to %q: %w", topic, err)
	}
	return nil
}

// IsConnected reports whether the broker session is active.
func (c *Client) IsConnected() bool {
	return c.backend.IsConnected()
}

// Disconnect ends the MQTT session after allowing queued work to quiesce.
func (c *Client) Disconnect() {
	c.backend.Disconnect(disconnectQuiesceMillis)
	c.connectionCount.Store(0)
}

func (c *Client) handleConnected() {
	connection := c.connectionCount.Add(1)
	if connection == 1 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultPublishTimeout)
	defer cancel()
	if err := c.subscribeCurrent(ctx); err != nil {
		c.logger.Error("failed to restore MQTT subscription", "error", err)
		return
	}
	c.logger.Info("restored MQTT subscription")
}

func (c *Client) subscribeCurrent(ctx context.Context) error {
	c.handlerMu.RLock()
	handler := c.handler
	c.handlerMu.RUnlock()
	if handler == nil {
		return nil
	}

	topic := ToDeviceTopic(c.plantID)
	token := c.backend.Subscribe(topic, toDeviceQoS, func(messageTopic string, payload []byte) {
		payloadCopy := append([]byte(nil), payload...)
		go handler(messageTopic, payloadCopy)
	})
	if err := waitToken(ctx, token); err != nil {
		return fmt.Errorf("subscribe to MQTT topic %q: %w", topic, err)
	}
	return nil
}

func waitToken(ctx context.Context, token mqttToken) error {
	if token == nil {
		return errors.New("MQTT operation returned a nil token")
	}

	select {
	case <-token.Done():
		return token.Error()
	case <-ctx.Done():
		// Prefer an already completed operation if completion and cancellation
		// became ready at the same time.
		select {
		case <-token.Done():
			return token.Error()
		default:
			return ctx.Err()
		}
	}
}

type noopLogger struct{}

func (noopLogger) Debug(string, ...any) {}
func (noopLogger) Info(string, ...any)  {}
func (noopLogger) Warn(string, ...any)  {}
func (noopLogger) Error(string, ...any) {}
func (noopLogger) With(...any) logbuf.Logger {
	return noopLogger{}
}
