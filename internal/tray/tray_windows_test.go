// SPDX-License-Identifier: GPL-2.0-only
// SPDX-FileCopyrightText: 2024 Kyle Sanderson

//go:build windows

package tray

import (
	"sync"
	"testing"
	"time"

	"github.com/KyleSanderson/EnergyStarGo/internal/logger"
)

func TestNewTray(t *testing.T) {
	log, _ := logger.New("", "info")
	callbacks := TrayCallbacks{
		OnPause:  func() {},
		OnResume: func() {},
		OnExit:   func() {},
		GetStats: func() string { return "test stats" },
		IsPaused: func() bool { return false },
	}

	tr := New(log, callbacks)
	if tr == nil {
		t.Fatal("New should not return nil")
	}
	if tr.log == nil {
		t.Error("tray logger should not be nil")
	}
}

func TestUtf16From(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"ascii", "hello"},
		{"with space", "hello world"},
		{"special chars", "EnergyStarGo - Running"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := utf16From(tt.input)
			if result == nil {
				t.Error("utf16From should not return nil")
			}
		})
	}
}

func TestTrayConstants(t *testing.T) {
	// Verify tray-related constants are correct
	if WM_USER != 0x0400 {
		t.Errorf("WM_USER = 0x%X, want 0x0400", WM_USER)
	}
	if WM_TRAYICON != WM_USER+1 {
		t.Errorf("WM_TRAYICON = 0x%X, want 0x%X", WM_TRAYICON, WM_USER+1)
	}
	if NIM_ADD != 0 {
		t.Errorf("NIM_ADD = %d, want 0", NIM_ADD)
	}
	if NIM_MODIFY != 1 {
		t.Errorf("NIM_MODIFY = %d, want 1", NIM_MODIFY)
	}
	if NIM_DELETE != 2 {
		t.Errorf("NIM_DELETE = %d, want 2", NIM_DELETE)
	}
}

func TestMenuItemIDs(t *testing.T) {
	// Verify all menu item IDs are unique
	ids := []uint32{idStatus, idStats, idSeparator, idPause, idResume, idRestore, idExit}
	seen := make(map[uint32]bool)
	for _, id := range ids {
		if id == idSeparator {
			continue // separators can share ID
		}
		if seen[id] {
			t.Errorf("duplicate menu item ID: %d", id)
		}
		seen[id] = true
	}
}

func TestTrayStopWithoutRun(t *testing.T) {
	log, _ := logger.New("", "info")
	tr := New(log, TrayCallbacks{})

	// Stop without Run should not panic
	tr.Stop()
}

func TestTrayShowNotificationWithoutRun(t *testing.T) {
	log, _ := logger.New("", "info")
	tr := New(log, TrayCallbacks{})

	// Should not panic when tray is not running
	tr.ShowNotification("Test", "Test message")
}

func TestTrayUpdateTooltipWithoutRun(t *testing.T) {
	log, _ := logger.New("", "info")
	tr := New(log, TrayCallbacks{})

	// Should not panic when tray is not running
	tr.UpdateTooltip("Test tooltip")
}

func TestHandleMenuCommand(t *testing.T) {
	log, _ := logger.New("", "info")

	var mu sync.Mutex
	pauseCalled := false
	resumeCalled := false
	restoreCalled := false

	callbacks := TrayCallbacks{
		OnPause:   func() { mu.Lock(); pauseCalled = true; mu.Unlock() },
		OnResume:  func() { mu.Lock(); resumeCalled = true; mu.Unlock() },
		OnRestore: func() { mu.Lock(); restoreCalled = true; mu.Unlock() },
		OnExit:    func() {},
	}

	tr := New(log, callbacks)

	handleMenuCommand(tr, idPause)
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	if !pauseCalled {
		t.Error("OnPause should have been called")
	}
	mu.Unlock()

	handleMenuCommand(tr, idResume)
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	if !resumeCalled {
		t.Error("OnResume should have been called")
	}
	mu.Unlock()

	handleMenuCommand(tr, idRestore)
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	if !restoreCalled {
		t.Error("OnRestore should have been called")
	}
	mu.Unlock()

	// Don't test exit since it calls Stop()
}

func TestNilCallbacksDoNotPanic(t *testing.T) {
	log, _ := logger.New("", "info")
	tr := New(log, TrayCallbacks{})

	// None of these should panic with nil callbacks
	handleMenuCommand(tr, idPause)
	handleMenuCommand(tr, idResume)
	handleMenuCommand(tr, idRestore)
	handleMenuCommand(tr, 9999) // unknown command
	time.Sleep(50 * time.Millisecond)
}
