package cloudtest

import (
	"bytes"
	"testing"
)

func TestBrokerCapturesAndClonesPublications(t *testing.T) {
	t.Parallel()

	broker := New(t, Options{Plaintext: true})
	if configuration := broker.CloudConfig("plant", "token"); configuration.UseTLS ||
		configuration.TLSInsecureSkipVerify {
		t.Fatalf("plaintext CloudConfig() = %+v", configuration)
	}
	payload := []byte(`{"request":true}`)
	broker.PublishRequest(t, "plant", payload)
	payload[0] = 'X'

	publications := broker.Publications()
	if len(publications) != 1 {
		t.Fatalf("publication count = %d, want 1", len(publications))
	}
	if publications[0].Topic != "plant/ModbusInMqtt/toDevice" ||
		publications[0].QoS != 1 ||
		publications[0].Retained {
		t.Fatalf("publication = %+v", publications[0])
	}
	if !bytes.Equal(publications[0].Payload, []byte(`{"request":true}`)) {
		t.Fatalf("payload = %q", publications[0].Payload)
	}

	publications[0].Payload[0] = 'Y'
	if broker.Publications()[0].Payload[0] == 'Y' {
		t.Fatal("Publications() returned shared payload storage")
	}
}

func TestBrokerTLSResponseAndKeepaliveAssertions(t *testing.T) {
	t.Parallel()

	broker := New(t, Options{})
	configuration := broker.CloudConfig("plant", "token")
	if configuration.MQTTAddress != "127.0.0.1" ||
		configuration.MQTTPort == 0 ||
		!configuration.UseTLS ||
		!configuration.TLSInsecureSkipVerify {
		t.Fatalf("CloudConfig() = %+v", configuration)
	}

	wantResponse := []byte(`{"OrderId":"order","Lines":[]}`)
	if err := broker.server.Publish(
		"plant/ModbusInMqtt/fromDevice",
		[]byte("{\n\"Lines\": [], \"OrderId\": \"order\"\n}"),
		false,
		2,
	); err != nil {
		t.Fatalf("publish response: %v", err)
	}
	broker.ExpectResponseJSON(t, "plant", wantResponse)
	if err := broker.server.Publish(
		"plant/ModbusInMqtt/fromDevice",
		[]byte(`{"OrderId":"second","Lines":[]}`),
		false,
		2,
	); err != nil {
		t.Fatalf("publish second response: %v", err)
	}
	broker.ExpectResponseJSONAt(
		t,
		"plant",
		2,
		[]byte(`{"Lines":[],"OrderId":"second"}`),
	)

	for range 3 {
		if err := broker.server.Publish(
			"plant/keepalive",
			[]byte{},
			false,
			1,
		); err != nil {
			t.Fatalf("publish keepalive: %v", err)
		}
	}
	if got := broker.ExpectKeepalives(t, "plant", 3); len(got) != 3 {
		t.Fatalf("keepalive count = %d, want 3", len(got))
	}
}
