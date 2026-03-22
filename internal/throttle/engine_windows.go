// SPDX-License-Identifier: GPL-2.0-only
// SPDX-FileCopyrightText: 2024 Kyle Sanderson

//go:build windows

// Package throttle implements the core EcoQoS process throttling engine.
package throttle

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"

	"github.com/KyleSanderson/EnergyStarGo/internal/config"
	"github.com/KyleSanderson/EnergyStarGo/internal/etw"
	"github.com/KyleSanderson/EnergyStarGo/internal/logger"
	"github.com/KyleSanderson/EnergyStarGo/internal/power"
	"github.com/KyleSanderson/EnergyStarGo/internal/winapi"
	"golang.org/x/sys/windows"
)

const (
	uwpFrameHostApp = "ApplicationFrameHost.exe"
)

var (
	// Syscalls for engine window management (similar to tray code)
	user32dll                   = windows.NewLazySystemDLL("user32.dll")
	engineProc_CreateWindowExW  = user32dll.NewProc("CreateWindowExW")
	engineProc_DefWindowProcW   = user32dll.NewProc("DefWindowProcW")
	engineProc_RegisterClassExW = user32dll.NewProc("RegisterClassExW")
	engineProc_DestroyWindow    = user32dll.NewProc("DestroyWindow")
)

