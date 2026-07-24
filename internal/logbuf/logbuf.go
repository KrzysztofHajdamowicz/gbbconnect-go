// Package logbuf provides application logging and the daily files consumed by
// the cloud log-streaming protocol.
package logbuf

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const redactedText = "[REDACTED]"

// Level is an application logging severity.
type Level int

const (
	LevelDebug Level = -4
	LevelInfo  Level = 0
	LevelWarn  Level = 4
	LevelError Level = 8
)

// Format selects the stdout log encoding.
type Format string

const (
	FormatText Format = "text"
	FormatJSON Format = "json"
)

// Logger is the logging contract shared by application packages.
type Logger interface {
	Debug(message string, attrs ...any)
	Info(message string, attrs ...any)
	Warn(message string, attrs ...any)
	Error(message string, attrs ...any)
	With(attrs ...any) Logger
}

// Options configures a Runtime.
type Options struct {
	Level  Level
	Format Format
	Output io.Writer
	LogDir string
	Now    func() time.Time
}

// Runtime owns the logger and its runtime-adjustable controls.
type Runtime struct {
	level          slog.LevelVar
	driverTrace    atomic.Bool
	driverTraceRaw atomic.Bool
	base           *logger
	fileState      *dailyFileState
}

// New creates a logging runtime.
func New(options Options) (*Runtime, error) {
	slogLevel, ok := options.Level.slogLevel()
	if !ok {
		return nil, fmt.Errorf("unsupported log level %d", options.Level)
	}

	format := options.Format
	if format == "" {
		format = FormatText
	}
	if format != FormatText && format != FormatJSON {
		return nil, fmt.Errorf("unsupported log format %q", format)
	}

	output := options.Output
	if output == nil {
		output = os.Stdout
	}

	runtime := &Runtime{}
	runtime.level.Set(slogLevel)

	handlerOptions := &slog.HandlerOptions{Level: &runtime.level}
	var stdoutHandler slog.Handler
	if format == FormatJSON {
		stdoutHandler = slog.NewJSONHandler(output, handlerOptions)
	} else {
		stdoutHandler = slog.NewTextHandler(output, handlerOptions)
	}

	handlers := []slog.Handler{stdoutHandler}
	if options.LogDir != "" {
		now := options.Now
		if now == nil {
			now = time.Now
		}

		fileState, err := newDailyFileState(options.LogDir, now)
		if err != nil {
			return nil, err
		}
		runtime.fileState = fileState
		handlers = append(handlers, &dailyFileHandler{
			level: &runtime.level,
			state: fileState,
		})
	}

	runtime.base = &logger{inner: slog.New(fanoutHandler(handlers))}
	return runtime, nil
}

// SetLevel changes the minimum emitted severity.
func (r *Runtime) SetLevel(level Level) error {
	slogLevel, ok := level.slogLevel()
	if !ok {
		return fmt.Errorf("unsupported log level %d", level)
	}
	r.level.Set(slogLevel)
	return nil
}

// Level returns the current minimum emitted severity.
func (r *Runtime) Level() Level {
	switch r.level.Level() {
	case slog.LevelDebug:
		return LevelDebug
	case slog.LevelWarn:
		return LevelWarn
	case slog.LevelError:
		return LevelError
	default:
		return LevelInfo
	}
}

// SetDriverTrace changes the decoded and raw driver trace switches.
func (r *Runtime) SetDriverTrace(decoded, raw bool) {
	r.driverTrace.Store(decoded)
	r.driverTraceRaw.Store(raw)
}

// DriverTraceEnabled reports whether decoded Modbus tracing is enabled.
func (r *Runtime) DriverTraceEnabled() bool {
	return r.driverTrace.Load()
}

// DriverTraceRawEnabled reports whether raw-frame tracing is enabled.
func (r *Runtime) DriverTraceRawEnabled() bool {
	return r.driverTraceRaw.Load()
}

// ApplyCloudLevel applies the GbbOptimizer LogLevel mapping.
func (r *Runtime) ApplyCloudLevel(value string) error {
	switch {
	case strings.EqualFold(value, "OnlyErrors"):
		r.level.Set(slog.LevelError)
		r.SetDriverTrace(false, false)
	case strings.EqualFold(value, "Min"):
		r.level.Set(slog.LevelInfo)
		r.SetDriverTrace(false, false)
	case strings.EqualFold(value, "Max"):
		r.level.Set(slog.LevelInfo)
		r.SetDriverTrace(true, true)
	default:
		return fmt.Errorf("unknown cloud log level %q", value)
	}
	return nil
}

// Close flushes and closes the optional daily file sink.
func (r *Runtime) Close() error {
	if r.fileState == nil {
		return nil
	}
	return r.fileState.close()
}

// Debug logs a debug message.
func (r *Runtime) Debug(message string, attrs ...any) {
	r.base.Debug(message, attrs...)
}

// Info logs an informational message.
func (r *Runtime) Info(message string, attrs ...any) {
	r.base.Info(message, attrs...)
}

// Warn logs a warning.
func (r *Runtime) Warn(message string, attrs ...any) {
	r.base.Warn(message, attrs...)
}

// Error logs an error.
func (r *Runtime) Error(message string, attrs ...any) {
	r.base.Error(message, attrs...)
}

// With returns a logger that includes attrs in every record.
func (r *Runtime) With(attrs ...any) Logger {
	return r.base.With(attrs...)
}

type logger struct {
	inner *slog.Logger
}

func (l *logger) Debug(message string, attrs ...any) {
	l.inner.Debug(message, attrs...)
}

func (l *logger) Info(message string, attrs ...any) {
	l.inner.Info(message, attrs...)
}

