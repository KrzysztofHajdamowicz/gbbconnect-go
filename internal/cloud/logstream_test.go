package cloud

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/state"
)

func TestDailyLogStreamerIncrementalReadPersistsAcrossRestart(t *testing.T) {
	t.Parallel()

	const day = "2026-07-24"
	logDirectory := t.TempDir()
	writeLogFile(t, logDirectory, day, "old\n")
	store := newTestStateStore(t)
	streamer := newTestLogStreamer(t, store, logDirectory, day)

	first, err := streamer.Prepare(7)
	if err != nil {
		t.Fatalf("first Prepare() error = %v", err)
	}
	if first.Text != nil {
		t.Fatalf("first Prepare() Text = %q, want nil", dereference(first.Text))
	}
	if first.State != (state.PlantState{LastLogDate: day, LastLogPos: 4}) {
		t.Fatalf("first Prepare() State = %#v", first.State)
	}
	beforeCommit, err := store.Load(7)
	if err != nil {
		t.Fatalf("Load() before commit error = %v", err)
	}
	if beforeCommit != (state.PlantState{}) {
		t.Fatalf("state advanced before commit: %#v", beforeCommit)
	}
	if err := streamer.Commit(7, first.State); err != nil {
		t.Fatalf("first Commit() error = %v", err)
	}

	appendLogFile(t, logDirectory, day, "new\n")
	second, err := streamer.Prepare(7)
	if err != nil {
		t.Fatalf("second Prepare() error = %v", err)
	}
	if got := dereference(second.Text); got != "new\n" {
		t.Fatalf("second Prepare() Text = %q, want new line", got)
	}
	if second.State.LastLogPos != 8 {
		t.Fatalf("second Prepare() position = %d, want 8", second.State.LastLogPos)
	}
	if err := streamer.Commit(7, second.State); err != nil {
		t.Fatalf("second Commit() error = %v", err)
	}

	appendLogFile(t, logDirectory, day, "after restart\n")
	restarted := newTestLogStreamer(t, store, logDirectory, day)
	third, err := restarted.Prepare(7)
	if err != nil {
		t.Fatalf("restart Prepare() error = %v", err)
	}
	if got := dereference(third.Text); got != "after restart\n" {
		t.Fatalf("restart Prepare() Text = %q", got)
	}
}

func TestDailyLogStreamerRollsYesterdayIntoToday(t *testing.T) {
	t.Parallel()

	const (
		yesterday = "2026-07-23"
		today     = "2026-07-24"
	)
	logDirectory := t.TempDir()
	writeLogFile(t, logDirectory, yesterday, "sent\nremaining\n")
	writeLogFile(t, logDirectory, today, "today\n")
	store := newTestStateStore(t)
	if err := store.Save(4, state.PlantState{
		LastLogDate: yesterday,
		LastLogPos:  int64(len("sent\n")),
	}); err != nil {
		t.Fatalf("seed state error = %v", err)
	}
	streamer := newTestLogStreamer(t, store, logDirectory, today)

	fromYesterday, err := streamer.Prepare(4)
	if err != nil {
		t.Fatalf("yesterday Prepare() error = %v", err)
	}
	if got := dereference(fromYesterday.Text); got != "remaining\n" {
		t.Fatalf("yesterday Text = %q", got)
	}
	if fromYesterday.State != (state.PlantState{
		LastLogDate: today,
		LastLogPos:  0,
	}) {
		t.Fatalf("yesterday next state = %#v", fromYesterday.State)
	}
	if err := streamer.Commit(4, fromYesterday.State); err != nil {
		t.Fatalf("yesterday Commit() error = %v", err)
	}

	fromToday, err := streamer.Prepare(4)
	if err != nil {
		t.Fatalf("today Prepare() error = %v", err)
	}
	if got := dereference(fromToday.Text); got != "today\n" {
		t.Fatalf("today Text = %q", got)
	}
	if fromToday.State.LastLogDate != today ||
		fromToday.State.LastLogPos != int64(len("today\n")) {
		t.Fatalf("today next state = %#v", fromToday.State)
	}
}

