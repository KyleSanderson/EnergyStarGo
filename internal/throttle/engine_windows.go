// SPDX-License-Identifier: GPL-2.0-only
// SPDX-FileCopyrightText: 2024 Kyle Sanderson

//go:build windows

// Package throttle implements the core EcoQoS process throttling engine.
package throttle

import (
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"

	"github.com/KyleSanderson/EnergyStarGo/internal/config"
	"github.com/KyleSanderson/EnergyStarGo/internal/logger"
	"github.com/KyleSanderson/EnergyStarGo/internal/winapi"
	"golang.org/x/sys/windows"
)

const (
	uwpFrameHostApp = "ApplicationFrameHost.exe"
)

// Stats tracks throttling statistics.
type Stats struct {
	mu                   sync.RWMutex
	TotalThrottled       int64
	TotalBoosted         int64
	HousekeepingRuns     int64
	ForegroundChanges    int64
	CurrentForeground    string
	CurrentForegroundPID uint32
}

func (s *Stats) IncrementThrottled()    { s.mu.Lock(); s.TotalThrottled++; s.mu.Unlock() }
func (s *Stats) IncrementBoosted()      { s.mu.Lock(); s.TotalBoosted++; s.mu.Unlock() }
func (s *Stats) IncrementHousekeeping() { s.mu.Lock(); s.HousekeepingRuns++; s.mu.Unlock() }
func (s *Stats) IncrementForeground()   { s.mu.Lock(); s.ForegroundChanges++; s.mu.Unlock() }

func (s *Stats) SetForeground(name string, pid uint32) {
	s.mu.Lock()
	s.CurrentForeground = name
	s.CurrentForegroundPID = pid
	s.mu.Unlock()
}

func (s *Stats) Snapshot() Stats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return Stats{
		TotalThrottled:       s.TotalThrottled,
		TotalBoosted:         s.TotalBoosted,
		HousekeepingRuns:     s.HousekeepingRuns,
		ForegroundChanges:    s.ForegroundChanges,
		CurrentForeground:    s.CurrentForeground,
		CurrentForegroundPID: s.CurrentForegroundPID,
	}
}

// Engine is the core throttle engine.
type Engine struct {
	cfg   *config.Config
	log   *logger.Logger
	stats Stats

	mu            sync.Mutex
	pendingPID    uint32
	pendingName   string
	throttledPIDs map[uint32]string // pid -> process name

	hookHandle uintptr
	stopCh     chan struct{}
	wg         sync.WaitGroup

	// Thread ID of the message loop thread, used for PostThreadMessage.
	msgThreadID uint32

	// Keep reference to callback to prevent GC
	winEventCallback uintptr

	// Pause state — set via SetPaused; guards throttling operations.
	pausedMu sync.Mutex
	paused   bool
}

// New creates a new throttle engine.
func New(cfg *config.Config, log *logger.Logger) *Engine {
	return &Engine{
		cfg:           cfg,
		log:           log,
		throttledPIDs: make(map[uint32]string),
		stopCh:        make(chan struct{}),
	}
}

// Stats returns a snapshot of current statistics.
func (e *Engine) Stats() Stats {
	return e.stats.Snapshot()
}

// SetPaused enables or disables the pause state. When paused, foreground events
// and housekeeping sweeps are skipped.
func (e *Engine) SetPaused(paused bool) {
	e.pausedMu.Lock()
	e.paused = paused
	e.pausedMu.Unlock()
}

// IsPaused reports whether the engine is currently paused.
func (e *Engine) IsPaused() bool {
	e.pausedMu.Lock()
	defer e.pausedMu.Unlock()
	return e.paused
}

