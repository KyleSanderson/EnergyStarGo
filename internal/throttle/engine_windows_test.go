// SPDX-License-Identifier: GPL-2.0-only
// SPDX-FileCopyrightText: 2024 Kyle Sanderson

//go:build windows

package throttle

import (
	"os"
	"sync"
	"testing"

	"github.com/KyleSanderson/EnergyStarGo/internal/config"
	"github.com/KyleSanderson/EnergyStarGo/internal/logger"
)

func newTestEngine(t *testing.T) *Engine {
	t.Helper()
	cfg := config.DefaultConfig()
	log, err := logger.New("", "debug")
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}
	return New(cfg, log)
}

func TestEngineNew(t *testing.T) {
	engine := newTestEngine(t)
	if engine == nil {
		t.Fatal("engine should not be nil")
	}
	if engine.cfg == nil {
		t.Error("engine config should not be nil")
	}
	if engine.log == nil {
		t.Error("engine logger should not be nil")
	}
	if engine.throttledPIDs == nil {
		t.Error("throttledPIDs map should be initialized")
	}
	if engine.stopCh == nil {
		t.Error("stopCh should be initialized")
	}
}

func TestEngineStatsInitial(t *testing.T) {
	engine := newTestEngine(t)
	stats := engine.Stats()

	if stats.TotalThrottled != 0 {
		t.Errorf("TotalThrottled = %d, want 0", stats.TotalThrottled)
	}
	if stats.TotalBoosted != 0 {
		t.Errorf("TotalBoosted = %d, want 0", stats.TotalBoosted)
	}
	if stats.HousekeepingRuns != 0 {
		t.Errorf("HousekeepingRuns = %d, want 0", stats.HousekeepingRuns)
	}
	if stats.ForegroundChanges != 0 {
		t.Errorf("ForegroundChanges = %d, want 0", stats.ForegroundChanges)
	}
	if stats.CurrentForeground != "" {
		t.Errorf("CurrentForeground = %q, want empty", stats.CurrentForeground)
	}
}

func TestStatsIncrement(t *testing.T) {
	var s Stats
	s.IncrementThrottled()
	s.IncrementThrottled()
	s.IncrementBoosted()
	s.IncrementHousekeeping()
	s.IncrementForeground()
	s.IncrementForeground()
	s.IncrementForeground()

	snap := s.Snapshot()
	if snap.TotalThrottled != 2 {
		t.Errorf("TotalThrottled = %d, want 2", snap.TotalThrottled)
	}
	if snap.TotalBoosted != 1 {
		t.Errorf("TotalBoosted = %d, want 1", snap.TotalBoosted)
	}
	if snap.HousekeepingRuns != 1 {
		t.Errorf("HousekeepingRuns = %d, want 1", snap.HousekeepingRuns)
	}
	if snap.ForegroundChanges != 3 {
		t.Errorf("ForegroundChanges = %d, want 3", snap.ForegroundChanges)
	}
}

func TestStatsSetForeground(t *testing.T) {
	var s Stats
	s.SetForeground("notepad.exe", 1234)

	snap := s.Snapshot()
	if snap.CurrentForeground != "notepad.exe" {
		t.Errorf("CurrentForeground = %q, want notepad.exe", snap.CurrentForeground)
	}
	if snap.CurrentForegroundPID != 1234 {
		t.Errorf("CurrentForegroundPID = %d, want 1234", snap.CurrentForegroundPID)
	}
}

func TestStatsConcurrency(t *testing.T) {
	var s Stats
	var wg sync.WaitGroup

	// Hammer the stats from multiple goroutines
	for i := 0; i < 100; i++ {
		wg.Add(4)
		go func() { defer wg.Done(); s.IncrementThrottled() }()
		go func() { defer wg.Done(); s.IncrementBoosted() }()
		go func() { defer wg.Done(); s.IncrementHousekeeping() }()
		go func() { defer wg.Done(); s.SetForeground("test.exe", 999) }()
	}
	wg.Wait()

	snap := s.Snapshot()
	if snap.TotalThrottled != 100 {
		t.Errorf("TotalThrottled = %d, want 100", snap.TotalThrottled)
	}
	if snap.TotalBoosted != 100 {
		t.Errorf("TotalBoosted = %d, want 100", snap.TotalBoosted)
	}
	if snap.HousekeepingRuns != 100 {
		t.Errorf("HousekeepingRuns = %d, want 100", snap.HousekeepingRuns)
	}
}