func TestDailyLogStreamerOldCursorJumpsToTodayEnd(t *testing.T) {
	t.Parallel()

	const today = "2026-07-24"
	logDirectory := t.TempDir()
	writeLogFile(t, logDirectory, today, "existing history\n")
	store := newTestStateStore(t)
	if err := store.Save(1, state.PlantState{
		LastLogDate: "2026-07-20",
		LastLogPos:  0,
	}); err != nil {
		t.Fatalf("seed state error = %v", err)
	}
	streamer := newTestLogStreamer(t, store, logDirectory, today)

	prepared, err := streamer.Prepare(1)
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if prepared.Text != nil {
		t.Fatalf("Prepare() Text = %q, want nil", dereference(prepared.Text))
	}
	if prepared.State != (state.PlantState{
		LastLogDate: today,
		LastLogPos:  int64(len("existing history\n")),
	}) {
		t.Fatalf("Prepare() State = %#v", prepared.State)
	}
}

func TestDailyLogStreamerValidationAndInvalidCursor(t *testing.T) {
	t.Parallel()

	store := newTestStateStore(t)
	if _, err := NewDailyLogStreamer(nil, t.TempDir(), time.Now); err == nil {
		t.Fatal("NewDailyLogStreamer(nil store) error = nil")
	}
	if _, err := NewDailyLogStreamer(store, "", time.Now); err == nil {
		t.Fatal("NewDailyLogStreamer(empty directory) error = nil")
	}

	logDirectory := t.TempDir()
	streamer := newTestLogStreamer(t, store, logDirectory, "2026-07-24")
	if err := store.Save(1, state.PlantState{
		LastLogDate: "not-a-date",
	}); err != nil {
		t.Fatalf("seed invalid date error = %v", err)
	}
	if _, err := streamer.Prepare(1); err == nil {
		t.Fatal("Prepare(invalid date) error = nil")
	}

	if err := store.Save(2, state.PlantState{
		LastLogDate: "2026-07-24",
		LastLogPos:  -1,
	}); err != nil {
		t.Fatalf("seed invalid position error = %v", err)
	}
	writeLogFile(t, logDirectory, "2026-07-24", "log\n")
	if _, err := streamer.Prepare(2); err == nil {
		t.Fatal("Prepare(negative position) error = nil")
	}
}

func newTestStateStore(t *testing.T) *state.Store {
	t.Helper()
	store, err := state.New(t.TempDir())
	if err != nil {
		t.Fatalf("state.New() error = %v", err)
	}
	return store
}

func newTestLogStreamer(
	t *testing.T,
	store *state.Store,
	logDirectory string,
	day string,
) *DailyLogStreamer {
	t.Helper()
	now, err := time.Parse(time.DateOnly, day)
	if err != nil {
		t.Fatalf("parse test day: %v", err)
	}
	streamer, err := NewDailyLogStreamer(store, logDirectory, func() time.Time {
		return now
	})
	if err != nil {
		t.Fatalf("NewDailyLogStreamer() error = %v", err)
	}
	return streamer
}

func writeLogFile(t *testing.T, directory, day, text string) {
	t.Helper()
	if err := os.WriteFile(
		filepath.Join(directory, day+".txt"),
		[]byte(text),
		0o600,
	); err != nil {
		t.Fatalf("write log file: %v", err)
	}
}

func appendLogFile(t *testing.T, directory, day, text string) {
	t.Helper()
	file, err := os.OpenFile(
		filepath.Join(directory, day+".txt"),
		os.O_APPEND|os.O_WRONLY,
		0o600,
	)
	if err != nil {
		t.Fatalf("open log file for append: %v", err)
	}
	if _, err := file.WriteString(text); err != nil {
		_ = file.Close()
		t.Fatalf("append log file: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close appended log file: %v", err)
	}
}