// toggleEfficiencyMode enables or disables EcoQoS for a process handle.
func (e *Engine) toggleEfficiencyMode(hProcess windows.Handle, processID uint32, enable bool) error {
	var stateMask uint32
	if enable {
		stateMask = winapi.PROCESS_POWER_THROTTLING_EXECUTION_SPEED
	}

	state := winapi.PROCESS_POWER_THROTTLING_STATE{
		Version:     winapi.PROCESS_POWER_THROTTLING_CURRENT_VERSION,
		ControlMask: winapi.PROCESS_POWER_THROTTLING_EXECUTION_SPEED,
		StateMask:   stateMask,
	}

	err := winapi.SetProcessInformation(
		hProcess,
		winapi.ProcessPowerThrottling,
		unsafe.Pointer(&state),
		uint32(unsafe.Sizeof(state)),
	)
	if err != nil {
		return err
	}

	var priorityClass uint32
	if enable {
		priorityClass = winapi.IDLE_PRIORITY_CLASS
	} else {
		priorityClass = winapi.NORMAL_PRIORITY_CLASS
	}

	if err := winapi.SetPriorityClass(hProcess, priorityClass); err != nil {
		return err
	}

	// Best-effort: throttle individual threads too (improves EcoQoS effectiveness on multi-threaded apps)
	e.throttleProcessThreads(processID, enable)
	return nil
}

// getProcessName returns the executable name for a process handle.
func (e *Engine) getProcessName(hProcess windows.Handle) string {
	name, err := winapi.QueryFullProcessImageName(hProcess)
	if err != nil {
		return ""
	}
	return filepath.Base(name)
}

