package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	store, err := New(directory)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	want := PlantState{LastLogDate: "2026-03-10", LastLogPos: 12345}
	if err := store.Save(7, want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	got, err := store.Load(7)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got != want {
		t.Fatalf("Load() = %#v, want %#v", got, want)
	}

	path := filepath.Join(directory, "plant-7.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	const expectedJSON = `{"last_log_date":"2026-03-10","last_log_pos":12345}` + "\n"
	if string(data) != expectedJSON {
		t.Fatalf("state JSON = %q, want %q", data, expectedJSON)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat state file: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("state permissions = %o, want 600", info.Mode().Perm())
	}
}

func TestLoadMissingReturnsZeroValue(t *testing.T) {
	t.Parallel()

	store, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	got, err := store.Load(404)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got != (PlantState{}) {
		t.Fatalf("Load() = %#v, want zero value", got)
	}
}

func TestConcurrentDistinctPlants(t *testing.T) {
	t.Parallel()

	const plantCount = 64
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var wait sync.WaitGroup
	for plant := 1; plant <= plantCount; plant++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			state := PlantState{
				LastLogDate: "2026-07-24",
				LastLogPos:  int64(plant * 100),
			}
			if err := store.Save(plant, state); err != nil {
				t.Errorf("Save(%d) error = %v", plant, err)
			}
		}()
	}
	wait.Wait()

	for plant := 1; plant <= plantCount; plant++ {
		got, err := store.Load(plant)
		if err != nil {
			t.Fatalf("Load(%d) error = %v", plant, err)
		}
		want := PlantState{LastLogDate: "2026-07-24", LastLogPos: int64(plant * 100)}
		if got != want {
			t.Errorf("Load(%d) = %#v, want %#v", plant, got, want)
		}
	}
}

func TestConcurrentSamePlantAlwaysLeavesValidJSON(t *testing.T) {
	t.Parallel()

	const writers = 32
	directory := t.TempDir()
	store, err := New(directory)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var wait sync.WaitGroup
	for writer := range writers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if err := store.Save(1, PlantState{
				LastLogDate: "2026-07-24",
				LastLogPos:  int64(writer),
			}); err != nil {
				t.Errorf("Save() error = %v", err)
			}
		}()
	}
	wait.Wait()

	data, err := os.ReadFile(filepath.Join(directory, "plant-1.json"))
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	var decoded PlantState
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("final state is invalid JSON: %v", err)
	}
	if decoded.LastLogDate != "2026-07-24" ||
		decoded.LastLogPos < 0 ||
		decoded.LastLogPos >= writers {
		t.Fatalf("unexpected final state: %#v", decoded)
	}

	temporaryFiles, err := filepath.Glob(filepath.Join(directory, "*.tmp"))
	if err != nil {
		t.Fatalf("glob temporary files: %v", err)
	}
	if len(temporaryFiles) != 0 {
		t.Fatalf("temporary files remain: %v", temporaryFiles)
	}
}

func TestResolveDirPrecedence(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	haDirectory := filepath.Join(root, "ha")
	if err := os.Mkdir(haDirectory, 0o750); err != nil {
		t.Fatalf("create HA directory: %v", err)
	}

	tests := []struct {
		name    string
		options DirOptions
		want    string
	}{
		{
			name: "explicit",
			options: DirOptions{
				Path:           filepath.Join(root, "explicit"),
				HADataDir:      haDirectory,
				DefaultDataDir: filepath.Join(root, "default"),
				LookupEnv:      mapEnvironment(map[string]string{"GBB_STATE_DIR": filepath.Join(root, "env")}),
			},
			want: filepath.Join(root, "explicit"),
		},
		{
			name: "environment",
			options: DirOptions{
				HADataDir:      haDirectory,
				DefaultDataDir: filepath.Join(root, "default"),
				LookupEnv:      mapEnvironment(map[string]string{"GBB_STATE_DIR": filepath.Join(root, "env")}),
			},
			want: filepath.Join(root, "env"),
		},
		{
			name: "Home Assistant",
			options: DirOptions{
				HADataDir:      haDirectory,
				DefaultDataDir: filepath.Join(root, "default"),
				LookupEnv:      emptyEnvironment,
			},
			want: filepath.Join(haDirectory, "state"),
		},
		{
			name: "OS data directory",
			options: DirOptions{
				HADataDir:      filepath.Join(root, "missing-ha"),
				DefaultDataDir: filepath.Join(root, "default"),
				LookupEnv:      emptyEnvironment,
			},
			want: filepath.Join(root, "default", "gbbconnect", "state"),
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := ResolveDir(test.options)
			if err != nil {
				t.Fatalf("ResolveDir() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("ResolveDir() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestInvalidStateAndDirectory(t *testing.T) {
	t.Parallel()

	if _, err := New(""); err == nil {
		t.Fatal("New(empty) error = nil")
	}

	directory := t.TempDir()
	store, err := New(directory)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(directory, "plant-1.json"), []byte("{"), 0o600); err != nil {
		t.Fatalf("write invalid state: %v", err)
	}
	if _, err := store.Load(1); err == nil {
		t.Fatal("Load(invalid JSON) error = nil")
	}
}

func mapEnvironment(values map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		value, ok := values[name]
		return value, ok
	}
}

func emptyEnvironment(string) (string, bool) {
	return "", false
}