func TestThrottledProcesses(t *testing.T) {
	engine := newTestEngine(t)

	// Initially empty
	procs := engine.ThrottledProcesses()
	if len(procs) != 0 {
		t.Errorf("expected empty throttled processes, got %d", len(procs))
	}

	// Modify internal state
	engine.mu.Lock()
	engine.throttledPIDs[100] = "test1.exe"
	engine.throttledPIDs[200] = "test2.exe"
	engine.mu.Unlock()

	procs = engine.ThrottledProcesses()
	if len(procs) != 2 {
		t.Errorf("expected 2 throttled processes, got %d", len(procs))
	}
	if procs[100] != "test1.exe" {
		t.Errorf("expected test1.exe for pid 100, got %s", procs[100])
	}
	if procs[200] != "test2.exe" {
		t.Errorf("expected test2.exe for pid 200, got %s", procs[200])
	}

	// Verify it returns a copy, not the original map
	delete(procs, 100)
	procs2 := engine.ThrottledProcesses()
	if len(procs2) != 2 {
		t.Error("deleting from returned map should not affect engine state")
	}
}

func TestThrottleAllUserBackgroundProcesses(t *testing.T) {
	engine := newTestEngine(t)

	// This test actually calls Windows APIs, so it's an integration test.
	// On Windows, it should throttle at least some processes.
	count := engine.ThrottleAllUserBackgroundProcesses()

	// We can't know exact count, but it should be non-negative
	if count < 0 {
		t.Errorf("throttle count should be non-negative, got %d", count)
	}

	stats := engine.Stats()
	if stats.HousekeepingRuns != 1 {
		t.Errorf("HousekeepingRuns = %d, want 1", stats.HousekeepingRuns)
	}

	t.Logf("Throttled %d processes in current session", count)
}

func TestRestoreAllProcesses(t *testing.T) {
	engine := newTestEngine(t)

	// First throttle some processes
	engine.ThrottleAllUserBackgroundProcesses()

	// Then restore
	restored := engine.RestoreAllProcesses()

	// Should have restored something (or at least not panic)
	t.Logf("Restored %d processes", restored)

	// After restore, throttledPIDs should be empty
	procs := engine.ThrottledProcesses()
	if len(procs) != 0 {
		t.Errorf("expected empty throttled processes after restore, got %d", len(procs))
	}
}

func TestEngineStop(t *testing.T) {
	engine := newTestEngine(t)

	// Stop should not panic even when called without RunMessageLoop
	engine.Stop()

	// Verify channel is closed
	select {
	case <-engine.stopCh:
		// OK, channel is closed
	default:
		t.Error("stopCh should be closed after Stop()")
	}
}

func TestEngineWithCustomConfig(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.AddBypassProcess("customapp.exe")
	cfg.HousekeepingSeconds = 60

	log, _ := logger.New("", "warn")
	engine := New(cfg, log)

	if !engine.cfg.ShouldBypass("customapp.exe") {
		t.Error("engine should respect custom bypass list")
	}
}

func TestThrottleProcessThreadsDoesNotPanic(t *testing.T) {
	engine := newTestEngine(t)
	// Should not panic with current process ID
	engine.throttleProcessThreads(uint32(os.Getpid()), true)
	engine.throttleProcessThreads(uint32(os.Getpid()), false)
}

func TestHandleForegroundEventWithZeroHwnd(t *testing.T) {
	engine := newTestEngine(t)

	// Should not panic with zero/invalid hwnd
	engine.HandleForegroundEvent(0)

	stats := engine.Stats()
	// With hwnd=0, GetWindowThreadProcessId returns 0, so no foreground change
	if stats.ForegroundChanges != 0 {
		t.Errorf("expected 0 foreground changes for invalid hwnd, got %d", stats.ForegroundChanges)
	}
}

func TestEnginePauseUnpause(t *testing.T) {
	engine := newTestEngine(t)

	if engine.IsPaused() {
		t.Error("engine should not be paused initially")
	}

	engine.SetPaused(true)
	if !engine.IsPaused() {
		t.Error("engine should be paused after SetPaused(true)")
	}

	// ThrottleAllUserBackgroundProcesses should return 0 while paused.
	count := engine.ThrottleAllUserBackgroundProcesses()
	if count != 0 {
		t.Errorf("expected 0 throttled processes while paused, got %d", count)
	}

	// HandleForegroundEvent should be a no-op while paused (no stat increment).
	engine.HandleForegroundEvent(0)
	stats := engine.Stats()
	if stats.ForegroundChanges != 0 {
		t.Errorf("expected 0 foreground changes while paused, got %d", stats.ForegroundChanges)
	}

	engine.SetPaused(false)
	if engine.IsPaused() {
		t.Error("engine should not be paused after SetPaused(false)")
	}
}
