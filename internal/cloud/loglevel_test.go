package cloud

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/logbuf"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/state"
)

func TestPersistentLogLevelControllerMappingAndRestart(t *testing.T) {
	t.Parallel()

	tests := []struct {
		value        string
		canonical    string
		level        logbuf.Level
		decodedTrace bool
		rawTrace     bool
	}{
		{value: "onlyerrors", canonical: "OnlyErrors", level: logbuf.LevelError},
		{value: "MIN", canonical: "Min", level: logbuf.LevelInfo},
		{
			value:        "mAx",
			canonical:    "Max",
			level:        logbuf.LevelInfo,
			decodedTrace: true,
			rawTrace:     true,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.canonical, func(t *testing.T) {
			t.Parallel()

			store, err := state.New(t.TempDir())
			if err != nil {
				t.Fatalf("state.New() error = %v", err)
			}
			firstRuntime := newDiscardRuntime(t, logbuf.LevelDebug)
			controller, err := NewPersistentLogLevelController(firstRuntime, store, LogLevelOptions{})
			if err != nil {
				t.Fatalf("NewPersistentLogLevelController() error = %v", err)
			}

			if err := controller.ApplyCloudLevel(test.value); err != nil {
				t.Fatalf("ApplyCloudLevel(%q) error = %v", test.value, err)
			}
			assertRuntimeControls(t, firstRuntime, test.level, test.decodedTrace, test.rawTrace)

			persisted, err := store.LoadRuntime()
			if err != nil {
				t.Fatalf("LoadRuntime() error = %v", err)
			}
			if persisted.LogLevel != test.canonical {
				t.Fatalf(
					"persisted LogLevel = %q, want %q",
					persisted.LogLevel,
					test.canonical,
				)
			}

			restartedRuntime := newDiscardRuntime(t, logbuf.LevelDebug)
			restartedRuntime.SetDriverTrace(!test.decodedTrace, !test.rawTrace)
			if _, err := NewPersistentLogLevelController(restartedRuntime, store, LogLevelOptions{}); err != nil {
				t.Fatalf("restart NewPersistentLogLevelController() error = %v", err)
			}
			assertRuntimeControls(
				t,
				restartedRuntime,
				test.level,
				test.decodedTrace,
				test.rawTrace,
			)
		})
	}
}

func TestPersistentLogLevelControllerRejectsUnknownWithoutChanges(t *testing.T) {
	t.Parallel()

	store, err := state.New(t.TempDir())
	if err != nil {
		t.Fatalf("state.New() error = %v", err)
	}
	logRuntime := newDiscardRuntime(t, logbuf.LevelDebug)
	logRuntime.SetDriverTrace(true, false)
	controller, err := NewPersistentLogLevelController(logRuntime, store, LogLevelOptions{})
	if err != nil {
		t.Fatalf("NewPersistentLogLevelController() error = %v", err)
	}

	err = controller.ApplyCloudLevel("unknown")
	if !errors.Is(err, logbuf.ErrUnknownCloudLevel) {
		t.Fatalf("ApplyCloudLevel(unknown) error = %v", err)
	}
	assertRuntimeControls(t, logRuntime, logbuf.LevelDebug, true, false)

	persisted, err := store.LoadRuntime()
	if err != nil {
		t.Fatalf("LoadRuntime() error = %v", err)
	}
	if persisted != (state.RuntimeState{}) {
		t.Fatalf("persisted state = %#v, want zero value", persisted)
	}
}

