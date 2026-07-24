package supervisor

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/cloud"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/config"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/logbuf"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/state"
)

// Options supplies the process resources shared by all plant workers.
type Options struct {
	Version  string
	StateDir string
	LogDir   string
	Logger   *logbuf.Runtime
}

type plantClient interface {
	cloud.KeepaliveClient
	cloud.ResponsePublisher
	SubscribeContext(ctx context.Context, handler cloud.MessageHandler) error
}

type requestProcessor interface {
	Handle(ctx context.Context, payload []byte) error
}

type workerLifecycle interface {
	Run(ctx context.Context) error
}

type dependencies struct {
	newClient  func(config.Cloud, logbuf.Logger) (plantClient, error)
	newHandler func(
		config.Plant,
		cloud.ResponsePublisher,
		logbuf.Logger,
		cloud.HandlerOptions,
	) (requestProcessor, error)
	newLifecycle func(
		string,
		cloud.KeepaliveClient,
		logbuf.Logger,
		cloud.KeepaliveOptions,
	) (workerLifecycle, error)
	wait func(context.Context, time.Duration) error
}

// Supervisor owns one independently restarting worker per eligible plant.
type Supervisor struct {
	config   config.Config
	options  Options
	logger   logbuf.Logger
	store    *state.Store
	logLevel cloud.LogLevelController
	lastLog  cloud.LastLogStreamer
	deps     dependencies
}

// New builds the shared runtime resources used by plant workers.
func New(configuration config.Config, options Options) (*Supervisor, error) {
	return newSupervisor(configuration, options, defaultDependencies())
}

func newSupervisor(
	configuration config.Config,
	options Options,
	deps dependencies,
) (*Supervisor, error) {
	if options.Logger == nil {
		return nil, errors.New("supervisor logging runtime is required")
	}
	if strings.TrimSpace(options.StateDir) == "" {
		return nil, errors.New("supervisor state directory is required")
	}
	if strings.TrimSpace(options.LogDir) == "" {
		return nil, errors.New("supervisor log directory is required")
	}
	if deps.newClient == nil ||
		deps.newHandler == nil ||
		deps.newLifecycle == nil ||
		deps.wait == nil {
		return nil, errors.New("supervisor dependencies are incomplete")
	}

	store, err := state.New(options.StateDir)
	if err != nil {
		return nil, err
	}
	logLevel, err := cloud.NewPersistentLogLevelController(
		options.Logger,
		store,
	)
	if err != nil {
		return nil, err
	}
	lastLog, err := cloud.NewDailyLogStreamer(store, options.LogDir, nil)
	if err != nil {
		return nil, err
	}

	return &Supervisor{
		config:   configuration,
		options:  options,
		logger:   options.Logger.With("component", "supervisor"),
		store:    store,
		logLevel: logLevel,
		lastLog:  lastLog,
		deps:     deps,
	}, nil
}

// Run starts eligible plants concurrently and blocks until ctx is cancelled.
func (supervisor *Supervisor) Run(ctx context.Context) error {
	if ctx == nil {
		return errors.New("supervisor context is nil")
	}

	var workers sync.WaitGroup
	for _, plant := range supervisor.config.Plants {
		plantLogger := supervisor.logger.With(
			"plant_number", plant.Number,
			"plant_name", plant.Name,
		)
		switch {
		case !plant.Enabled:
			plantLogger.Info("skipping disabled plant")
			continue
		case strings.TrimSpace(plant.Cloud.PlantID) == "" ||
			strings.TrimSpace(plant.Cloud.PlantToken) == "":
			plantLogger.Warn("skipping plant without cloud credentials")
			continue
		}

		plant := plant
		workers.Add(1)
		go func() {
			defer workers.Done()
			supervisor.runPlant(ctx, plant, plantLogger)
		}()
	}

	<-ctx.Done()
	workers.Wait()
	return nil
}

func (supervisor *Supervisor) runPlant(
	ctx context.Context,
	plant config.Plant,
	logger logbuf.Logger,
) {
	for ctx.Err() == nil {
		err := supervisor.safePlantCycle(ctx, plant, logger)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			logger.Error("plant worker cycle failed", "error", err)
		} else {
			logger.Warn("plant worker cycle stopped unexpectedly")
		}

		backoff := cloud.ProductionReconnectBackoff
		if supervisor.config.Runtime.Debug {
			backoff = cloud.DebugReconnectBackoff
		}
		logger.Warn("restarting plant worker after backoff", "duration", backoff)
		if err := supervisor.deps.wait(ctx, backoff); err != nil {
			if ctx.Err() == nil {
				logger.Error("plant worker backoff failed", "error", err)
			}
			return
		}
	}
}

