package cloud

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/state"
)

// LastLogRead is a prepared incremental log response and its next cursor.
type LastLogRead struct {
	Text  *string
	State state.PlantState
}

// LastLogStreamer reads and commits per-plant incremental log cursors.
type LastLogStreamer interface {
	Prepare(plantNumber int) (LastLogRead, error)
	Commit(plantNumber int, next state.PlantState) error
}

// DailyLogStreamer serves the daily files written by the logging runtime.
type DailyLogStreamer struct {
	store  *state.Store
	logDir string
	now    func() time.Time
}

// NewDailyLogStreamer creates an incremental daily-log reader.
func NewDailyLogStreamer(
	store *state.Store,
	logDir string,
	now func() time.Time,
) (*DailyLogStreamer, error) {
	if store == nil {
		return nil, errors.New("log stream state store is required")
	}
	if logDir == "" {
		return nil, errors.New("log stream directory is required")
	}
	if now == nil {
		now = time.Now
	}
	return &DailyLogStreamer{store: store, logDir: logDir, now: now}, nil
}

// Prepare reads logs from the saved cursor without advancing durable state.
func (streamer *DailyLogStreamer) Prepare(plantNumber int) (LastLogRead, error) {
	current, err := streamer.store.Load(plantNumber)
	if err != nil {
		return LastLogRead{}, err
	}

	today := streamer.now()
	today = time.Date(
		today.Year(),
		today.Month(),
		today.Day(),
		0,
		0,
		0,
		0,
		today.Location(),
	)
	yesterday := today.AddDate(0, 0, -1)

	if current.LastLogDate == "" {
		return streamer.initializeAtEnd(today)
	}
	cursorDate, err := time.ParseInLocation(
		time.DateOnly,
		current.LastLogDate,
		today.Location(),
	)
	if err != nil {
		return LastLogRead{}, fmt.Errorf(
			"parse last log date %q: %w",
			current.LastLogDate,
			err,
		)
	}
	if cursorDate.Before(yesterday) {
		return streamer.initializeAtEnd(today)
	}

	text, nextPosition, exists, err := streamer.readFrom(
		current.LastLogDate,
		current.LastLogPos,
	)
	if err != nil {
		return LastLogRead{}, err
	}
	next := current
	if exists {
		next.LastLogPos = nextPosition
	}
	if cursorDate.Equal(yesterday) {
		next.LastLogDate = today.Format(time.DateOnly)
		next.LastLogPos = 0
	}
	return LastLogRead{Text: text, State: next}, nil
}

// Commit advances the durable cursor after the MQTT response was published.
func (streamer *DailyLogStreamer) Commit(
	plantNumber int,
	next state.PlantState,
) error {
	return streamer.store.Save(plantNumber, next)
}

func (streamer *DailyLogStreamer) initializeAtEnd(today time.Time) (LastLogRead, error) {
	position, err := streamer.fileSize(today.Format(time.DateOnly))
	if err != nil {
		return LastLogRead{}, err
	}
	return LastLogRead{
		State: state.PlantState{
			LastLogDate: today.Format(time.DateOnly),
			LastLogPos:  position,
		},
	}, nil
}

func (streamer *DailyLogStreamer) fileSize(day string) (int64, error) {
	info, err := os.Stat(streamer.path(day))
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("stat daily log %s: %w", day, err)
	}
	if !info.Mode().IsRegular() {
		return 0, fmt.Errorf("daily log %s is not a regular file", day)
	}
	return info.Size(), nil
}

func (streamer *DailyLogStreamer) readFrom(
	day string,
	position int64,
) (*string, int64, bool, error) {
	if position < 0 {
		return nil, position, false, fmt.Errorf(
			"last log position must not be negative: %d",
			position,
		)
	}

	file, err := os.Open(streamer.path(day))
	if os.IsNotExist(err) {
		return nil, position, false, nil
	}
	if err != nil {
		return nil, position, false, fmt.Errorf("open daily log %s: %w", day, err)
	}
	defer func() {
		_ = file.Close()
	}()

	info, err := file.Stat()
	if err != nil {
		return nil, position, false, fmt.Errorf("stat open daily log %s: %w", day, err)
	}
	end := info.Size()
	if position >= end {
		empty := ""
		return &empty, position, true, nil
	}

	data, err := io.ReadAll(io.NewSectionReader(file, position, end-position))
	if err != nil {
		return nil, position, false, fmt.Errorf("read daily log %s: %w", day, err)
	}
	text := string(data)
	return &text, end, true, nil
}

func (streamer *DailyLogStreamer) path(day string) string {
	return filepath.Join(streamer.logDir, day+".txt")
}

var _ LastLogStreamer = (*DailyLogStreamer)(nil)
