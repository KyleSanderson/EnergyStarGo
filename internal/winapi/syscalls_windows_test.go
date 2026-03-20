// SPDX-License-Identifier: GPL-2.0-only
// SPDX-FileCopyrightText: 2024 Kyle Sanderson

//go:build windows

package winapi

import (
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestGetCurrentProcessSessionId(t *testing.T) {
	sessionID, err := GetCurrentProcessSessionId()
	if err != nil {
		t.Fatalf("GetCurrentProcessSessionId failed: %v", err)
	}
	t.Logf("Current session ID: %d", sessionID)
}

func TestProcessIdToSessionId(t *testing.T) {
	pid := windows.GetCurrentProcessId()
	sessionID, err := ProcessIdToSessionId(pid)
	if err != nil {
		t.Fatalf("ProcessIdToSessionId failed: %v", err)
	}
	t.Logf("PID %d session ID: %d", pid, sessionID)
}

func TestOpenCloseProcess(t *testing.T) {
	pid := windows.GetCurrentProcessId()
	handle, err := OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		t.Fatalf("OpenProcess failed: %v", err)
	}
	defer CloseHandle(handle)

	if handle == 0 {
		t.Error("handle should not be zero")
	}
}

func TestQueryFullProcessImageName(t *testing.T) {
	pid := windows.GetCurrentProcessId()
	handle, err := OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		t.Fatalf("OpenProcess failed: %v", err)
	}
	defer CloseHandle(handle)

	name, err := QueryFullProcessImageName(handle)
	if err != nil {
		t.Fatalf("QueryFullProcessImageName failed: %v", err)
	}
	if name == "" {
		t.Error("process name should not be empty")
	}
	t.Logf("Current process: %s", name)

	// Should end in .exe
	if !strings.HasSuffix(strings.ToLower(name), ".exe") {
		t.Errorf("process name should end with .exe: %s", name)
	}
}

func TestCreateToolhelp32Snapshot(t *testing.T) {
	snapshot, err := CreateToolhelp32Snapshot(TH32CS_SNAPPROCESS, 0)
	if err != nil {
		t.Fatalf("CreateToolhelp32Snapshot failed: %v", err)
	}
	defer CloseHandle(snapshot)

	var entry PROCESSENTRY32W
	err = Process32First(snapshot, &entry)
	if err != nil {
		t.Fatalf("Process32First failed: %v", err)
	}

	name := ProcessNameFromEntry(&entry)
	if name == "" {
		t.Error("first process name should not be empty")
	}
	t.Logf("First process: %s (PID %d)", name, entry.ProcessID)

	// Enumerate a few more
	count := 1
	for {
		err = Process32Next(snapshot, &entry)
		if err != nil {
			break
		}
		count++
	}

	if count < 5 {
		t.Errorf("expected at least 5 processes, found %d", count)
	}
	t.Logf("Total processes enumerated: %d", count)
}

func TestGetForegroundWindow(t *testing.T) {
	hwnd := GetForegroundWindow()
	// In a test environment, there might not be a foreground window
	t.Logf("Foreground window handle: 0x%X", hwnd)
}

func TestSetPriorityClassCurrentProcess(t *testing.T) {
	pid := windows.GetCurrentProcessId()
	handle, err := OpenProcess(PROCESS_SET_INFORMATION|PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		t.Fatalf("OpenProcess failed: %v", err)
	}
	defer CloseHandle(handle)

	// Get current priority
	origPriority, err := windows.GetPriorityClass(handle)
	if err != nil {
		t.Fatalf("GetPriorityClass failed: %v", err)
	}
	t.Logf("Original priority: 0x%X", origPriority)

	// Set to idle
	err = SetPriorityClass(handle, IDLE_PRIORITY_CLASS)
	if err != nil {
		t.Fatalf("SetPriorityClass to IDLE failed: %v", err)
	}

	// Restore original
	err = SetPriorityClass(handle, origPriority)
	if err != nil {
		t.Fatalf("SetPriorityClass restore failed: %v", err)
	}
}

func TestSetProcessInformationThrottle(t *testing.T) {
	pid := windows.GetCurrentProcessId()
	handle, err := OpenProcess(PROCESS_SET_INFORMATION, false, pid)
	if err != nil {
		t.Fatalf("OpenProcess failed: %v", err)
	}
	defer CloseHandle(handle)

	// Enable throttle
	stateOn := PROCESS_POWER_THROTTLING_STATE{
		Version:     PROCESS_POWER_THROTTLING_CURRENT_VERSION,
		ControlMask: PROCESS_POWER_THROTTLING_EXECUTION_SPEED,
		StateMask:   PROCESS_POWER_THROTTLING_EXECUTION_SPEED,
	}

	err = SetProcessInformation(handle, ProcessPowerThrottling, nil, 0)
	// We expect this to fail because size is 0, but it shouldn't crash
	t.Logf("SetProcessInformation with size=0: %v (expected failure)", err)

	// Now try with proper size - this will actually throttle the test process
	err = SetProcessInformation(handle, ProcessPowerThrottling,
		unsafe.Pointer(&stateOn),
		uint32(unsafe.Sizeof(stateOn)))
	// On Windows 11 this should succeed
	t.Logf("SetProcessInformation throttle ON: %v", err)

	// Immediately restore
	stateOff := PROCESS_POWER_THROTTLING_STATE{
		Version:     PROCESS_POWER_THROTTLING_CURRENT_VERSION,
		ControlMask: PROCESS_POWER_THROTTLING_EXECUTION_SPEED,
		StateMask:   0,
	}
	err = SetProcessInformation(handle, ProcessPowerThrottling,
		unsafe.Pointer(&stateOff),
		uint32(unsafe.Sizeof(stateOff)))
	t.Logf("SetProcessInformation throttle OFF: %v", err)
}

func TestProcessNameFromEntry(t *testing.T) {
	var entry PROCESSENTRY32W
	// Fill with a known name
	name := "test.exe"
	nameUTF16, _ := windows.UTF16FromString(name)
	copy(entry.ExeFile[:], nameUTF16)

	result := ProcessNameFromEntry(&entry)
	if result != name {
		t.Errorf("ProcessNameFromEntry = %q, want %q", result, name)
	}
}
