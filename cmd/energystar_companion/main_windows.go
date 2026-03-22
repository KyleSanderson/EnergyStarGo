// SPDX-License-Identifier: GPL-2.0-only
// SPDX-FileCopyrightText: 2024 Kyle Sanderson

//go:build windows

// Package main implements the EnergyStarGo companion application.
// Runs in user session (Session 1+) and detects foreground window changes,
// sending foreground process IDs to the service via named pipe.
package main

import (
	"fmt"
	"os"
	"runtime"
	"syscall"
	"time"

	"golang.org/x/sys/windows"

	"github.com/KyleSanderson/EnergyStarGo/internal/logger"
	"github.com/KyleSanderson/EnergyStarGo/internal/winapi"
)

const (
	pipeNameFormat = `\\.\pipe\EnergyStarGo-Foreground`
)

var (
	user32dll           = windows.NewLazySystemDLL("user32.dll")
	procSetWinEventHook = user32dll.NewProc("SetWinEventHookW")
	procUnhookWinEvent  = user32dll.NewProc("UnhookWinEvent")
)

// Companion represents the foreground monitor application.
type Companion struct {
	log        *logger.Logger
	hookHandle uintptr
	pipeHandle windows.Handle
	stopCh     chan struct{}
	lastPID    uint32
}

// NewCompanion creates a new companion instance.
func NewCompanion() *Companion {
	log := logger.New(true, false) // Enable console, disable eventlog
	return &Companion{
		log:    log,
		stopCh: make(chan struct{}),
	}
}

// Start initializes the companion (foreground hook + pipe connection).
func (c *Companion) Start() error {
	c.log.Info("companion: starting foreground monitor")

	// Connect to service pipe (retry with backoff)
	if err := c.connectPipe(); err != nil {
		c.log.Warn("companion: failed to connect to service pipe, running in degraded mode", "error", err)
		// Continue anyway; pipe connection is best-effort
	}

	// Install foreground window hook
	callback := syscall.NewCallback(c.winEventProc)
	c.hookHandle = winapi.SetWinEventHook(
		winapi.EVENT_SYSTEM_FOREGROUND,
		winapi.EVENT_SYSTEM_FOREGROUND,
		0,
		callback,
		0, 0,
		winapi.WINEVENT_OUTOFCONTEXT,
	)
	if c.hookHandle == 0 {
		return fmt.Errorf("failed to install foreground hook")
	}
	c.log.Info("companion: foreground hook installed")
	return nil
}

// connectPipe attempts to connect to the service's named pipe (with retry).
func (c *Companion) connectPipe() error {
	// Retry up to 5 times with 100ms backoff (service might not have created pipe yet)
	for attempt := 0; attempt < 5; attempt++ {
		handle, err := windows.CreateFile(
			syscall.StringToUTF16Ptr(pipeNameFormat),
			windows.GENERIC_WRITE,
			0,
			nil,
			windows.OPEN_EXISTING,
			0,
			0,
		)
		if err == nil {
			c.pipeHandle = handle
			c.log.Debug("companion: connected to service pipe")
			return nil
		}

		if attempt < 4 {
			time.Sleep(100 * time.Millisecond)
		}
	}

	return fmt.Errorf("failed to connect to service pipe after retries")
}

// sendForegroundPID sends the foreground process ID to the service via pipe.
func (c *Companion) sendForegroundPID(pid uint32) {
	if c.pipeHandle == 0 {
		return // Pipe not connected
	}

	if pid == c.lastPID {
		return // Same PID, skip redundant message
	}

	// Send 4-byte PID to service
	pidBytes := [4]byte{
		byte(pid),
		byte(pid >> 8),
		byte(pid >> 16),
		byte(pid >> 24),
	}

	var written uint32
	err := windows.WriteFile(c.pipeHandle, pidBytes[:], &written, nil)
	if err != nil {
		c.log.Debug("companion: failed to send PID to pipe", "pid", pid, "error", err)
		// Close pipe on write error; will reconnect later
		windows.CloseHandle(c.pipeHandle)
		c.pipeHandle = 0
		_ = c.connectPipe() // Try to reconnect
		return
	}

	c.lastPID = pid
	c.log.Debug("companion: sent foreground PID to service", "pid", pid)
}

// winEventProc is the WinEvent callback for foreground window changes.
func (c *Companion) winEventProc(
	hWinEventHook uintptr,
	event uint32,
	hwnd uintptr,
	idObject int32,
	idChild int32,
	dwEventThread uint32,
	dwmsEventTime uint32,
) uintptr {
	// Get the process ID of the foreground window
	var procID uint32
	if winapi.GetWindowThreadProcessId(hwnd, &procID) == 0 || procID == 0 {
		return 0
	}

	// Send to service pipe
	c.sendForegroundPID(procID)
	return 0
}

// Stop removes the hook and closes the pipe.
func (c *Companion) Stop() {
	close(c.stopCh)

	if c.hookHandle != 0 {
		winapi.UnhookWinEvent(c.hookHandle)
		c.hookHandle = 0
		c.log.Info("companion: foreground hook removed")
	}

	if c.pipeHandle != 0 {
		windows.CloseHandle(c.pipeHandle)
		c.pipeHandle = 0
	}

	c.log.Info("companion: stopped")
}

// MessageLoop runs the companion's message loop (keeps process alive).
func (c *Companion) MessageLoop() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// Windows message loop (processes WinEvent messages)
	var msg winapi.MSG
	for {
		got, err := winapi.GetMessage(&msg)
		if err != nil {
			c.log.Error("GetMessage failed", "error", err)
			break
		}
		if !got { // WM_QUIT
			break
		}
		winapi.TranslateMessage(&msg)
		winapi.DispatchMessage(&msg)
	}
}

func main() {
	companion := NewCompanion()

	if err := companion.Start(); err != nil {
		fmt.Printf("ERROR: %v\n", err)
		os.Exit(1)
	}

	// Run message loop (blocks until Stop called or WM_QUIT)
	companion.MessageLoop()

	companion.Stop()
}
