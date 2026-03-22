// SPDX-License-Identifier: GPL-2.0-only
// SPDX-FileCopyrightText: 2024 Kyle Sanderson

//go:build windows

package winapi

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// ETW Mode Flags
const (
	EVENT_TRACE_REAL_TIME_MODE       = 0x00000100
	EVENT_TRACE_FILE_MODE_SEQUENTIAL = 0x00000001
	PROCESS_TRACE_MODE_RAW_TIMESTAMP = 0x00000001
	PROCESS_TRACE_MODE_REAL_TIME     = 0x00000100

	// Trace Levels
	TRACE_LEVEL_CRITICAL    = 1
	TRACE_LEVEL_ERROR       = 2
	TRACE_LEVEL_WARNING     = 3
	TRACE_LEVEL_INFORMATION = 4
	TRACE_LEVEL_VERBOSE     = 5

	// Enable flags for Kernel-Process
	EVENT_ENABLE_KEYWORD_PROCESS_CREATE = 0x00000010

	// Control codes
	EVENT_CONTROL_CODE_ENABLE  = 1
	EVENT_CONTROL_CODE_DISABLE = 0

	// Return codes
	ERROR_SUCCESS        = 0
	ERROR_ALREADY_EXISTS = 183
	ERROR_NOT_FOUND      = 1168
)

// Kernel-Process Provider GUID
var KernelProcessProviderGUID = windows.GUID{
	Data1: 0x22FB2CD6,
	Data2: 0x0E7B,
	Data3: 0x422B,
	Data4: [8]byte{0xA0, 0xC7, 0x2F, 0xAD, 0x1F, 0xD0, 0xE7, 0x16},
}

// ETW Syscall procs
var (
	modAdvapi32 = windows.NewLazySystemDLL("advapi32.dll")

	procStartTraceW    = modAdvapi32.NewProc("StartTraceW")
	procControlTraceW  = modAdvapi32.NewProc("ControlTraceW")
	procOpenTraceW     = modAdvapi32.NewProc("OpenTraceW")
	procProcessTraceW  = modAdvapi32.NewProc("ProcessTraceW")
	procCloseTrace     = modAdvapi32.NewProc("CloseTrace")
	procEnableTraceEx2 = modAdvapi32.NewProc("EnableTraceEx2")
	procCreateEventW   = modAdvapi32.NewProc("CreateEventW")
)

// WNODE_HEADER - Part of EVENT_TRACE_PROPERTIES
type WNODE_HEADER struct {
	BufferSize        uint32
	ProviderId        uint32
	HistoricalContext uint64
	TimeStamp         int64
	Guid              windows.GUID
	ClientContext     uint32
	Flags             uint32
}

// EVENT_TRACE_PROPERTIES - Used to configure ETW session
type EVENT_TRACE_PROPERTIES struct {
	Wnode               WNODE_HEADER
	BufferSize          uint32
	MinimumBuffers      uint32
	MaximumBuffers      uint32
	MaximumFileSize     uint32
	LogFileMode         uint32
	FlushTimer          uint32
	EnableFlags         uint32
	AgeLimit            int32
	NumberOfBuffers     uint32
	FreeBuffers         uint32
	EventsLost          uint32
	BuffersWritten      uint32
	LogBuffersLost      uint32
	RealTimeBuffersLost uint32
	LoggerThreadId      uint32
	LogFileNameOffset   uint32
	LoggerNameOffset    uint32
}

// ENABLE_TRACE_PARAMETERS - For EnableTraceEx2
type ENABLE_TRACE_PARAMETERS struct {
	Version          uint32
	EnableProperty   uint32
	ControlFlags     uint32
	SourceId         windows.GUID
	EnableFilterDesc uintptr // EVENT_FILTER_DESCRIPTOR, unused for now
	FilterDescCount  uint32
}

// EVENT_TRACE_HEADER - Header of each ETW event
type EVENT_TRACE_HEADER struct {
	Size            uint16
	HeaderType      uint16
	Flags           uint16
	EventProperty   uint16
	ThreadId        uint32
	ProcessId       uint32
	TimeStamp       int64
	ProviderId      windows.GUID
	EventDescriptor EVENT_DESCRIPTOR
	KernelTime      uint32
	UserTime        uint32
	ProcessorTime   uint64
}

