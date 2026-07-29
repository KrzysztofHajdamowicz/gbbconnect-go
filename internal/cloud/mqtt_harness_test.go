package cloud

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/cloudtest"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/config"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/driver"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/logbuf"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/modbus"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/testutil"
)

func TestRealMQTTClientRequestResponseAndKeepaliveHarness(t *testing.T) {
	broker := cloudtest.New(t, cloudtest.Options{})
	cloudConfiguration := broker.CloudConfig(testPlantID, testPlantToken)
	if !cloudConfiguration.TLSInsecureSkipVerify {
		t.Fatal("TLS test broker did not enable explicit self-signed trust")
	}

	client, err := NewClient(cloudConfiguration, nil)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	t.Cleanup(client.Disconnect)

	responseRTU, err := modbus.DecodeHex("0103060012003400565886")
	if err != nil {
		t.Fatalf("decode canned RTU response: %v", err)
	}
	inverter := &fakeHandlerDriver{responses: [][]byte{responseRTU}}
	plant := testHandlerPlant()
	plant.Cloud = cloudConfiguration
	handler, err := NewRequestHandler(
		plant,
		client,
		nil,
		HandlerOptions{
			Version:     "1.3.0-go",
			Environment: "Linux",
			DriverFactory: func(
				config.Plant,
				logbuf.Logger,
			) (driver.Driver, error) {
				return inverter, nil
			},
			LogLevelController: noopLogLevelController{},
			LastLogStreamer:    noopLastLogStreamer{},
		},
	)
	if err != nil {
		t.Fatalf("NewRequestHandler() error = %v", err)
	}

	handlerErrors := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.SubscribeContext(
		ctx,
		func(topic string, payload []byte) {
			if topic != ToDeviceTopic(testPlantID) {
				handlerErrors <- errors.New("unexpected MQTT request topic: " + topic)
				return
			}
			handlerErrors <- handler.Handle(ctx, payload)
		},
	); err != nil {
		t.Fatalf("SubscribeContext() error = %v", err)
	}
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect() to TLS test broker error = %v", err)
	}

	request := []byte(
		`{"OrderId":"read-batch-001","Lines":[` +
			`{"LineNo":1,"Modbus":"0103009C0003C5E5"}]}`,
	)
	broker.PublishRequest(t, testPlantID, request)
	broker.ExpectResponseJSON(
		t,
		testPlantID,
		testutil.ReadFixture(t, "protocol_response.json"),
	)
	if err := <-handlerErrors; err != nil {
		t.Fatalf("request handler error = %v", err)
	}
	if len(inverter.requests) != 1 {
		t.Fatalf("inverter request count = %d, want 1", len(inverter.requests))
	}
	testutil.AssertBytesEqual(
		t,
		inverter.requests[0],
		testutil.ReadHexFixture(t, "modbus_read_rtu.hex"),
	)

	clock := newFakeLoopClock(time.Now())
	clock.afterWait = func(waitCount int) {
		if waitCount == 3 {
			cancel()
		}
	}
	loop, err := newKeepaliveLoop(
		testPlantID,
		client,
		nil,
		KeepaliveOptions{},
		clock,
	)
	if err != nil {
		t.Fatalf("newKeepaliveLoop() error = %v", err)
	}
	if err := loop.Run(ctx); err != nil {
		t.Fatalf("keepalive Run() error = %v", err)
	}
	broker.ExpectKeepalives(t, testPlantID, 3)
}

func TestRealMQTTClientConnectsToPlaintextBroker(t *testing.T) {
	broker := cloudtest.New(t, cloudtest.Options{Plaintext: true})
	cloudConfiguration := broker.CloudConfig(testPlantID, testPlantToken)
	if cloudConfiguration.UseTLS {
		t.Fatal("plaintext test broker did not disable TLS")
	}

	client, err := NewClient(cloudConfiguration, nil)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	t.Cleanup(client.Disconnect)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect() to plaintext test broker error = %v", err)
	}

	clock := newFakeLoopClock(time.Now())
	clock.afterWait = func(waitCount int) {
		if waitCount == 3 {
			cancel()
		}
	}
	loop, err := newKeepaliveLoop(
		testPlantID,
		client,
		nil,
		KeepaliveOptions{},
		clock,
	)
	if err != nil {
		t.Fatalf("newKeepaliveLoop() error = %v", err)
	}
	if err := loop.Run(ctx); err != nil {
		t.Fatalf("keepalive Run() error = %v", err)
	}
	broker.ExpectKeepalives(t, testPlantID, 3)
}
