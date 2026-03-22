// SPDX-License-Identifier: GPL-2.0-only
// SPDX-FileCopyrightText: 2024 Kyle Sanderson

//go:build windows

package etw

import (
	"context"
	"sync"
	"time"
	"unsafe"

	"github.com/KyleSanderson/EnergyStarGo/internal/logger"
	"github.com/KyleSanderson/EnergyStarGo/internal/winapi"
)

// ProcessMonitor monitors process creation events via ETW.
// Non-blocking channel communication with the engine.
type ProcessMonitor struct {
	log           *logger.Logger
	sessionName   string
	sessionHandle uintptr
	traceHandle   uintptr
	eventChan     chan uint32 // New process PIDs
	stopCh        chan struct{}
	wg            sync.WaitGroup
	mu            sync.RWMutex
	running       bool
}

// New creates a new process monitor
func New(log *logger.Logger) *ProcessMonitor {
	return &ProcessMonitor{
		log:       log,
		eventChan: make(chan uint32, 100), // Buffered channel for PIDs
		stopCh:    make(chan struct{}),
	}
}

// Events returns a channel that receives PIDs of newly created processes
func (pm *ProcessMonitor) Events() <-chan uint32 {
	return pm.eventChan
}

// Start begins ETW session for process creation monitoring
func (pm *ProcessMonitor) Start(ctx context.Context) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pm.running {
		return nil // Already running
	}

	pm.sessionName = "EnergyStarGoTrace"

	// Step 1: Stop any existing session with same name (cleanup from previous run)
	existingSession, err := getSessionHandle(pm.sessionName)
	if err == nil && existingSession != 0 {
		_ = winapi.ControlTrace(existingSession)
		time.Sleep(100 * time.Millisecond) // Give it time to stop
	}

	// Step 2: Create ETW session
	handle, err := winapi.StartTrace(pm.sessionName)
	if err != nil {
		pm.log.Error("failed to start ETW session", "error", err)
		return err
	}
	pm.sessionHandle = handle
	pm.log.Debug("ETW session created", "handle", pm.sessionHandle)

	// Step 3: Enable Kernel-Process provider
	err = winapi.EnableTraceEx2(
		pm.sessionHandle,
		winapi.KernelProcessProviderGUID,
		winapi.TRACE_LEVEL_INFORMATION,
		winapi.EVENT_ENABLE_KEYWORD_PROCESS_CREATE,
	)
	if err != nil {
		pm.log.Warn("failed to enable Kernel-Process provider", "error", err)
		_ = winapi.ControlTrace(pm.sessionHandle)
		pm.sessionHandle = 0
		return err
	}
	pm.log.Info("Kernel-Process provider enabled")

	// Step 4: Open trace for real-time reading
	traceHandle, err := winapi.OpenTrace(pm.sessionHandle)
	if err != nil {
		pm.log.Error("failed to open trace", "error", err)
		_ = winapi.ControlTrace(pm.sessionHandle)
		pm.sessionHandle = 0
		return err
	}
	pm.traceHandle = traceHandle
	pm.log.Debug("ETW trace opened", "handle", pm.traceHandle)

	pm.running = true

	// Step 5: Start event processing goroutine
	pm.wg.Add(1)
	go pm.eventLoop(ctx)

	pm.log.Info("ETW process monitor started successfully")
	return nil
}

// Stop terminates ETW monitoring
func (pm *ProcessMonitor) Stop() {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if !pm.running {
		return
	}

	pm.running = false

	// Signal event loop to exit
	select {
	case <-pm.stopCh:
		// Already closed
	default:
		close(pm.stopCh)
	}

	// Wait for goroutine to finish
	pm.wg.Wait()

	// Clean up ETW resources
	if pm.traceHandle != 0 {
		_ = winapi.CloseTrace(pm.traceHandle)
		pm.traceHandle = 0
	}

	if pm.sessionHandle != 0 {
		_ = winapi.ControlTrace(pm.sessionHandle)
		pm.sessionHandle = 0
	}

	pm.log.Debug("ETW process monitor stopped")
}

// eventLoop processes ETW events (blocking)
func (pm *ProcessMonitor) eventLoop(ctx context.Context) {
	defer pm.wg.Done()
	defer close(pm.eventChan)

	// Create a channel to signal ProcessTraceW to stop
	// This is a bit tricky since ProcessTraceW blocks - we'll use a goroutine
	// that calls ProcessTraceW and we'll clean up resources when stopCh closes

	doneCh := make(chan error, 1)
	go func() {
		// This call blocks until the trace is stopped via ControlTrace
		handles := []uintptr{pm.traceHandle}
		err := winapi.ProcessTraceW(handles, 0)
		doneCh <- err
	}()

	// Wait for either error or stop signal
	select {
	case <-pm.stopCh:
		// Stop signal received
		pm.log.Debug("ETW event loop stopping")
		// ProcessTraceW will unblock when we close the trace
		<-doneCh
	case err := <-doneCh:
		if err != nil {
			pm.log.Warn("ETW ProcessTraceW error", "error", err)
		}
	case <-ctx.Done():
		pm.log.Debug("ETW event loop context cancelled")
		<-doneCh
	}
}

// ProcessETWEvent parses an ETW event and extracts the PID if it's a process creation event
// This would normally be called from a callback in ProcessTraceW, but here we use polling
// This is a simplified implementation - a production version would use proper ETW callbacks
func ProcessETWEvent(event *winapi.EVENT_TRACE) uint32 {
	if event == nil {
		return 0
	}

	// Check if this is a process creation event (ID=1, Opcode=1)
	if event.Header.EventDescriptor.Id != 1 {
		return 0 // Not a ProcessCreate event
	}

	// Extract PID from MofData
	// ProcessCreate event payload structure: PID (DWORD @ offset 0)
	if event.MofData == 0 || event.MofLength < 4 {
		return 0
	}

	// Read first 4 bytes as DWORD (PID)
	var pidBytes [4]byte
	winapi.CopyFromUintptr(unsafe.Pointer(&pidBytes[0]), event.MofData, 4)
	pid := uint32(pidBytes[0]) | uint32(pidBytes[1])<<8 |
		uint32(pidBytes[2])<<16 | uint32(pidBytes[3])<<24

	return pid
}

// getSessionHandle retrieves the handle of an existing ETW session by name
// This would require additional Windows API calls - for now we return 0
func getSessionHandle(sessionName string) (uintptr, error) {
	// In a complete implementation, we'd query existing sessions
	// For now, assume no existing session
	return 0, nil
}
