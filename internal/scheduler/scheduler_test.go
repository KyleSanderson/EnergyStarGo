// SPDX-License-Identifier: GPL-2.0-only
// SPDX-FileCopyrightText: 2024 Kyle Sanderson <kyle@kylesanderson.dev>

package scheduler

import (
	"testing"
	"time"

	"github.com/KyleSanderson/EnergyStarGo/internal/config"
)

// at returns a time.Time for the given hour and minute on an arbitrary fixed date.
func at(h, m int) time.Time {
	return time.Date(2024, 6, 15, h, m, 0, 0, time.Local)
}

func TestCheckNow_EmptySchedule(t *testing.T) {
	if got := CheckNow(nil); got != "" {
		t.Errorf("nil entries: expected \"\", got %q", got)
	}
	if got := CheckNow([]Entry{}); got != "" {
		t.Errorf("empty entries: expected \"\", got %q", got)
	}
}

func TestCheckAt_WithinRange(t *testing.T) {
	entries := []Entry{
		{From: "09:00", To: "17:00", Profile: config.ProfileAggressive},
	}
	for _, tc := range []struct {
		h, m int
		want config.Profile
	}{
		{9, 0, config.ProfileAggressive},   // exact start (inclusive)
		{12, 30, config.ProfileAggressive}, // midday
		{16, 59, config.ProfileAggressive}, // one minute before end
		{17, 0, ""},                        // exact end (exclusive)
		{8, 59, ""},                        // before start
		{17, 1, ""},                        // after end
	} {
		got := checkAt(entries, at(tc.h, tc.m))
		if got != tc.want {
			t.Errorf("at %02d:%02d: expected %q, got %q", tc.h, tc.m, tc.want, got)
		}
	}
}

func TestCheckAt_MidnightCrossing(t *testing.T) {
	entries := []Entry{
		{From: "22:00", To: "06:00", Profile: config.ProfileBalanced},
	}
	for _, tc := range []struct {
		h, m int
		want config.Profile
	}{
		{22, 0, config.ProfileBalanced},  // exact start
		{23, 30, config.ProfileBalanced}, // late night
		{0, 0, config.ProfileBalanced},   // midnight
		{5, 59, config.ProfileBalanced},  // one minute before end
		{6, 0, ""},                       // exact end (exclusive)
		{12, 0, ""},                      // midday
		{21, 59, ""},                     // one minute before start
	} {
		got := checkAt(entries, at(tc.h, tc.m))
		if got != tc.want {
			t.Errorf("at %02d:%02d: expected %q, got %q", tc.h, tc.m, tc.want, got)
		}
	}
}

func TestCheckAt_MultipleEntries_FirstWins(t *testing.T) {
	entries := []Entry{
		{From: "09:00", To: "17:00", Profile: config.ProfileAggressive},
		{From: "08:00", To: "18:00", Profile: config.ProfileBalanced},
	}
	// Both match at 12:00; the first entry should take priority.
	got := checkAt(entries, at(12, 0))
	if got != config.ProfileAggressive {
		t.Errorf("expected first matching entry %q, got %q", config.ProfileAggressive, got)
	}
}

func TestCheckAt_MultipleEntries_OnlySecondMatches(t *testing.T) {
	entries := []Entry{
		{From: "09:00", To: "12:00", Profile: config.ProfileAggressive},
		{From: "12:00", To: "18:00", Profile: config.ProfileBalanced},
	}
	got := checkAt(entries, at(14, 0))
	if got != config.ProfileBalanced {
		t.Errorf("expected %q, got %q", config.ProfileBalanced, got)
	}
}

func TestCheckAt_InvalidEntry_Skipped(t *testing.T) {
	entries := []Entry{
		{From: "notaTime", To: "17:00", Profile: config.ProfileAggressive},
		{From: "09:00", To: "badTime", Profile: config.ProfileAggressive},
		{From: "09:00", To: "17:00", Profile: config.ProfileBalanced},
	}
	got := checkAt(entries, at(12, 0))
	if got != config.ProfileBalanced {
		t.Errorf("expected invalid entries to be skipped, got %q", got)
	}
}

func TestCheckAt_AllInvalidEntries(t *testing.T) {
	entries := []Entry{
		{From: "bad", To: "worse", Profile: config.ProfileAggressive},
	}
	got := checkAt(entries, at(12, 0))
	if got != "" {
		t.Errorf("expected empty for all-invalid entries, got %q", got)
	}
}

func TestParseHHMM(t *testing.T) {
	cases := []struct {
		s       string
		h, m    int
		wantErr bool
	}{
		{"00:00", 0, 0, false},
		{"23:59", 23, 59, false},
		{"09:05", 9, 5, false},
		{"24:00", 0, 0, true},
		{"12:60", 0, 0, true},
		{"abc", 0, 0, true},
		{"12", 0, 0, true},
		{"-1:00", 0, 0, true},
	}
	for _, tc := range cases {
		h, m, err := parseHHMM(tc.s)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseHHMM(%q): expected error, got h=%d m=%d", tc.s, h, m)
			}
		} else {
			if err != nil {
				t.Errorf("parseHHMM(%q): unexpected error: %v", tc.s, err)
			} else if h != tc.h || m != tc.m {
				t.Errorf("parseHHMM(%q): expected %d:%d, got %d:%d", tc.s, tc.h, tc.m, h, m)
			}
		}
	}
}