func TestPersistentLogLevelControllerValidationAndInvalidRestore(t *testing.T) {
	t.Parallel()

	logRuntime := newDiscardRuntime(t, logbuf.LevelInfo)
	store, err := state.New(t.TempDir())
	if err != nil {
		t.Fatalf("state.New() error = %v", err)
	}
	if _, err := NewPersistentLogLevelController(nil, store, LogLevelOptions{}); err == nil {
		t.Fatal("NewPersistentLogLevelController(nil runtime) error = nil")
	}
	if _, err := NewPersistentLogLevelController(logRuntime, nil, LogLevelOptions{}); err == nil {
		t.Fatal("NewPersistentLogLevelController(nil store) error = nil")
	}

	invalidDirectory := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(invalidDirectory, "runtime.json"),
		[]byte(`{"log_level":"all"}`),
		0o600,
	); err != nil {
		t.Fatalf("write invalid runtime state: %v", err)
	}
	invalidStore, err := state.New(invalidDirectory)
	if err != nil {
		t.Fatalf("state.New(invalid directory) error = %v", err)
	}
	if _, err := NewPersistentLogLevelController(logRuntime, invalidStore, LogLevelOptions{}); !errors.Is(
		err,
		logbuf.ErrUnknownCloudLevel,
	) {
		t.Fatalf("invalid restore error = %v", err)
	}
}

func TestPersistentLogLevelControllerIgnoreRemote(t *testing.T) {
	t.Parallel()

	store, err := state.New(t.TempDir())
	if err != nil {
		t.Fatalf("state.New() error = %v", err)
	}
	logRuntime := newDiscardRuntime(t, logbuf.LevelDebug)
	logRuntime.SetDriverTrace(true, true)
	controller, err := NewPersistentLogLevelController(
		logRuntime,
		store,
		LogLevelOptions{IgnoreRemote: true},
	)
	if err != nil {
		t.Fatalf("NewPersistentLogLevelController() error = %v", err)
	}

	if err := controller.ApplyCloudLevel("OnlyErrors"); err != nil {
		t.Fatalf("ApplyCloudLevel(OnlyErrors) error = %v", err)
	}
	assertRuntimeControls(t, logRuntime, logbuf.LevelDebug, true, true)

	persisted, err := store.LoadRuntime()
	if err != nil {
		t.Fatalf("LoadRuntime() error = %v", err)
	}
	if persisted != (state.RuntimeState{}) {
		t.Fatalf("persisted state = %#v, want zero value", persisted)
	}

	if err := controller.ApplyCloudLevel("unknown"); !errors.Is(
		err,
		logbuf.ErrUnknownCloudLevel,
	) {
		t.Fatalf("ApplyCloudLevel(unknown) error = %v", err)
	}
}

func TestPersistentLogLevelControllerIgnoreRemoteSkipsPersistedRestore(t *testing.T) {
	t.Parallel()

	store, err := state.New(t.TempDir())
	if err != nil {
		t.Fatalf("state.New() error = %v", err)
	}
	if err := store.SaveRuntime(state.RuntimeState{LogLevel: "OnlyErrors"}); err != nil {
		t.Fatalf("SaveRuntime() error = %v", err)
	}

	logRuntime := newDiscardRuntime(t, logbuf.LevelDebug)
	logRuntime.SetDriverTrace(true, true)
	if _, err := NewPersistentLogLevelController(
		logRuntime,
		store,
		LogLevelOptions{IgnoreRemote: true},
	); err != nil {
		t.Fatalf("NewPersistentLogLevelController() error = %v", err)
	}
	assertRuntimeControls(t, logRuntime, logbuf.LevelDebug, true, true)
}

func newDiscardRuntime(t *testing.T, level logbuf.Level) *logbuf.Runtime {
	t.Helper()
	runtime, err := logbuf.New(logbuf.Options{Level: level, Output: io.Discard})
	if err != nil {
		t.Fatalf("logbuf.New() error = %v", err)
	}
	return runtime
}

func assertRuntimeControls(
	t *testing.T,
	runtime *logbuf.Runtime,
	level logbuf.Level,
	decodedTrace bool,
	rawTrace bool,
) {
	t.Helper()
	if runtime.Level() != level {
		t.Errorf("runtime level = %v, want %v", runtime.Level(), level)
	}
	if runtime.DriverTraceEnabled() != decodedTrace {
		t.Errorf(
			"decoded trace = %t, want %t",
			runtime.DriverTraceEnabled(),
			decodedTrace,
		)
	}
	if runtime.DriverTraceRawEnabled() != rawTrace {
		t.Errorf(
			"raw trace = %t, want %t",
			runtime.DriverTraceRawEnabled(),
			rawTrace,
		)
	}
}