// engineWNDCLASSEXW is window class structure for engine window
type engineWNDCLASSEXW struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  uintptr
	LpszClassName uintptr
	HIconSm       uintptr
}

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

	// Hidden window for power notifications (service mode)
	hwnd uintptr

	// Power notification registration handles
	powerSourceNotifHandle  uintptr
	powerPlanNotifHandle    uintptr
	displayStateNotifHandle uintptr
	batteryNotifHandle      uintptr

	// ETW process creation monitoring
	etwMonitor *etw.ProcessMonitor

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
		etwMonitor:    etw.New(log),
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
	controlMask := uint32(winapi.PROCESS_POWER_THROTTLING_EXECUTION_SPEED | winapi.PROCESS_POWER_THROTTLING_IGNORE_TIMER_RESOLUTION)
	var stateMask uint32
	if enable {
		stateMask = controlMask
	}

	state := winapi.PROCESS_POWER_THROTTLING_STATE{
		Version:     winapi.PROCESS_POWER_THROTTLING_CURRENT_VERSION,
		ControlMask: controlMask,
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

	// Best-effort: reduce memory working set priority
	memPri := winapi.MEMORY_PRIORITY_INFORMATION{MemoryPriority: winapi.MEMORY_PRIORITY_NORMAL}
	if enable {
		memPri.MemoryPriority = winapi.MEMORY_PRIORITY_VERY_LOW
	}
	_ = winapi.SetProcessInformation(hProcess, winapi.ProcessMemoryPriority, unsafe.Pointer(&memPri), uint32(unsafe.Sizeof(memPri)))

	// Best-effort: throttle individual threads too (improves EcoQoS effectiveness on multi-threaded apps)
	e.throttleProcessThreads(processID, enable)

	// Best-effort: set GPU scheduling priority when gpu_throttling is enabled
	if e.cfg.GPUThrottling {
		gpuPriority := winapi.D3DKMT_SCHEDULINGPRIORITYCLASS_NORMAL
		if enable {
			gpuPriority = winapi.D3DKMT_SCHEDULINGPRIORITYCLASS_IDLE
		}
		_ = winapi.SetProcessGPUSchedulingPriority(hProcess, uint32(gpuPriority))
	}

	// Best-effort: pin throttled processes to specific CPU cores when configured
	if enable && e.cfg.ThrottledAffinityMask != 0 {
		_ = winapi.SetProcessAffinityMask(hProcess, uintptr(e.cfg.ThrottledAffinityMask))
	}

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

	// Check window title bypass patterns before the main bypass check
	if len(e.cfg.BypassWindowTitles) > 0 {
		title := strings.ToLower(winapi.GetWindowText(hwnd))
		for _, pattern := range e.cfg.BypassWindowTitles {
			if strings.Contains(title, strings.ToLower(pattern)) {
				e.cfg.AddBypassProcess(appName)
				break
			}
		}
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
	// Determine if running as a service account (SYSTEM, LOCAL SERVICE, NETWORK SERVICE, or MSA)
	isService := winapi.IsCurrentUserServiceAccount()
	var currentSessionID uint32
	if !isService {
		var err error
		currentSessionID, err = winapi.GetCurrentProcessSessionId()
		if err != nil {
			e.log.Error("failed to get current session ID", "error", err)
			return 0
		}
	}

	for {
		procName := winapi.ProcessNameFromEntry(&entry)
		pid := entry.ProcessID

		if pid != pendingPID && !e.cfg.ShouldBypass(procName) {
			// If not a service, only throttle processes in our session
			if isService {
				// Service: throttle all
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
			} else {
				// User: throttle only our session
				procSessionID, err := winapi.ProcessIdToSessionId(pid)
				if err == nil && procSessionID == currentSessionID {
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

// RunMessageLoop runs the Win32 message loop with foreground event hook and power notifications.
// This must be called from the main goroutine (locked OS thread).
func (e *Engine) RunMessageLoop() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// Record the thread ID so Stop() can post WM_QUIT from another goroutine.
	atomic.StoreUint32(&e.msgThreadID, winapi.GetCurrentThreadId())

	// Create a hidden window to receive power notifications
	e.createHiddenWindow()
	defer func() {
		if e.hwnd != 0 {
			engineProc_DestroyWindow.Call(e.hwnd)
			e.hwnd = 0
		}
	}()

	// Register for power notifications (even in service mode)
	e.registerPowerNotifications()
	defer e.unregisterPowerNotifications()

	// Start ETW-based process creation monitoring (Phase 2 optimization)
	// This is best-effort; failures fall back to housekeeping timer
	ctx := context.Background()
	_ = e.etwMonitor.Start(ctx)
	defer e.etwMonitor.Stop()

	// Create the WinEvent callback (used in tray mode; optional in service mode)
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

	// Install the hook (fails in service mode due to Session 0 isolation)
	e.hookHandle = winapi.SetWinEventHook(
		winapi.EVENT_SYSTEM_FOREGROUND,
		winapi.EVENT_SYSTEM_FOREGROUND,
		0,
		e.winEventCallback,
		0, 0,
		winapi.WINEVENT_OUTOFCONTEXT|winapi.WINEVENT_SKIPOWNPROCESS,
	)
	if e.hookHandle != 0 {
		e.log.Info("foreground event hook installed (tray mode)")
		defer func() {
			winapi.UnhookWinEvent(e.hookHandle)
			e.log.Info("foreground event hook removed")
		}()
	} else {
		e.log.Debug("foreground event hook unavailable (service mode - Session 0 isolation)")
		// Continue anyway: power events + ETW still work in service mode
	}

	// Message loop — GetMessage blocks until a message is available.
	// Stop() posts WM_QUIT via PostThreadMessage to unblock it.
	// Power notifications (WM_POWERBROADCAST) are processed here.
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

// createHiddenWindow creates a hidden message-only window for receiving notifications.
// Called from message loop thread, following same pattern as tray_windows.go
func (e *Engine) createHiddenWindow() {
	className := "EnergyStarGoEngine"
	classNameUTF16, _ := syscall.UTF16FromString(className)
	windowNameUTF16, _ := syscall.UTF16FromString("EnergyStarGoEngine")

	// Create the window procedure callback
	wndProcCallback := syscall.NewCallback(e.wndProc)

	// Define window class (WNDCLASSEXW)
	wc := engineWNDCLASSEXW{
		CbSize:        uint32(unsafe.Sizeof(engineWNDCLASSEXW{})),
		LpfnWndProc:   wndProcCallback,
		LpszClassName: uintptr(unsafe.Pointer(&classNameUTF16[0])),
	}

	// Register window class (using direct syscall like tray code)
	engineProc_RegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	// Create message-only window (parent=HWND_MESSAGE=-3 as uintptr)
	hwnd, _, _ := engineProc_CreateWindowExW.Call(
		0, // dwExStyle
		uintptr(unsafe.Pointer(&classNameUTF16[0])),  // lpClassName
		uintptr(unsafe.Pointer(&windowNameUTF16[0])), // lpWindowName
		0,          // dwStyle
		0, 0, 0, 0, // x, y, cx, cy
		uintptr(0xFFFFFFFD), // hWndParent = HWND_MESSAGE
		0,                   // hMenu
		0,                   // hInstance
		0,                   // lpParam
	)

	if hwnd == 0 {
		e.log.Warn("failed to create hidden window")
		return
	}

	e.hwnd = hwnd
	e.log.Debug("hidden window created", "hwnd", e.hwnd)
}

// wndProc is the window procedure callback for handling messages.
// Called from message loop when messages arrive.
func (e *Engine) wndProc(hwnd uintptr, msg uint32, wParam uintptr, lParam uintptr) uintptr {
	switch msg {
	case winapi.WM_POWERBROADCAST:
		return e.handlePowerBroadcast(wParam, lParam)
	case 0x001C: // WM_SETTINGCHANGE
		e.log.Debug("settings changed notification received")
		return 0
	default:
		// Call DefWindowProcW
		ret, _, _ := engineProc_DefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
		return ret
	}
}

// handlePowerBroadcast processes WM_POWERBROADCAST messages (AC/battery/power plan changes).
func (e *Engine) handlePowerBroadcast(wParam uintptr, lParam uintptr) uintptr {
	if wParam != winapi.PBT_POWERSETTINGCHANGE {
		return 0
	}

	// Parse power setting structure
	if lParam == 0 {
		return 0
	}

	pbs := (*winapi.POWERBROADCAST_SETTING)(unsafe.Pointer(lParam))
	if pbs == nil {
		return 0
	}

	// Convert PowerSetting bytes to GUID for comparison
	var settingGUID [16]byte
	copy(settingGUID[:], pbs.PowerSetting[:])
	guid := guidFromBytes(settingGUID)

	// AC/DC power source change detected
	if guid == winapi.GUID_ACDC_POWER_SOURCE {
		if pbs.DataLength >= 1 {
			onAC := pbs.Data[0] == 0
			state := "battery"
			if onAC {
				state = "AC"
			}
			e.log.Info("power source changed", "state", state)

			// Update config profile based on power state
			if e.cfg.AutoProfile.Enabled {
				if onAC {
					e.cfg.SetProfile(e.cfg.AutoProfile.OnAC)
					e.log.Debug("auto-profile: switched to AC profile")
				} else {
					e.cfg.SetProfile(e.cfg.AutoProfile.OnBattery)
					e.log.Debug("auto-profile: switched to battery profile")
				}
			}

			// Trigger immediate throttle sweep
			if !e.cfg.BoostForegroundOnly {
				e.ThrottleAllUserBackgroundProcesses()
			}
		}
		return 0
	}

	// Battery percentage change
	if guid == winapi.GUID_BATTERY_PERCENTAGE_REMAINING {
		if pbs.DataLength >= 1 {
			battPercent := uint32(pbs.Data[0])
			e.log.Debug("battery percentage changed", "percent", battPercent)

			// Check if battery is low
			if e.cfg.LowBatterySuspendPercent > 0 && int(battPercent) <= e.cfg.LowBatterySuspendPercent {
				idleMin := int(power.IdleSeconds()) / 60
				if e.cfg.IdleSuspendMinutes == 0 || idleMin >= e.cfg.IdleSuspendMinutes {
					e.log.Warn("battery critical, suspending", "percent", battPercent)
					_ = power.Suspend(false, false)
				}
			}
		}
		return 0
	}

	// Power scheme/plan change (High Performance check)
	if guid == winapi.GUID_POWER_SCHEME_PERSONALITY {
		// If data length is sufficient, try to parse the plan GUID
		if pbs.DataLength >= 16 {
			var planGUID [16]byte
			// Copy up to 16 bytes or DataLength, whichever is smaller
			copyLen := pbs.DataLength
			if copyLen > 16 {
				copyLen = 16
			}
			copy(planGUID[:copyLen], pbs.Data[:copyLen])
			planGUIDStruct := guidFromBytes(planGUID)

			if planGUIDStruct == winapi.GUID_POWER_PLAN_HIGH_PERFORMANCE {
				e.SetPaused(true)
				e.log.Info("High Performance plan detected, pausing throttling")
			} else {
				e.SetPaused(false)
				e.log.Info("resumed throttling (no longer High Performance)")
				if !e.cfg.BoostForegroundOnly {
					e.ThrottleAllUserBackgroundProcesses()
				}
			}
		}
		return 0
	}

	// Display state change (screen on/off)
	if guid == winapi.GUID_CONSOLE_DISPLAY_STATE {
		if pbs.DataLength >= 4 {
			// Safely read 4-byte state value using pointer conversion
			bytes := (*[4]byte)(unsafe.Pointer(&pbs.Data[0]))
			state := uint32(bytes[0]) | uint32(bytes[1])<<8 | uint32(bytes[2])<<16 | uint32(bytes[3])<<24
			displayOn := state != winapi.DISPLAY_OFF
			if !displayOn && e.cfg.ThrottleOnDisplayOff {
				e.cfg.SetProfile(config.ProfileAggressive)
				e.log.Info("display off: aggressive profile activated")
				if !e.cfg.BoostForegroundOnly {
					e.ThrottleAllUserBackgroundProcesses()
				}
			}
		}
		return 0
	}

	return 0
}

// guidFromBytes converts a 16-byte array to a GUID.
func guidFromBytes(data [16]byte) winapi.GUID {
	return winapi.GUID{
		Data1: uint32(data[0]) | uint32(data[1])<<8 | uint32(data[2])<<16 | uint32(data[3])<<24,
		Data2: uint16(data[4]) | uint16(data[5])<<8,
		Data3: uint16(data[6]) | uint16(data[7])<<8,
		Data4: [8]byte{data[8], data[9], data[10], data[11], data[12], data[13], data[14], data[15]},
	}
}

// registerPowerNotifications registers the window for power notifications.
func (e *Engine) registerPowerNotifications() {
	if e.hwnd == 0 {
		return
	}

	e.log.Debug("registering power notifications")

	// AC/DC power source
	guid := winapi.GUID_ACDC_POWER_SOURCE
	if h, err := winapi.RegisterPowerSettingNotification(e.hwnd, &guid, winapi.DEVICE_NOTIFY_WINDOW_HANDLE); err != nil {
		e.log.Debug("failed to register AC/DC notification", "error", err)
	} else {
		e.powerSourceNotifHandle = h
		e.log.Debug("AC/DC power source notifications registered")
	}

	// Battery percentage
	guid = winapi.GUID_BATTERY_PERCENTAGE_REMAINING
	if h, err := winapi.RegisterPowerSettingNotification(e.hwnd, &guid, winapi.DEVICE_NOTIFY_WINDOW_HANDLE); err != nil {
		e.log.Debug("failed to register battery notification", "error", err)
	} else {
		e.batteryNotifHandle = h
		e.log.Debug("battery percentage notifications registered")
	}

	// Power scheme/plan
	guid = winapi.GUID_POWER_SCHEME_PERSONALITY
	if h, err := winapi.RegisterPowerSettingNotification(e.hwnd, &guid, winapi.DEVICE_NOTIFY_WINDOW_HANDLE); err != nil {
		e.log.Debug("failed to register power plan notification", "error", err)
	} else {
		e.powerPlanNotifHandle = h
		e.log.Debug("power plan notifications registered")
	}

	// Display state
	guid = winapi.GUID_CONSOLE_DISPLAY_STATE
	if h, err := winapi.RegisterPowerSettingNotification(e.hwnd, &guid, winapi.DEVICE_NOTIFY_WINDOW_HANDLE); err != nil {
		e.log.Debug("failed to register display state notification", "error", err)
	} else {
		e.displayStateNotifHandle = h
		e.log.Debug("display state notifications registered")
	}
}

// unregisterPowerNotifications unregisters all power notifications.
func (e *Engine) unregisterPowerNotifications() {
	if e.powerSourceNotifHandle != 0 {
		_ = winapi.UnregisterPowerSettingNotification(e.powerSourceNotifHandle)
		e.powerSourceNotifHandle = 0
	}
	if e.batteryNotifHandle != 0 {
		_ = winapi.UnregisterPowerSettingNotification(e.batteryNotifHandle)
		e.batteryNotifHandle = 0
	}
	if e.powerPlanNotifHandle != 0 {
		_ = winapi.UnregisterPowerSettingNotification(e.powerPlanNotifHandle)
		e.powerPlanNotifHandle = 0
	}
	if e.displayStateNotifHandle != 0 {
		_ = winapi.UnregisterPowerSettingNotification(e.displayStateNotifHandle)
		e.displayStateNotifHandle = 0
	}
	e.log.Debug("power notifications unregistered")
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

// ThrottledCount returns the number of currently throttled processes.
func (e *Engine) ThrottledCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.throttledPIDs)
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
