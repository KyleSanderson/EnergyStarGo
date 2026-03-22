// SPDX-License-Identifier: GPL-2.0-only
// SPDX-FileCopyrightText: 2024 Kyle Sanderson

//go:build windows

package winapi

import (
	"testing"
	"unsafe"
)

func TestProcessPowerThrottlingStateSize(t *testing.T) {
	// The struct must be exactly 12 bytes (3 x uint32)
	size := unsafe.Sizeof(PROCESS_POWER_THROTTLING_STATE{})
	if size != 12 {
		t.Errorf("PROCESS_POWER_THROTTLING_STATE size = %d, want 12", size)
	}
}

func TestProcessPowerThrottlingStateLayout(t *testing.T) {
	s := PROCESS_POWER_THROTTLING_STATE{
		Version:     PROCESS_POWER_THROTTLING_CURRENT_VERSION,
		ControlMask: PROCESS_POWER_THROTTLING_EXECUTION_SPEED,
		StateMask:   PROCESS_POWER_THROTTLING_EXECUTION_SPEED,
	}
	if s.Version != 1 {
		t.Errorf("Version = %d, want 1", s.Version)
	}
	if s.ControlMask != 1 {
		t.Errorf("ControlMask = %d, want 1", s.ControlMask)
	}
	if s.StateMask != 1 {
		t.Errorf("StateMask = %d, want 1", s.StateMask)
	}
}

func TestThrottleOnOffStates(t *testing.T) {
	// Throttle ON
	on := PROCESS_POWER_THROTTLING_STATE{
		Version:     PROCESS_POWER_THROTTLING_CURRENT_VERSION,
		ControlMask: PROCESS_POWER_THROTTLING_EXECUTION_SPEED,
		StateMask:   PROCESS_POWER_THROTTLING_EXECUTION_SPEED,
	}

	// Throttle OFF
	off := PROCESS_POWER_THROTTLING_STATE{
		Version:     PROCESS_POWER_THROTTLING_CURRENT_VERSION,
		ControlMask: PROCESS_POWER_THROTTLING_EXECUTION_SPEED,
		StateMask:   0,
	}

	if on.StateMask != PROCESS_POWER_THROTTLING_EXECUTION_SPEED {
		t.Error("throttle ON should have StateMask set")
	}
	if off.StateMask != 0 {
		t.Error("throttle OFF should have StateMask = 0")
	}
	if on.ControlMask != off.ControlMask {
		t.Error("ControlMask should be same for both on and off")
	}
}

func TestConstants(t *testing.T) {
	tests := []struct {
		name     string
		value    uint32
		expected uint32
	}{
		{"ProcessPowerThrottling", ProcessPowerThrottling, 4},
		{"PROCESS_POWER_THROTTLING_CURRENT_VERSION", PROCESS_POWER_THROTTLING_CURRENT_VERSION, 1},
		{"PROCESS_POWER_THROTTLING_EXECUTION_SPEED", PROCESS_POWER_THROTTLING_EXECUTION_SPEED, 0x1},
		{"EVENT_SYSTEM_FOREGROUND", EVENT_SYSTEM_FOREGROUND, 0x0003},
		{"WINEVENT_OUTOFCONTEXT", WINEVENT_OUTOFCONTEXT, 0},
		{"WINEVENT_SKIPOWNPROCESS", WINEVENT_SKIPOWNPROCESS, 2},
		{"PROCESS_SET_INFORMATION", PROCESS_SET_INFORMATION, 0x0200},
		{"PROCESS_QUERY_LIMITED_INFORMATION", PROCESS_QUERY_LIMITED_INFORMATION, 0x1000},
		{"IDLE_PRIORITY_CLASS", IDLE_PRIORITY_CLASS, 0x40},
		{"NORMAL_PRIORITY_CLASS", NORMAL_PRIORITY_CLASS, 0x20},
		{"HIGH_PRIORITY_CLASS", HIGH_PRIORITY_CLASS, 0x80},
		{"TH32CS_SNAPPROCESS", TH32CS_SNAPPROCESS, 0x2},
		{"WM_QUIT", WM_QUIT, 0x0012},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.value != tt.expected {
				t.Errorf("%s = 0x%X, want 0x%X", tt.name, tt.value, tt.expected)
			}
		})
	}
}

func TestProcessEntry32WSize(t *testing.T) {
	// PROCESSENTRY32W should be the standard size
	entry := PROCESSENTRY32W{}
	size := unsafe.Sizeof(entry)
	// It should be reasonably large (556 bytes on Windows)
	if size < 300 {
		t.Errorf("PROCESSENTRY32W seems too small: %d bytes", size)
	}
}

func TestMSGStruct(t *testing.T) {
	msg := MSG{}
	// MSG struct should exist and have proper fields
	msg.Hwnd = 0
	msg.Message = WM_QUIT
	msg.WParam = 0
	msg.LParam = 0
	msg.Time = 0
	msg.Pt = POINT{X: 0, Y: 0}

	if msg.Message != WM_QUIT {
		t.Errorf("MSG.Message = %d, want %d", msg.Message, WM_QUIT)
	}
}

func TestProcessAccessFlagsCombinations(t *testing.T) {
	// Typical access flags for throttling
	throttleAccess := uint32(PROCESS_SET_INFORMATION)
	if throttleAccess != 0x0200 {
		t.Errorf("throttle access = 0x%X, want 0x0200", throttleAccess)
	}

	queryAndSet := uint32(PROCESS_QUERY_LIMITED_INFORMATION | PROCESS_SET_INFORMATION)
	if queryAndSet != 0x1200 {
		t.Errorf("query+set access = 0x%X, want 0x1200", queryAndSet)
	}
}

func TestPriorityClassValues(t *testing.T) {
	// Make sure priority classes are unique and correct
	priorities := map[string]uint32{
		"IDLE":         IDLE_PRIORITY_CLASS,
		"BELOW_NORMAL": BELOW_NORMAL_PRIORITY_CLASS,
		"NORMAL":       NORMAL_PRIORITY_CLASS,
		"ABOVE_NORMAL": ABOVE_NORMAL_PRIORITY_CLASS,
		"HIGH":         HIGH_PRIORITY_CLASS,
		"REALTIME":     REALTIME_PRIORITY_CLASS,
	}

	seen := make(map[uint32]string)
	for name, value := range priorities {
		if existing, ok := seen[value]; ok {
			t.Errorf("duplicate priority class value 0x%X: %s and %s", value, name, existing)
		}
		seen[value] = name
	}

	// Verify that key priority class values match Windows definitions
	if IDLE_PRIORITY_CLASS != 0x40 {
		t.Errorf("IDLE_PRIORITY_CLASS = 0x%X, want 0x40", IDLE_PRIORITY_CLASS)
	}
	if NORMAL_PRIORITY_CLASS != 0x20 {
		t.Errorf("NORMAL_PRIORITY_CLASS = 0x%X, want 0x20", NORMAL_PRIORITY_CLASS)
	}
	if HIGH_PRIORITY_CLASS != 0x80 {
		t.Errorf("HIGH_PRIORITY_CLASS = 0x%X, want 0x80", HIGH_PRIORITY_CLASS)
	}
}
