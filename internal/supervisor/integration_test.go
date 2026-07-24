package supervisor

import (
	"context"
	"encoding/json"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/cloud"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/cloudtest"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/config"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/invertertest"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/logbuf"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/modbus"
)

const integrationEnvironment = "Integration"

func TestSupervisorEndToEndCompatibilityFlow(t *testing.T) {
	broker := cloudtest.New(t, cloudtest.Options{})
	primary := invertertest.Start(
		t,
		invertertest.ProtocolSolarmanV5,
		invertertest.ScenarioNormal,
	)
	sub := invertertest.Start(
		t,
		invertertest.ProtocolSolarmanV5,
		invertertest.ScenarioNormal,
	)

	const (
		plantID   = "e2e-plant"
		subSerial = int64(987654321)
	)
	plant := integrationPlant(1, plantID, primary, broker)
	subPlant := sub.Plant()
	plant.SubInverters = []config.SubInverter{{
		Serial:       subSerial,
		DongleSerial: subPlant.Serial,
		Address:      subPlant.Address,
		Port:         subPlant.Port,
	}}

	clock := newIntegrationClock()
	tracker := &disconnectTracker{}
	service := newTestSupervisor(
		t,
		integrationConfig(plant),
		integrationDependencies(
			map[string]*integrationClock{plantID: clock},
			map[string]*disconnectTracker{plantID: tracker},
		),
	)
	running := startIntegrationSupervisor(t, service)
	broker.ExpectKeepalives(t, plantID, 1)

	readRequest := modbus.BuildReadHoldingRegisters(1, 0x009C, 2)
	broker.PublishRequest(t, plantID, marshalIntegrationJSON(t, map[string]any{
		"OrderId": "read-e2e",
		"Lines": []map[string]any{{
			"LineNo": 1,
			"Modbus": modbus.EncodeHex(readRequest),
		}},
	}))
	readResponse := modbus.AppendCRC([]byte{1, 3, 4, 0, 1, 0, 2})
	broker.ExpectResponseJSONAt(
		t,
		plantID,
		1,
		integrationResponse(t, map[string]any{
			"OrderId": "read-e2e",
			"Lines": []map[string]any{{
				"LineNo": 1,
				"Modbus": modbus.EncodeHex(readResponse),
			}},
		}),
	)

	cascadeRequest := modbus.BuildReadHoldingRegisters(1, 0, 1)
	_, decodeError := modbus.DecodeHex("GG")
	broker.PublishRequest(t, plantID, marshalIntegrationJSON(t, map[string]any{
		"OrderId": "cascade-e2e",
		"Lines": []map[string]any{
			{"LineNo": 1, "Modbus": modbus.EncodeHex(cascadeRequest)},
			{"LineNo": 2, "Modbus": "GG"},
			{"LineNo": 3, "Modbus": modbus.EncodeHex(cascadeRequest)},
		},
	}))
	cascadeResponse := modbus.AppendCRC([]byte{1, 3, 2, 0, 1})
	broker.ExpectResponseJSONAt(
		t,
		plantID,
		2,
		integrationResponse(t, map[string]any{
			"OrderId": "cascade-e2e",
			"Lines": []map[string]any{
				{
					"LineNo": 1,
					"Modbus": modbus.EncodeHex(cascadeResponse),
				},
				{"LineNo": 2, "Error": decodeError.Error()},
				{"LineNo": 3},
			},
		}),
	)

	broker.PublishRequest(t, plantID, marshalIntegrationJSON(t, map[string]any{
		"OrderId":       "sub-e2e",
		"SubInverterSN": strconv.FormatInt(subSerial, 10),
		"Lines": []map[string]any{{
			"LineNo": 1,
			"Modbus": modbus.EncodeHex(cascadeRequest),
		}},
	}))
	broker.ExpectResponseJSONAt(
		t,
		plantID,
		3,
		integrationResponse(t, map[string]any{
			"OrderId":       "sub-e2e",
			"SubInverterSN": strconv.FormatInt(subSerial, 10),
			"Lines": []map[string]any{{
				"LineNo": 1,
				"Modbus": modbus.EncodeHex(cascadeResponse),
			}},
		}),
	)
	if primary.Requests() != 2 || sub.Requests() != 1 {
		t.Fatalf(
			"inverter requests primary=%d sub=%d, want 2 and 1",
			primary.Requests(),
			sub.Requests(),
		)
	}

	const unknownSerial = "999"
	broker.PublishRequest(t, plantID, marshalIntegrationJSON(t, map[string]any{
		"OrderId":       "unknown-sub-e2e",
		"SubInverterSN": unknownSerial,
		"Lines": []map[string]any{{
			"LineNo": 1,
			"Modbus": modbus.EncodeHex(cascadeRequest),
		}},
	}))
	broker.ExpectResponseJSONAt(
		t,
		plantID,
		4,
		integrationResponse(t, map[string]any{
			"Error":         "Inverter SerialNumber not found: 999 on Slave Inverters list!",
			"OrderId":       "unknown-sub-e2e",
			"SubInverterSN": unknownSerial,
			"Lines":         []map[string]any{{"LineNo": 1}},
		}),
	)
	if primary.Requests() != 2 || sub.Requests() != 1 {
		t.Fatal("unknown sub-inverter request reached an inverter transport")
	}

	clock.AdvanceMinute(t)
	broker.ExpectKeepalives(t, plantID, 2)
	clock.AdvanceMinute(t)
	broker.ExpectKeepalives(t, plantID, 3)

	running.Stop(t)
	assertPlantStateExists(t, service, plant.Number)
	if tracker.disconnects.Load() == 0 {
		t.Fatal("MQTT client was not disconnected during shutdown")
	}
}

