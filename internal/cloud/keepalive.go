package cloud

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/logbuf"
)

const (
	// KeepaliveInterval is the compatibility heartbeat period.
	KeepaliveInterval = time.Minute
	// ProductionReconnectBackoff is the retry delay used in normal operation.
	ProductionReconnectBackoff = 5 * time.Minute
	// DebugReconnectBackoff is the shorter retry delay used in debug mode.
	DebugReconnectBackoff = 10 * time.Second
)

// KeepaliveClient is the MQTT lifecycle required by KeepaliveLoop.
type KeepaliveClient interface {
	Connect(ctx context.Context) error
	IsConnected() bool
	PublishContext(ctx context.Context, topic string, payload []byte, qos byte) error
	Disconnect()
}

// KeepaliveOptions controls compatibility timing.
type KeepaliveOptions struct {
	Debug bool
}

// KeepaliveLoop owns the MQTT connect, heartbeat, and reconnect lifecycle for
// one enabled plant.
type KeepaliveLoop struct {
	plantID string
	client  KeepaliveClient
	logger  logbuf.Logger
	debug   bool
	clock   loopClock
}

// NewKeepaliveLoop creates a per-plant cloud lifecycle loop.
func NewKeepaliveLoop(
	plantID string,
	client KeepaliveClient,
	logger logbuf.Logger,
	options KeepaliveOptions,
) (*KeepaliveLoop, error) {
	return newKeepaliveLoop(plantID, client, logger, options, realLoopClock{})
}

func newKeepaliveLoop(
	plantID string,
	client KeepaliveClient,
	logger logbuf.Logger,
	options KeepaliveOptions,
	clock loopClock,
) (*KeepaliveLoop, error) {
	if strings.TrimSpace(plantID) == "" {
		return nil, errors.New("keepalive plant id is required")
	}
	if client == nil {
		return nil, errors.New("keepalive MQTT client is required")
	}
	if clock == nil {
		return nil, errors.New("keepalive clock is required")
	}
	if logger == nil {
		logger = noopLogger{}
	}

	return &KeepaliveLoop{
		plantID: plantID,
		client:  client,
		logger: logger.With(
			"component", "mqtt_keepalive",
			"plant_id", plantID,
		),
		debug: options.Debug,
		clock: clock,
	}, nil
}

// Run connects the plant, publishes an immediate heartbeat, and continues at
// one-minute intervals until ctx is cancelled. It is safe to call Run only once
// at a time for a given loop.
func (loop *KeepaliveLoop) Run(ctx context.Context) error {
	if ctx == nil {
		return errors.New("keepalive context is nil")
	}
	defer loop.client.Disconnect()

	for {
		if ctx.Err() != nil {
			return nil
		}
		cycleStart := loop.clock.Now()

		if !loop.client.IsConnected() {
			if err := loop.client.Connect(ctx); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				loop.logger.Error("MQTT connection cycle failed", "error", err)
				backoff := loop.reconnectBackoff()
				loop.logger.Warn("waiting before MQTT reconnect", "duration", backoff)
				if err := loop.wait(ctx, backoff); err != nil {
					return err
				}
				continue
			}
			if !loop.client.IsConnected() {
				loop.logger.Error("MQTT connect completed without an active connection")
				backoff := loop.reconnectBackoff()
				loop.logger.Warn("waiting before MQTT reconnect", "duration", backoff)
				if err := loop.wait(ctx, backoff); err != nil {
					return err
				}
				continue
			}
		}

		loop.logger.Debug("sending MQTT keepalive")
		if err := loop.client.PublishContext(
			ctx,
			KeepaliveTopic(loop.plantID),
			[]byte{},
			1,
		); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			loop.logger.Error("MQTT keepalive failed", "error", err)
		}

		if ctx.Err() != nil {
			return nil
		}
		remaining := KeepaliveInterval - loop.clock.Now().Sub(cycleStart)
		if remaining > 0 {
			if err := loop.wait(ctx, remaining); err != nil {
				return err
			}
		}
	}
}

func (loop *KeepaliveLoop) reconnectBackoff() time.Duration {
	if loop.debug {
		return DebugReconnectBackoff
	}
	return ProductionReconnectBackoff
}

func (loop *KeepaliveLoop) wait(ctx context.Context, duration time.Duration) error {
	if err := loop.clock.Wait(ctx, duration); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("wait in MQTT keepalive loop: %w", err)
	}
	return nil
}

type loopClock interface {
	Now() time.Time
	Wait(ctx context.Context, duration time.Duration) error
}

type realLoopClock struct{}

func (realLoopClock) Now() time.Time {
	return time.Now()
}

func (realLoopClock) Wait(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()

	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
