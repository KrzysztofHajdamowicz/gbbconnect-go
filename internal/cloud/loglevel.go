package cloud

import (
	"errors"
	"fmt"
	"sync"

	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/logbuf"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/state"
)

// LogLevelController changes the live application logging controls.
type LogLevelController interface {
	ApplyCloudLevel(value string) error
}

// LogLevelOptions adjusts how remote logging controls are handled.
type LogLevelOptions struct {
	// IgnoreRemote pins logging to the local configuration: remote LogLevel
	// values are still validated but never applied or persisted. Set by
	// runtime.debug so debugging sessions keep their configured verbosity.
	IgnoreRemote bool
}

// PersistentLogLevelController applies cloud logging controls and saves their
// canonical value in the global runtime state.
type PersistentLogLevelController struct {
	mu           sync.Mutex
	runtime      *logbuf.Runtime
	store        *state.Store
	persisted    state.RuntimeState
	ignoreRemote bool
	lastIgnored  string
}

// NewPersistentLogLevelController restores a saved cloud LogLevel and returns
// the controller used by request handlers.
func NewPersistentLogLevelController(
	runtime *logbuf.Runtime,
	store *state.Store,
	options LogLevelOptions,
) (*PersistentLogLevelController, error) {
	if runtime == nil {
		return nil, errors.New("log runtime is required")
	}
	if store == nil {
		return nil, errors.New("runtime state store is required")
	}

	persisted, err := store.LoadRuntime()
	if err != nil {
		return nil, fmt.Errorf("load cloud log level override: %w", err)
	}
	if persisted.LogLevel != "" {
		if options.IgnoreRemote {
			runtime.Info(
				"ignoring persisted remote logging level override (debug mode)",
				"log_level", persisted.LogLevel,
			)
		} else {
			// Emitted before applying so the notice is visible while the local
			// configuration still permits it.
			runtime.Warn(
				"logging level overridden by remote side",
				"log_level", persisted.LogLevel,
			)
			if err := runtime.ApplyCloudLevel(persisted.LogLevel); err != nil {
				return nil, fmt.Errorf("restore cloud log level override: %w", err)
			}
		}
	}

	return &PersistentLogLevelController{
		runtime:      runtime,
		store:        store,
		persisted:    persisted,
		ignoreRemote: options.IgnoreRemote,
	}, nil
}

// ApplyCloudLevel persists a validated value before changing live logging.
// With IgnoreRemote it only validates and reports the request.
func (controller *PersistentLogLevelController) ApplyCloudLevel(value string) error {
	canonical, err := logbuf.CanonicalCloudLevel(value)
	if err != nil {
		return err
	}

	controller.mu.Lock()
	defer controller.mu.Unlock()

	if controller.ignoreRemote {
		if canonical != controller.lastIgnored {
			controller.runtime.Info(
				"ignoring remote logging level change (debug mode)",
				"log_level", canonical,
			)
			controller.lastIgnored = canonical
		}
		return nil
	}

	if canonical == controller.persisted.LogLevel {
		return nil
	}
	// Emitted before applying so the notice is visible while the current level
	// still permits it (the remote side may be silencing the log).
	controller.runtime.Warn(
		"logging level overridden by remote side",
		"log_level", canonical,
	)

	next := controller.persisted
	next.LogLevel = canonical
	if err := controller.store.SaveRuntime(next); err != nil {
		return fmt.Errorf("persist cloud log level %q: %w", canonical, err)
	}
	if err := controller.runtime.ApplyCloudLevel(canonical); err != nil {
		return fmt.Errorf("apply cloud log level %q: %w", canonical, err)
	}
	controller.persisted = next
	return nil
}

var _ LogLevelController = (*PersistentLogLevelController)(nil)
