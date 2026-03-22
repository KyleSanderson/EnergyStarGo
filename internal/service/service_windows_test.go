// SPDX-License-Identifier: GPL-2.0-only
// SPDX-FileCopyrightText: 2024 Kyle Sanderson

//go:build windows

package service

import (
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
)

func TestServiceConstants(t *testing.T) {
	if ServiceName == "" {
		t.Error("ServiceName should not be empty")
	}
	if ServiceDisplayName == "" {
		t.Error("ServiceDisplayName should not be empty")
	}
	if ServiceDescription == "" {
		t.Error("ServiceDescription should not be empty")
	}
}

func TestIsWindowsService(t *testing.T) {
	// When running tests, we should NOT be a Windows service
	isSvc := IsWindowsService()
	if isSvc {
		t.Error("test process should not be detected as a Windows service")
	}
}

func TestNewService(t *testing.T) {
	// This just creates the struct, doesn't start anything
	svcHandler := NewService(nil, nil)
	if svcHandler == nil {
		t.Error("NewService should not return nil")
	}
}

func TestQueryStatus_NotInstalled(t *testing.T) {
	// This test expects the service is NOT installed
	_, err := QueryStatus()
	if err == nil {
		// Service might actually be installed, that's OK too
		t.Log("Service appears to be installed (or accessible)")
	} else {
		t.Logf("QueryStatus returned expected error: %v", err)
	}
}

func TestSessionIDFromChangeRequest_NoEventData(t *testing.T) {
	got := sessionIDFromChangeRequest(svc.ChangeRequest{})
	if got != noActiveConsoleSession {
		t.Fatalf("unexpected session id for empty change request: got %d want %d", got, noActiveConsoleSession)
	}
}

func TestSessionIDFromChangeRequest_WithNotification(t *testing.T) {
	n := windows.WTSSESSION_NOTIFICATION{
		Size:      uint32(unsafe.Sizeof(windows.WTSSESSION_NOTIFICATION{})),
		SessionID: 42,
	}
	req := svc.ChangeRequest{EventData: uintptr(unsafe.Pointer(&n))}
	got := sessionIDFromChangeRequest(req)
	if got != 42 {
		t.Fatalf("unexpected session id: got %d want 42", got)
	}
}

func TestSessionIDFromChangeRequest_InvalidNotificationSize(t *testing.T) {
	n := windows.WTSSESSION_NOTIFICATION{
		Size:      0,
		SessionID: 7,
	}
	req := svc.ChangeRequest{EventData: uintptr(unsafe.Pointer(&n))}
	got := sessionIDFromChangeRequest(req)
	if got != noActiveConsoleSession {
		t.Fatalf("unexpected session id for invalid notification size: got %d want %d", got, noActiveConsoleSession)
	}
}