// HandleForegroundEvent processes a foreground window change event.
func (e *Engine) HandleForegroundEvent(hwnd uintptr) {
	if e.IsPaused() {
		return
	}
	var procID uint32
	threadID := winapi.GetWindowThreadProcessId(hwnd, &procID)
	if threadID == 0 || procID == 0 {
		return
	}

	hProcess, err := winapi.OpenProcess(
		winapi.PROCESS_QUERY_LIMITED_INFORMATION|winapi.PROCESS_SET_INFORMATION,
		false, procID,
	)
	if err != nil {
		return
	}
	defer winapi.CloseHandle(hProcess)

	appName := e.getProcessName(hProcess)
	if appName == "" {
		return
	}

	e.stats.IncrementForeground()
	e.log.ForegroundChange(appName, procID)

	// UWP special handling: find the actual child process
	if strings.EqualFold(appName, uwpFrameHostApp) {
		hProcess, procID, appName = e.resolveUWPProcess(hwnd, hProcess, procID)
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	bypass := e.cfg.ShouldBypass(appName)

	if !bypass {
		// Boost the foreground process
		if err := e.toggleEfficiencyMode(hProcess, procID, false); err == nil {
			e.log.Boost(appName, procID)
			e.stats.IncrementBoosted()
		}
		e.stats.SetForeground(appName, procID)

		// Remove from throttled set
		delete(e.throttledPIDs, procID)
	}

	// Throttle the previous foreground process
	if e.pendingPID != 0 && e.pendingPID != procID {
		prevHandle, err := winapi.OpenProcess(
			winapi.PROCESS_SET_INFORMATION, false, e.pendingPID,
		)
		if err == nil {
			if err := e.toggleEfficiencyMode(prevHandle, e.pendingPID, true); err == nil {
				e.log.Throttle(e.pendingName, e.pendingPID)
				e.stats.IncrementThrottled()
				e.throttledPIDs[e.pendingPID] = e.pendingName
			}
			winapi.CloseHandle(prevHandle)
		}
	}

	if !bypass {
		e.pendingPID = procID
		e.pendingName = appName
	}
}

// resolveUWPProcess finds the actual UWP process behind ApplicationFrameHost.
func (e *Engine) resolveUWPProcess(hwnd uintptr, parentHandle windows.Handle, parentPID uint32) (windows.Handle, uint32, string) {
	type result struct {
		handle windows.Handle
		pid    uint32
		name   string
		found  bool
	}

	var res result
	res.handle = parentHandle
	res.pid = parentPID
	res.name = uwpFrameHostApp

	callback := syscall.NewCallback(func(childHwnd uintptr, lparam uintptr) uintptr {
		if res.found {
			return 1 // continue but ignore
		}

		var childPID uint32
		if winapi.GetWindowThreadProcessId(childHwnd, &childPID) == 0 {
			return 1
		}
		if childPID == parentPID {
			return 1
		}

		childHandle, err := winapi.OpenProcess(
			winapi.PROCESS_QUERY_LIMITED_INFORMATION|winapi.PROCESS_SET_INFORMATION,
			false, childPID,
		)
		if err != nil {
			return 1
		}

		childName := e.getProcessName(childHandle)
		if childName != "" {
			res.found = true
			winapi.CloseHandle(parentHandle)
			res.handle = childHandle
			res.pid = childPID
			res.name = childName
		} else {
			winapi.CloseHandle(childHandle)
		}

		return 1
	})

	winapi.EnumChildWindows(hwnd, callback)
	return res.handle, res.pid, res.name
}

// ThrottleAllUserBackgroundProcesses throttles all processes in the current session.
func (e *Engine) ThrottleAllUserBackgroundProcesses() int {
	if e.IsPaused() {
		return 0
	}
	sessionID, err := winapi.GetCurrentProcessSessionId()
	if err != nil {
		e.log.Error("failed to get current session ID", "error", err)
		return 0
	}

	snapshot, err := winapi.CreateToolhelp32Snapshot(winapi.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		e.log.Error("failed to create process snapshot", "error", err)
		return 0
	}
	defer winapi.CloseHandle(snapshot)

	var entry winapi.PROCESSENTRY32W
	err = winapi.Process32First(snapshot, &entry)
	if err != nil {
		e.log.Error("failed to get first process", "error", err)
		return 0
	}

	e.mu.Lock()
	pendingPID := e.pendingPID
	e.mu.Unlock()

	count := 0
	for {
		procName := winapi.ProcessNameFromEntry(&entry)
		pid := entry.ProcessID

		// Skip the foreground process and bypassed processes
		if pid != pendingPID && !e.cfg.ShouldBypass(procName) {
			// Check session ID
			procSessionID, err := winapi.ProcessIdToSessionId(pid)
			if err == nil && procSessionID == sessionID {
				hProcess, err := winapi.OpenProcess(
					winapi.PROCESS_SET_INFORMATION, false, pid,
				)
				if err == nil {
					if e.toggleEfficiencyMode(hProcess, pid, true) == nil {
						count++
						e.mu.Lock()
						e.throttledPIDs[pid] = procName
						e.mu.Unlock()
					}
					winapi.CloseHandle(hProcess)
				}
			}
		}

		err = winapi.Process32Next(snapshot, &entry)
		if err != nil {
			break
		}
	}

	e.stats.IncrementHousekeeping()
	e.log.Housekeeping(count)
	return count
}

// RestoreAllProcesses removes throttling from all previously throttled processes.
func (e *Engine) RestoreAllProcesses() int {
	e.mu.Lock()
	pids := make(map[uint32]string, len(e.throttledPIDs))
	for k, v := range e.throttledPIDs {
		pids[k] = v
	}
	e.mu.Unlock()

	count := 0
	for pid, name := range pids {
		hProcess, err := winapi.OpenProcess(
			winapi.PROCESS_SET_INFORMATION, false, pid,
		)
		if err != nil {
			continue
		}
		if e.toggleEfficiencyMode(hProcess, pid, false) == nil {
			e.log.Boost(name, pid)
			count++
		}
		winapi.CloseHandle(hProcess)
	}

	e.mu.Lock()
	e.throttledPIDs = make(map[uint32]string)
	e.mu.Unlock()

	e.log.Info("restored all processes", "count", count)
	return count
}

// RunMessageLoop runs the Win32 message loop with foreground event hook.
// This must be called from the main goroutine (locked OS thread).
func (e *Engine) RunMessageLoop() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// Record the thread ID so Stop() can post WM_QUIT from another goroutine.
	atomic.StoreUint32(&e.msgThreadID, winapi.GetCurrentThreadId())

	// Create the WinEvent callback
	e.winEventCallback = syscall.NewCallback(func(
		hWinEventHook uintptr,
		event uint32,
		hwnd uintptr,
		idObject int32,
		idChild int32,
		dwEventThread uint32,
		dwmsEventTime uint32,
	) uintptr {
		e.HandleForegroundEvent(hwnd)
		return 0
	})

	// Install the hook
	e.hookHandle = winapi.SetWinEventHook(
		winapi.EVENT_SYSTEM_FOREGROUND,
		winapi.EVENT_SYSTEM_FOREGROUND,
		0,
		e.winEventCallback,
		0, 0,
		winapi.WINEVENT_OUTOFCONTEXT|winapi.WINEVENT_SKIPOWNPROCESS,
	)
	if e.hookHandle == 0 {
		e.log.Error("failed to set WinEvent hook")
		return
	}
	e.log.Info("foreground event hook installed")

	defer func() {
		winapi.UnhookWinEvent(e.hookHandle)
		e.log.Info("foreground event hook removed")
	}()

	// Message loop — GetMessage blocks until a message is available.
	// Stop() posts WM_QUIT via PostThreadMessage to unblock it.
	var msg winapi.MSG
	for {
		got, err := winapi.GetMessage(&msg)
		if err != nil {
			e.log.Error("GetMessage failed", "error", err)
			break
		}
		if !got { // WM_QUIT
			break
		}
		winapi.TranslateMessage(&msg)
		winapi.DispatchMessage(&msg)
	}
}

