// SPDX-License-Identifier: GPL-2.0-only
// SPDX-FileCopyrightText: 2024 Kyle Sanderson <kyle@kylesanderson.dev>

//go:build windows

// Package uac provides User Account Control elevation helpers.
package uac

import (
	"os"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modshell32         = windows.NewLazySystemDLL("shell32.dll")
	procShellExecuteEx = modshell32.NewProc("ShellExecuteExW")
)

// SHELLEXECUTEINFOW holds parameters for ShellExecuteExW.
type SHELLEXECUTEINFOW struct {
	CbSize         uint32
	FMask          uint32
	Hwnd           uintptr
	LpVerb         *uint16
	LpFile         *uint16
	LpParameters   *uint16
	LpDirectory    *uint16
	NShow          int32
	HInstApp       uintptr
	LpIDList       uintptr
	LpClass        *uint16
	HkeyClass      uintptr
	DwHotKey       uint32
	HIconOrMonitor uintptr
	HProcess       uintptr
}

const (
	SEE_MASK_NOCLOSEPROCESS = 0x00000040
	SEE_MASK_UNICODE        = 0x00004000
)

// IsElevated returns true if the current process is running with administrative privileges.
func IsElevated() bool {
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return false
	}
	defer token.Close()

	var elevation struct{ TokenIsElevated uint32 }
	var n uint32
	if err := windows.GetTokenInformation(
		token,
		windows.TokenElevation,
		(*byte)(unsafe.Pointer(&elevation)),
		uint32(unsafe.Sizeof(elevation)),
		&n,
	); err != nil {
		return false
	}
	return elevation.TokenIsElevated != 0
}

// Elevate re-launches the current executable with all provided args using
// ShellExecuteEx with the "runas" verb to trigger a UAC elevation prompt.
// Returns nil if the elevated process launched successfully; the caller should
// call os.Exit after this returns nil.
func Elevate(args ...string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}

	verbPtr, err := syscall.UTF16PtrFromString("runas")
	if err != nil {
		return err
	}
	exePtr, err := syscall.UTF16PtrFromString(exe)
	if err != nil {
		return err
	}

	var paramsPtr *uint16
	if len(args) > 0 {
		paramsPtr, err = syscall.UTF16PtrFromString(buildArgs(args))
		if err != nil {
			return err
		}
	}

	info := SHELLEXECUTEINFOW{
		FMask:        SEE_MASK_NOCLOSEPROCESS | SEE_MASK_UNICODE,
		LpVerb:       verbPtr,
		LpFile:       exePtr,
		LpParameters: paramsPtr,
		NShow:        0, // SW_HIDE — no console window flash for sub-commands
	}
	info.CbSize = uint32(unsafe.Sizeof(info))

	r1, _, lastErr := procShellExecuteEx.Call(uintptr(unsafe.Pointer(&info)))
	if r1 == 0 {
		return lastErr
	}
	if info.HProcess != 0 {
		windows.CloseHandle(windows.Handle(info.HProcess))
	}
	return nil
}

// buildArgs joins args into a single command-line string, quoting arguments
// that contain spaces, tabs, or double-quote characters.
func buildArgs(args []string) string {
	var sb strings.Builder
	for i, a := range args {
		if i > 0 {
			sb.WriteByte(' ')
		}
		if strings.ContainsAny(a, " \t\"") {
			sb.WriteByte('"')
			for _, c := range a {
				if c == '"' {
					sb.WriteByte('\\')
				}
				sb.WriteRune(c)
			}
			sb.WriteByte('"')
		} else {
			sb.WriteString(a)
		}
	}
	return sb.String()
}
