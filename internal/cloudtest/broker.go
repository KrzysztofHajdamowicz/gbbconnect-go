// Package cloudtest provides an in-process GbbOptimizer-side MQTT harness.
package cloudtest

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"io"
	"log/slog"
	"math/big"
	"net"
	"reflect"
	"sync"
	"testing"
	"time"

	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"
	"github.com/mochi-mqtt/server/v2/packets"

	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/config"
)

const (
	defaultTimeout = 3 * time.Second
	listenerID     = "cloud-test"
)

// Options controls the test listener. TLS with a generated self-signed
// certificate is the default because the production client always uses TLS.
type Options struct {
	Plaintext bool
}

// Publication is a broker-observed MQTT publish.
type Publication struct {
	Topic    string
	Payload  []byte
	QoS      byte
	Retained bool
}

// Broker is an in-process MQTT broker with direct publish and capture helpers.
type Broker struct {
	server    *mqtt.Server
	host      string
	port      int
	plaintext bool

	mu           sync.Mutex
	publications []Publication
	changed      chan struct{}
}

// New starts a broker on an ephemeral loopback port and registers cleanup with
// the test.
func New(t *testing.T, options Options) *Broker {
	t.Helper()

	server := mqtt.New(&mqtt.Options{
		InlineClient: true,
		Logger: slog.New(slog.NewTextHandler(
			io.Discard,
			nil,
		)),
	})
	if err := server.AddHook(new(auth.AllowHook), nil); err != nil {
		t.Fatalf("add MQTT allow hook: %v", err)
	}

	listenerConfig := listeners.Config{
		ID:      listenerID,
		Address: "127.0.0.1:0",
	}
	if !options.Plaintext {
		listenerConfig.TLSConfig = &tls.Config{
			Certificates: []tls.Certificate{selfSignedCertificate(t)},
			MinVersion:   tls.VersionTLS12,
		}
	}
	listener := listeners.NewTCP(listenerConfig)
	if err := server.AddListener(listener); err != nil {
		t.Fatalf("add MQTT listener: %v", err)
	}

	host, portText, err := net.SplitHostPort(listener.Address())
	if err != nil {
		t.Fatalf("parse MQTT listener address %q: %v", listener.Address(), err)
	}
	port, err := net.LookupPort("tcp", portText)
	if err != nil {
		t.Fatalf("parse MQTT listener port %q: %v", portText, err)
	}

	broker := &Broker{
		server:    server,
		host:      host,
		port:      port,
		plaintext: options.Plaintext,
		changed:   make(chan struct{}, 1),
	}
	capture := func(
		_ *mqtt.Client,
		_ packets.Subscription,
		packet packets.Packet,
	) {
		broker.capture(packet)
	}
	for identifier, filter := range []string{
		"+/ModbusInMqtt/#",
		"+/keepalive",
	} {
		if err := server.Subscribe(filter, identifier+1, capture); err != nil {
			t.Fatalf("subscribe MQTT capture %q: %v", filter, err)
		}
	}
	if err := server.Serve(); err != nil {
		t.Fatalf("start MQTT broker: %v", err)
	}
	t.Cleanup(func() {
		if err := server.Close(); err != nil {
			t.Errorf("close MQTT broker: %v", err)
		}
	})
	return broker
}

// CloudConfig returns endpoint and credentials suitable for the production
// cloud client. A plaintext broker is useful only to non-production test
// clients because gbbconnect intentionally always dials MQTT over TLS.
func (broker *Broker) CloudConfig(plantID, plantToken string) config.Cloud {
	return config.Cloud{
		PlantID:               plantID,
		PlantToken:            plantToken,
		MQTTAddress:           broker.host,
		MQTTPort:              broker.port,
		TLSInsecureSkipVerify: !broker.plaintext,
	}
}

// PublishRequest injects a QoS 1 cloud request on the canonical to-device
// topic.
func (broker *Broker) PublishRequest(
	t *testing.T,
	plantID string,
	payload []byte,
) {
	t.Helper()
	topic := plantID + "/ModbusInMqtt/toDevice"
	if err := broker.server.Publish(topic, bytes.Clone(payload), false, 1); err != nil {
		t.Fatalf("publish MQTT cloud request: %v", err)
	}
}

// ExpectResponseJSON waits for the canonical QoS 2 response and compares JSON
// semantically.
func (broker *Broker) ExpectResponseJSON(
	t *testing.T,
	plantID string,
	want []byte,
) Publication {
	t.Helper()
	return broker.ExpectResponseJSONAt(t, plantID, 1, want)
}

