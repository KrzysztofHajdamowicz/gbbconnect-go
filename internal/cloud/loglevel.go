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

// PersistentLogLevelController applies cloud logging controls and saves their
// canonical value in the global runtime state.
type PersistentLogLevelController struct {
	mu        sync.Mutex
	runtime   *logbuf.Runtime
	store     *state.Store
	persisted state.RuntimeState
}

// NewPersistentLogLevelController restores a saved cloud LogLevel and returns
// the controller used by request handlers.
func NewPersistentLogLevelController(
	runtime *logbuf.Runtime,
	store *state.Store,
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
		if err := runtime.ApplyCloudLevel(persisted.LogLevel); err != nil {
			return nil, fmt.Errorf("restore cloud log level override: %w", err)
		}
	}

	return &PersistentLogLevelController{
		runtime:   runtime,
		store:     store,
		persisted: persisted,
	}, nil
}

// ApplyCloudLevel persists a validated value before changing live logging.
func (controller *PersistentLogLevelController) ApplyCloudLevel(value string) error {
	canonical, err := logbuf.CanonicalCloudLevel(value)
	if err != nil {
		return err
	}

	controller.mu.Lock()
	defer controller.mu.Unlock()

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
