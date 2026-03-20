// SPDX-License-Identifier: GPL-2.0-only
// SPDX-FileCopyrightText: 2024 Kyle Sanderson

package winapi

// PROCESS_POWER_THROTTLING_STATE is the struct passed to SetProcessInformation
// for ProcessPowerThrottling class. Size: 12 bytes (3 x uint32).
type PROCESS_POWER_THROTTLING_STATE struct {
	Version     uint32
	ControlMask uint32
	StateMask   uint32
}

// PROCESSENTRY32W is used with CreateToolhelp32Snapshot/Process32First/Process32Next.
type PROCESSENTRY32W struct {
	Size            uint32
	CntUsage        uint32
	ProcessID       uint32
	DefaultHeapID   uintptr
	ModuleID        uint32
	CntThreads      uint32
	ParentProcessID uint32
	PriClassBase    int32
	Flags           uint32
	ExeFile         [260]uint16
}

// MSG is the Win32 MSG struct for the message loop.
type MSG struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      POINT
}

// POINT is a Win32 POINT struct.
type POINT struct {
	X int32
	Y int32
}

// THREADENTRY32 is used with CreateToolhelp32Snapshot/Thread32First/Thread32Next.
type THREADENTRY32 struct {
	Size           uint32
	Usage          uint32
	ThreadID       uint32
	OwnerProcessID uint32
	BasePri        int32
	DeltaPri       int32
	Flags          uint32
}
