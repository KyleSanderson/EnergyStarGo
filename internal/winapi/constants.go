// SPDX-License-Identifier: GPL-2.0-only
// SPDX-FileCopyrightText: 2024 Kyle Sanderson

// Package winapi provides Windows API declarations for process throttling via EcoQoS.
// This package is only supported on Windows 11 (build >= 22000).
package winapi

// Process access rights
const (
	PROCESS_TERMINATE                 = 0x0001
	PROCESS_CREATE_THREAD             = 0x0002
	PROCESS_SET_QUOTA                 = 0x0100
	PROCESS_SET_INFORMATION           = 0x0200
	PROCESS_QUERY_INFORMATION         = 0x0400
	PROCESS_QUERY_LIMITED_INFORMATION = 0x1000
	PROCESS_ALL_ACCESS                = 0x001F0FFF
	SYNCHRONIZE                       = 0x00100000
)

// PROCESS_INFORMATION_CLASS enum
const (
	ProcessMemoryPriority        = 0
	ProcessMemoryExhaustionInfo  = 1
	ProcessAppMemoryInfo         = 2
	ProcessInPrivateInfo         = 3
	ProcessPowerThrottling       = 4
	ProcessReservedValue1        = 5
	ProcessTelemetryCoverageInfo = 6
	ProcessProtectionLevelInfo   = 7
	ProcessLeapSecondInfo        = 8
)

// PROCESS_POWER_THROTTLING flags
const (
	PROCESS_POWER_THROTTLING_CURRENT_VERSION = 1
	PROCESS_POWER_THROTTLING_EXECUTION_SPEED = 0x1
)

// Priority classes
const (
	IDLE_PRIORITY_CLASS         = 0x0040
	BELOW_NORMAL_PRIORITY_CLASS = 0x4000
	NORMAL_PRIORITY_CLASS       = 0x0020
	ABOVE_NORMAL_PRIORITY_CLASS = 0x8000
	HIGH_PRIORITY_CLASS         = 0x0080
	REALTIME_PRIORITY_CLASS     = 0x0100
)

// Window event constants
const (
	EVENT_SYSTEM_FOREGROUND = 0x0003
	WINEVENT_OUTOFCONTEXT   = 0x0000
	WINEVENT_SKIPOWNPROCESS = 0x0002
	WINEVENT_SKIPOWNTHREAD  = 0x0001
)

// Message loop constants
const (
	WM_QUIT = 0x0012
)

// Toolhelp32 snapshot flags
const (
	TH32CS_SNAPPROCESS = 0x00000002
	TH32CS_SNAPTHREAD  = 0x00000004
)

// Thread access rights
const (
	THREAD_SET_INFORMATION   = 0x0020
	THREAD_QUERY_INFORMATION = 0x0040
)

// THREAD_INFORMATION_CLASS enum
const (
	ThreadPowerThrottling = 1
)
