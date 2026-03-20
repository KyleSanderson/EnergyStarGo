// SPDX-License-Identifier: GPL-2.0-only
// SPDX-FileCopyrightText: 2024 Kyle Sanderson <kyle@kylesanderson.dev>

//go:build windows

package autostart

import "testing"

func TestIsEnabled_DoesNotPanic(t *testing.T) {
	_, err := IsEnabled()
	if err != nil {
		t.Logf("IsEnabled: %v (may be expected in restricted environments)", err)
	}
}

func TestEnableDisable(t *testing.T) {
	if err := Enable(); err != nil {
		t.Skipf("Enable failed (may require admin or registry access): %v", err)
	}

	enabled, err := IsEnabled()
	if err != nil {
		t.Fatalf("IsEnabled after Enable: %v", err)
	}
	if !enabled {
		t.Error("expected IsEnabled() == true after Enable()")
	}

	if err := Disable(); err != nil {
		t.Fatalf("Disable: %v", err)
	}

	enabled, err = IsEnabled()
	if err != nil {
		t.Fatalf("IsEnabled after Disable: %v", err)
	}
	if enabled {
		t.Error("expected IsEnabled() == false after Disable()")
	}
}

func TestEnableWithArgs(t *testing.T) {
	const testArgs = "run --tray --test"
	if err := EnableWithArgs(testArgs); err != nil {
		t.Skipf("EnableWithArgs failed: %v", err)
	}

	enabled, err := IsEnabled()
	if err != nil {
		t.Fatalf("IsEnabled: %v", err)
	}
	if !enabled {
		t.Error("expected IsEnabled() == true after EnableWithArgs()")
	}

	// Clean up.
	if err := Disable(); err != nil {
		t.Errorf("Disable cleanup: %v", err)
	}
}
