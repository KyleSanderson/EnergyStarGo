// SPDX-License-Identifier: GPL-2.0-only
// SPDX-FileCopyrightText: 2024 Kyle Sanderson <kyle@kylesanderson.dev>

// Package scheduler provides time-based profile switching for EnergyStarGo.
package scheduler

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/KyleSanderson/EnergyStarGo/internal/config"
	"github.com/KyleSanderson/EnergyStarGo/internal/logger"
)

// Entry defines a time range and the profile to activate within it.
type Entry struct {
	From    string         // "HH:MM" 24-hour format (inclusive)
	To      string         // "HH:MM" 24-hour format (exclusive)
	Profile config.Profile
}

// Scheduler checks schedule entries every 30 seconds and calls onChange when
// the active profile changes.
type Scheduler struct {
	mu       sync.Mutex
	log      *logger.Logger
	entries  []Entry
	onChange func(config.Profile)
	stopCh   chan struct{}
	active   config.Profile
}

// New creates a new Scheduler. onChange is called whenever the active profile changes.
func New(log *logger.Logger, entries []Entry, onChange func(config.Profile)) *Scheduler {
	return &Scheduler{
		log:      log,
		entries:  entries,
		onChange: onChange,
		stopCh:   make(chan struct{}),
	}
}

// Start begins the scheduling goroutine. It checks the schedule immediately
// and then every 30 seconds thereafter.
func (s *Scheduler) Start() {
	go func() {
		s.check()
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.check()
			case <-s.stopCh:
				return
			}
		}
	}()
}

// Stop stops the scheduling goroutine.
func (s *Scheduler) Stop() {
	close(s.stopCh)
}

// ActiveProfile returns the currently active scheduled profile.
// An empty string means no schedule entry matches now; the caller should use
// its configured default profile.
func (s *Scheduler) ActiveProfile() config.Profile {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active
}

// CheckNow evaluates the schedule entries against the current time and returns
// the first matching profile, or "" if no entry matches.
func CheckNow(entries []Entry) config.Profile {
	return checkAt(entries, time.Now())
}

// check evaluates the schedule and fires onChange when the active profile changes.
func (s *Scheduler) check() {
	profile := CheckNow(s.entries)

	s.mu.Lock()
	changed := profile != s.active
	prev := s.active
	if changed {
		s.active = profile
	}
	s.mu.Unlock()

	if changed {
		if s.log != nil {
			s.log.Info("scheduler profile changed", "from", prev, "to", profile)
		}
		if s.onChange != nil {
			s.onChange(profile)
		}
	}
}

// checkAt evaluates entries against the given time t.
// Exported for cross-package testing; unexported for library users.
func checkAt(entries []Entry, t time.Time) config.Profile {
	h, m := t.Hour(), t.Minute()
	current := h*60 + m // minutes since midnight

	for _, e := range entries {
		fromH, fromM, err := parseHHMM(e.From)
		if err != nil {
			continue
		}
		toH, toM, err := parseHHMM(e.To)
		if err != nil {
			continue
		}
		from := fromH*60 + fromM
		to := toH*60 + toM

		var active bool
		if from <= to {
			// Normal range: [from, to)
			active = current >= from && current < to
		} else {
			// Wraps midnight: current >= from OR current < to
			active = current >= from || current < to
		}
		if active {
			return e.Profile
		}
	}
	return ""
}

// parseHHMM parses a "HH:MM" string and returns hours and minutes.
func parseHHMM(s string) (int, int, error) {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid time %q: expected HH:MM", s)
	}
	h, err := strconv.Atoi(parts[0])
	if err != nil || h < 0 || h > 23 {
		return 0, 0, fmt.Errorf("invalid hour in %q", s)
	}
	m, err := strconv.Atoi(parts[1])
	if err != nil || m < 0 || m > 59 {
		return 0, 0, fmt.Errorf("invalid minute in %q", s)
	}
	return h, m, nil
}
