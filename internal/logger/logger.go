// SPDX-License-Identifier: GPL-2.0-only
// SPDX-FileCopyrightText: 2024 Kyle Sanderson

// Package logger provides structured logging for EnergyStarGo using zerolog.
package logger

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

// Logger wraps zerolog.Logger with application-specific methods.
type Logger struct {
	zl zerolog.Logger
	file *os.File // non-nil if logging to file
}

// New creates a new Logger. If logFile is non-empty, logs are written to both
// stderr and the file. Level is one of "debug", "info", "warn", "error".
func New(logFile string, level string) (*Logger, error) {
	var lvl zerolog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = zerolog.DebugLevel
	case "warn", "warning":
		lvl = zerolog.WarnLevel
	case "error":
		lvl = zerolog.ErrorLevel
	default:
		lvl = zerolog.InfoLevel
	}

	consoleWriter := zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339}

	var w io.Writer = consoleWriter
	var file *os.File
	if logFile != "" {
		f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			return nil, err
		}
		w = io.MultiWriter(consoleWriter, f)
		file = f
	}

	zl := zerolog.New(w).With().Timestamp().Logger().Level(lvl)
	return &Logger{zl: zl, file: file}, nil
}

// Close closes the log file if one was opened.
func (l *Logger) Close() error {
	if l.file != nil {
		err := l.file.Close()
		l.file = nil
		return err
	}
	return nil
}

// Info logs an info-level message with optional key-value pairs.
func (l *Logger) Info(msg string, keysAndValues ...any) {
	ev := l.zl.Info()
	addFields(ev, keysAndValues)
	ev.Msg(msg)
}

// Error logs an error-level message with optional key-value pairs.
func (l *Logger) Error(msg string, keysAndValues ...any) {
	ev := l.zl.Error()
	addFields(ev, keysAndValues)
	ev.Msg(msg)
}

// Debug logs a debug-level message with optional key-value pairs.
func (l *Logger) Debug(msg string, keysAndValues ...any) {
	ev := l.zl.Debug()
	addFields(ev, keysAndValues)
	ev.Msg(msg)
}

// Warn logs a warn-level message with optional key-value pairs.
func (l *Logger) Warn(msg string, keysAndValues ...any) {
	ev := l.zl.Warn()
	addFields(ev, keysAndValues)
	ev.Msg(msg)
}

// Throttle logs a process throttle event.
func (l *Logger) Throttle(processName string, pid uint32) {
	l.zl.Info().Str("process", processName).Uint32("pid", pid).Msg("throttle")
}

// Boost logs a process boost (unthrottle) event.
func (l *Logger) Boost(processName string, pid uint32) {
	l.zl.Info().Str("process", processName).Uint32("pid", pid).Msg("boost")
}

// Housekeeping logs a housekeeping sweep.
func (l *Logger) Housekeeping(count int) {
	l.zl.Info().Int("processes_throttled", count).Msg("housekeeping")
}

// ForegroundChange logs a foreground window change.
func (l *Logger) ForegroundChange(processName string, pid uint32) {
	l.zl.Debug().Str("process", processName).Uint32("pid", pid).Msg("foreground_change")
}

// addFields adds key-value pairs to a zerolog event.
func addFields(ev *zerolog.Event, keysAndValues []any) {
	for i := 0; i+1 < len(keysAndValues); i += 2 {
		key, ok := keysAndValues[i].(string)
		if !ok {
			key = fmt.Sprint(keysAndValues[i])
		}
		val := keysAndValues[i+1]
		switch v := val.(type) {
		case error:
			ev.AnErr(key, v)
		case string:
			ev.Str(key, v)
		case int:
			ev.Int(key, v)
		case int64:
			ev.Int64(key, v)
		case uint32:
			ev.Uint32(key, v)
		case float64:
			ev.Float64(key, v)
		case bool:
			ev.Bool(key, v)
		default:
			ev.Interface(key, v)
		}
	}
}