func TestSupervisorMultiPlantFaultIsolation(t *testing.T) {
	broker := cloudtest.New(t, cloudtest.Options{})
	failing := invertertest.Start(
		t,
		invertertest.ProtocolSolarmanV5,
		invertertest.ScenarioMalformed,
	)
	healthy := invertertest.Start(
		t,
		invertertest.ProtocolSolarmanV5,
		invertertest.ScenarioNormal,
	)

	const (
		failingID = "faulted-plant"
		healthyID = "healthy-plant"
	)
	failingPlant := integrationPlant(1, failingID, failing, broker)
	healthyPlant := integrationPlant(2, healthyID, healthy, broker)
	clocks := map[string]*integrationClock{
		failingID: newIntegrationClock(),
		healthyID: newIntegrationClock(),
	}
	trackers := map[string]*disconnectTracker{
		failingID: {},
		healthyID: {},
	}
	service := newTestSupervisor(
		t,
		integrationConfig(failingPlant, healthyPlant),
		integrationDependencies(clocks, trackers),
	)
	running := startIntegrationSupervisor(t, service)
	broker.ExpectKeepalives(t, failingID, 1)
	broker.ExpectKeepalives(t, healthyID, 1)

	request := modbus.BuildReadHoldingRegisters(1, 0, 1)
	payload := func(orderID string) []byte {
		return marshalIntegrationJSON(t, map[string]any{
			"OrderId": orderID,
			"Lines": []map[string]any{{
				"LineNo": 1,
				"Modbus": modbus.EncodeHex(request),
			}},
		})
	}
	broker.PublishRequest(t, failingID, payload("faulted"))
	broker.PublishRequest(t, healthyID, payload("healthy"))

	broker.ExpectResponseJSON(
		t,
		healthyID,
		integrationResponse(t, map[string]any{
			"OrderId": "healthy",
			"Lines": []map[string]any{{
				"LineNo": 1,
				"Modbus": modbus.EncodeHex(
					modbus.AppendCRC([]byte{1, 3, 2, 0, 1}),
				),
			}},
		}),
	)
	broker.ExpectResponseJSON(
		t,
		failingID,
		integrationResponse(t, map[string]any{
			"OrderId": "faulted",
			"Lines": []map[string]any{{
				"LineNo": 1,
				"Error":  "SolarmanV5: Wrong ControlCode",
			}},
		}),
	)

	running.Stop(t)
	assertPlantStateExists(t, service, failingPlant.Number)
	assertPlantStateExists(t, service, healthyPlant.Number)
	for plantID, tracker := range trackers {
		if tracker.disconnects.Load() == 0 {
			t.Fatalf("MQTT client for %s was not disconnected", plantID)
		}
	}
}

