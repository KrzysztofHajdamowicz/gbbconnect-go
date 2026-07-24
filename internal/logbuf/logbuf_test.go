package logbuf

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLevelGating(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	runtime, err := New(Options{
		Level:  LevelError,
		Format: FormatText,
		Output: &output,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	runtime.Debug("debug")
	runtime.Info("info")
	runtime.Warn("warn")
	runtime.Error("error")

	text := output.String()
	if strings.Contains(text, "debug") || strings.Contains(text, "info") || strings.Contains(text, "warn") {
		t.Fatalf("errors-only output contains suppressed messages: %q", text)
	}
	if !strings.Contains(text, "error") {
		t.Fatalf("errors-only output does not contain error: %q", text)
	}

	if err := runtime.SetLevel(LevelDebug); err != nil {
		t.Fatalf("SetLevel() error = %v", err)
	}
	runtime.Debug("now-visible")
	if !strings.Contains(output.String(), "now-visible") {
		t.Fatalf("debug output was not enabled: %q", output.String())
	}
}

func TestCloudLevelMapping(t *testing.T) {
	t.Parallel()

	runtime, err := New(Options{Level: LevelDebug, Output: io.Discard})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	tests := []struct {
		value        string
		level        Level
		decodedTrace bool
		rawTrace     bool
	}{
		{value: "onlyerrors", level: LevelError},
		{value: "MIN", level: LevelInfo},
		{value: "Max", level: LevelInfo, decodedTrace: true, rawTrace: true},
	}

	for _, test := range tests {
		if err := runtime.ApplyCloudLevel(test.value); err != nil {
			t.Fatalf("ApplyCloudLevel(%q) error = %v", test.value, err)
		}
		if runtime.Level() != test.level {
			t.Errorf("ApplyCloudLevel(%q) level = %v, want %v", test.value, runtime.Level(), test.level)
		}
		if runtime.DriverTraceEnabled() != test.decodedTrace {
			t.Errorf("ApplyCloudLevel(%q) decoded trace = %t, want %t",
				test.value, runtime.DriverTraceEnabled(), test.decodedTrace)
		}
		if runtime.DriverTraceRawEnabled() != test.rawTrace {
			t.Errorf("ApplyCloudLevel(%q) raw trace = %t, want %t",
				test.value, runtime.DriverTraceRawEnabled(), test.rawTrace)
		}
	}

	if err := runtime.ApplyCloudLevel("unknown"); err == nil {
		t.Fatal("ApplyCloudLevel(unknown) error = nil, want an error")
	}
}

func TestJSONAndSecretRedaction(t *testing.T) {
	t.Parallel()

	const token = "super-secret-token"
	var output bytes.Buffer
	runtime, err := New(Options{
		Level:  LevelInfo,
		Format: FormatJSON,
		Output: &output,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	runtime.With("plant", 17).Info("connected", "plant_token", Secret(token))

	if strings.Contains(output.String(), token) {
		t.Fatalf("output contains raw token: %q", output.String())
	}

	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatalf("JSON output is invalid: %v", err)
	}
	if record["plant_token"] != redactedText {
		t.Fatalf("plant_token = %v, want %q", record["plant_token"], redactedText)
	}
	if record["plant"] != float64(17) {
		t.Fatalf("plant = %v, want 17", record["plant"])
	}
}

func TestDailyFileRolloverAndFormat(t *testing.T) {
	t.Parallel()

	clock := &testClock{value: time.Date(2026, 7, 24, 12, 34, 56, 789_000_000, time.Local)}
	logDir := t.TempDir()
	runtime, err := New(Options{
		Level:  LevelDebug,
		Output: io.Discard,
		LogDir: logDir,
		Now:    clock.Now,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	runtime.With("plant", "main").Info("connected")
	clock.Set(time.Date(2026, 7, 25, 1, 2, 3, 4_000_000, time.Local))
	runtime.Error("connection lost", "retry", 1)
	if err := runtime.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	first := readFile(t, filepath.Join(logDir, "2026-07-24.txt"))
	if first != "2026-07-24 12:34:56.789: connected plant=\"main\"\n" {
		t.Fatalf("first daily log = %q", first)
	}

	second := readFile(t, filepath.Join(logDir, "2026-07-25.txt"))
	if second != "2026-07-25 01:02:03.004: ERROR: connection lost retry=1\n" {
		t.Fatalf("second daily log = %q", second)
	}
}

func TestDailyFileConcurrentWrites(t *testing.T) {
	t.Parallel()

	const goroutines = 32
	const linesPerGoroutine = 40

	clock := &testClock{value: time.Date(2026, 7, 24, 12, 0, 0, 0, time.Local)}
	logDir := t.TempDir()
	runtime, err := New(Options{
		Level:  LevelInfo,
		Output: io.Discard,
		LogDir: logDir,
		Now:    clock.Now,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var wait sync.WaitGroup
	for worker := range goroutines {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for line := range linesPerGoroutine {
				runtime.Info("concurrent", "worker", worker, "line", line)
			}
		}()
	}
	wait.Wait()

	if err := runtime.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	file, err := os.Open(filepath.Join(logDir, "2026-07-24.txt"))
	if err != nil {
		t.Fatalf("open daily log: %v", err)
	}

	lineCount := 0
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lineCount++
		text := scanner.Text()
		if !strings.Contains(text, ": concurrent worker=") || !strings.Contains(text, " line=") {
			t.Fatalf("partial or malformed line: %q", text)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan daily log: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close daily log: %v", err)
	}

	want := goroutines * linesPerGoroutine
	if lineCount != want {
		t.Fatalf("line count = %d, want %d", lineCount, want)
	}
}

func TestInvalidOptions(t *testing.T) {
	t.Parallel()

	if _, err := New(Options{Level: Level(99), Output: io.Discard}); err == nil {
		t.Fatal("New() with invalid level error = nil")
	}
	if _, err := New(Options{Level: LevelInfo, Format: Format("xml"), Output: io.Discard}); err == nil {
		t.Fatal("New() with invalid format error = nil")
	}

	runtime, err := New(Options{Level: LevelInfo, Output: io.Discard})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := runtime.SetLevel(Level(99)); err == nil {
		t.Fatal("SetLevel() with invalid level error = nil")
	}
}

type testClock struct {
	mu    sync.Mutex
	value time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.value
}

func (c *testClock) Set(value time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.value = value
}

func readFile(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
