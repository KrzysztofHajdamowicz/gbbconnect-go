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
	Version       string
	StateDir      string
	LogDir        string
	Logger        *logbuf.Runtime
	ShutdownGrace time.Duration
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
	shutdown chan struct{}
	stopOnce sync.Once
}

// DefaultShutdownGrace bounds the time allowed for in-flight cloud requests.
const DefaultShutdownGrace = 30 * time.Second

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
	if options.ShutdownGrace < 0 {
		return nil, errors.New("supervisor shutdown grace must not be negative")
	}
	if options.ShutdownGrace == 0 {
		options.ShutdownGrace = DefaultShutdownGrace
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
		shutdown: make(chan struct{}),
	}, nil
}

// Run starts eligible plants concurrently and blocks until ctx is cancelled.
func (supervisor *Supervisor) Run(ctx context.Context) error {
	if ctx == nil {
		return errors.New("supervisor context is nil")
	}

	runCtx, cancelWorkers := context.WithCancel(ctx)
	defer cancelWorkers()

	var workers sync.WaitGroup
	var errorMu sync.Mutex
	var workerErrors []error
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
			if err := supervisor.runPlant(runCtx, plant, plantLogger); err != nil {
				errorMu.Lock()
				workerErrors = append(workerErrors, err)
				errorMu.Unlock()
			}
		}()
	}

	select {
	case <-ctx.Done():
	case <-supervisor.shutdown:
	}
	cancelWorkers()
	workers.Wait()
	return errors.Join(workerErrors...)
}

// Shutdown requests the same graceful path used by process cancellation. It is
// safe to call repeatedly and is the hook used by service integrations.
func (supervisor *Supervisor) Shutdown() {
	supervisor.stopOnce.Do(func() {
		close(supervisor.shutdown)
	})
}

func (supervisor *Supervisor) runPlant(
	ctx context.Context,
	plant config.Plant,
	logger logbuf.Logger,
) error {
	var terminalError error
	for ctx.Err() == nil {
		err := supervisor.safePlantCycle(ctx, plant, logger)
		if ctx.Err() != nil {
			terminalError = err
			break
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
			break
		}
	}
	return errors.Join(
		terminalError,
		supervisor.persistPlantState(plant),
	)
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
	requestCtx, cancelRequests := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelRequests()

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

	requests := newRequestGate()
	panicSignal := make(chan error, 1)
	if err := client.SubscribeContext(
		cycleCtx,
		func(_ string, payload []byte) {
			if !requests.begin() {
				return
			}
			defer requests.done()
			defer func() {
				if recovered := recover(); recovered != nil {
					select {
					case panicSignal <- newPanicError(recovered):
					default:
					}
				}
			}()
			if err := handler.Handle(requestCtx, payload); err != nil &&
				requestCtx.Err() == nil {
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
		requests.close()
		if ctx.Err() != nil {
			supervisor.finishPlantShutdown(
				logger,
				requests,
				cancelRequests,
			)
			return nil
		}
		cancelRequests()
		if !requests.wait(supervisor.options.ShutdownGrace) {
			logger.Warn("request did not stop before worker restart")
		}
		if err != nil {
			return err
		}
		return errors.New("MQTT lifecycle stopped unexpectedly")
	case err := <-panicSignal:
		requests.close()
		cancelCycle()
		client.Disconnect()
		<-lifecycleDone
		cancelRequests()
		if !requests.wait(supervisor.options.ShutdownGrace) {
			logger.Warn("request did not stop after handler panic")
		}
		return err
	case <-ctx.Done():
		requests.close()
		cancelCycle()
		client.Disconnect()
		<-lifecycleDone
		supervisor.finishPlantShutdown(
			logger,
			requests,
			cancelRequests,
		)
		return nil
	}
}

func (supervisor *Supervisor) finishPlantShutdown(
	logger logbuf.Logger,
	requests *requestGate,
	cancelRequests context.CancelFunc,
) {
	if !requests.wait(supervisor.options.ShutdownGrace) {
		logger.Warn(
			"shutdown grace expired; cancelling in-flight requests",
			"grace", supervisor.options.ShutdownGrace,
		)
		cancelRequests()
	} else {
		cancelRequests()
	}
}

func (supervisor *Supervisor) persistPlantState(plant config.Plant) error {
	current, err := supervisor.store.Load(plant.Number)
	if err != nil {
		return fmt.Errorf("load final state for plant %d: %w", plant.Number, err)
	}
	if err := supervisor.store.Save(plant.Number, current); err != nil {
		return fmt.Errorf("save final state for plant %d: %w", plant.Number, err)
	}
	return nil
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

type requestGate struct {
	mu        sync.Mutex
	accepting bool
	active    sync.WaitGroup
}

func newRequestGate() *requestGate {
	return &requestGate{accepting: true}
}

func (gate *requestGate) begin() bool {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	if !gate.accepting {
		return false
	}
	gate.active.Add(1)
	return true
}

func (gate *requestGate) done() {
	gate.active.Done()
}

func (gate *requestGate) close() {
	gate.mu.Lock()
	gate.accepting = false
	gate.mu.Unlock()
}

func (gate *requestGate) wait(timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		gate.active.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
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