func integrationConfig(plants ...config.Plant) config.Config {
	configuration := config.Default()
	configuration.Runtime.GBBEnvironment = integrationEnvironment
	configuration.Plants = plants
	return configuration
}

func integrationPlant(
	number int,
	plantID string,
	harness *invertertest.Harness,
	broker *cloudtest.Broker,
) config.Plant {
	plant := harness.Plant()
	plant.Number = number
	plant.Name = plantID
	plant.Enabled = true
	plant.Cloud = broker.CloudConfig(plantID, "integration-token")
	return plant
}

func integrationDependencies(
	clocks map[string]*integrationClock,
	trackers map[string]*disconnectTracker,
) dependencies {
	deps := defaultDependencies()
	deps.newClient = func(
		configuration config.Cloud,
		logger logbuf.Logger,
	) (plantClient, error) {
		client, err := cloud.NewClient(configuration, logger)
		if err != nil {
			return nil, err
		}
		return &trackingPlantClient{
			plantClient: client,
			tracker:     trackers[configuration.PlantID],
		}, nil
	}
	deps.newLifecycle = func(
		plantID string,
		client cloud.KeepaliveClient,
		logger logbuf.Logger,
		options cloud.KeepaliveOptions,
	) (workerLifecycle, error) {
		options.Clock = clocks[plantID]
		return cloud.NewKeepaliveLoop(plantID, client, logger, options)
	}
	return deps
}

func integrationResponse(t *testing.T, values map[string]any) []byte {
	t.Helper()
	values["GbbVersion"] = "test-version"
	values["GbbEnvironment"] = integrationEnvironment
	return marshalIntegrationJSON(t, values)
}

func marshalIntegrationJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal integration JSON: %v", err)
	}
	return encoded
}

type integrationClock struct {
	mu      sync.Mutex
	now     time.Time
	waits   chan time.Duration
	advance chan struct{}
}

func newIntegrationClock() *integrationClock {
	return &integrationClock{
		now:     time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC),
		waits:   make(chan time.Duration),
		advance: make(chan struct{}),
	}
}

func (clock *integrationClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}

func (clock *integrationClock) Wait(
	ctx context.Context,
	duration time.Duration,
) error {
	select {
	case clock.waits <- duration:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-clock.advance:
		clock.mu.Lock()
		clock.now = clock.now.Add(duration)
		clock.mu.Unlock()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (clock *integrationClock) AdvanceMinute(t *testing.T) {
	t.Helper()
	select {
	case duration := <-clock.waits:
		if duration != cloud.KeepaliveInterval {
			t.Fatalf(
				"keepalive wait = %s, want %s",
				duration,
				cloud.KeepaliveInterval,
			)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for keepalive clock")
	}
	clock.advance <- struct{}{}
}

type disconnectTracker struct {
	disconnects atomic.Int32
}

type trackingPlantClient struct {
	plantClient
	tracker *disconnectTracker
}

func (client *trackingPlantClient) Disconnect() {
	client.tracker.disconnects.Add(1)
	client.plantClient.Disconnect()
}

type integrationSupervisorRun struct {
	cancel context.CancelFunc
	done   <-chan error
	once   sync.Once
}

func startIntegrationSupervisor(
	t *testing.T,
	service *Supervisor,
) *integrationSupervisorRun {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	running := &integrationSupervisorRun{
		cancel: cancel,
		done:   runAsync(service, ctx),
	}
	t.Cleanup(func() {
		running.Stop(t)
	})
	return running
}

func (running *integrationSupervisorRun) Stop(t *testing.T) {
	t.Helper()
	running.once.Do(func() {
		running.cancel()
		select {
		case err := <-running.done:
			if err != nil {
				t.Errorf("Supervisor.Run() error = %v", err)
			}
		case <-time.After(3 * time.Second):
			t.Error("timed out stopping integration supervisor")
		}
	})
}