func (supervisor *Supervisor) safePlantCycle(
	ctx context.Context,
	plant config.Plant,
	logger logbuf.Logger,
) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = newPanicError(recovered)
		}
	}()
	return supervisor.runPlantCycle(ctx, plant, logger)
}

func (supervisor *Supervisor) runPlantCycle(
	ctx context.Context,
	plant config.Plant,
	logger logbuf.Logger,
) error {
	if _, err := supervisor.store.Load(plant.Number); err != nil {
		return err
	}

	client, err := supervisor.deps.newClient(plant.Cloud, logger)
	if err != nil {
		return fmt.Errorf("create MQTT client: %w", err)
	}
	if client == nil {
		return errors.New("MQTT client factory returned nil")
	}
	cycleCtx, cancelCycle := context.WithCancel(ctx)
	defer cancelCycle()

	handler, err := supervisor.deps.newHandler(
		plant,
		client,
		logger,
		cloud.HandlerOptions{
			Version:            supervisor.options.Version,
			Environment:        supervisor.config.Runtime.GBBEnvironment,
			LogLevelController: supervisor.logLevel,
			LastLogStreamer:    supervisor.lastLog,
		},
	)
	if err != nil {
		return fmt.Errorf("create cloud request handler: %w", err)
	}
	if handler == nil {
		return errors.New("request handler factory returned nil")
	}

	panicSignal := make(chan error, 1)
	if err := client.SubscribeContext(
		cycleCtx,
		func(_ string, payload []byte) {
			defer func() {
				if recovered := recover(); recovered != nil {
					select {
					case panicSignal <- newPanicError(recovered):
					default:
					}
				}
			}()
			if err := handler.Handle(cycleCtx, payload); err != nil &&
				cycleCtx.Err() == nil {
				logger.Error("cloud request failed", "error", err)
			}
		},
	); err != nil {
		return fmt.Errorf("subscribe cloud request handler: %w", err)
	}

	lifecycle, err := supervisor.deps.newLifecycle(
		plant.Cloud.PlantID,
		client,
		logger,
		cloud.KeepaliveOptions{Debug: supervisor.config.Runtime.Debug},
	)
	if err != nil {
		return fmt.Errorf("create MQTT lifecycle: %w", err)
	}
	if lifecycle == nil {
		return errors.New("MQTT lifecycle factory returned nil")
	}

	lifecycleDone := make(chan error, 1)
	go func() {
		var result error
		defer func() {
			if recovered := recover(); recovered != nil {
				result = newPanicError(recovered)
			}
			lifecycleDone <- result
		}()
		result = lifecycle.Run(cycleCtx)
	}()

	select {
	case err := <-lifecycleDone:
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return nil
		}
		return errors.New("MQTT lifecycle stopped unexpectedly")
	case err := <-panicSignal:
		cancelCycle()
		client.Disconnect()
		<-lifecycleDone
		return err
	case <-ctx.Done():
		cancelCycle()
		client.Disconnect()
		<-lifecycleDone
		return nil
	}
}

func defaultDependencies() dependencies {
	return dependencies{
		newClient: func(
			configuration config.Cloud,
			logger logbuf.Logger,
		) (plantClient, error) {
			return cloud.NewClient(configuration, logger)
		},
		newHandler: func(
			plant config.Plant,
			publisher cloud.ResponsePublisher,
			logger logbuf.Logger,
			options cloud.HandlerOptions,
		) (requestProcessor, error) {
			return cloud.NewRequestHandler(plant, publisher, logger, options)
		},
		newLifecycle: func(
			plantID string,
			client cloud.KeepaliveClient,
			logger logbuf.Logger,
			options cloud.KeepaliveOptions,
		) (workerLifecycle, error) {
			return cloud.NewKeepaliveLoop(plantID, client, logger, options)
		},
		wait: waitContext,
	}
}

func waitContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type panicError struct {
	value any
	stack []byte
}

func newPanicError(value any) error {
	return &panicError{value: value, stack: debug.Stack()}
}

func (err *panicError) Error() string {
	return fmt.Sprintf("recovered panic: %v\n%s", err.value, err.stack)
}
