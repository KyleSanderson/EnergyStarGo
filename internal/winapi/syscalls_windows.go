// SPDX-License-Identifier: GPL-2.0-only
// SPDX-FileCopyrightText: 2024 Kyle Sanderson

//go:build windows

package winapi

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modkernel32 = windows.NewLazySystemDLL("kernel32.dll")
	moduser32   = windows.NewLazySystemDLL("user32.dll")

	procSetProcessInformation    = modkernel32.NewProc("SetProcessInformation")
	procCreateToolhelp32Snapshot = modkernel32.NewProc("CreateToolhelp32Snapshot")
	procProcess32FirstW          = modkernel32.NewProc("Process32FirstW")
	procProcess32NextW           = modkernel32.NewProc("Process32NextW")
	procProcessIdToSessionId     = modkernel32.NewProc("ProcessIdToSessionId")

	procSetWinEventHook     = moduser32.NewProc("SetWinEventHook")
	procUnhookWinEvent      = moduser32.NewProc("UnhookWinEvent")
	procGetMessageW         = moduser32.NewProc("GetMessageW")
	procTranslateMessage    = moduser32.NewProc("TranslateMessage")
	procDispatchMessageW    = moduser32.NewProc("DispatchMessageW")
	procPostQuitMessage     = moduser32.NewProc("PostQuitMessage")
	procPostThreadMessageW  = moduser32.NewProc("PostThreadMessageW")
	procGetForegroundWindow = moduser32.NewProc("GetForegroundWindow")
)

// SetProcessInformation sets process information for the given process handle.
func SetProcessInformation(hProcess windows.Handle, processInformationClass int, processInformation unsafe.Pointer, processInformationSize uint32) error {
	r1, _, err := procSetProcessInformation.Call(
		uintptr(hProcess),
		uintptr(processInformationClass),
		uintptr(processInformation),
		uintptr(processInformationSize),
	)
	if r1 == 0 {
		return err
	}
	return nil
}

// SetPriorityClass sets the priority class for the specified process.
func SetPriorityClass(hProcess windows.Handle, priorityClass uint32) error {
	return windows.SetPriorityClass(hProcess, priorityClass)
}

// OpenProcess opens an existing local process object.
func OpenProcess(desiredAccess uint32, inheritHandle bool, processID uint32) (windows.Handle, error) {
	return windows.OpenProcess(desiredAccess, inheritHandle, processID)
}

// CloseHandle closes an open object handle.
func CloseHandle(handle windows.Handle) error {
	return windows.CloseHandle(handle)
}

// GetWindowThreadProcessId retrieves the thread and process ID for the specified window.
func GetWindowThreadProcessId(hwnd uintptr, processID *uint32) uint32 {
	ret, _, _ := moduser32.NewProc("GetWindowThreadProcessId").Call(
		hwnd,
		uintptr(unsafe.Pointer(processID)),
	)
	return uint32(ret)
}

// QueryFullProcessImageName retrieves the full name of the executable image for the specified process.
func QueryFullProcessImageName(hProcess windows.Handle) (string, error) {
	buf := make([]uint16, 1024)
	size := uint32(len(buf))
	err := windows.QueryFullProcessImageName(hProcess, 0, &buf[0], &size)
	if err != nil {
		return "", err
	}
	return syscall.UTF16ToString(buf[:size]), nil
}

// EnumChildWindows enumerates the child windows that belong to the specified parent window.
func EnumChildWindows(hwnd uintptr, callback uintptr) {
	moduser32.NewProc("EnumChildWindows").Call(
		hwnd,
		callback,
		0,
	)
}

// SetWinEventHook installs a hook function for a range of events.
// Returns a hook handle or 0 on failure.
func SetWinEventHook(eventMin, eventMax uint32, hmodWinEventProc uintptr, callback uintptr, idProcess, idThread, dwFlags uint32) uintptr {
	ret, _, _ := procSetWinEventHook.Call(
		uintptr(eventMin),
		uintptr(eventMax),
		hmodWinEventProc,
		callback,
		uintptr(idProcess),
		uintptr(idThread),
		uintptr(dwFlags),
	)
	return ret
}