// EVENT_DESCRIPTOR - Describes event type/id/level
type EVENT_DESCRIPTOR struct {
	Id      uint16
	Version uint8
	Channel uint8
	Level   uint8
	Opcode  uint8
	Task    uint16
	Keyword uint64
}

// EVENT_TRACE - Complete ETW event
type EVENT_TRACE struct {
	Header           EVENT_TRACE_HEADER
	InstanceId       uint32
	ParentInstanceId uint32
	ParentGuid       windows.GUID
	MofData          uintptr
	MofLength        uint32
	ClientContext    uint32
}

// EnableTraceEx2 enables events from a specified provider (via EnableTraceEx2)
func EnableTraceEx2(sessionHandle uintptr, providerGUID windows.GUID, level uint8, keyword uint64) error {
	params := &ENABLE_TRACE_PARAMETERS{
		Version: 2,
	}

	ret, _, err := procEnableTraceEx2.Call(
		sessionHandle,
		uintptr(unsafe.Pointer(&providerGUID)),
		EVENT_CONTROL_CODE_ENABLE,
		uintptr(level),
		uintptr(keyword),
		0, // options
		0, // timeout
		uintptr(unsafe.Pointer(params)),
	)

	if ret != ERROR_SUCCESS {
		return err
	}
	return nil
}

// StartTrace creates an ETW session
func StartTrace(sessionName string) (uintptr, error) {
	sessionNameUTF16, _ := windows.UTF16FromString(sessionName)

	var sessionHandle uintptr

	// Create and configure session properties
	props := &EVENT_TRACE_PROPERTIES{
		BufferSize:     4096,
		MinimumBuffers: 2,
		MaximumBuffers: 256,
		LogFileMode:    EVENT_TRACE_REAL_TIME_MODE,
		EnableFlags:    0x00000001, // Process tracing
		FlushTimer:     1,
	}

	props.Wnode.BufferSize = uint32(unsafe.Sizeof(*props))
	props.Wnode.Flags = 0x00020000 // WNODE_FLAG_TRACED_GUID

	ret, _, err := procStartTraceW.Call(
		uintptr(unsafe.Pointer(&sessionHandle)),
		uintptr(unsafe.Pointer(&sessionNameUTF16[0])),
		uintptr(unsafe.Pointer(props)),
	)

	if ret != ERROR_SUCCESS && ret != uintptr(ERROR_ALREADY_EXISTS) {
		return 0, err
	}

	return sessionHandle, nil
}

// OpenTrace opens a realtime ETW trace for reading
func OpenTrace(sessionHandle uintptr) (uintptr, error) {
	logfileUTF16, _ := windows.UTF16FromString("EnergyStarGoTrace")

	ret, _, err := procOpenTraceW.Call(
		uintptr(unsafe.Pointer(&logfileUTF16[0])),
		0,          // flags
		uintptr(0), // reserved
	)

	if ret == 0 || ret == 0xFFFFFFFF {
		return 0, err
	}

	return ret, nil
}

// ProcessTraceW processes events from an ETW trace (blocking)
// Returns when session closes or error occurs
func ProcessTraceW(traceHandles []uintptr, timeout uint32) error {
	var traceHandlesPtr uintptr
	if len(traceHandles) > 0 {
		traceHandlesPtr = uintptr(unsafe.Pointer(&traceHandles[0]))
	}

	ret, _, err := procProcessTraceW.Call(
		traceHandlesPtr,
		uintptr(len(traceHandles)),
		uintptr(unsafe.Pointer(nil)), // startTime
		uintptr(unsafe.Pointer(nil)), // endTime
	)

	if ret != ERROR_SUCCESS {
		return err
	}
	return nil
}

// CloseTrace closes an ETW session
func CloseTrace(sessionHandle uintptr) error {
	ret, _, err := procCloseTrace.Call(sessionHandle)

	if ret != ERROR_SUCCESS && ret != uintptr(ERROR_NOT_FOUND) {
		return err
	}
	return nil
}

// ControlTrace stops an ETW session
func ControlTrace(sessionHandle uintptr) error {
	props := &EVENT_TRACE_PROPERTIES{
		BufferSize: 4096,
	}
	props.Wnode.BufferSize = uint32(unsafe.Sizeof(*props))

	ret, _, err := procControlTraceW.Call(
		sessionHandle,
		uintptr(0),
		uintptr(unsafe.Pointer(props)),
		1, // EVENT_TRACE_CONTROL_STOP
	)

	if ret != ERROR_SUCCESS {
		return err
	}
	return nil
}
