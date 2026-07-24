// Package state persists per-plant runtime state.
package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

// PlantState is the durable log-streaming cursor for one plant.
type PlantState struct {
	LastLogDate string `json:"last_log_date"`
	LastLogPos  int64  `json:"last_log_pos"`
}

// RuntimeState contains process-wide overrides changed by cloud requests.
type RuntimeState struct {
	LogLevel string `json:"log_level,omitempty"`
}

// DirOptions controls state-directory resolution.
type DirOptions struct {
	Path            string
	HADataDir       string
	DefaultDataDir  string
	LookupEnv       func(string) (string, bool)
	OperatingSystem string
}

// Store atomically persists plant state files.
type Store struct {
	dir         string
	locks       sync.Map
	runtimeLock sync.Mutex
}

// ResolveDir resolves the writable state directory.
func ResolveDir(options DirOptions) (string, error) {
	if options.Path != "" {
		return options.Path, nil
	}

	lookupEnv := options.LookupEnv
	if lookupEnv == nil {
		lookupEnv = os.LookupEnv
	}
	if path, ok := lookupEnv("GBB_STATE_DIR"); ok && path != "" {
		return path, nil
	}

	haDataDir := options.HADataDir
	if haDataDir == "" {
		haDataDir = "/data"
	}
	if info, err := os.Stat(haDataDir); err == nil && info.IsDir() {
		return filepath.Join(haDataDir, "state"), nil
	}

	dataDir := options.DefaultDataDir
	if dataDir == "" {
		operatingSystem := options.OperatingSystem
		if operatingSystem == "" {
			operatingSystem = runtime.GOOS
		}
		if operatingSystem == "windows" {
			if programData, ok := lookupEnv("ProgramData"); ok && programData != "" {
				dataDir = programData
			} else {
				dataDir = `C:\ProgramData`
			}
		} else {
			var err error
			dataDir, err = os.UserConfigDir()
			if err != nil {
				return "", fmt.Errorf("resolve OS data directory: %w", err)
			}
		}
	}
	return filepath.Join(dataDir, "gbbconnect", "state"), nil
}

// New creates a state store and its directory.
func New(dir string) (*Store, error) {
	if dir == "" {
		return nil, fmt.Errorf("state directory must not be empty")
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("create state directory: %w", err)
	}
	return &Store{dir: dir}, nil
}

// Load returns the current plant state. A missing file yields the zero value.
func (store *Store) Load(plantNumber int) (PlantState, error) {
	lock := store.lockFor(plantNumber)
	lock.Lock()
	defer lock.Unlock()

	path := store.path(plantNumber)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return PlantState{}, nil
	}
	if err != nil {
		return PlantState{}, fmt.Errorf("read state for plant %d: %w", plantNumber, err)
	}

	var result PlantState
	if err := json.Unmarshal(data, &result); err != nil {
		return PlantState{}, fmt.Errorf("decode state for plant %d: %w", plantNumber, err)
	}
	return result, nil
}

// Save atomically replaces the current plant state.
func (store *Store) Save(plantNumber int, state PlantState) error {
	lock := store.lockFor(plantNumber)
	lock.Lock()
	defer lock.Unlock()

	if err := store.saveJSON(
		fmt.Sprintf("state for plant %d", plantNumber),
		store.path(plantNumber),
		fmt.Sprintf(".plant-%d-*.tmp", plantNumber),
		state,
	); err != nil {
		return err
	}
	return nil
}

// LoadRuntime returns the process-wide runtime overrides. A missing file yields
// the zero value.
func (store *Store) LoadRuntime() (RuntimeState, error) {
	store.runtimeLock.Lock()
	defer store.runtimeLock.Unlock()

	data, err := os.ReadFile(store.runtimePath())
	if os.IsNotExist(err) {
		return RuntimeState{}, nil
	}
	if err != nil {
		return RuntimeState{}, fmt.Errorf("read runtime state: %w", err)
	}

	var result RuntimeState
	if err := json.Unmarshal(data, &result); err != nil {
		return RuntimeState{}, fmt.Errorf("decode runtime state: %w", err)
	}
	return result, nil
}

// SaveRuntime atomically replaces the process-wide runtime overrides.
func (store *Store) SaveRuntime(state RuntimeState) error {
	store.runtimeLock.Lock()
	defer store.runtimeLock.Unlock()

	return store.saveJSON(
		"runtime state",
		store.runtimePath(),
		".runtime-*.tmp",
		state,
	)
}

func (store *Store) saveJSON(description, path, temporaryPattern string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode %s: %w", description, err)
	}
	data = append(data, '\n')

	temporary, err := os.CreateTemp(store.dir, temporaryPattern)
	if err != nil {
		return fmt.Errorf("create temporary %s: %w", description, err)
	}
	temporaryPath := temporary.Name()
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()

	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("set temporary %s permissions: %w", description, err)
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write temporary %s: %w", description, err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync temporary %s: %w", description, err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary %s: %w", description, err)
	}

	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace %s: %w", description, err)
	}
	removeTemporary = false
	return nil
}

func (store *Store) path(plantNumber int) string {
	return filepath.Join(store.dir, fmt.Sprintf("plant-%d.json", plantNumber))
}

func (store *Store) runtimePath() string {
	return filepath.Join(store.dir, "runtime.json")
}

func (store *Store) lockFor(plantNumber int) *sync.Mutex {
	lock, _ := store.locks.LoadOrStore(plantNumber, &sync.Mutex{})
	return lock.(*sync.Mutex)
}
