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
	// PROCESS_STATE_CHANGE_ACCESS is used for boost/throttle transitions where
	// both querying process metadata and setting process/thread state are needed.
	PROCESS_STATE_CHANGE_ACCESS = PROCESS_QUERY_LIMITED_INFORMATION | PROCESS_SET_INFORMATION
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
	PROCESS_POWER_THROTTLING_CURRENT_VERSION         = 1
	PROCESS_POWER_THROTTLING_EXECUTION_SPEED         = 0x1
	PROCESS_POWER_THROTTLING_IGNORE_TIMER_RESOLUTION = 0x2
)

// Process memory priority levels
const (
	MEMORY_PRIORITY_VERY_LOW     = 1
	MEMORY_PRIORITY_LOW          = 2
	MEMORY_PRIORITY_MEDIUM       = 3
	MEMORY_PRIORITY_BELOW_NORMAL = 4
	MEMORY_PRIORITY_NORMAL       = 5
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
	// THREAD_INFORMATION_CLASS value from processthreadsapi.h
	// (ThreadMemoryPriority=0, ThreadAbsoluteCpuPriority=1,
	// ThreadDynamicCodePolicy=2, ThreadPowerThrottling=3).
	ThreadPowerThrottling = 3
)

// Monitor flags
const (
	MONITOR_DEFAULTTONEAREST = 0x00000002
)

// Power broadcast constants
const (
	WM_POWERBROADCAST           = 0x0218
	PBT_POWERSETTINGCHANGE      = 0x8013
	DEVICE_NOTIFY_WINDOW_HANDLE = 0x00000000
	WM_WTSSESSION_CHANGE        = 0x02B1
)

// WTS session change notification codes
const (
	WTS_SESSION_LOGON          = 0x5
	WTS_SESSION_LOGOFF         = 0x6
	WTS_SESSION_LOCK           = 0x7
	WTS_SESSION_UNLOCK         = 0x8
	WTS_SESSION_REMOTE_CONTROL = 0x9
	WTS_SESSION_CREATE         = 0xA
	WTS_SESSION_TERMINATE      = 0xB
)

// WTS session notification flags
const (
	NOTIFY_FOR_THIS_SESSION = 0x0
	NOTIFY_FOR_ALL_SESSIONS = 0x1
)

// Display power states from GUID_CONSOLE_DISPLAY_STATE
const (
	DISPLAY_OFF    = 0
	DISPLAY_ON     = 1
	DISPLAY_DIMMED = 2
)

// D3DKMT GPU scheduling priority classes (gdi32.dll)
const (
	D3DKMT_SCHEDULINGPRIORITYCLASS_IDLE         = 0
	D3DKMT_SCHEDULINGPRIORITYCLASS_BELOW_NORMAL = 1
	D3DKMT_SCHEDULINGPRIORITYCLASS_NORMAL       = 2
	D3DKMT_SCHEDULINGPRIORITYCLASS_ABOVE_NORMAL = 3
	D3DKMT_SCHEDULINGPRIORITYCLASS_HIGH         = 4
	D3DKMT_SCHEDULINGPRIORITYCLASS_REALTIME     = 5
)