// UnhookWinEvent removes a hook set by SetWinEventHook.
func UnhookWinEvent(hook uintptr) bool {
	ret, _, _ := procUnhookWinEvent.Call(hook)
	return ret != 0
}

// GetMessage retrieves a message from the calling thread's message queue.
func GetMessage(msg *MSG) (bool, error) {
	ret, _, err := procGetMessageW.Call(
		uintptr(unsafe.Pointer(msg)),
		0, 0, 0,
	)
	if int32(ret) == -1 {
		return false, err
	}
	return ret != 0, nil
}

// TranslateMessage translates virtual-key messages.
func TranslateMessage(msg *MSG) {
	procTranslateMessage.Call(uintptr(unsafe.Pointer(msg)))
}

// DispatchMessage dispatches a message to a window procedure.
func DispatchMessage(msg *MSG) {
	procDispatchMessageW.Call(uintptr(unsafe.Pointer(msg)))
}

// PostQuitMessage indicates to the system that a thread has made a request to quit.
func PostQuitMessage(exitCode int) {
	procPostQuitMessage.Call(uintptr(exitCode))
}

// PostThreadMessage posts a message to the message queue of the specified thread.
func PostThreadMessage(threadID uint32, msg uint32, wParam, lParam uintptr) error {
	ret, _, err := procPostThreadMessageW.Call(
		uintptr(threadID),
		uintptr(msg),
		wParam,
		lParam,
	)
	if ret == 0 {
		return err
	}
	return nil
}

// GetCurrentThreadId returns the thread ID of the calling thread.
func GetCurrentThreadId() uint32 {
	ret, _, _ := modkernel32.NewProc("GetCurrentThreadId").Call()
	return uint32(ret)
}

// GetForegroundWindow retrieves a handle to the foreground window.
func GetForegroundWindow() uintptr {
	ret, _, _ := procGetForegroundWindow.Call()
	return ret
}

// CreateToolhelp32Snapshot takes a snapshot of processes, threads, etc.
func CreateToolhelp32Snapshot(flags, processID uint32) (windows.Handle, error) {
	ret, _, err := procCreateToolhelp32Snapshot.Call(
		uintptr(flags),
		uintptr(processID),
	)
	handle := windows.Handle(ret)
	if handle == windows.InvalidHandle {
		return 0, err
	}
	return handle, nil
}

// Process32First retrieves info about the first process in a snapshot.
func Process32First(snapshot windows.Handle, entry *PROCESSENTRY32W) error {
	entry.Size = uint32(unsafe.Sizeof(*entry))
	ret, _, err := procProcess32FirstW.Call(
		uintptr(snapshot),
		uintptr(unsafe.Pointer(entry)),
	)
	if ret == 0 {
		return err
	}
	return nil
}

// Process32Next retrieves info about the next process in a snapshot.
func Process32Next(snapshot windows.Handle, entry *PROCESSENTRY32W) error {
	entry.Size = uint32(unsafe.Sizeof(*entry))
	ret, _, err := procProcess32NextW.Call(
		uintptr(snapshot),
		uintptr(unsafe.Pointer(entry)),
	)
	if ret == 0 {
		return err
	}
	return nil
}

// ProcessIdToSessionId retrieves the session ID for the specified process.
func ProcessIdToSessionId(processID uint32) (uint32, error) {
	var sessionID uint32
	ret, _, err := procProcessIdToSessionId.Call(
		uintptr(processID),
		uintptr(unsafe.Pointer(&sessionID)),
	)
	if ret == 0 {
		return 0, err
	}
	return sessionID, nil
}

// GetCurrentProcessSessionId returns the session ID of the current process.
func GetCurrentProcessSessionId() (uint32, error) {
	pid := windows.GetCurrentProcessId()
	return ProcessIdToSessionId(pid)
}

// ProcessNameFromEntry extracts the process name string from a PROCESSENTRY32W.
func ProcessNameFromEntry(entry *PROCESSENTRY32W) string {
	return syscall.UTF16ToString(entry.ExeFile[:])
}
