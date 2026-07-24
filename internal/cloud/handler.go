package cloud

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"

	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/config"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/driver"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/logbuf"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/modbus"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/protocol"
)

const responseQoS byte = 2

// ResponsePublisher is the MQTT operation used by RequestHandler.
type ResponsePublisher interface {
	PublishContext(ctx context.Context, topic string, payload []byte, qos byte) error
}

// DriverFactory constructs the target driver for one request.
type DriverFactory func(plant config.Plant, logger logbuf.Logger) (driver.Driver, error)

// HandlerOptions supplies response metadata and test seams.
type HandlerOptions struct {
	Version            string
	Environment        string
	DriverFactory      DriverFactory
	LogLevelController LogLevelController
	LastLogStreamer    LastLogStreamer
}

// RequestHandler serializes and processes cloud requests for one plant.
type RequestHandler struct {
	plant       config.Plant
	publisher   ResponsePublisher
	logger      logbuf.Logger
	version     string
	environment string
	newDriver   DriverFactory
	logLevel    LogLevelController
	lastLog     LastLogStreamer
	token       chan struct{}
}

// NewRequestHandler creates a per-plant cloud request processor.
func NewRequestHandler(
	plant config.Plant,
	publisher ResponsePublisher,
	logger logbuf.Logger,
	options HandlerOptions,
) (*RequestHandler, error) {
	if strings.TrimSpace(plant.Cloud.PlantID) == "" {
		return nil, errors.New("request handler plant id is required")
	}
	if publisher == nil {
		return nil, errors.New("request handler publisher is required")
	}
	if options.LogLevelController == nil {
		return nil, errors.New("request handler log level controller is required")
	}
	if options.LastLogStreamer == nil {
		return nil, errors.New("request handler last log streamer is required")
	}
	if logger == nil {
		logger = noopLogger{}
	}

	version := options.Version
	if version == "" {
		version = "dev"
	}
	environment := options.Environment
	if environment == "" {
		environment = defaultEnvironment(runtime.GOOS)
	}
	factory := options.DriverFactory
	if factory == nil {
		factory = driver.New
	}

	handler := &RequestHandler{
		plant:     plant,
		publisher: publisher,
		logger: logger.With(
			"component", "cloud_handler",
			"plant_id", plant.Cloud.PlantID,
		),
		version:     version,
		environment: environment,
		newDriver:   factory,
		logLevel:    options.LogLevelController,
		lastLog:     options.LastLogStreamer,
		token:       make(chan struct{}, 1),
	}
	handler.token <- struct{}{}
	return handler, nil
}

// Handle decodes, executes, and publishes one MQTT request. Calls are
// serialized for the lifetime of the handler.
func (handler *RequestHandler) Handle(ctx context.Context, payload []byte) error {
	if ctx == nil {
		return errors.New("request handler context is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	select {
	case <-handler.token:
		defer func() {
			handler.token <- struct{}{}
		}()
	case <-ctx.Done():
		return ctx.Err()
	}

	header, err := protocol.Decode(payload)
	if err != nil {
		return err
	}
	if header == nil {
		return nil
	}

	header.GBBVersion = stringPointer(handler.version)
	header.GBBEnvironment = stringPointer(handler.environment)
	if err := handler.applyLogLevel(header); err != nil {
		return err
	}
	handler.execute(ctx, header)
	lastLog, commitLastLog := handler.prepareLastLog(header)

	response, err := protocol.Encode(header)
	if err != nil {
		return err
	}
	if err := handler.publisher.PublishContext(
		ctx,
		FromDeviceTopic(handler.plant.Cloud.PlantID),
		response,
		responseQoS,
	); err != nil {
		return fmt.Errorf("publish cloud response: %w", err)
	}
	if commitLastLog {
		if err := handler.lastLog.Commit(handler.plant.Number, lastLog.State); err != nil {
			return fmt.Errorf("persist last log cursor: %w", err)
		}
	}
	return nil
}

func (handler *RequestHandler) applyLogLevel(header *protocol.Header) error {
	if header.LogLevel == nil {
		return nil
	}

	err := handler.logLevel.ApplyCloudLevel(*header.LogLevel)
	if errors.Is(err, logbuf.ErrUnknownCloudLevel) {
		handler.logger.Warn(
			"unknown cloud log level",
			"log_level", *header.LogLevel,
		)
		return nil
	}
	if err != nil {
		return fmt.Errorf("apply cloud log level: %w", err)
	}
	return nil
}

func (handler *RequestHandler) prepareLastLog(
	header *protocol.Header,
) (LastLogRead, bool) {
	if header.SendLastLog == nil || *header.SendLastLog == 0 {
		return LastLogRead{}, false
	}

	lastLog, err := handler.lastLog.Prepare(handler.plant.Number)
	if err != nil {
		handler.logger.Error("get last log failed", "error", err)
		return LastLogRead{}, false
	}
	header.LastLog = lastLog.Text
	return lastLog, true
}

func (handler *RequestHandler) execute(ctx context.Context, header *protocol.Header) {
	inverterDriver, err := handler.newDriver(handler.plant, handler.logger)
	if err != nil {
		setGlobalError(header, err)
		return
	}
	if inverterDriver == nil {
		setGlobalError(header, errors.New("driver factory returned nil"))
		return
	}
	defer func() {
		if closeErr := inverterDriver.Close(); closeErr != nil {
			handler.logger.Warn("close inverter driver failed", "error", closeErr)
		}
	}()

	if err := inverterDriver.Connect(ctx); err != nil {
		setGlobalError(header, err)
		return
	}

	for index := range header.Lines {
		line := &header.Lines[index]
		if line.Modbus == nil {
			continue
		}

		request, err := modbus.DecodeHex(*line.Modbus)
		if err == nil {
			var response []byte
			response, err = inverterDriver.SendDataToDevice(ctx, request)
			if err == nil {
				encoded := modbus.EncodeHex(response)
				line.Modbus = &encoded
				continue
			}
		}

		message := err.Error()
		line.Error = &message
		clearModbus(header.Lines[index:])
		break
	}
}

func setGlobalError(header *protocol.Header, err error) {
	message := err.Error()
	header.Error = &message
	clearModbus(header.Lines)
}

func clearModbus(lines []protocol.Line) {
	for index := range lines {
		lines[index].Modbus = nil
	}
}

func stringPointer(value string) *string {
	return &value
}

func defaultEnvironment(goos string) string {
	if goos == "" {
		return "gbbconnect-go"
	}
	return strings.ToUpper(goos[:1]) + goos[1:]
}