func (l *logger) Warn(message string, attrs ...any) {
	l.inner.Warn(message, attrs...)
}

func (l *logger) Error(message string, attrs ...any) {
	l.inner.Error(message, attrs...)
}

func (l *logger) With(attrs ...any) Logger {
	return &logger{inner: l.inner.With(attrs...)}
}

// Secret returns an attribute value that never renders its input.
func Secret(_ string) any {
	return secretValue{}
}

type secretValue struct{}

func (secretValue) String() string {
	return redactedText
}

func (secretValue) LogValue() slog.Value {
	return slog.StringValue(redactedText)
}

func (level Level) slogLevel() (slog.Level, bool) {
	switch level {
	case LevelDebug:
		return slog.LevelDebug, true
	case LevelInfo:
		return slog.LevelInfo, true
	case LevelWarn:
		return slog.LevelWarn, true
	case LevelError:
		return slog.LevelError, true
	default:
		return 0, false
	}
}

type fanoutHandler []slog.Handler

func (h fanoutHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, child := range h {
		if child.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (h fanoutHandler) Handle(ctx context.Context, record slog.Record) error {
	var errs []error
	for _, child := range h {
		if child.Enabled(ctx, record.Level) {
			errs = append(errs, child.Handle(ctx, record))
		}
	}
	return errors.Join(errs...)
}

func (h fanoutHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	children := make([]slog.Handler, len(h))
	for index, child := range h {
		children[index] = child.WithAttrs(attrs)
	}
	return fanoutHandler(children)
}

func (h fanoutHandler) WithGroup(name string) slog.Handler {
	children := make([]slog.Handler, len(h))
	for index, child := range h {
		children[index] = child.WithGroup(name)
	}
	return fanoutHandler(children)
}

type dailyFileHandler struct {
	level  slog.Leveler
	state  *dailyFileState
	attrs  []slog.Attr
	groups []string
}

func (h *dailyFileHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level.Level()
}

func (h *dailyFileHandler) Handle(_ context.Context, record slog.Record) error {
	if !h.Enabled(context.Background(), record.Level) {
		return nil
	}

	attrs := make([]slog.Attr, 0, len(h.attrs)+record.NumAttrs())
	attrs = append(attrs, h.attrs...)
	record.Attrs(func(attr slog.Attr) bool {
		attrs = append(attrs, attr)
		return true
	})

	return h.state.write(record.Level, record.Message, h.groups, attrs)
}

func (h *dailyFileHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	clone := *h
	clone.attrs = append(append([]slog.Attr(nil), h.attrs...), attrs...)
	return &clone
}

func (h *dailyFileHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	clone := *h
	clone.groups = append(append([]string(nil), h.groups...), name)
	return &clone
}

type dailyFileState struct {
	mu      sync.Mutex
	dir     string
	now     func() time.Time
	day     string
	current *os.File
}

func newDailyFileState(dir string, now func() time.Time) (*dailyFileState, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("create log directory: %w", err)
	}
	return &dailyFileState{dir: dir, now: now}, nil
}

func (s *dailyFileState) write(level slog.Level, message string, groups []string, attrs []slog.Attr) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	day := now.Format(time.DateOnly)
	if err := s.openDay(day); err != nil {
		return err
	}

	var line strings.Builder
	line.WriteString(now.Format("2006-01-02 15:04:05.000"))
	line.WriteString(": ")
	switch {
	case level >= slog.LevelError:
		line.WriteString("ERROR: ")
	case level >= slog.LevelWarn:
		line.WriteString("WARN: ")
	case level <= slog.LevelDebug:
		line.WriteString("DEBUG: ")
	}
	line.WriteString(message)

	prefix := strings.Join(groups, ".")
	for _, attr := range attrs {
		appendAttr(&line, prefix, attr)
	}
	line.WriteByte('\n')

	if _, err := io.WriteString(s.current, line.String()); err != nil {
		return fmt.Errorf("write daily log: %w", err)
	}
	return nil
}

func (s *dailyFileState) openDay(day string) error {
	if s.current != nil && s.day == day {
		return nil
	}
	if s.current != nil {
		if err := s.current.Close(); err != nil {
			return fmt.Errorf("close previous daily log: %w", err)
		}
		s.current = nil
		s.day = ""
	}

	path := filepath.Join(s.dir, day+".txt")
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open daily log: %w", err)
	}
	s.current = file
	s.day = day
	return nil
}

func (s *dailyFileState) close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.current == nil {
		return nil
	}
	err := s.current.Close()
	s.current = nil
	s.day = ""
	if err != nil {
		return fmt.Errorf("close daily log: %w", err)
	}
	return nil
}

func appendAttr(line *strings.Builder, prefix string, attr slog.Attr) {
	if attr.Equal(slog.Attr{}) {
		return
	}

	value := attr.Value.Resolve()
	key := attr.Key
	if prefix != "" {
		key = prefix + "." + key
	}

	if value.Kind() == slog.KindGroup {
		for _, nested := range value.Group() {
			appendAttr(line, key, nested)
		}
		return
	}

	line.WriteByte(' ')
	line.WriteString(key)
	line.WriteByte('=')
	if value.Kind() == slog.KindString {
		line.WriteString(strconv.Quote(value.String()))
		return
	}
	_, _ = fmt.Fprint(line, value.Any())
}

var (
	_ Logger         = (*Runtime)(nil)
	_ Logger         = (*logger)(nil)
	_ slog.Handler   = fanoutHandler(nil)
	_ slog.Handler   = (*dailyFileHandler)(nil)
	_ slog.LogValuer = secretValue{}
)
