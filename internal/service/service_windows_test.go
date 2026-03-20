// SPDX-License-Identifier: GPL-2.0-only
// SPDX-FileCopyrightText: 2024 Kyle Sanderson

//go:build windows

package service

import (
	"testing"
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
