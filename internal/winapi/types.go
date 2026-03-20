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

// RECT is the Win32 RECT struct.
type RECT struct {
	Left   int32
	Top    int32
	Right  int32
	Bottom int32
}

// MONITORINFO contains information about a display monitor.
type MONITORINFO struct {
	CbSize    uint32
	RcMonitor RECT
	RcWork    RECT
	DwFlags   uint32
}

// POWERBROADCAST_SETTING is the struct delivered via WM_POWERBROADCAST /
// PBT_POWERSETTINGCHANGE in lParam.
type POWERBROADCAST_SETTING struct {
	PowerSetting [16]byte
	DataLength   uint32
	Data         [1]byte
}

// MEMORY_PRIORITY_INFORMATION is used with SetProcessInformation
// for ProcessMemoryPriority class to control working set priority.
type MEMORY_PRIORITY_INFORMATION struct {
	MemoryPriority uint32
}

// MEMORYSTATUSEX contains information about the current state of both
// physical and virtual memory, including extended memory.
type MEMORYSTATUSEX struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}