// Stop signals the engine to stop the message loop.
// Safe to call from any goroutine.
func (e *Engine) Stop() {
	select {
	case <-e.stopCh:
		return // already stopped
	default:
		close(e.stopCh)
	}
	// Post WM_QUIT to the message loop thread so GetMessage returns 0.
	if tid := atomic.LoadUint32(&e.msgThreadID); tid != 0 {
		winapi.PostThreadMessage(tid, winapi.WM_QUIT, 0, 0)
	}
}

// ThrottledProcesses returns a copy of the currently throttled process map.
func (e *Engine) ThrottledProcesses() map[uint32]string {
	e.mu.Lock()
	defer e.mu.Unlock()
	result := make(map[uint32]string, len(e.throttledPIDs))
	for k, v := range e.throttledPIDs {
		result[k] = v
	}
	return result
}

// throttleProcessThreads applies EcoQoS-style speed throttling to all threads
// of the given process. Errors are ignored (best-effort; some system threads reject it).
func (e *Engine) throttleProcessThreads(processID uint32, enable bool) {
	// TH32CS_SNAPTHREAD with processID=0 gives ALL threads across the system
	snapshot, err := winapi.CreateToolhelp32Snapshot(winapi.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return
	}
	defer winapi.CloseHandle(snapshot)

	var entry winapi.THREADENTRY32
	if err := winapi.Thread32First(snapshot, &entry); err != nil {
		return
	}

	var stateMask uint32
	if enable {
		stateMask = winapi.PROCESS_POWER_THROTTLING_EXECUTION_SPEED
	}
	state := winapi.PROCESS_POWER_THROTTLING_STATE{
		Version:     winapi.PROCESS_POWER_THROTTLING_CURRENT_VERSION,
		ControlMask: winapi.PROCESS_POWER_THROTTLING_EXECUTION_SPEED,
		StateMask:   stateMask,
	}

	for {
		if entry.OwnerProcessID == processID {
			hThread, err := winapi.OpenThread(
				winapi.THREAD_SET_INFORMATION, false, entry.ThreadID,
			)
			if err == nil {
				_ = winapi.SetThreadInformation(
					hThread,
					winapi.ThreadPowerThrottling,
					unsafe.Pointer(&state),
					uint32(unsafe.Sizeof(state)),
				)
				winapi.CloseHandle(hThread)
			}
		}
		if err := winapi.Thread32Next(snapshot, &entry); err != nil {
			break
		}
	}
}