// ExpectResponseJSONAt waits for the numbered canonical QoS 2 response and
// compares its JSON semantically. Ordinals start at one.
func (broker *Broker) ExpectResponseJSONAt(
	t *testing.T,
	plantID string,
	ordinal int,
	want []byte,
) Publication {
	t.Helper()
	if ordinal < 1 {
		t.Fatalf("MQTT response ordinal must be positive, got %d", ordinal)
	}

	topic := plantID + "/ModbusInMqtt/fromDevice"
	publications := broker.waitFor(t, topic, ordinal, defaultTimeout)
	got := publications[ordinal-1]
	if got.QoS != 2 {
		t.Fatalf("MQTT response QoS = %d, want 2", got.QoS)
	}
	assertJSONEqual(t, got.Payload, want)
	return got
}

// ExpectKeepalives waits for count canonical QoS 1 empty heartbeats.
func (broker *Broker) ExpectKeepalives(
	t *testing.T,
	plantID string,
	count int,
) []Publication {
	t.Helper()

	if count < 1 {
		t.Fatalf("keepalive count must be positive, got %d", count)
	}
	topic := plantID + "/keepalive"
	publications := broker.waitFor(t, topic, count, defaultTimeout)
	for index, publication := range publications {
		if publication.QoS != 1 {
			t.Fatalf("MQTT keepalive %d QoS = %d, want 1", index+1, publication.QoS)
		}
		if len(publication.Payload) != 0 {
			t.Fatalf(
				"MQTT keepalive %d payload = %q, want empty",
				index+1,
				publication.Payload,
			)
		}
	}
	return publications
}

// Publications returns an independent snapshot of all observed publishes.
func (broker *Broker) Publications() []Publication {
	broker.mu.Lock()
	defer broker.mu.Unlock()

	result := make([]Publication, len(broker.publications))
	for index, publication := range broker.publications {
		result[index] = clonePublication(publication)
	}
	return result
}

func (broker *Broker) capture(packet packets.Packet) {
	publication := Publication{
		Topic:    packet.TopicName,
		Payload:  bytes.Clone(packet.Payload),
		QoS:      packet.FixedHeader.Qos,
		Retained: packet.FixedHeader.Retain,
	}
	broker.mu.Lock()
	broker.publications = append(broker.publications, publication)
	broker.mu.Unlock()
	select {
	case broker.changed <- struct{}{}:
	default:
	}
}

func (broker *Broker) waitFor(
	t *testing.T,
	topic string,
	count int,
	timeout time.Duration,
) []Publication {
	t.Helper()

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		var matches []Publication
		for _, publication := range broker.Publications() {
			if publication.Topic == topic {
				matches = append(matches, publication)
			}
		}
		if len(matches) >= count {
			return matches[:count]
		}

		select {
		case <-broker.changed:
		case <-timer.C:
			t.Fatalf(
				"timed out waiting for %d MQTT publishes to %q; observed %#v",
				count,
				topic,
				broker.Publications(),
			)
		}
	}
}

func clonePublication(publication Publication) Publication {
	publication.Payload = bytes.Clone(publication.Payload)
	return publication
}

func assertJSONEqual(t *testing.T, got, want []byte) {
	t.Helper()

	var gotValue any
	if err := json.Unmarshal(got, &gotValue); err != nil {
		t.Fatalf("decode actual MQTT response JSON %q: %v", got, err)
	}
	var wantValue any
	if err := json.Unmarshal(want, &wantValue); err != nil {
		t.Fatalf("decode expected MQTT response JSON %q: %v", want, err)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Fatalf("MQTT response JSON = %s, want %s", got, want)
	}
}

func selfSignedCertificate(t *testing.T) tls.Certificate {
	t.Helper()

	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate MQTT TLS key: %v", err)
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: big.NewInt(now.UnixNano()),
		Subject: pkix.Name{
			CommonName: "gbbconnect cloud test",
		},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1)},
	}
	der, err := x509.CreateCertificate(
		rand.Reader,
		template,
		template,
		&privateKey.PublicKey,
		privateKey,
	)
	if err != nil {
		t.Fatalf("create MQTT TLS certificate: %v", err)
	}
	return tls.Certificate{
		Certificate: [][]byte{der},
		PrivateKey:  privateKey,
		Leaf:        template,
	}
}
