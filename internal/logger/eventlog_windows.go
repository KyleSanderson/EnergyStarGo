// SPDX-License-Identifier: GPL-2.0-only
// SPDX-FileCopyrightText: 2024 Kyle Sanderson

//go:build windows

package logger

import (
	"golang.org/x/sys/windows/svc/eventlog"

	"github.com/rs/zerolog"
)

// eventlogHook is a zerolog hook that forwards log entries to the Windows
// Application Event Log. It requires the event source to be registered (done
// automatically on first call with admin rights).
type eventlogHook struct {
	el *eventlog.Log
}

// Run implements zerolog.Hook. It is called for every log entry.
func (h *eventlogHook) Run(_ *zerolog.Event, level zerolog.Level, message string) {
	const eventID uint32 = 1
	switch level {
	case zerolog.ErrorLevel, zerolog.FatalLevel, zerolog.PanicLevel:
		_ = h.el.Error(eventID, message)
	case zerolog.WarnLevel:
		_ = h.el.Warning(eventID, message)
	default:
		_ = h.el.Info(eventID, message)
	}
}

// EnableEventLog adds a Windows Application Event Log sink to the logger.
// The source name is registered on first call; this requires elevated privileges.
// Subsequent calls without admin rights work as long as the source is already
// registered.
func (l *Logger) EnableEventLog(source string) error {
	const supported = eventlog.Error | eventlog.Warning | eventlog.Info

	el, err := eventlog.Open(source)
	if err != nil {
		// Source not yet registered — try to install it.
		if installErr := eventlog.InstallAsEventCreate(source, supported); installErr != nil {
			return err // return original open error, not the install error
		}
		el, err = eventlog.Open(source)
		if err != nil {
			return err
		}
	}

	l.zl = l.zl.Hook(&eventlogHook{el: el})
	return nil
}
